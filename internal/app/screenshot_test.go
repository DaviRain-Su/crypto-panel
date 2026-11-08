package app

import (
	"image/png"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/ethereum/go-ethereum/common"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/evmtx"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/intents"
	"github.com/davirain-su/crypto-panel/internal/passkey"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// TestScreenshots writes PNGs of the app's pages for a visual check. It runs
// only when SHOTS=1, so the ordinary test run does not write files.
func TestScreenshots(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("set SHOTS=1 to write screenshots")
	}
	dir := os.Getenv("SHOTS_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	save := func(t *testing.T, tt *ui.Tester, name string) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	portfolio := func(t *testing.T) *App {
		a := newTestApp(t)
		a.settings.Accounts = []wallet.Address{
			{Addr: walletTestAddr, Label: "Main"},
			{Addr: "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", Label: "Savings"},
		}
		a.lastUpdated = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
		a.snap = &snapshot{
			at:    a.lastUpdated,
			block: 26370027,
			accounts: []accountSnapshot{
				{
					addr:   walletTestAddr,
					native: big.NewInt(1_500_000_000_000_000_000),
					tokens: []chain.TokenBalance{{
						Token:  chains.Ethereum.Tokens[0],
						Amount: big.NewInt(1_200_000_000),
					}},
				},
				{
					addr:   "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed",
					native: big.NewInt(250_000_000_000_000_000),
					tokens: []chain.TokenBalance{{
						Token:  chains.Ethereum.Tokens[1],
						Amount: big.NewInt(500_000_000),
					}},
				},
			},
		}
		return a
	}

	a := portfolio(t)
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-portfolio-light")
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	tt.SetDark(true)
	save(t, tt, "new-portfolio-dark")
	a.settings.Language = i18n.Chinese
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-portfolio-zh")

	walletPage := func(t *testing.T) *App {
		a := newTestApp(t)
		a.passkeyKind = passkey.KindTouchID
		a.authenticate = func(string) error { return nil }
		acc1, err := a.keyring.Import(walletTestKey, "Trading")
		if err != nil {
			t.Fatal(err)
		}
		acc2, err := a.keyring.Create("Savings")
		if err != nil {
			t.Fatal(err)
		}
		a.keyring.LockAccount(acc2.Addr)
		a.settings.Wallet = []wallet.Account{acc1, acc2}
		a.settings = a.settings.Validate()
		a.route = routeWallet
		a.lastUpdated = time.Now()
		a.snap = &snapshot{at: a.lastUpdated, accounts: []accountSnapshot{
			{addr: acc1.Addr, native: big.NewInt(240_000_000_000_000_000)},
			{addr: acc2.Addr, native: big.NewInt(85_000_000_000_000_000)},
		}}
		return a
	}
	a = walletPage(t)
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-wallet-light")
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	tt.SetDark(true)
	save(t, tt, "new-wallet-dark")
	a.settings.Language = i18n.Chinese
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-wallet-zh")

	a, _, _ = newDappApp(t)
	a.route = routeWallet
	a.nearSendHash = "CbSQfoTGYCie6fwFyvZe2upMFbHvvCnfKZGeKKru3MMu"
	a.nearSendNotice = i18n.T(i18n.English, i18n.WalletNearSendSent, a.nearSendHash)
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-wallet-near")

	a = newTestApp(t)
	a.route = routePlugins
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-plugins-empty")
	a.setPluginTab("store")
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-store-empty")

	// The launcher with two installed apps, and one app's own settings page.
	a = newTestApp(t)
	installExamplePlugin(t, a, "gas")
	installExamplePlugin(t, a, "fear-greed")
	a.route = routePlugins
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-apps-launcher")
	a.openPluginDetail("com.example.gas")
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-apps-detail")

	a = newTestApp(t)
	a.passkeyKind = passkey.KindTouchID
	a.authenticate = func(string) error { return nil }
	a.route = routeSettings
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-settings-light")
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	tt.SetDark(true)
	save(t, tt, "new-settings-dark")
	a.settings.AgentEnabled = true
	a.agentAddr = "127.0.0.1:8765"
	a.agentToken = "Zk3n0Qe8Vt7bY2mR5cLx9WpA4sD6fGhJ1uKiO0aXeQw"
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-settings-agent")

	a = intentsTestApp(t)
	a.route = routeIntents
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-intents-light")
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	tt.SetDark(true)
	save(t, tt, "new-intents-dark")
	a.intentsQuote = &intents.Quote{
		AmountIn:      "1000000000000000000000000",
		AmountInText:  "1",
		AmountOut:     "10000000",
		AmountOutText: "10",
		MinAmountOut:  "9900000",
		Deadline:      time.Date(2026, 11, 9, 20, 30, 0, 0, time.UTC),
		CorrelationID: "quote-1",
		Request: intents.QuoteRequest{
			OriginAsset:      testOriginAsset,
			DestinationAsset: testDestAsset,
			RefundTo:         "alice.testnet",
			Recipient:        testRecipient,
		},
	}
	a.intentsNotice = i18n.T(i18n.English, i18n.IntentsAccepted)
	save(t, ui.NewTester(a.view, windowWidth, windowHeight), "new-intents-quote")

	origin, _ := a.intentsToken(testOriginAsset)
	destination, _ := a.intentsToken(testDestAsset)
	apIntents := &intentsApproval{
		t: func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		req: intentsApprovalRequest{
			quote:       *a.intentsQuote,
			origin:      origin,
			destination: destination,
			refundTo:    "alice.testnet",
			recipient:   testRecipient,
		},
	}
	save(t, ui.NewTester(apIntents.view, 480, 620), "new-intents-approval")

	// The asset picker open on a longer list, as 1Click's is.
	a = intentsTestApp(t)
	a.intentsTokens = append(a.intentsTokens,
		intents.Token{AssetID: "nep141:eth.omft.near", Symbol: "ETH", Blockchain: "eth", Decimals: 18},
		intents.Token{AssetID: "nep141:usdt.tether-token.near", Symbol: "USDT", Blockchain: "near", Decimals: 6},
		intents.Token{AssetID: "nep141:sol.omft.near", Symbol: "SOL", Blockchain: "sol", Decimals: 9},
		intents.Token{AssetID: "nep141:btc.omft.near", Symbol: "BTC", Blockchain: "btc", Decimals: 8},
	)
	a.route = routeIntents
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Pay with"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	save(t, tt, "new-intents-picker")

	a = newTestApp(t)
	a.passkeyKind = passkey.KindTouchID
	ap := approvalFor(&evmtx.Summary{
		From:  common.HexToAddress(walletTestAddr),
		To:    common.HexToAddress("0x27b1fdb04752bbc536007a920d24acb045561c26"),
		Value: big.NewInt(0),
		Data:  []byte{1, 2, 3, 4},
		Gas:   21000,
		Fee:   big.NewInt(63_000_000_000_000),
		Call:  "Transfer 1,200 USDC to 0x27b1…1c26",
	})
	ap.authRequired = true
	ap.authName = "Touch ID"
	ap.authenticate = a.authenticate
	save(t, ui.NewTester(ap.view, 460, 600), "new-approval-light")
	apTT := ui.NewTester(ap.view, 460, 600)
	apTT.SetDark(true)
	save(t, apTT, "new-approval-dark")

	ip := &installPrompt{
		t: func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		m: plugin.Manifest{
			ID: "com.example.signer", Name: "Signer", Version: "1.0.0", Author: "Ada",
			Description: "Signs what you ask it to.",
			Permissions: plugin.Permissions{
				Network: []string{"api.example.com"}, Chains: []string{"eip155:1"},
				Read: []string{"balances"}, Sign: true, StorageKb: 64,
			},
		},
		req: installRequest{source: "https://example.com/registry.json"},
	}
	save(t, ui.NewTester(ip.view, 460, 620), "new-install-prompt")
}
