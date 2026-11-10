package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/neartx"
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
		a.draftNearAccount != "" || a.draftNearLabel != "" || a.walletErr != "" || a.nearErr != "" {
		t.Fatalf("drafts left behind: key=%q label=%q nearKey=%q nearAccount=%q nearLabel=%q walletErr=%q nearErr=%q",
			a.draftKey, a.draftWalletLabel, a.draftNearKey, a.draftNearAccount, a.draftNearLabel, a.walletErr, a.nearErr)
	}
}

func fillImportDrafts(a *App) {
	a.draftKey = "SECRET-KEY"
	a.draftWalletLabel = "SECRET-LABEL"
	a.draftNearKey = "ed25519:SECRET-NEAR"
	a.draftNearAccount = "alice.testnet"
	a.draftNearLabel = "SECRET-NEAR-LABEL"
	a.walletErr = "The wallet is unavailable."
	a.nearErr = "That is not a valid private key."
}

func TestWalletImportFoldClearsDrafts(t *testing.T) {
	for _, id := range []string{chains.Ethereum.ID, chains.NearTestnet.ID, chains.SolanaMainnet.ID} {
		t.Run(id, func(t *testing.T) {
			a := newTestApp(t)
			a.settings.NetworkID = id
			tt := openWalletImport(t, a)
			fillImportDrafts(a)
			// Closing the toggle drops the drafts. A second click opens an
			// empty form, so the toggle both opens and closes.
			if err := tt.Click("Import a private key"); err != nil {
				t.Fatal(err)
			}
			if a.walletImportOpen {
				t.Fatal("the toggle did not fold the form")
			}
			assertImportDraftsClear(t, a)
			if err := tt.Click("Import a private key"); err != nil {
				t.Fatal(err)
			}
			if !a.walletImportOpen {
				t.Fatal("the toggle did not open the form again")
			}
			assertImportDraftsClear(t, a)
			if exactText(tt, "SECRET-LABEL") || exactText(tt, "alice.testnet") || tt.HasText("SECRET-KEY") || tt.HasText("SECRET-NEAR") {
				t.Fatalf("reopened form still shows the draft: %q", tt.Texts())
			}
			if tt.HasText("The wallet is unavailable.") || tt.HasText("That is not a valid private key.") {
				t.Fatalf("reopened form still shows the old error: %q", tt.Texts())
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
			if strings.Contains(a.walletErr, "not-a-real-key") || strings.Contains(a.nearErr, "not-a-real-key") || tt.HasText("not-a-real-key") {
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
