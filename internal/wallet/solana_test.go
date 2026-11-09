package wallet

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"testing"
)

func TestSolanaKeyringCreateImportAndSecret(t *testing.T) {
	store := memStore{}
	k := NewSolanaKeyring(store, nil)
	acc, err := k.Create("Main")
	if err != nil {
		t.Fatal(err)
	}
	if !k.Unlocked(acc.Address) {
		t.Fatal("a created account is locked")
	}
	key, ok := k.Key(acc.Address)
	if !ok {
		t.Fatal("no key")
	}
	secret := SolanaSecretString(key)
	if _, err := ParseSolanaSecret(secret); err != nil {
		t.Fatal(err)
	}
	again := NewSolanaKeyring(store, []SolanaAccount{acc})
	if err := again.Unlock(acc.Address); err != nil {
		t.Fatal(err)
	}
	got, ok := again.Key(acc.Address)
	if !ok || !bytes.Equal(got, key) {
		t.Fatal("the keychain did not return the same key")
	}

	// A second import of the same key is refused. A JSON array of the 64
	// bytes, the form solana-keygen writes, imports as the same account.
	if _, err := k.Import(secret, ""); err != ErrDuplicateKey {
		t.Fatalf("duplicate err = %v", err)
	}
	nums := make([]int, len(key))
	for i, b := range key {
		nums[i] = int(b)
	}
	raw, err := json.Marshal(nums)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSolanaSecret(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parsed, key) {
		t.Fatal("the JSON array did not decode to the keypair")
	}
	if _, err := ParseSolanaSecret("not a key"); err != ErrInvalidKey {
		t.Fatalf("bad key err = %v", err)
	}
	pub := key.Public().(ed25519.PublicKey)
	if acc.Address == "" || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("address %q", acc.Address)
	}
}

// memStore is a SecretStore in memory. The app tests have their own copy.
type memStore map[string][]byte

func (m memStore) Set(key string, value []byte) error {
	m[key] = append([]byte(nil), value...)
	return nil
}

func (m memStore) Get(key string) ([]byte, error) {
	v, ok := m[key]
	if !ok {
		return nil, ErrNoAccount
	}
	return append([]byte(nil), v...), nil
}

func (m memStore) Delete(key string) error {
	delete(m, key)
	return nil
}
