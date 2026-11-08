package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/pages"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// pluginPageKey names one page of one plugin, as the manager opens it.
type pluginPageKey struct {
	id   string
	page string
}

// pluginPageState is what the Plugins segment shows for an open page.
type pluginPageState struct {
	key      pluginPageKey
	name     string
	unsigned bool
	page     pages.Page
	err      string
	loading  bool
}

// initPlugins opens the plugins folder under the app's data directory.
func (a *App) initPlugins(dir string) {
	a.plugins = plugin.NewStore(
		filepath.Join(dir, "plugins"),
		mygo.Secrets.Service("crypto-panel-plugins"),
	)
	a.pluginRunners = map[string]*plugin.Runner{}
	a.pluginLogs = map[string]*plugin.LogBuffer{}
	a.pluginEnabled = map[string]*bool{}
	a.pluginDrafts = map[string]*string{}
	a.pluginLogsOpen = map[string]bool{}
	a.refreshPlugins()
}

// refreshPlugins re-reads the plugins folder. It runs on the main thread,
// after an install, a removal or a toggle, never per frame.
func (a *App) refreshPlugins() {
	if a.plugins == nil {
		return
	}
	items, err := a.plugins.List()
	if err != nil {
		a.pluginNotice = err.Error()
		return
	}
	a.pluginItems = items
	for _, item := range items {
		id := item.Manifest.ID
		if id == "" {
			continue
		}
		enabled := item.Enabled
		a.pluginEnabled[id] = &enabled
		for _, field := range item.Manifest.Settings {
			key := id + "/" + field.Key
			if _, ok := a.pluginDrafts[key]; ok {
				continue
			}
			// A secret is never read back into the form: the field starts
			// empty and is written only when the user types a new value.
			value := ""
			if !field.Secret {
				value, _ = a.plugins.Setting(id, field.Key)
			}
			a.pluginDrafts[key] = &value
		}
	}
	// A plugin page whose plugin was removed or turned off falls back to the
	// manager, so the window never shows a page that no longer exists.
	if a.route.kind == "plugin" && !a.pluginPageAvailable(items, a.route.id) {
		a.closePluginPage()
	}
}

// pluginPageAvailable tells whether id is installed, readable and on.
func (a *App) pluginPageAvailable(items []plugin.Installed, id string) bool {
	for _, item := range items {
		if item.Manifest.ID == id {
			return item.Err == "" && item.Enabled
		}
	}
	return false
}

// pluginLog returns the log buffer of one plugin, creating it on first use.
func (a *App) pluginLog(id string) *plugin.LogBuffer {
	buffer, ok := a.pluginLogs[id]
	if !ok {
		buffer = plugin.NewLogBuffer(200)
		a.pluginLogs[id] = buffer
	}
	return buffer
}

// installPlugin asks for a folder, shows what it may do, and copies it into
// the plugins folder. A folder is unsigned: there is no registry entry to
// check it against, and the prompt says so.
func (a *App) installPlugin() {
	if a.plugins == nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       a.t(i18n.PluginInstallTitle),
		ButtonLabel: a.t(i18n.PluginInstall),
		Directory:   true,
	})
	if err != nil {
		a.pluginNotice = a.t(i18n.PluginInstallFailed, err.Error())
		a.invalidate()
		return
	}
	if len(paths) == 0 {
		return
	}
	src := paths[0]
	m, err := plugin.LoadManifest(src)
	if err == nil {
		err = m.ValidateDir(src)
	}
	if err != nil {
		a.pluginNotice = a.t(i18n.PluginInstallFailed, err.Error())
		a.invalidate()
		return
	}
	a.confirmInstall(m, installRequest{unsigned: true, source: a.t(i18n.StoreSourceFolder)}, func(ok bool) {
		if !ok {
			return
		}
		m, err := a.plugins.Install(src)
		if err != nil {
			a.pluginNotice = a.t(i18n.PluginInstallFailed, err.Error())
			a.invalidate()
			return
		}
		a.pluginNotice = a.t(i18n.PluginInstalled, m.Name)
		a.refreshPlugins()
		a.invalidate()
	})
}

// removePlugin asks, then deletes the plugin's folder and its secrets.
func (a *App) removePlugin(id string) {
	if a.plugins == nil {
		return
	}
	inst, err := a.plugins.Get(id)
	if err != nil {
		a.pluginNotice = err.Error()
		a.invalidate()
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:               mygo.MessageWarning,
		Title:              a.t(i18n.PluginRemoveTitle),
		Message:            a.t(i18n.PluginRemoveBody, inst.Manifest.Name),
		Buttons:            []string{a.t(i18n.Remove), a.t(i18n.Cancel)},
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	if err := a.plugins.Remove(id); err != nil {
		a.pluginNotice = err.Error()
		a.invalidate()
		return
	}
	a.stopRunner(id)
	delete(a.pluginLogs, id)
	a.pluginNotice = a.t(i18n.PluginRemoved, inst.Manifest.Name)
	a.refreshPlugins()
	a.invalidate()
}

// setPluginEnabled turns a plugin on or off and drops its runtime when it is
// turned off, so a disabled plugin holds no memory.
func (a *App) setPluginEnabled(id string, enabled bool) {
	if a.plugins == nil {
		return
	}
	if err := a.plugins.SetEnabled(id, enabled); err != nil {
		a.pluginNotice = err.Error()
		a.invalidate()
		return
	}
	if !enabled {
		a.stopRunner(id)
	}
	a.refreshPlugins()
	a.invalidate()
}

// savePluginSettings stores every field of a plugin's settings form.
func (a *App) savePluginSettings(m plugin.Manifest) {
	if a.plugins == nil {
		return
	}
	for _, field := range m.Settings {
		draft := a.pluginDrafts[m.ID+"/"+field.Key]
		if draft == nil {
			continue
		}
		// An empty secret field means "leave the stored secret alone".
		if field.Secret && *draft == "" {
			continue
		}
		if err := a.plugins.SetSetting(m.ID, field.Key, *draft); err != nil {
			a.pluginNotice = err.Error()
			a.invalidate()
			return
		}
	}
	// The runtime snapshots settings and the endpoint when it is created;
	// drop it so the next page reads the new values.
	a.stopRunner(m.ID)
	if a.route.kind == "plugin" && a.route.id == m.ID {
		a.loadPluginPage()
	}
	a.pluginNotice = a.t(i18n.PluginSettingsSaved)
	a.invalidate()
}

func (a *App) stopRunner(id string) {
	if runner, ok := a.pluginRunners[id]; ok {
		runner.Close()
		delete(a.pluginRunners, id)
	}
}

// stopAllRunners drops every runtime, so the next page of each plugin reads
// the new endpoint or settings.
func (a *App) stopAllRunners() {
	for id, runner := range a.pluginRunners {
		runner.Close()
		delete(a.pluginRunners, id)
	}
}

// pluginKey is the open plugin page, or the zero key when the window shows
// another page.
func (a *App) pluginKey() pluginPageKey {
	if a.route.kind != "plugin" {
		return pluginPageKey{}
	}
	return pluginPageKey{id: a.route.id, page: a.route.page}
}

// openPluginPage selects one page of a plugin and loads it.
func (a *App) openPluginPage(id, page string) {
	a.route = route{kind: "plugin", id: id, page: page}
	a.loadPluginPage()
}

// closePluginPage leaves a plugin page for the manager, dropping the page
// it held.
func (a *App) closePluginPage() {
	a.pluginGen++
	a.pluginView = pluginPageState{}
	if a.route.kind == "plugin" {
		a.route = routePlugins
	}
	a.invalidate()
}

// loadPluginPage computes the open page in the background. The runtime is
// created on the main thread (compiling the module is cheap and touches the
// runner map); the call itself runs on its own goroutine, so a slow plugin
// never freezes the window.
func (a *App) loadPluginPage() {
	key := a.pluginKey()
	if key.id == "" || a.plugins == nil {
		return
	}
	inst, err := a.plugins.Get(key.id)
	if err != nil {
		a.pluginView = pluginPageState{key: key, err: err.Error()}
		return
	}
	state := pluginPageState{
		key:      key,
		name:     inst.Manifest.Name,
		unsigned: !inst.Signed,
	}
	if !inst.Enabled {
		state.err = a.t(i18n.PluginDisabled)
		a.pluginView = state
		return
	}
	runner, err := a.pluginRunner(inst)
	if err != nil {
		state.err = err.Error()
		a.pluginView = state
		return
	}
	a.pluginGen++
	gen := a.pluginGen
	state.loading = true
	a.pluginView = state
	go func() {
		page, err := runner.Page(key.page)
		mygo.RunOnMain(func() {
			if gen != a.pluginGen || a.pluginKey() != key {
				return
			}
			state.loading = false
			if err != nil {
				state.err = err.Error()
			} else {
				state.page = page
			}
			a.pluginView = state
			a.invalidate()
		})
	}()
}

// pluginRunner returns the plugin's runtime, compiling its source the first
// time. It runs on the main thread.
func (a *App) pluginRunner(inst plugin.Installed) (*plugin.Runner, error) {
	id := inst.Manifest.ID
	if runner, ok := a.pluginRunners[id]; ok {
		return runner, nil
	}
	source, err := os.ReadFile(filepath.Join(inst.Dir, filepath.FromSlash(inst.Manifest.Main)))
	if err != nil {
		return nil, fmt.Errorf("plugin %s: %w", id, err)
	}
	host := plugin.NewHost(a.pluginHostDeps(inst.Manifest))
	runner, err := plugin.NewRunner(inst.Manifest, source, host, a.lang())
	if err != nil {
		return nil, err
	}
	a.pluginRunners[id] = runner
	return runner, nil
}

// pluginHostDeps snapshots what the plugin may read, so the runtime's
// goroutine never touches app state the main thread writes.
func (a *App) pluginHostDeps(m plugin.Manifest) plugin.HostDeps {
	values := make(map[string]string, len(m.Settings))
	for _, field := range m.Settings {
		if value, err := a.plugins.Setting(m.ID, field.Key); err == nil {
			values[field.Key] = value
		}
	}
	watched := make([]wallet.Address, 0, len(a.settings.Accounts)+len(a.settings.Wallet))
	watched = append(watched, a.settings.Accounts...)
	watched = append(watched, a.walletAddresses()...)
	accounts := make([]plugin.Account, 0, len(watched))
	for _, acc := range watched {
		kind, ok := wallet.KindOf(acc.Addr)
		if !ok {
			continue
		}
		accounts = append(accounts, plugin.Account{
			Address: acc.Addr,
			Label:   acc.Label,
			Family:  string(kind),
		})
	}
	kv, err := a.plugins.KV(m.ID)
	if err != nil {
		kv = nil
	}
	logs := a.pluginLog(m.ID)
	locale := a.lang()
	// One pinned reader per chain the manifest names: the active network
	// uses the app's own reader (so the user's custom endpoint applies), the
	// others their preset endpoints. The plugin never picks a URL.
	readers := make(map[string]chain.Reader, len(m.Permissions.Chains))
	for _, id := range m.Permissions.Chains {
		if id == a.settings.NetworkID && a.reader != nil {
			readers[id] = a.reader
			continue
		}
		network, ok := chains.ByID(id)
		if !ok {
			continue
		}
		if reader, err := chains.NewReader(network, ""); err == nil {
			readers[id] = reader
		}
	}
	return plugin.HostDeps{
		Manifest: m,
		Settings: func(key string) (string, error) {
			value, ok := values[key]
			if !ok {
				return "", nil
			}
			return value, nil
		},
		KV: kv,
		RPC: func(chainID, method string, params any) (json.RawMessage, error) {
			reader, ok := readers[chainID]
			if !ok {
				return nil, fmt.Errorf("chain %q is not available", chainID)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return reader.RPC(ctx, method, params)
		},
		Accounts: func() []plugin.Account { return accounts },
		Sign: func(req plugin.SignRequest) (plugin.SignResult, error) {
			return a.signForPlugin(m, readers, req)
		},
		Log:    logs.Add,
		Locale: func() string { return locale },
		Translate: func(key string, args []any) string {
			return i18n.T(locale, i18n.Key(key), args...)
		},
	}
}
