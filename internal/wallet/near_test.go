package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/davirain-su/crypto-panel/internal/neartx"
)

func TestNearCreate(t *testing.T) {
	store := mapStore{}
	k := NewNearKeyring(store, nil)
	acc, err := k.Create("Testnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(acc.AccountID) != 64 {
		t.Fatalf("account id = %q", acc.AccountID)
	}
	if _, err := hex.DecodeString(acc.AccountID); err != nil {
		t.Fatalf("account id is not hex: %v", err)
	}
	pub, err := neartx.ParsePublicKey(acc.PublicKey)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	if hex.EncodeToString(pub.Bytes[:]) != acc.AccountID {
		t.Fatal("the account id is not the public key")
	}
	if !k.Unlocked(acc.AccountID) || !k.Has(acc.AccountID) {
		t.Fatal("a created account is not unlocked")
	}
	if _, ok := store[nearKeySecret(acc.AccountID)]; !ok {
		t.Fatal("the key is not in the store")
	}
	if acc.Label != "Testnet" {
		t.Fatalf("label = %q", acc.Label)
	}
}

func TestNearImport(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := neartx.SecretKeyString(key)
	implicit := hex.EncodeToString(key.Public().(ed25519.PublicKey))

	store := mapStore{}
	k := NewNearKeyring(store, nil)
	acc, err := k.Import(secret, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if acc.AccountID != implicit {
		t.Fatalf("account id = %q, want %q", acc.AccountID, implicit)
	}
	if !k.Unlocked(acc.AccountID) {
		t.Fatal("an imported account is not unlocked")
	}
	if _, err := k.Import(secret, "", ""); err != ErrDuplicateKey {
		t.Fatalf("duplicate import: %v", err)
	}

	// A named account is named by the user, not derived from the key.
	named, err := k.Import(secret, "alice.testnet", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if named.AccountID != "alice.testnet" {
		t.Fatalf("named account = %q", named.AccountID)
	}
	if _, err := k.Import("not a key", "", ""); err != ErrInvalidKey {
		t.Fatalf("invalid key: %v", err)
	}
	if _, err := k.Import(secret, "Alice", ""); err != ErrInvalidAddress {
		t.Fatalf("invalid account: %v", err)
	}
}

func TestNearLockUnlockReveal(t *testing.T) {
	store := mapStore{}
	k := NewNearKeyring(store, nil)
	acc, err := k.Create("")
	if err != nil {
		t.Fatal(err)
	}
	key, ok := k.Key(acc.AccountID)
	if !ok {
		t.Fatal("the key is not in memory")
	}
	k.Lock()
	if k.Unlocked(acc.AccountID) {
		t.Fatal("Lock left the key in memory")
	}
	if err := k.Unlock(acc.AccountID); err != nil {
		t.Fatal(err)
	}
	again, ok := k.Key(acc.AccountID)
	if !ok || string(again) != string(key) {
		t.Fatal("Unlock read another key")
	}
	revealed, err := k.Reveal(acc.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(revealed, "ed25519:") {
		t.Fatalf("revealed = %q", revealed)
	}
	parsed, err := neartx.ParseSecretKey(revealed)
	if err != nil || string(parsed) != string(key) {
		t.Fatalf("the revealed key does not parse back: %v", err)
	}
	k.LockAccount(acc.AccountID)
	if k.Unlocked(acc.AccountID) {
		t.Fatal("LockAccount left the key in memory")
	}
}

func TestNearRemove(t *testing.T) {
	store := mapStore{}
	k := NewNearKeyring(store, nil)
	acc, err := k.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Remove(acc.AccountID); err != nil {
		t.Fatal(err)
	}
	if k.Has(acc.AccountID) || k.Unlocked(acc.AccountID) {
		t.Fatal("the account is still in the keyring")
	}
	if _, ok := store[nearKeySecret(acc.AccountID)]; ok {
		t.Fatal("the key is still in the store")
	}
	if err := k.Remove(acc.AccountID); err != ErrNoAccount {
		t.Fatalf("second remove: %v", err)
	}
}

func TestNearKeyringDropsInvalidAccounts(t *testing.T) {
	k := NewNearKeyring(mapStore{}, []NearAccount{
		{AccountID: "alice.testnet", PublicKey: "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq"},
		{AccountID: "Alice", PublicKey: "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq"},
		{AccountID: "bob.testnet", PublicKey: "nope"},
	})
	if got := k.Accounts(); len(got) != 1 || got[0].AccountID != "alice.testnet" {
		t.Fatalf("accounts = %+v", got)
	}
}

func TestNearAddressKind(t *testing.T) {
	for _, s := range []string{
		"alice.testnet",
		"acfb74d1570268f22df01eb6c3f38d57c1915d3be92afb30210eb4bc2d004220",
	} {
		if kind, ok := KindOf(s); !ok || kind != Near {
			t.Errorf("KindOf(%q) = %q, %v", s, kind, ok)
		}
		got, err := Normalize(Near, s)
		if err != nil || got != s {
			t.Errorf("Normalize(%q) = %q, %v", s, got, err)
		}
	}
	for _, s := range []string{"Alice.testnet", ".alice.testnet", "alice..testnet"} {
		if _, err := Normalize(Near, s); err == nil {
			t.Errorf("Normalize(%q) accepted it", s)
		}
	}
}
