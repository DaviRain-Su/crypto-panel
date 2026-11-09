package wallet

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/davirain-su/crypto-panel/internal/base58"
)

// SolanaKeyring holds the wallet's Solana accounts and, while unlocked, their
// ed25519 keys. A key is stored under solana:key:<address>, never in settings.
// It is safe from any goroutine, as the EVM keyring is.
type SolanaKeyring struct {
	mu       sync.RWMutex
	store    SecretStore
	accounts []SolanaAccount
	keys     map[string]ed25519.PrivateKey
}

// NewSolanaKeyring returns a keyring over store, holding accounts. Accounts
// that are not valid Solana addresses are dropped.
func NewSolanaKeyring(store SecretStore, accounts []SolanaAccount) *SolanaKeyring {
	k := &SolanaKeyring{store: store, keys: map[string]ed25519.PrivateKey{}}
	for _, acc := range accounts {
		addr, err := Normalize(Solana, acc.Address)
		if err != nil {
			continue
		}
		acc.Address = addr
		k.accounts = append(k.accounts, acc)
	}
	return k
}

// Accounts returns the Solana accounts, in the order they were added.
func (k *SolanaKeyring) Accounts() []SolanaAccount {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return append([]SolanaAccount(nil), k.accounts...)
}

// Has reports whether addr is an account of the keyring.
func (k *SolanaKeyring) Has(addr string) bool {
	addr, err := Normalize(Solana, addr)
	if err != nil {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.hasLocked(addr)
}

func (k *SolanaKeyring) hasLocked(addr string) bool {
	for _, acc := range k.accounts {
		if acc.Address == addr {
			return true
		}
	}
	return false
}

// Unlocked reports whether the key of addr is in memory.
func (k *SolanaKeyring) Unlocked(addr string) bool {
	addr, err := Normalize(Solana, addr)
	if err != nil {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.keys[addr]
	return ok
}

// Create makes a new ed25519 account and returns it unlocked. The address is
// the base58 public key. The account can receive SOL once it is funded; this
// package does not send.
func (k *SolanaKeyring) Create(label string) (SolanaAccount, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return SolanaAccount{}, fmt.Errorf("generating a key: %w", err)
	}
	return k.add(key, label)
}

// Import adds the account of a Solana secret and returns it unlocked. The
// secret is what Phantom and solana-keygen export: base58 of the 64-byte
// keypair, or a JSON array of those 64 bytes. A 32-byte seed, base58 or as
// a JSON array, is accepted too.
func (k *SolanaKeyring) Import(secret, label string) (SolanaAccount, error) {
	key, err := ParseSolanaSecret(secret)
	if err != nil {
		return SolanaAccount{}, err
	}
	return k.add(key, label)
}

func (k *SolanaKeyring) add(key ed25519.PrivateKey, label string) (SolanaAccount, error) {
	pub := key.Public().(ed25519.PublicKey)
	addr := base58.Encode(pub)
	k.mu.RLock()
	duplicate := k.hasLocked(addr)
	k.mu.RUnlock()
	if duplicate {
		return SolanaAccount{}, ErrDuplicateKey
	}
	// The store is written before the lock is taken: the keychain may ask
	// the user, and the wallet page must stay drawable meanwhile.
	if err := k.store.Set(solanaKeySecret(addr), []byte(key)); err != nil {
		return SolanaAccount{}, fmt.Errorf("saving the key: %w", err)
	}
	acc := SolanaAccount{Address: addr, Label: strings.TrimSpace(label)}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.hasLocked(addr) {
		return SolanaAccount{}, ErrDuplicateKey
	}
	k.accounts = append(k.accounts, acc)
	k.keys[addr] = key
	return acc, nil
}

// Remove deletes the account and its key. The account leaves memory first,
// so no later read can use the key after the removal; a store that refuses
// the delete puts the account back.
func (k *SolanaKeyring) Remove(addr string) error {
	addr, err := Normalize(Solana, addr)
	if err != nil {
		return err
	}
	k.mu.Lock()
	idx := -1
	for i, acc := range k.accounts {
		if acc.Address == addr {
			idx = i
			break
		}
	}
	if idx < 0 {
		k.mu.Unlock()
		return ErrNoAccount
	}
	removed := k.accounts[idx]
	k.accounts = append(k.accounts[:idx], k.accounts[idx+1:]...)
	delete(k.keys, addr)
	k.mu.Unlock()
	if err := k.store.Delete(solanaKeySecret(addr)); err != nil {
		k.mu.Lock()
		k.accounts = append(k.accounts, removed)
		k.mu.Unlock()
		return fmt.Errorf("deleting the key: %w", err)
	}
	return nil
}

// Unlock reads addr's key from the store into memory.
func (k *SolanaKeyring) Unlock(addr string) error {
	addr, err := Normalize(Solana, addr)
	if err != nil {
		return err
	}
	k.mu.RLock()
	found := k.hasLocked(addr)
	_, unlocked := k.keys[addr]
	k.mu.RUnlock()
	if !found {
		return ErrNoAccount
	}
	if unlocked {
		return nil
	}
	raw, err := k.store.Get(solanaKeySecret(addr))
	if err != nil {
		return fmt.Errorf("reading the key: %w", err)
	}
	var key ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		key = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		key = ed25519.PrivateKey(append([]byte(nil), raw...))
	default:
		return errors.New("reading the key: unexpected key length")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[addr] = key
	return nil
}

// Lock forgets every key in memory.
func (k *SolanaKeyring) Lock() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = map[string]ed25519.PrivateKey{}
}

// LockAccount forgets addr's key, leaving the other accounts as they were.
func (k *SolanaKeyring) LockAccount(addr string) {
	if addr, err := Normalize(Solana, addr); err == nil {
		k.mu.Lock()
		defer k.mu.Unlock()
		delete(k.keys, addr)
	}
}

// Key returns addr's key while it is unlocked.
func (k *SolanaKeyring) Key(addr string) (ed25519.PrivateKey, bool) {
	addr, err := Normalize(Solana, addr)
	if err != nil {
		return nil, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	key, ok := k.keys[addr]
	if !ok {
		return nil, false
	}
	return append(ed25519.PrivateKey(nil), key...), true
}

func solanaKeySecret(addr string) string { return "solana:key:" + addr }

// SolanaSecretString is the secret Phantom and solana-keygen show: base58 of
// the 64-byte keypair, seed then public key.
func SolanaSecretString(key ed25519.PrivateKey) string {
	return base58.Encode(key)
}

// ParseSolanaSecret reads a Solana secret. Accepted forms are the base58
// 64-byte keypair, base58 of a 32-byte seed, and a JSON array of 64 or 32
// bytes (the file solana-keygen writes).
func ParseSolanaSecret(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ErrInvalidKey
	}
	if strings.HasPrefix(s, "[") {
		var nums []int
		if err := json.Unmarshal([]byte(s), &nums); err != nil {
			return nil, ErrInvalidKey
		}
		raw := make([]byte, len(nums))
		for i, n := range nums {
			if n < 0 || n > 255 {
				return nil, ErrInvalidKey
			}
			raw[i] = byte(n)
		}
		return solanaKeyFromBytes(raw)
	}
	raw, err := base58.Decode(s)
	if err != nil {
		return nil, ErrInvalidKey
	}
	return solanaKeyFromBytes(raw)
}

func solanaKeyFromBytes(raw []byte) (ed25519.PrivateKey, error) {
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		key := ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize])
		if !bytes.Equal(key.Public().(ed25519.PublicKey), raw[ed25519.SeedSize:]) {
			return nil, ErrInvalidKey
		}
		return key, nil
	default:
		return nil, ErrInvalidKey
	}
}
