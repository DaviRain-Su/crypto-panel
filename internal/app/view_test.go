package app

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

const (
	viewAddr    = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	viewAddrEIP = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	viewSol     = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a := &App{
		settings:     DefaultSettings(),
		settingsPath: filepath.Join(t.TempDir(), "settings.json"),
		systemLocale: i18n.English,
	}
	a.newReader()
	a.initPlugins(t.TempDir())
	a.keyring = wallet.NewKeyring(memStore{}, nil)
	a.startRefresh = func(int, chain.Reader, []string) {}
	a.runOnMain = func(fn func()) { fn() }
	a.goAsync = func(fn func()) { fn() }
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(true) }
	return a
}

func TestViewEmptyState(t *testing.T) {
	a := newTestApp(t)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Crypto Panel", "Wallet & watch", "No addresses yet", "Add"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
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
	for _, want := range []string{"加密面板", "钱包与只读", "还没有地址", "添加"} {
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
