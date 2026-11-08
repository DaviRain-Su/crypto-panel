// Package app is the trusted shell: the main window, the sidebar, the pages,
// the settings, the plugin manager and the refresh loop. It watches addresses
// and reads balances; plugins run in their own sandboxed runtimes and reach
// the shell only through the host functions their manifest grants. It holds
// no keys.
package app

import (
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// The main window's default and smallest size in DIPs, and the sidebar's
// width. The window is a normal desktop window, not a menu bar panel.
const (
	windowWidth     = 1000
	windowHeight    = 720
	minWindowWidth  = 760
	minWindowHeight = 520
	sidebarWidth    = 216
)

// route is what the sidebar has selected: one of the shell's pages, or a
// page a plugin added. The zero route is the portfolio, the page the app
// opens on.
type route struct {
	kind string // "", "plugins", "settings" or "plugin"
	id   string // plugin id, for kind "plugin"
	page string // plugin page name, for kind "plugin"
}

var (
	routePortfolio = route{}
	routeWallet    = route{kind: "wallet"}
	routePlugins   = route{kind: "plugins"}
	routeSettings  = route{kind: "settings"}
)

// App is the shell's state. Everything here is read and changed on the main
// thread, by the view and by window callbacks, except the network work,
// which comes back through mygo.RunOnMain.
type App struct {
	settings     Settings
	settingsPath string

	win      *mygo.Window
	route    route
	quitting bool
	// pal is the appearance's colors, set for each frame the view draws.
	pal palette

	draftAddr   string
	draftLabel  string
	addErr      string
	rpcDraft    string
	settingsErr string

	// keyring holds the native wallet. Its keys live in the OS keychain; the
	// settings keep only the accounts' addresses and labels.
	keyring          *wallet.Keyring
	draftKey         string
	draftWalletLabel string
	walletErr        string
	walletNotice     string
	// newKey is a key just created, shown once so the user can save it;
	// revealedKey is a key the user asked to see. Both are cleared as soon as
	// they are no longer on screen.
	newKey       string
	newKeyAddr   string
	revealedKey  string
	revealedAddr string

	systemLocale string
	reader       chain.Reader

	snap        *snapshot
	loading     bool
	lastErr     string
	lastUpdated time.Time
	// gen counts changes to what is being watched, so an answer from before
	// a change is dropped instead of shown.
	gen int

	// plugins is the installed plugins folder. The rest is the manager's
	// state: the list last read, one runtime and one log per plugin, the
	// switch and form values the view binds to, and the page it shows.
	plugins        *plugin.Store
	pluginItems    []plugin.Installed
	pluginRunners  map[string]*plugin.Runner
	pluginLogs     map[string]*plugin.LogBuffer
	pluginEnabled  map[string]*bool
	pluginDrafts   map[string]*string
	pluginLogsOpen map[string]bool
	pluginNotice   string
	pluginView     pluginPageState
	pluginGen      int

	// The plugin store: the tab the Plugins page shows, the catalog last
	// read, the registry form, and the entry being installed.
	pluginTab       string
	storeURLDraft   string
	storeFormErr    string
	storeItems      []plugin.RegistryEntry
	storeErr        string
	storeLoading    bool
	storeLoaded     bool
	storeGen        int
	storeInstalling string

	// startRefresh starts the network work. Tests replace it.
	startRefresh func(gen int, reader chain.Reader, addrs []string)
	// runOnMain runs work on the UI thread, and goAsync off it. Tests
	// replace both with direct calls.
	runOnMain func(func())
	goAsync   func(func())
	// confirmInstall shows the permission prompt and reports the answer.
	// Tests replace it.
	confirmInstall func(plugin.Manifest, installRequest, func(bool))
	// installOpen is set while the permission prompt is on screen.
	installOpen bool

	// approvalMu shows one approval window at a time, however many plugins
	// ask at once.
	approvalMu sync.Mutex
}

// New loads the settings and returns the shell, before the app is ready.
func New() (*App, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "settings.json")
	settings, loadErr := LoadSettings(path)
	if loadErr != nil {
		log.Printf("crypto-panel: %v", loadErr)
	}
	a := &App{
		settings:      settings,
		settingsPath:  path,
		systemLocale:  i18n.English,
		rpcDraft:      settings.RPCURL,
		storeURLDraft: settings.PluginStoreURL,
	}
	a.newReader()
	a.initPlugins(dir)
	a.keyring = wallet.NewKeyring(mygo.Secrets.Service("crypto-panel-wallet"), settings.Wallet)
	a.startRefresh = a.fetch
	a.runOnMain = mygo.RunOnMain
	a.goAsync = func(fn func()) { go fn() }
	a.confirmInstall = a.askInstall
	return a, nil
}

// newReader rebuilds the reader after the network or endpoint changed, and
// drops the plugin runtimes that snapshotted the old one.
func (a *App) newReader() {
	a.stopAllRunners()
	reader, err := a.settings.Reader()
	if err != nil {
		log.Printf("crypto-panel: %v", err)
		a.reader = nil
		return
	}
	a.reader = reader
}

// network is the preset the app currently reads.
func (a *App) network() chain.Network { return a.settings.Network() }

// Ready opens the main window and starts the refresh loop. It runs once the
// app is ready, as mygo.App.WhenReady asks.
func (a *App) Ready() {
	mygo.App.SetLanguages(i18n.English, i18n.Chinese)
	if prefs := mygo.App.PreferredLanguages(); len(prefs) > 0 {
		a.systemLocale = prefs[0]
	}
	a.applyLocale()
	a.createWindow()
	// The Dock icon, or launching the app again, brings the window back.
	mygo.App.OnActivate(func(bool) { a.showWindow() })
	mygo.App.OnSecondInstance(func([]string, string) { a.showWindow() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		a.stopAllRunners()
	})
	go a.refreshLoop()
	a.requestRefresh()
}

// createWindow opens the app's one window: a sidebar of pages beside the
// selected page.
func (a *App) createWindow() {
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:                mygo.App.Name(),
		Width:                windowWidth,
		Height:               windowHeight,
		MinWidth:             minWindowWidth,
		MinHeight:            minWindowHeight,
		StateKey:             "main",
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 18, Y: 18},
		Vibrancy:             mygo.VibrancySidebar,
		BackgroundColor:      "light-dark(#ffffff, #1c1c1e)",
		Content:              ui.View(a.view),
	})
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		// On macOS the window closes to the Dock: the app keeps watching,
		// and the Dock icon brings the window back. Elsewhere closing the
		// window quits, as desktop apps do.
		if runtime.GOOS == "darwin" {
			a.win.Hide()
			return
		}
		mygo.App.Quit()
	})
	// A plugin folder added by hand shows up when the user comes back to
	// the window, without an install or a restart.
	a.win.OnFocus(func() {
		a.refreshPlugins()
		a.invalidate()
	})
}

// showWindow brings the main window forward.
func (a *App) showWindow() {
	if a.win == nil {
		return
	}
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.win.Show()
	a.win.Focus()
	mygo.App.Focus()
}

// Settings returns a copy of the current settings, for tests and later
// milestones.
func (a *App) Settings() Settings {
	s := a.settings
	s.Accounts = append([]wallet.Address(nil), a.settings.Accounts...)
	s.Wallet = append([]wallet.Account(nil), a.settings.Wallet...)
	return s
}

func (a *App) lang() string {
	return i18n.Resolve(a.settings.Language, a.systemLocale)
}

func (a *App) t(key i18n.Key, args ...any) string {
	return i18n.T(a.lang(), key, args...)
}

func (a *App) applyLocale() {
	mygo.App.SetLocale(a.settings.Language)
}

func (a *App) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// goTo selects a page of the shell. A revealed key does not outlive the
// wallet page.
func (a *App) goTo(r route) {
	if a.route == r {
		return
	}
	if a.route.kind == "wallet" && r.kind != "wallet" {
		a.revealedKey, a.revealedAddr = "", ""
		a.walletNotice = ""
	}
	a.route = r
	a.invalidate()
}

// touch records that what the app watches changed.
func (a *App) touch() { a.gen++ }

func (a *App) save() {
	if err := SaveSettings(a.settingsPath, a.settings); err != nil {
		a.settingsErr = a.t(i18n.SaveFailed)
		log.Printf("crypto-panel: saving settings: %v", err)
		return
	}
	a.settingsErr = ""
}

func (a *App) addAccount() {
	family, ok := addressFamily(a.draftAddr)
	if !ok {
		a.addErr = a.t(i18n.InvalidAddress)
		return
	}
	if family != a.network().Family {
		a.addErr = a.t(i18n.WrongFamily)
		return
	}
	kind, _ := wallet.KindOf(a.draftAddr)
	addr, err := wallet.Normalize(kind, a.draftAddr)
	if err != nil {
		a.addErr = a.t(i18n.InvalidAddress)
		return
	}
	if a.settings.Has(addr) {
		a.addErr = a.t(i18n.DuplicateAddress)
		return
	}
	a.settings.Accounts = append(a.settings.Accounts, wallet.Address{
		Addr:  addr,
		Label: strings.TrimSpace(a.draftLabel),
	})
	a.settings = a.settings.Validate()
	a.draftAddr, a.draftLabel, a.addErr = "", "", ""
	a.save()
	a.touch()
	a.requestRefresh()
}

func (a *App) removeAccount(addr string) {
	kept := a.settings.Accounts[:0]
	for _, acc := range a.settings.Accounts {
		if acc.Addr != addr {
			kept = append(kept, acc)
		}
	}
	a.settings.Accounts = kept
	a.save()
	a.touch()
	a.requestRefresh()
}

func (a *App) setNetwork(id string) {
	if a.settings.NetworkID == id {
		return
	}
	a.settings.NetworkID = id
	a.settings.RPCURL = ""
	a.rpcDraft = ""
	a.settings = a.settings.Validate()
	a.newReader()
	a.save()
	a.touch()
	a.requestRefresh()
}

func (a *App) applyRPC() {
	raw := strings.TrimSpace(a.rpcDraft)
	switch {
	case raw == "":
		a.settings.RPCURL = ""
	case !ValidRPCURL(raw):
		a.settingsErr = a.t(i18n.InvalidURL)
		return
	default:
		a.settings.RPCURL = raw
	}
	a.settings = a.settings.Validate()
	a.rpcDraft = a.settings.RPCURL
	a.newReader()
	a.save()
	a.touch()
	a.requestRefresh()
}

func (a *App) resetRPC() {
	a.rpcDraft = ""
	a.settings.RPCURL = ""
	a.settings = a.settings.Validate()
	a.newReader()
	a.save()
	a.touch()
	a.requestRefresh()
}

func (a *App) setRefreshSeconds(n int) {
	if a.settings.RefreshSeconds == n {
		return
	}
	a.settings.RefreshSeconds = n
	a.settings = a.settings.Validate()
	a.save()
}

func (a *App) setLanguage(tag string) {
	if a.settings.Language == tag {
		return
	}
	a.settings.Language = tag
	a.settings = a.settings.Validate()
	a.save()
	a.applyLocale()
	a.invalidate()
}
