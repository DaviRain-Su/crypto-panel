package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/neartx"
)

func TestNearWalletCreate(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	if len(a.settings.NearWallet) != 1 {
		t.Fatalf("near accounts = %+v", a.settings.NearWallet)
	}
	acc := a.settings.NearWallet[0]
	if len(acc.AccountID) != 64 {
		t.Fatalf("account id = %q", acc.AccountID)
	}
	if _, err := hex.DecodeString(acc.AccountID); err != nil {
		t.Fatalf("account id is not hex: %v", err)
	}
	if a.newKey == "" || a.newKeyAddr != acc.AccountID {
		t.Fatalf("the new key is not shown: %q %q", a.newKey, a.newKeyAddr)
	}
	if !strings.HasPrefix(a.newKey, "ed25519:") {
		t.Fatalf("new key = %q", a.newKey)
	}
	if !a.nearKeyring.Unlocked(acc.AccountID) {
		t.Fatal("a created NEAR account is not unlocked")
	}
	// The key is in the keychain, not in settings.json.
	data, err := os.ReadFile(a.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), a.newKey) {
		t.Fatal("settings.json holds the private key")
	}
	var saved Settings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.NearWallet) != 1 || saved.NearWallet[0].AccountID != acc.AccountID {
		t.Fatalf("saved near accounts = %+v", saved.NearWallet)
	}
}

func TestNearWalletImport(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	implicit := hex.EncodeToString(key.Public().(ed25519.PublicKey))

	a := newTestApp(t)
	a.draftNearKey = neartx.SecretKeyString(key)
	a.draftNearAccount = "alice.testnet"
	a.draftNearLabel = "Alice"
	a.importNearAccount()
	if len(a.settings.NearWallet) != 1 {
		t.Fatalf("near accounts = %+v", a.settings.NearWallet)
	}
	acc := a.settings.NearWallet[0]
	if acc.AccountID != "alice.testnet" || acc.Label != "Alice" {
		t.Fatalf("account = %+v", acc)
	}
	if acc.PublicKey != neartx.PublicKeyOf(key).String() {
		t.Fatalf("public key = %q", acc.PublicKey)
	}
	if a.draftNearKey != "" || a.draftNearAccount != "" || a.draftNearLabel != "" {
		t.Fatal("the form was not cleared")
	}

	// The same key under its implicit account is a second account, and the
	// same account twice is refused.
	a.draftNearKey = neartx.SecretKeyString(key)
	a.importNearAccount()
	if len(a.settings.NearWallet) != 2 || a.settings.NearWallet[1].AccountID != implicit {
		t.Fatalf("near accounts = %+v", a.settings.NearWallet)
	}
	a.draftNearKey = neartx.SecretKeyString(key)
	a.draftNearAccount = "alice.testnet"
	a.importNearAccount()
	if a.nearErr == "" {
		t.Fatal("a duplicate import was accepted")
	}
	if len(a.settings.NearWallet) != 2 {
		t.Fatalf("near accounts = %+v", a.settings.NearWallet)
	}
}

func TestNearWalletRejectsABadKey(t *testing.T) {
	a := newTestApp(t)
	a.draftNearKey = "not a key"
	a.importNearAccount()
	if a.nearErr == "" {
		t.Fatal("an invalid key was accepted")
	}
	if len(a.settings.NearWallet) != 0 {
		t.Fatalf("near accounts = %+v", a.settings.NearWallet)
	}
}

func TestNearWalletLockUnlockRemove(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	acc := a.settings.NearWallet[0]
	a.lockNearAccount(acc.AccountID)
	if a.nearKeyring.Unlocked(acc.AccountID) {
		t.Fatal("lockNearAccount left the key in memory")
	}
	a.finishNearUnlock(acc.AccountID)
	if !a.nearKeyring.Unlocked(acc.AccountID) {
		t.Fatal("the key was not unlocked")
	}
	a.finishNearReveal(acc.AccountID)
	if a.revealedAddr != acc.AccountID || !strings.HasPrefix(a.revealedKey, "ed25519:") {
		t.Fatalf("revealed = %q for %q", a.revealedKey, a.revealedAddr)
	}
	a.removeNearAccount(acc.AccountID)
	if len(a.settings.NearWallet) != 0 {
		t.Fatalf("near accounts = %+v", a.settings.NearWallet)
	}
	if a.revealedKey != "" || a.newKey != "" {
		t.Fatal("a removed account's key is still on screen")
	}
}

func TestNearWalletPageShowsAccounts(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	acc := a.settings.NearWallet[0]
	a.newKey, a.newKeyAddr = "", ""
	a.settings.NetworkID = "near:testnet"
	a.route = routeWallet
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		acc.AccountID, acc.PublicKey, "Unlocked",
		"ed25519",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	// The import form stays folded until the toggle opens it.
	if tt.HasText("Account ID (optional)") {
		t.Errorf("the import form is open before it was asked for: %q", tt.Texts())
	}
	if err := tt.Click("Import a private key"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Account ID (optional)") {
		t.Errorf("the import form did not open: %q", tt.Texts())
	}
	if tt.HasText("EVM accounts") || tt.HasText("NEAR accounts") {
		t.Fatalf("the page is still split by chain section: %q", tt.Texts())
	}
}

func TestNearWalletIsWatchedOnNear(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = "near:testnet"
	a.settings = a.settings.Validate()
	a.createNearAccount()
	acc := a.settings.NearWallet[0]
	watched := a.settings.WatchedOf(chain.Near)
	if len(watched) != 1 || watched[0].Addr != acc.AccountID {
		t.Fatalf("watched on NEAR = %+v", watched)
	}
	if got := a.settings.WatchedOf(chain.EVM); len(got) != 0 {
		t.Fatalf("watched on EVM = %+v", got)
	}
}
