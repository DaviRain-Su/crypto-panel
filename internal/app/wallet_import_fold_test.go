package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/passkey"
	"github.com/davirain-su/lintel/internal/wallet"
)

// exactText reports whether the last frame has a label whose whole text is s.
func exactText(tt *ui.Tester, s string) bool {
	for _, text := range tt.Texts() {
		if text == s {
			return true
		}
	}
	return false
}

func openWalletImport(t *testing.T, a *App) *ui.Tester {
	t.Helper()
	a.route = routeWallet
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Import a private key"); err != nil {
		t.Fatal(err)
	}
	if !a.walletImportOpen {
		t.Fatal("the import form did not open")
	}
	return tt
}

func assertImportDraftsClear(t *testing.T, a *App) {
	t.Helper()
	if a.draftKey != "" || a.draftWalletLabel != "" || a.draftNearKey != "" ||
		a.draftNearAccount != "" || a.draftNearLabel != "" || a.importErr != "" {
		t.Fatalf("drafts left behind: key=%q label=%q nearKey=%q nearAccount=%q nearLabel=%q importErr=%q",
			a.draftKey, a.draftWalletLabel, a.draftNearKey, a.draftNearAccount, a.draftNearLabel, a.importErr)
	}
}

func fillImportDrafts(a *App) {
	a.draftKey = "SECRET-KEY"
	a.draftWalletLabel = "SECRET-LABEL"
	a.draftNearKey = "ed25519:SECRET-NEAR"
	a.draftNearAccount = "alice.testnet"
	a.draftNearLabel = "SECRET-NEAR-LABEL"
	a.importErr = "That is not a valid private key."
	// A New-wallet failure shares the page with the form and must survive
	// folding and opening it.
	a.walletErr = "The system keychain refused: disk full"
}

func TestWalletImportFoldClearsDrafts(t *testing.T) {
	for _, id := range []string{chains.Ethereum.ID, chains.NearTestnet.ID, chains.SolanaMainnet.ID} {
		t.Run(id, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.NetworkID = id
			tt := openWalletImport(t, a)
			fillImportDrafts(a)
			// The form heading repeats the button's words, so fold and
			// reopen go through the button's handler.
			a.toggleWalletImport()
			tt = ui.NewTester(a.view, windowWidth, windowHeight)
			if a.walletImportOpen {
				t.Fatal("the toggle did not fold the form")
			}
			assertImportDraftsClear(t, a)
			a.toggleWalletImport()
			tt = ui.NewTester(a.view, windowWidth, windowHeight)
			if !a.walletImportOpen {
				t.Fatal("the toggle did not open the form again")
			}
			assertImportDraftsClear(t, a)
			if exactText(tt, "SECRET-LABEL") || exactText(tt, "alice.testnet") || tt.HasText("SECRET-KEY") || tt.HasText("SECRET-NEAR") {
				t.Fatalf("reopened form still shows the draft: %q", tt.Texts())
			}
			if tt.HasText("That is not a valid private key.") {
				t.Fatalf("the import error survived folding: %q", tt.Texts())
			}
			if a.walletErr != "The system keychain refused: disk full" || !tt.HasText(a.walletErr) {
				t.Fatalf("opening the form hid the wallet error %q in %q", a.walletErr, tt.Texts())
			}
		})
	}
}

func TestWalletImportPageSwitchClearsDrafts(t *testing.T) {
	for _, id := range []string{chains.Ethereum.ID, chains.NearTestnet.ID, chains.SolanaMainnet.ID} {
		t.Run(id, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.NetworkID = id
			a.route = routeWallet
			a.walletImportOpen = true
			fillImportDrafts(a)
			a.goTo(routePortfolio)
			if a.walletImportOpen {
				t.Fatal("leaving the wallet page left the import form open")
			}
			assertImportDraftsClear(t, a)
			a.goTo(routeWallet)
			tt := ui.NewTester(a.view, windowWidth, windowHeight)
			if err := tt.Click("Import a private key"); err != nil {
				t.Fatal(err)
			}
			assertImportDraftsClear(t, a)
			if exactText(tt, "SECRET-LABEL") || tt.HasText("SECRET-KEY") || tt.HasText("SECRET-NEAR") {
				t.Fatalf("the form still holds the draft after a page switch: %q", tt.Texts())
			}
		})
	}
}

func TestWalletImportEmptyEnterKeepsTheError(t *testing.T) {
	cases := []struct {
		id    string
		label string
		drop  func(*App)
	}{
		{chains.Ethereum.ID, "Private key", func(a *App) { a.keyring = nil }},
		{chains.NearTestnet.ID, "ed25519 private key", func(a *App) { a.nearKeyring = nil }},
		{chains.SolanaMainnet.ID, "Solana secret key", func(a *App) { a.solanaKeyring = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.NetworkID = tc.id
			tc.drop(a)
			tt := openWalletImport(t, a)
			if err := tt.Click(tc.label); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyEnter)
			if !a.walletImportOpen {
				t.Fatal("an empty submit that failed folded the form")
			}
			if !tt.HasText("The wallet is unavailable.") {
				t.Fatalf("the error is not on screen: %q", tt.Texts())
			}
		})
	}
}

func TestWalletImportFailureStaysOpen(t *testing.T) {
	for _, id := range []string{chains.Ethereum.ID, chains.NearTestnet.ID, chains.SolanaMainnet.ID} {
		t.Run(id, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.NetworkID = id
			tt := openWalletImport(t, a)
			a.draftKey = "not-a-real-key"
			a.walletImportOpen = true
			tt = ui.NewTester(a.view, windowWidth, windowHeight)
			if err := tt.Click("Import"); err != nil {
				t.Fatal(err)
			}
			if !a.walletImportOpen {
				t.Fatal("a failed import folded the form")
			}
			if !tt.HasText("That is not a valid private key.") {
				t.Fatalf("the error is not on screen: %q", tt.Texts())
			}
			if strings.Contains(a.importErr, "not-a-real-key") || tt.HasText("not-a-real-key") {
				t.Fatal("the rejected key is in the error")
			}
		})
	}
}

func TestWalletImportSuccessFolds(t *testing.T) {
	t.Run("enter", func(t *testing.T) {
		a := newTestApp(t)
		tt := openWalletImport(t, a)
		if err := tt.Click("Private key"); err != nil {
			t.Fatal(err)
		}
		tt.Type(walletTestKey)
		tt.Key(0, ui.KeyEnter)
		if len(a.settings.Wallets) != 1 || a.settings.Wallets[0].EVM == nil {
			t.Fatalf("wallets = %+v", a.settings.Wallets)
		}
		if a.walletImportOpen || exactText(tt, "Private key") {
			t.Fatalf("Enter on a successful import left the form open (open=%v)", a.walletImportOpen)
		}
		if a.draftKey != "" {
			t.Fatalf("draft key = %q", a.draftKey)
		}
	})
	t.Run("button", func(t *testing.T) {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		a := newTestApp(t)
		a.settings.NetworkID = chains.SolanaMainnet.ID
		tt := openWalletImport(t, a)
		if err := tt.Click("Solana secret key"); err != nil {
			t.Fatal(err)
		}
		tt.Type(wallet.SolanaSecretString(key))
		if err := tt.Click("Import"); err != nil {
			t.Fatal(err)
		}
		if len(a.settings.Wallets) != 1 || a.settings.Wallets[0].Solana == nil {
			t.Fatalf("wallets = %+v", a.settings.Wallets)
		}
		if a.walletImportOpen || exactText(tt, "Solana secret key") {
			t.Fatalf("the Import button left the form open (open=%v)", a.walletImportOpen)
		}
	})
	t.Run("near", func(t *testing.T) {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		a := newTestApp(t)
		a.settings.NetworkID = chains.NearTestnet.ID
		tt := openWalletImport(t, a)
		if err := tt.Click("ed25519 private key"); err != nil {
			t.Fatal(err)
		}
		tt.Type(neartx.SecretKeyString(key))
		tt.Key(0, ui.KeyEnter)
		if len(a.settings.Wallets) != 1 || a.settings.Wallets[0].Near == nil {
			t.Fatalf("wallets = %+v", a.settings.Wallets)
		}
		if a.walletImportOpen || a.draftKey != "" || a.draftNearKey != "" {
			t.Fatalf("NEAR import left the form open=%v key=%q near=%q", a.walletImportOpen, a.draftKey, a.draftNearKey)
		}
	})
}

// refuseStore is a keychain that refuses every write.
type refuseStore struct{ memStore }

func (refuseStore) Set(string, []byte) error { return errors.New("disk full") }

func TestNewWalletErrorStaysVisibleWhileTheFormIsFolded(t *testing.T) {
	families := []struct {
		id   string
		fail string
	}{
		{chains.Ethereum.ID, "evm"},
		{chains.NearTestnet.ID, "near"},
		{chains.SolanaMainnet.ID, "solana"},
	}
	for _, tc := range families {
		t.Run(tc.id, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.NetworkID = tc.id
			a.route = routeWallet
			store := func(name string) wallet.SecretStore {
				if name == tc.fail {
					return refuseStore{}
				}
				return memStore{}
			}
			a.keyring = wallet.NewKeyring(store("evm"), nil)
			a.nearKeyring = wallet.NewNearKeyring(store("near"), nil)
			a.solanaKeyring = wallet.NewSolanaKeyring(store("solana"), nil)
			tt := ui.NewTester(a.view, windowWidth, windowHeight)
			if a.walletImportOpen {
				t.Fatal("the import form started open")
			}
			if err := tt.Click("New wallet"); err != nil {
				t.Fatal(err)
			}
			if a.walletImportOpen {
				t.Fatal("a New wallet failure opened the import form")
			}
			if a.walletErr == "" || !tt.HasText(a.walletErr) {
				t.Fatalf("onScreen=false walletErr=%q texts=%q", a.walletErr, tt.Texts())
			}
			kept := a.walletErr
			if err := tt.Click("Import a private key"); err != nil {
				t.Fatal(err)
			}
			if !a.walletImportOpen {
				t.Fatal("the import form did not open")
			}
			if a.walletErr != kept || !tt.HasText(kept) {
				t.Fatalf("opening the form hid the error: walletErr=%q onScreen=%v", a.walletErr, tt.HasText(kept))
			}
		})
	}
}

func TestRevealCancelStaysVisibleWhileTheFormIsFolded(t *testing.T) {
	a := newTestApp(t)
	a.route = routeWallet
	a.createWalletAccount()
	w := a.settings.Wallets[0]
	a.passkeyKind = passkey.KindTouchID
	a.authenticate = func(string) error { return passkey.ErrCanceled }
	cases := []struct {
		id     string
		reveal func()
	}{
		{chains.Ethereum.ID, func() { a.revealKey(w.EVM.Addr) }},
		{chains.NearTestnet.ID, func() { a.revealNearKey(w.Near.AccountID) }},
		{chains.SolanaMainnet.ID, func() { a.revealSolanaKey(w.Solana.Address) }},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			a.settings.NetworkID = tc.id
			a.walletErr, a.nearErr = "", ""
			a.walletImportOpen = false
			tc.reveal()
			tt := ui.NewTester(a.view, windowWidth, windowHeight)
			if a.walletImportOpen {
				t.Fatal("reveal opened the import form")
			}
			if !tt.HasText("Authentication canceled.") {
				t.Fatalf("onScreen=false walletErr=%q nearErr=%q texts=%q", a.walletErr, a.nearErr, tt.Texts())
			}
			a.toggleWalletImport()
			tt = ui.NewTester(a.view, windowWidth, windowHeight)
			if !a.walletImportOpen || !tt.HasText("Authentication canceled.") {
				t.Fatal("opening the import form hid the canceled authentication")
			}
		})
	}
}

func TestRouteChangeClearsTheKeyDraft(t *testing.T) {
	for _, id := range []string{chains.Ethereum.ID, chains.NearTestnet.ID, chains.SolanaMainnet.ID} {
		t.Run(id, func(t *testing.T) {
			leave := []struct {
				name string
				fn   func(*App)
			}{
				{"detail", func(a *App) { a.openPluginDetail("com.example.gas") }},
				{"page", func(a *App) { a.openPluginPage("com.example.gas", "overview") }},
				{"creator", func(a *App) { a.openCreator("") }},
			}
			for _, step := range leave {
				t.Run(step.name, func(t *testing.T) {
					a := newTestApp(t)
					a.settings.NetworkID = id
					a.route = routeWallet
					a.walletImportOpen = true
					a.draftKey = "0x1111111111111111111111111111111111111111111111111111111111111111"
					a.draftWalletLabel = "lbl"
					a.draftNearKey = "ed25519:secret"
					step.fn(a)
					if a.draftKey != "" || a.draftNearKey != "" || a.draftWalletLabel != "" || a.walletImportOpen {
						t.Fatalf("draftStillHasKey key=%q near=%q label=%q open=%v", a.draftKey, a.draftNearKey, a.draftWalletLabel, a.walletImportOpen)
					}
					a.goTo(routeWallet)
					tt := ui.NewTester(a.view, windowWidth, windowHeight)
					if err := tt.Click("Import a private key"); err != nil {
						t.Fatal(err)
					}
					if a.draftKey != "" || tt.HasText("0x1111") {
						t.Fatalf("the key is back in the form: %q", a.draftKey)
					}
				})
			}
		})
	}
}

func TestOnlyGoToAssignsTheRoute(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, "a.route =") && !strings.Contains(line, "a.route ==") && !strings.Contains(line, "a.route !=") && !strings.Contains(line, "a.route = r") {
				t.Errorf("%s:%d assigns the route outside goTo: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
