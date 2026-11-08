package wallet

import (
	"errors"
	"strings"
	"testing"
)

// mapStore is a SecretStore in memory.
type mapStore map[string][]byte

func (m mapStore) Set(key string, value []byte) error {
	m[key] = append([]byte(nil), value...)
	return nil
}

func (m mapStore) Get(key string) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, errors.New("no such secret")
	}
	return v, nil
}

func (m mapStore) Delete(key string) error {
	delete(m, key)
	return nil
}

// The key and address of the web3.js documentation's example account.
const (
	testKey  = "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	testAddr = "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23"
)

func TestImportDerivesTheAddress(t *testing.T) {
	store := mapStore{}
	k := NewKeyring(store, nil)
	acc, err := k.Import(testKey, "Main")
	if err != nil {
		t.Fatal(err)
	}
	if acc.Addr != testAddr {
		t.Fatalf("address = %q, want %q", acc.Addr, testAddr)
	}
	if acc.Label != "Main" {
		t.Fatalf("label = %q", acc.Label)
	}
	if !k.Unlocked(acc.Addr) {
		t.Fatal("an imported account is not unlocked")
	}
	if _, ok := store[keySecret(testAddr)]; !ok {
		t.Fatalf("the key is not in the store: %v", store)
	}
	if !k.Has(testAddr) {
		t.Fatal("Has is false for an account of the wallet")
	}
}

func TestImportAcceptsAPrefixedKey(t *testing.T) {
	k := NewKeyring(mapStore{}, nil)
	acc, err := k.Import("0x"+testKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if acc.Addr != testAddr {
		t.Fatalf("address = %q", acc.Addr)
	}
}

func TestImportRefusesAnotherKey(t *testing.T) {
	k := NewKeyring(mapStore{}, nil)
	if _, err := k.Import("not a key", ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("err = %v, want ErrInvalidKey", err)
	}
	if _, err := k.Import(strings.Repeat("11", 31), ""); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("a short key: err = %v, want ErrInvalidKey", err)
	}
}

func TestImportRefusesADuplicate(t *testing.T) {
	k := NewKeyring(mapStore{}, nil)
	if _, err := k.Import(testKey, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Import(testKey, ""); !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("err = %v, want ErrDuplicateKey", err)
	}
}

func TestCreateMakesANewAccount(t *testing.T) {
	store := mapStore{}
	k := NewKeyring(store, nil)
	acc, err := k.Create("New")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := KindOf(acc.Addr); !ok {
		t.Fatalf("address = %q", acc.Addr)
	}
	if !k.Unlocked(acc.Addr) {
		t.Fatal("a created account is not unlocked")
	}
	raw, ok := store[keySecret(acc.Addr)]
	if !ok || len(raw) != 32 {
		t.Fatalf("stored key = %x", raw)
	}
	other, err := k.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if other.Addr == acc.Addr {
		t.Fatal("two created accounts share an address")
	}
}

func TestLockForgetsTheKeys(t *testing.T) {
	store := mapStore{}
	k := NewKeyring(store, nil)
	acc, err := k.Import(testKey, "")
	if err != nil {
		t.Fatal(err)
	}
	k.Lock()
	if k.Unlocked(acc.Addr) {
		t.Fatal("locked, but the key is in memory")
	}
	if _, ok := k.Key(acc.Addr); ok {
		t.Fatal("Key returned a locked key")
	}
	if err := k.Unlock(acc.Addr); err != nil {
		t.Fatal(err)
	}
	if !k.Unlocked(acc.Addr) {
		t.Fatal("Unlock did not read the key")
	}
}

func TestLockAccountLeavesTheOthers(t *testing.T) {
	k := NewKeyring(mapStore{}, nil)
	first, err := k.Create("")
	if err != nil {
		t.Fatal(err)
	}
	second, err := k.Create("")
	if err != nil {
		t.Fatal(err)
	}
	k.LockAccount(first.Addr)
	if k.Unlocked(first.Addr) {
		t.Fatal("the locked account is still in memory")
	}
	if !k.Unlocked(second.Addr) {
		t.Fatal("locking one account locked another")
	}
}

func TestUnlockReadsTheStore(t *testing.T) {
	store := mapStore{}
	first := NewKeyring(store, nil)
	acc, err := first.Import(testKey, "Main")
	if err != nil {
		t.Fatal(err)
	}
	// A later run of the app starts with the account in the settings and
	// the key in the store.
	second := NewKeyring(store, first.Accounts())
	if second.Unlocked(acc.Addr) {
		t.Fatal("a new keyring starts unlocked")
	}
	if err := second.Unlock(acc.Addr); err != nil {
		t.Fatal(err)
	}
	if !second.Unlocked(acc.Addr) {
		t.Fatal("Unlock did not read the key")
	}
	if err := second.Unlock("0x27b1fdb04752bbc536007a920d24acb045561c26"); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("err = %v, want ErrNoAccount", err)
	}
}

func TestRemoveDeletesTheKey(t *testing.T) {
	store := mapStore{}
	k := NewKeyring(store, nil)
	acc, err := k.Import(testKey, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Remove(acc.Addr); err != nil {
		t.Fatal(err)
	}
	if k.Has(acc.Addr) {
		t.Fatal("the account is still in the wallet")
	}
	if _, ok := store[keySecret(acc.Addr)]; ok {
		t.Fatal("the key is still in the store")
	}
	if k.Unlocked(acc.Addr) {
		t.Fatal("the key is still in memory")
	}
	if err := k.Remove(acc.Addr); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("err = %v, want ErrNoAccount", err)
	}
}

func TestNewKeyringDropsBadAccounts(t *testing.T) {
	k := NewKeyring(mapStore{}, []Account{
		{Addr: "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23"},
		{Addr: "nonsense"},
	})
	if len(k.Accounts()) != 1 || k.Accounts()[0].Addr != testAddr {
		t.Fatalf("accounts = %+v", k.Accounts())
	}
}
