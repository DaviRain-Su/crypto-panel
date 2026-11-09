package app

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/wallet"
)

const (
	viewAddr    = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	viewAddrEIP = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	viewSol     = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := &App{
		settings:      DefaultSettings(),
		settingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		systemLocale:  i18n.English,
		pluginSecrets: memStore{},
	}
	a.newReader()
	a.initPlugins(t.TempDir())
	a.keyring = wallet.NewKeyring(memStore{}, nil)
	a.nearKeyring = wallet.NewNearKeyring(memStore{}, nil)
	a.solanaKeyring = wallet.NewSolanaKeyring(memStore{}, nil)
	a.startRefresh = func(int, chain.Reader, []string) {}
	a.runOnMain = func(fn func()) { fn() }
	a.goAsync = func(fn func()) { fn() }
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(true) }
	a.confirmConnect = func(string) bool { return true }
	a.approve = func(approvalRequest) bool { return true }
	a.dappConnected = map[string]bool{}
	a.openExternal = func(string) error { return nil }
	return a
}

func TestViewEmptyState(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Lintel", "Wallet & watch", "No addresses yet", "Add"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestViewSidebarCollapses(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Wallet & watch") {
		t.Fatal("the expanded sidebar has no labels")
	}
	press := func(name string) {
		t.Helper()
		info, ok := tt.Access(name)
		if !ok {
			t.Fatalf("no %q control", name)
		}
		tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	}
	press("Collapse sidebar")
	if !a.settings.SidebarCollapsed {
		t.Fatal("the sidebar did not collapse")
	}
	if tt.HasText("Wallet & watch") {
		t.Error("the collapsed sidebar still shows its labels")
	}
	press("Expand sidebar")
	if a.settings.SidebarCollapsed {
		t.Fatal("the sidebar did not expand")
	}
	if !tt.HasText("Wallet & watch") {
		t.Error("the expanded sidebar has no labels")
	}
}

func TestViewSidebarRailCentersIcons(t *testing.T) {
	a := newTestApp(t)
	a.settings.SidebarCollapsed = true
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Settings")
	if !ok {
		t.Fatal("no Settings control")
	}
	cx := info.Bounds.X + info.Bounds.W/2
	// The rail is 64 DIPs. The control should sit on its center, and be
	// large enough to hold a 22pt icon.
	if cx < 26 || cx > 38 {
		t.Fatalf("settings rail control not centered: %+v", info.Bounds)
	}
	if info.Bounds.W < 40 || info.Bounds.H < 40 {
		t.Fatalf("settings rail control too small: %+v", info.Bounds)
	}
}

func TestViewSidebarSelectionUsesAccent(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Portfolio")
	if !ok {
		t.Fatal("no Portfolio control")
	}
	tt.ClickAt(info.Bounds.X+4, info.Bounds.Y+info.Bounds.H/2)
	tt.Frame()
	info, ok = tt.AccessByRole(ui.RoleButton, "Portfolio")
	if !ok {
		t.Fatal("Portfolio control disappeared")
	}
	// Sample the row's fill, in from the right edge, clear of the label.
	x := int(info.Bounds.X + info.Bounds.W - 6)
	y := int(info.Bounds.Y + info.Bounds.H/2)
	r, g, b, _ := tt.Image().At(x, y).RGBA()
	// Accent in light mode is #4e6b52: green, darker than the paper sidebar.
	if g>>8 < 0x40 || g < r || g < b {
		t.Fatalf("focused selection is not the accent at (%d,%d): #%02x%02x%02x", x, y, r>>8, g>>8, b>>8)
	}
}

func TestSidebarClickSelectsPage(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Wallet")
	if !ok {
		t.Fatal("no Wallet control")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	if a.route.kind != "wallet" {
		t.Fatalf("clicking Wallet left the route at %q", a.route.kind)
	}
	info, ok = tt.AccessByRole(ui.RoleButton, "Settings")
	if !ok {
		t.Fatal("no Settings control")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	if a.route.kind != "settings" {
		t.Fatalf("clicking Settings left the route at %q", a.route.kind)
	}
}

func TestSidebarTooltip(t *testing.T) {
	a := newTestApp(t)
	a.settings.SidebarCollapsed = true
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Intents")
	if !ok {
		t.Fatal("no Intents control")
	}
	if info.Description != "Intents" {
		t.Fatalf("collapsed Intents tooltip = %q", info.Description)
	}
}

func TestSidebarUpClampsAtStart(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Portfolio")
	if !ok {
		t.Fatal("no Portfolio control")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Key(0, ui.KeyUp)
	if a.route.kind != "" {
		t.Fatalf("up from the first page went to %q", a.route.kind)
	}
}

func TestSidebarDownClampsAtEnd(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Portfolio")
	if !ok {
		t.Fatal("no Portfolio control")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Key(0, ui.KeyEnd)
	if a.route.kind != "settings" {
		t.Fatalf("end went to %q", a.route.kind)
	}
	tt.Key(0, ui.KeyDown)
	if a.route.kind != "settings" {
		t.Fatalf("down from the last page went to %q", a.route.kind)
	}
}

func TestSidebarHomeEnd(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Wallet")
	if !ok {
		t.Fatal("no Wallet control")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Key(0, ui.KeyEnd)
	if a.route.kind != "settings" {
		t.Fatalf("end went to %q", a.route.kind)
	}
	tt.Key(0, ui.KeyHome)
	if a.route.kind != "" {
		t.Fatalf("home went to %q", a.route.kind)
	}
}

func TestViewSidebarKeys(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	info, ok := tt.AccessByRole(ui.RoleButton, "Portfolio")
	if !ok {
		t.Fatal("no Portfolio control")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Key(0, ui.KeyDown)
	if a.route.kind != "wallet" {
		t.Fatalf("down from Portfolio went to %q", a.route.kind)
	}
	tt.Key(0, ui.KeyEnd)
	if a.route.kind != "settings" {
		t.Fatalf("end went to %q", a.route.kind)
	}
	tt.Key(0, ui.KeyHome)
	if a.route.kind != "" {
		t.Fatalf("home went to %q", a.route.kind)
	}
}

func TestViewShowsFiatValues(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr}}
	a.snap = &snapshot{
		accounts: []accountSnapshot{{addr: viewAddr, native: big.NewInt(2_000_000_000_000_000_000)}},
		usd:      map[string]float64{"ETH": 3000},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("≈ $6,000.00") {
		t.Fatalf("missing the fiat value in %q", tt.Texts())
	}
	a.settings.HideFiat = true
	tt.Frame()
	if tt.HasText("≈ $6,000.00") {
		t.Error("the fiat value is still shown after it was turned off")
	}
}

func TestUSDText(t *testing.T) {
	cases := map[float64]string{
		0:        "$0.00",
		0.0042:   "$0.0042",
		12.5:     "$12.50",
		1234.5:   "$1,234.50",
		1234567:  "$1,234,567.00",
		99999999: "$99,999,999.00",
	}
	for in, want := range cases {
		if got := usdText(in); got != want {
			t.Errorf("usdText(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestViewShowsBalances(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr, Label: "Main"}}
	a.lastUpdated = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	a.snap = &snapshot{
		at:    a.lastUpdated,
		block: 42,
		accounts: []accountSnapshot{{
			addr:   viewAddr,
			native: big.NewInt(1_500_000_000_000_000_000),
			tokens: []chain.TokenBalance{{
				Token:  chains.Ethereum.Tokens[0], // USDC, 6 decimals
				Amount: big.NewInt(1_200_000_000),
			}},
		}},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"1.5 ETH", "Main", wallet.Checksum(viewAddr), "USDC", "1,200", "Block 42",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestViewShowsSolanaBalances(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.newReader()
	a.settings.Accounts = []wallet.Address{{Addr: viewSol, Label: "Sol"}}
	a.snap = &snapshot{
		accounts: []accountSnapshot{{
			addr:   viewSol,
			native: big.NewInt(2_500_000_000), // 2.5 SOL
			tokens: []chain.TokenBalance{{
				Token:  chains.SolanaMainnet.Tokens[0], // USDC, 6 decimals
				Amount: big.NewInt(42_000_000),
			}},
		}},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"2.5 SOL", "Sol", viewSol, "USDC", "42", "Solana"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestViewAddsAddress(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Address"); err != nil {
		t.Fatal(err)
	}
	tt.Type(viewAddrEIP)
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Accounts) != 1 {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if got := a.settings.Accounts[0].Addr; got != viewAddr {
		t.Fatalf("stored %q, want lowercase %q", got, viewAddr)
	}
	if !tt.HasText(wallet.Short(viewAddr)) {
		t.Errorf("the new account is not shown: %q", tt.Texts())
	}
}

func TestViewAddsSolanaAddress(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.newReader()
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Address"); err != nil {
		t.Fatal(err)
	}
	tt.Type(viewSol)
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Accounts) != 1 || a.settings.Accounts[0].Addr != viewSol {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if !tt.HasText(wallet.Short(viewSol)) {
		t.Errorf("the new account is not shown: %q", tt.Texts())
	}
}

func TestViewRejectsInvalidAddress(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Address"); err != nil {
		t.Fatal(err)
	}
	tt.Type("0x123")
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Accounts) != 0 {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if !tt.HasText("not a valid address") {
		t.Errorf("the error is not shown: %q", tt.Texts())
	}
}

func TestViewRejectsOtherFamily(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Address"); err != nil {
		t.Fatal(err)
	}
	tt.Type(viewSol)
	if err := tt.Click("Add"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Accounts) != 0 {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if !tt.HasText("another chain type") {
		t.Errorf("the error is not shown: %q", tt.Texts())
	}
}

func TestViewHidesOtherFamilies(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewSol, Label: "Sol"}}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if tt.HasText("Sol") {
		t.Errorf("a Solana account shows on Ethereum: %q", tt.Texts())
	}
	if !tt.HasText("No addresses yet") {
		t.Errorf("the empty state is not shown: %q", tt.Texts())
	}
}

func TestViewRemovesAddress(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr, Label: "Main"}}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Remove"); err != nil {
		t.Fatal(err)
	}
	if len(a.settings.Accounts) != 0 {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if !tt.HasText("No addresses yet") {
		t.Errorf("the empty state is not shown: %q", tt.Texts())
	}
}

func TestViewSpeaksChinese(t *testing.T) {
	a := newTestApp(t)
	a.settings.Language = i18n.Chinese
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"门厅", "钱包与只读", "还没有地址", "添加"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestViewSettingsPage(t *testing.T) {
	a := newTestApp(t)
	a.route = routeSettings
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Chain type", "Chain", "Ethereum", "RPC endpoint", "Language", "Refresh every", "Version 0.1.0",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestViewShowsRefreshError(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr}}
	a.lastErr = "ethereum-rpc.publicnode.com: boom"
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("boom") {
		t.Errorf("the error is not shown: %q", tt.Texts())
	}
}
