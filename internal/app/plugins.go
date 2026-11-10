package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/wallet"
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
	official bool
	page     pages.Page
	err      string
	loading  bool
	hash     string
	updated  time.Time
	hasPage  bool
}

// newPluginSecrets is the production credential backend for plugin secrets
// and install provenance. Tests replace it so they cannot reach the real
// keychain by forgetting to inject one.
var newPluginSecrets = func() plugin.Secrets {
	// Same service name as before the product rename, so plugin secrets
	// already in the keychain stay reachable.
	return mygo.Secrets.Service("crypto-panel-plugins")
}

// initPlugins opens the plugins folder under the app's data directory and
// seeds the official examples on first launch.
func (a *App) initPlugins(dir string) {
	secrets := a.pluginSecrets
	if secrets == nil {
		secrets = newPluginSecrets()
	}
	a.plugins = plugin.NewStore(filepath.Join(dir, "plugins"), secrets)
	a.pluginRunners = map[string]*plugin.Runner{}
	a.pluginRunnerHashes = map[string]string{}
	a.pluginLogs = map[string]*plugin.LogBuffer{}
	a.pluginEnabled = map[string]*bool{}
	a.pluginDrafts = map[string]*string{}
	a.pluginSettingStatus = map[string][]plugin.SettingStatus{}
	a.pluginLogsOpen = map[string]bool{}
	if err := a.plugins.EnsureOfficial(); err != nil {
		a.pluginNotice = a.pluginErrorText(err)
	}
	manager, err := plugin.NewManager(a.plugins)
	if err != nil {
		a.pluginNotice = a.t(i18n.PluginManagerFailed, a.pluginErrorText(err))
	} else {
		a.pluginManager = manager
	}
	a.refreshPlugins()
	a.refreshProvenanceLocks()
}

// refreshPlugins re-reads the plugins folder. It runs on the main thread,
// after an install, a removal or a toggle, never per frame.
func (a *App) refreshPlugins() {
	if a.plugins == nil {
		return
	}
	items, err := a.plugins.List()
	if err != nil {
		a.pluginNotice = a.pluginErrorText(err)
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
	// 验证记录只属于仍安装的同一包；卸载或更新不能保留旧的通过状态。
	for id, current := range a.installationVerifications {
		inst, err := a.plugins.Get(id)
		hash, hashErr := a.plugins.PackageHash(id)
		if err != nil || inst.Err != "" || hashErr != nil || hash != current.result.PackageHash {
			a.forgetInstallationVerification(id)
		} else if !inst.Enabled {
			a.invalidateInstallationVerification(id, "APP_DISABLED")
		}
	}
	// A plugin page whose plugin was removed or turned off falls back to the
	// manager, so the window never shows a page that no longer exists.
	if a.route.kind == "plugin" && !a.pluginPageAvailable(items, a.route.id) {
		a.closePluginPage()
	}
	// 设置状态缓存随列表刷新：移除的条目丢弃，仍存在的重新读取。
	present := make(map[string]bool, len(items))
	for _, item := range items {
		if id := item.Manifest.ID; id != "" {
			present[id] = true
		}
	}
	for id := range a.pluginSettingStatus {
		if !present[id] {
			delete(a.pluginSettingStatus, id)
			continue
		}
		a.loadPluginSettingStatus(id)
	}
}

// loadPluginSettingStatus caches one app's settings status so the form never
// reads the keychain per frame. A busy lease keeps the last known status.
func (a *App) loadPluginSettingStatus(id string) {
	if a.plugins == nil {
		return
	}
	statuses, err := a.plugins.DescribeSettings(id)
	if err != nil {
		return
	}
	a.pluginSettingStatus[id] = statuses
}

// refreshProvenanceLocks reloads the locked-app list off the UI thread.
// The Apps page only reads the cached slice.
func (a *App) refreshProvenanceLocks() {
	if a.plugins == nil || a.goAsync == nil || a.runOnMain == nil {
		return
	}
	plugins := a.plugins
	a.goAsync(func() {
		list, err := plugins.LockedLocalApps()
		a.runOnMain(func() {
			if err == nil && a.plugins == plugins {
				a.provenanceLocked = list
			}
			a.invalidate()
		})
	})
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
		a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
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
		a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
		a.invalidate()
		return
	}
	a.confirmInstall(m, installRequest{unsigned: true, source: a.t(i18n.StoreSourceFolder)}, func(ok bool) {
		if !ok {
			return
		}
		m, err := a.plugins.Install(src)
		if err != nil {
			a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
			a.invalidate()
			return
		}
		a.pluginNotice = a.t(i18n.PluginInstalled, m.Name)
		a.refreshPlugins()
		a.refreshProvenanceLocks()
		a.invalidate()
	})
}

// installPluginZip asks for an archive, unpacks it, shows what it may do,
// and installs it as a local, unsigned app.
func (a *App) installPluginZip() {
	if a.plugins == nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       a.t(i18n.AppZipTitle),
		ButtonLabel: a.t(i18n.StoreInstall),
		Filters:     []mygo.FileFilter{{Name: "zip", Extensions: []string{"zip"}}},
	})
	if err != nil {
		a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
		a.invalidate()
		return
	}
	if len(paths) == 0 {
		return
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
		a.invalidate()
		return
	}
	dir, m, err := a.plugins.UnpackLocal(data)
	if err != nil {
		a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
		a.invalidate()
		return
	}
	a.confirmInstall(m, installRequest{unsigned: true, source: a.t(i18n.StoreSourceZip)}, func(ok bool) {
		defer a.plugins.DiscardUnpacked(dir)
		if !ok {
			return
		}
		m, err := a.plugins.Install(dir)
		if err != nil {
			a.pluginNotice = a.t(i18n.PluginInstallFailed, a.pluginErrorText(err))
			a.invalidate()
			return
		}
		a.pluginNotice = a.t(i18n.PluginInstalled, m.Name)
		a.refreshPlugins()
		a.refreshProvenanceLocks()
		a.invalidate()
	})
}

// confirmPluginRemoval asks before a native uninstall. Tests replace it so
// they can approve the removal without a window.
var confirmPluginRemoval = func(title, message, remove, cancel string) bool {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:               mygo.MessageWarning,
		Title:              title,
		Message:            message,
		Buttons:            []string{remove, cancel},
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	return err == nil && res.Button == 0
}

func (a *App) clearPluginDrafts(id string) {
	for key := range a.pluginDrafts {
		if strings.HasPrefix(key, id+"/") {
			delete(a.pluginDrafts, key)
		}
	}
}

// removeBrokenPlugin deletes a folder the store could not read.
func (a *App) removeBrokenPlugin(dir string) {
	if a.plugins == nil {
		return
	}
	name := filepath.Base(dir)
	if !confirmPluginRemoval(a.t(i18n.PluginRemoveTitle), a.t(i18n.PluginRemoveBody, name), a.t(i18n.Remove), a.t(i18n.Cancel)) {
		return
	}
	remove := a.plugins.RemoveAt
	if a.pluginManager != nil {
		remove = a.pluginManager.RemoveAt
	}
	if err := remove(dir); err != nil {
		a.pluginNotice = a.pluginErrorText(err)
		a.invalidate()
		return
	}
	a.stopRunner(name)
	delete(a.pluginLogs, name)
	a.clearPluginDrafts(name)
	a.pluginNotice = a.t(i18n.PluginRemoved, name)
	a.refreshPlugins()
	a.refreshProvenanceLocks()
	a.invalidate()
}

// removePlugin asks, then deletes the plugin's folder and its secrets.
func (a *App) removePlugin(id string) {
	if a.plugins == nil {
		return
	}
	inst, err := a.plugins.Get(id)
	if err != nil {
		a.pluginNotice = a.pluginErrorText(err)
		a.invalidate()
		return
	}
	if !confirmPluginRemoval(a.t(i18n.PluginRemoveTitle), a.t(i18n.PluginRemoveBody, inst.Manifest.Name), a.t(i18n.Remove), a.t(i18n.Cancel)) {
		return
	}
	remove := a.plugins.Remove
	if a.pluginManager != nil {
		remove = a.pluginManager.Remove
	}
	if err := remove(id); err != nil {
		a.pluginNotice = a.pluginErrorText(err)
		a.invalidate()
		return
	}
	a.stopRunner(id)
	delete(a.pluginLogs, id)
	a.clearPluginDrafts(id)
	a.pluginNotice = a.t(i18n.PluginRemoved, inst.Manifest.Name)
	a.refreshPlugins()
	a.refreshProvenanceLocks()
	a.invalidate()
}

// setPluginEnabled turns a plugin on or off and drops its runtime when it is
// turned off, so a disabled plugin holds no memory.
func (a *App) setPluginEnabled(id string, enabled bool) {
	if a.plugins == nil {
		return
	}
	setEnabled := a.plugins.SetEnabled
	if a.pluginManager != nil {
		setEnabled = a.pluginManager.SetEnabled
	}
	if err := setEnabled(id, enabled); err != nil {
		a.pluginNotice = a.pluginErrorText(err)
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
	values := map[string]string{}
	for _, field := range m.Settings {
		draft := a.pluginDrafts[m.ID+"/"+field.Key]
		if draft == nil || (field.Secret && *draft == "") {
			continue
		}
		values[field.Key] = *draft
	}
	set := a.plugins.SetSettings
	if a.pluginManager != nil {
		set = a.pluginManager.SetSettings
	}
	_, err := set(m.ID, values)
	// 部分系统凭据写入失败时也必须使旧设置快照失效。
	a.stopRunner(m.ID)
	a.loadPluginSettingStatus(m.ID)
	if a.pluginKey().id == m.ID {
		a.pluginGen++
		a.pluginView = pluginPageState{}
	}
	// 只清空确实已应用的 secret 草稿：零写入的失败（busy、校验）保留输入，
	// 部分失败按事务报告的 applied/failed 区分，让用户只补输失败的字段。
	applied := map[string]bool{}
	var writeErr *plugin.SettingsWriteError
	if errors.As(err, &writeErr) {
		for _, key := range writeErr.AppliedKeys {
			applied[key] = true
		}
	} else if err == nil {
		for key := range values {
			applied[key] = true
		}
	}
	for _, field := range m.Settings {
		if !field.Secret || !applied[field.Key] {
			continue
		}
		if draft := a.pluginDrafts[m.ID+"/"+field.Key]; draft != nil {
			*draft = ""
		}
	}
	if err != nil {
		a.invalidateInstallationVerification(m.ID, "SETTINGS_WRITE_FAILED")
		a.pluginNotice = a.t(i18n.PluginSettingsWriteFailed)
		a.invalidate()
		return
	}
	if a.installationVerifications[m.ID] != nil {
		a.invalidateInstallationVerification(m.ID, "SETTINGS_CHANGED")
		a.verifyInstalledApp(m.ID)
	} else if a.pluginKey().id == m.ID {
		a.loadPluginPage()
	}
	a.pluginNotice = a.t(i18n.PluginSettingsSaved)
	a.invalidate()
}

func (a *App) stopRunner(id string) {
	delete(a.pluginRunnerHashes, id)
	if runner, ok := a.pluginRunners[id]; ok {
		delete(a.pluginRunners, id)
		// Runner.Close 仅设置原子关闭标记，当前调用的结果由 generation 丢弃。
		runner.Close()
	}
}

// stopAllRunners 使旧网络和设置的运行结果失效，下一次执行重建 runtime。
func (a *App) stopAllRunners() {
	for id := range a.installationVerifications {
		a.invalidateInstallationVerification(id, "HOST_CHANGED")
	}
	a.pluginGen++
	if a.pluginKey().id != "" {
		a.pluginView = pluginPageState{}
	}
	for id := range a.pluginRunners {
		a.stopRunner(id)
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

// closePluginPage leaves an app for the launcher, dropping the page it
// held.
func (a *App) closePluginPage() {
	a.pluginGen++
	a.pluginView = pluginPageState{}
	a.pluginDetail = ""
	if a.route.kind == "plugin" {
		a.route = routePlugins
	}
	a.invalidate()
}

// openPluginApp opens an app at its first page, as a desktop opens an app.
// An app with nothing to open shows its settings instead.
func (a *App) openPluginApp(id string) {
	inst, ok := a.installedPlugin(id)
	if !ok || inst.Err != "" || !inst.Enabled {
		a.openPluginDetail(id)
		return
	}
	if len(inst.Manifest.Navigation) == 0 {
		a.pluginDetail = id
		a.pluginNotice = a.t(i18n.AppNoPages)
		a.invalidate()
		return
	}
	a.pluginDetail = ""
	a.openPluginPage(id, inst.Manifest.Navigation[0].Page)
}

// openPluginDetail shows one app's settings on the Apps page.
func (a *App) openPluginDetail(id string) {
	a.coding.open = false
	a.pluginDetail = id
	a.pluginTab = "installed"
	a.route = routePlugins
	a.loadPluginSettingStatus(id)
	a.invalidate()
}

// closePluginDetail goes back to the launcher.
func (a *App) closePluginDetail() {
	a.pluginDetail = ""
	a.invalidate()
}

// openApps selects the Apps page at its launcher, whatever it showed
// before.
func (a *App) openApps() {
	a.pluginDetail = ""
	a.pluginTab = "installed"
	a.goTo(routePlugins)
	a.invalidate()
}

// loadPluginPage computes the open page in the background. The runtime is
// created on the main thread (compiling the module is cheap and touches the
// runner map); the call itself runs on its own goroutine, so a slow plugin
// never freezes the window. The execution lease is held from before Get until
// the page call finishes, and released via defer on every path — including a
// panic in the synchronous section, which would otherwise wedge the app busy.
func (a *App) loadPluginPage() {
	key := a.pluginKey()
	if key.id == "" || a.plugins == nil {
		return
	}
	if a.pluginView.key == key && a.pluginView.loading {
		return
	}
	var release func() = func() {}
	if a.pluginManager != nil {
		var err error
		release, err = a.pluginManager.TryExecution(key.id)
		if err != nil {
			if a.pluginView.key != key {
				a.pluginView = pluginPageState{key: key, err: a.t(i18n.PluginBusy)}
			}
			return
		}
	}
	// release 现在覆盖 Get 到 goroutine 结束的每一条路径；同步段的 panic
	// 也会经 defer 释放租约，不会把应用永久卡在 busy。
	defer func() {
		if r := recover(); r != nil {
			release()
			panic(r)
		}
	}()
	inst, err := a.plugins.Get(key.id)
	if err != nil {
		release()
		a.pluginView = pluginPageState{key: key, err: a.t(i18n.PluginUnavailable)}
		return
	}
	state := pluginPageState{key: key, name: inst.Manifest.Name, unsigned: inst.Err == "" && !inst.Signed && !inst.Official, official: inst.Err == "" && inst.Official}
	if inst.Err != "" {
		release()
		a.stopRunner(key.id)
		state.err = a.pluginErrText(inst.Err)
		a.pluginView = state
		return
	}
	if !inst.Enabled {
		release()
		state.err = a.t(i18n.PluginDisabled)
		a.pluginView = state
		return
	}
	declared := false
	for _, nav := range inst.Manifest.Navigation {
		if nav.Page == key.page {
			declared = true
			break
		}
	}
	if !declared {
		release()
		state.err = a.t(i18n.PluginPageMissing)
		a.pluginView = state
		return
	}
	state.hash, err = a.plugins.PackageHash(key.id)
	if err != nil {
		release()
		state.err = a.t(i18n.PluginVerifyFailed)
		a.pluginView = state
		return
	}
	if old := a.pluginView; old.key == key && old.hash == state.hash && old.hasPage {
		state.page, state.updated, state.hasPage = old.page, old.updated, true
	}
	runner, err := a.pluginRunner(inst, state.hash)
	if err != nil {
		release()
		state.err = a.t(i18n.PluginLoadFailed)
		a.pluginView = state
		return
	}
	a.pluginGen++
	gen := a.pluginGen
	state.loading = true
	a.pluginView = state
	a.goAsync(func() {
		page, err := runner.Page(key.page)
		release()
		a.runOnMain(func() {
			if gen != a.pluginGen || a.pluginKey() != key {
				return
			}
			state.loading = false
			if err != nil {
				state.err = a.t(i18n.PluginRefreshKept)
			} else {
				state.page = page
				state.updated = time.Now()
				state.hasPage = true
			}
			a.pluginView = state
			a.invalidate()
		})
	})
}

// pluginRunner returns the plugin's runtime, compiling its source the first
// time. It runs on the main thread.
func (a *App) pluginRunner(inst plugin.Installed, hash string) (*plugin.Runner, error) {
	id := inst.Manifest.ID
	if runner, ok := a.pluginRunners[id]; ok {
		if a.pluginRunnerHashes[id] == hash {
			return runner, nil
		}
		a.stopRunner(id)
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
	a.pluginRunnerHashes[id] = hash
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
		NearSign: func(req plugin.NearSignRequest) (plugin.SignResult, error) {
			return a.signNearForPlugin(m, readers, req)
		},
		Log:    logs.Add,
		Locale: func() string { return locale },
		Translate: func(key string, args []any) string {
			return i18n.T(locale, i18n.Key(key), args...)
		},
	}
}
