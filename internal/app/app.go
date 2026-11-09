// Package app is the trusted shell: the main window, the sidebar, the pages,
// the settings, the plugin manager, the native wallet and the refresh loop.
// It watches addresses and reads balances; plugins run in their own sandboxed
// runtimes and reach the shell only through the host functions their manifest
// grants. The wallet's keys live in the OS keychain and are never handed to a
// plugin.
package app

import (
	"log"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/intents"
	"github.com/davirain-su/lintel/internal/passkey"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/prices"
	"github.com/davirain-su/lintel/internal/wallet"
)

// The main window's default and smallest size in DIPs, and the sidebar's
// width. The window is a normal desktop window.
const (
	windowWidth     = 1060
	windowHeight    = 760
	minWindowWidth  = 820
	minWindowHeight = 560
	sidebarWidth    = 232
	// sidebarRailWidth is the collapsed sidebar: the icon rail.
	sidebarRailWidth = 64
	// contentMaxWidth is how wide the page column grows. The header and
	// the body share it, so a wider window uses the room up to this cap.
	contentMaxWidth = 1100
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
	routeApps      = route{kind: "apps"}
	routeIntents   = route{kind: "intents"}
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
	// pageBox is the page column, so the launcher can count columns from
	// the width the last frame gave it.
	pageBox ui.Handle
	// passkeyDetails unfolds the passkey explanation on the wallet page.
	passkeyDetails bool

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
	// nearKeyring holds the wallet's NEAR accounts and their ed25519 keys.
	nearKeyring      *wallet.NearKeyring
	draftNearKey     string
	draftNearAccount string
	draftNearLabel   string
	nearErr          string
	// solanaKeyring holds the wallet's Solana accounts and their ed25519 keys.
	solanaKeyring *wallet.SolanaKeyring
	// The wallet's own NEAR transfer: the account it leaves, the form, and
	// the last transaction's hash.
	nearSendFrom   string
	nearSendTo     string
	nearSendAmount string
	nearSendErr    string
	nearSendNotice string
	nearSendHash   string
	nearSendBusy   bool
	// newKey is a key just created, shown once so the user can save it;
	// revealedKey is a key the user asked to see. shownKeys is the same for
	// a wallet that was created with one key per chain. All of them are
	// cleared as soon as they are no longer on screen.
	newKey       string
	newKeyAddr   string
	revealedKey  string
	revealedAddr string
	shownKeys    []shownKey
	// passkeyKind is the system check the wallet can require; authenticate
	// asks for it, and tests replace it. authBusy names the account whose
	// prompt is up.
	passkeyKind  passkey.Kind
	authenticate func(string) error
	authBusy     string

	// The passkey wallet: the browser ceremony that creates and uses the
	// credential, the form for sending from its NEAR account, and the state
	// the card shows. The credential's private key never enters the app.
	passkeyBusy   bool
	passkeyErr    string
	passkeyNotice string
	// passkeyPrepared is the one-time list of every network's address, shown
	// after a passkey is created and cleared when the user dismisses it.
	passkeyPrepared   bool
	passkeyDeployHash string
	passkeySendTo     string
	passkeySendAmount string
	passkeySendErr    string
	passkeySendNotice string
	passkeySendHash   string
	passkeySendBusy   bool
	passkeyEVMBusy    bool
	passkeyOp         *passkeyOperation
	newCeremony       func(func(string) error) (passkeyCeremony, error)
	openExternal      func(string) error

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
	plugins             *plugin.Store
	pluginManager       *plugin.Manager
	coding              codingState
	pluginItems         []plugin.Installed
	pluginRunners       map[string]*plugin.Runner
	pluginRunnerHashes  map[string]string
	pluginLogs          map[string]*plugin.LogBuffer
	pluginEnabled       map[string]*bool
	pluginDrafts        map[string]*string
	pluginSettingStatus map[string][]plugin.SettingStatus
	pluginLogsOpen      map[string]bool
	pluginNotice        string
	provenanceLocked    []plugin.Installed
	pluginView          pluginPageState
	pluginGen           int

	// 安装后验证只保存当前进程中的安全元数据，不复用普通页面的签名能力。
	installationVerifications        map[string]*installationVerification
	runningInstallationVerifications int

	// The plugin store: the tab the Apps page shows, the app whose settings
	// are open, the catalog last read, the registry form, and the entry
	// being installed.
	pluginTab       string
	pluginDetail    string
	storeQuery      string
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
	// priceSource reads approximate USD prices. Nil turns fiat values off,
	// as the tests do.
	priceSource priceSource
	// runOnMain runs work on the UI thread, and goAsync off it. Tests
	// replace both with direct calls.
	runOnMain func(func())
	goAsync   func(func())
	// confirmInstall shows the permission prompt and reports the answer.
	// Tests replace it.
	confirmInstall    func(plugin.Manifest, installRequest, func(bool))
	confirmProvenance func(plugin.ProvenanceView, func(bool))
	// installOpen is set while the permission prompt is on screen.
	installOpen bool
	// confirmConnect asks the user to let a DApp see the NEAR accounts, and
	// approve shows a transaction's decode when tests replace the window.
	// Production leaves approve nil: approver() uses askApproval, and passkey
	// deploy/send use approvePasskey so a canceled operation can close or
	// dequeue that prompt. openApproval, when set, replaces the passkey window.
	confirmConnect func(origin string) bool
	approve        func(approvalRequest) bool
	openApproval   func(approvalRequest, func(bool)) func()

	// approvalMu shows one approval window at a time, however many plugins
	// or pages ask at once.
	approvalMu sync.Mutex

	// The DApp page: its untrusted web view, the URL bar, and the origins
	// the user connected. A page that is not connected cannot sign.
	dappView      *mygo.WebView
	dappURLDraft  string
	dappErr       string
	dappConnected map[string]bool
	dappBridge    *dappBridgeState

	// 本机 API 的监听器、系统凭据服务、Settings 显示值与端口输入。
	// pluginSecrets 非空时代替 crypto-panel-plugins。测试必须设置它；
	// 生产路径留空，由 initPlugins 使用真实凭据服务。
	pluginSecrets  plugin.Secrets
	agentSecrets   wallet.SecretStore
	agentSrv       *agent.Server
	agentErr       string
	agentAddr      string
	agentToken     string
	agentPortDraft string

	// The Intents page: the assets 1Click supports, the swap the user is
	// asking about, and the last dry quote. approveIntents shows a quote's
	// conditions; tests replace it.
	intentsClient        *intents.Client
	intentsTokens        []intents.Token
	intentsTokensErr     string
	intentsTokensLoading bool
	intentsTokensLoaded  bool
	intentsOrigin        string
	intentsDestination   string
	intentsAmount        string
	intentsRecipient     string
	intentsRefund        string
	intentsErr           string
	intentsBusy          bool
	intentsQuote         *intents.Quote
	intentsAccepted      bool
	intentsNotice        string
	approveIntents       func(intentsApprovalRequest) bool
}

// appUserDataDir, appRunOnMain and appGoAsync are the startup hooks. Tests
// replace them so New can load the locked list without the UI loop.
var (
	appUserDataDir = func() (string, error) { return mygo.App.Path(mygo.PathUserData) }
	appRunOnMain   = mygo.RunOnMain
	appGoAsync     = func(fn func()) { go fn() }
)

// New loads the settings and returns the shell, before the app is ready.
func New() (*App, error) {
	dir, err := appUserDataDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "settings.json")
	settings, loadErr := LoadSettings(path)
	if loadErr != nil {
		log.Printf("lintel: %v", loadErr)
	}
	a := &App{
		settings:       settings,
		settingsPath:   path,
		systemLocale:   i18n.English,
		rpcDraft:       settings.RPCURL,
		storeURLDraft:  settings.PluginStoreURL,
		agentPortDraft: strconv.Itoa(settings.AgentPort),
		dappConnected:  map[string]bool{},
	}
	a.newReader()
	a.initPlugins(dir)
	// Keychain service names are how existing secrets are found. They stay
	// on the original name so a rename of the product does not orphan them.
	a.agentSecrets = mygo.Secrets.Service("crypto-panel-agent")
	secrets := mygo.Secrets.Service("crypto-panel-wallet")
	a.keyring = wallet.NewKeyring(secrets, settings.Wallet)
	a.nearKeyring = wallet.NewNearKeyring(secrets, settings.NearWallet)
	a.solanaKeyring = wallet.NewSolanaKeyring(secrets, settings.SolanaAccounts())
	a.intentsClient = intents.New()
	a.priceSource = prices.New()
	a.startRefresh = a.fetch
	a.runOnMain = appRunOnMain
	a.goAsync = appGoAsync
	a.confirmInstall = a.askInstall
	a.confirmProvenance = a.openProvenanceConfirm
	a.refreshProvenanceLocks()
	a.initAuthoring()
	a.confirmConnect = a.askConnect
	// approve stays nil. Plugins, the wallet, and NEAR send go through
	// approver(), which falls back to askApproval. Passkey deploy and send
	// use approvePasskey instead, so cancellation reaches the native wait.
	a.approveIntents = a.askIntentsApproval
	a.passkeyKind = passkey.Check()
	a.authenticate = passkey.Authenticate
	a.newCeremony = func(open func(string) error) (passkeyCeremony, error) {
		return newCeremonyServer(open)
	}
	a.openExternal = mygo.Shell.OpenExternal
	return a, nil
}

// newReader rebuilds the reader after the network or endpoint changed, and
// drops the plugin runtimes that snapshotted the old one.
func (a *App) newReader() {
	a.cancelPasskeyOperations()
	a.stopAllRunners()
	reader, err := a.settings.Reader()
	if err != nil {
		log.Printf("lintel: %v", err)
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
	a.registerDappScheme()
	a.createWindow()
	a.startAgent()
	// The Dock icon, or launching the app again, brings the window back.
	mygo.App.OnActivate(func(bool) { a.showWindow() })
	mygo.App.OnSecondInstance(func([]string, string) { a.showWindow() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.cancelPasskeyOperations()
		a.quitting = true
		if a.coding.cancel != nil {
			a.coding.cancel()
		}
		a.stopAgent()
		a.stopAllRunners()
		if a.pluginManager != nil {
			_ = a.pluginManager.Close()
		}
	})
	// A locked screen is a locked wallet.
	mygo.Power.OnLockScreen(func() { mygo.RunOnMain(a.lockAll) })
	go a.refreshLoop()
	a.requestRefresh()
	a.refreshPasskeyEVMAddress()
	a.watchPasskeyChains()
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
		BackgroundColor:      "light-dark(#f6f4ef, #141513)",
		Content:              ui.View(a.view),
	})
	a.win.OnClose(func(e *mygo.CloseEvent) {
		a.cancelPasskeyOperations()
		if a.quitting {
			return
		}
		e.PreventDefault()
		// On macOS the window closes to the Dock: the app keeps watching,
		// and the Dock icon brings the window back. Elsewhere closing the
		// window quits, as desktop apps do.
		if runtime.GOOS == "darwin" {
			a.lockAll()
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
	s.NearWallet = append([]wallet.NearAccount(nil), a.settings.NearWallet...)
	s.Wallets = append([]wallet.Container(nil), a.settings.Wallets...)
	if s.Passkey != nil {
		p := *s.Passkey
		s.Passkey = &p
	}
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
		a.newKey, a.newKeyAddr = "", ""
		a.shownKeys = nil
		a.walletNotice = ""
		a.passkeyPrepared = false
	}
	if a.route.kind == "plugins" && r.kind != "plugins" {
		a.pluginDetail = ""
	}
	a.route = r
	if r == routeIntents && !a.intentsTokensLoaded && !a.intentsTokensLoading {
		a.loadIntentsTokens()
	}
	a.invalidate()
}

// touch records that what the app watches changed.
func (a *App) touch() { a.gen++ }

func (a *App) save() {
	if err := SaveSettings(a.settingsPath, a.settings); err != nil {
		a.settingsErr = a.t(i18n.SaveFailed)
		log.Printf("lintel: saving settings: %v", err)
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
	a.refreshPasskeyEVMAddress()
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
