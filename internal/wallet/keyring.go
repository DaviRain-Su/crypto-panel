package wallet

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"strings"
	"sync"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// The wallet holds EVM keys. A key is never in the app's settings: it lives
// in the OS keychain, under the account's address, and is read into memory
// only while the account is unlocked.
//
// A keyring is safe from any goroutine: the wallet page changes it on the
// main thread while a plugin's signing call reads it on its own.

// ErrInvalidKey reports a string that is not a 32-byte private key.
var ErrInvalidKey = errors.New("not a valid private key")

// ErrDuplicateKey reports a key whose account is already in the wallet.
var ErrDuplicateKey = errors.New("that account is already in the wallet")

// ErrNoAccount reports an address the wallet does not hold.
var ErrNoAccount = errors.New("that account is not in the wallet")

// SecretStore keeps the wallet's keys outside the app's settings: the OS
// keychain in the app, a map in tests.
type SecretStore interface {
	Set(key string, value []byte) error
	Get(key string) ([]byte, error)
	Delete(key string) error
}

// Account is one account of the wallet. Its key is not here: it is in the
// SecretStore under keySecret(Addr) while the account exists.
type Account struct {
	Addr  string `json:"address"`
	Label string `json:"label,omitempty"`
}

// Keyring holds the wallet's accounts and, while unlocked, their keys.
type Keyring struct {
	// mu guards accounts and keys. The store is called without it, because
	// the keychain may wait on the user.
	mu       sync.RWMutex
	store    SecretStore
	accounts []Account
	keys     map[string]*ecdsa.PrivateKey
}

// NewKeyring returns a keyring over store, holding accounts. Accounts that
// are not valid EVM addresses are dropped.
func NewKeyring(store SecretStore, accounts []Account) *Keyring {
	k := &Keyring{store: store, keys: map[string]*ecdsa.PrivateKey{}}
	for _, acc := range accounts {
		addr, err := Normalize(EVM, acc.Addr)
		if err != nil {
			continue
		}
		acc.Addr = addr
		k.accounts = append(k.accounts, acc)
	}
	return k
}

// Accounts returns the wallet's accounts, in the order they were added.
func (k *Keyring) Accounts() []Account {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return append([]Account(nil), k.accounts...)
}

// Has reports whether addr is an account of the wallet.
func (k *Keyring) Has(addr string) bool {
	addr, err := Normalize(EVM, addr)
	if err != nil {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.hasLocked(addr)
}

// hasLocked is Has for a normalized address; the caller holds mu.
func (k *Keyring) hasLocked(addr string) bool {
	for _, acc := range k.accounts {
		if acc.Addr == addr {
			return true
		}
	}
	return false
}

// Unlocked reports whether the key of addr is in memory.
func (k *Keyring) Unlocked(addr string) bool {
	addr, err := Normalize(EVM, addr)
	if err != nil {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.keys[addr]
	return ok
}

// Create makes a new account with a fresh key and returns it unlocked.
func (k *Keyring) Create(label string) (Account, error) {
	key, err := ethcrypto.GenerateKey()
	if err != nil {
		return Account{}, fmt.Errorf("generating a key: %w", err)
	}
	return k.add(key, label)
}

// Import adds the account of a hex private key and returns it unlocked. The
// key may carry a 0x prefix.
func (k *Keyring) Import(hexKey, label string) (Account, error) {
	raw := strings.TrimSpace(hexKey)
	raw = strings.TrimPrefix(strings.TrimPrefix(raw, "0x"), "0X")
	key, err := ethcrypto.HexToECDSA(raw)
	if err != nil {
		return Account{}, ErrInvalidKey
	}
	return k.add(key, label)
}

func (k *Keyring) add(key *ecdsa.PrivateKey, label string) (Account, error) {
	addr := strings.ToLower(ethcrypto.PubkeyToAddress(key.PublicKey).Hex())
	k.mu.RLock()
	duplicate := k.hasLocked(addr)
	k.mu.RUnlock()
	if duplicate {
		return Account{}, ErrDuplicateKey
	}
	// The store is written before the lock is taken: the keychain may ask
	// the user, and the wallet page must stay drawable meanwhile.
	if err := k.store.Set(keySecret(addr), ethcrypto.FromECDSA(key)); err != nil {
		return Account{}, fmt.Errorf("saving the key: %w", err)
	}
	acc := Account{Addr: addr, Label: strings.TrimSpace(label)}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.hasLocked(addr) {
		// Another goroutine added the same account while the store wrote.
		return Account{}, ErrDuplicateKey
	}
	k.accounts = append(k.accounts, acc)
	k.keys[addr] = key
	return acc, nil
}

// Remove deletes the account and its key. The account leaves memory first,
// so no signing call can read the key after the removal; a store that
// refuses the delete puts the account back.
func (k *Keyring) Remove(addr string) error {
	addr, err := Normalize(EVM, addr)
	if err != nil {
		return err
	}
	k.mu.Lock()
	idx := -1
	for i, acc := range k.accounts {
		if acc.Addr == addr {
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
	if err := k.store.Delete(keySecret(addr)); err != nil {
		k.mu.Lock()
		k.accounts = append(k.accounts, removed)
		k.mu.Unlock()
		return fmt.Errorf("deleting the key: %w", err)
	}
	return nil
}

// Unlock reads addr's key from the store into memory.
func (k *Keyring) Unlock(addr string) error {
	addr, err := Normalize(EVM, addr)
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
	raw, err := k.store.Get(keySecret(addr))
	if err != nil {
		return fmt.Errorf("reading the key: %w", err)
	}
	key, err := ethcrypto.ToECDSA(raw)
	if err != nil {
		return fmt.Errorf("reading the key: %w", err)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[addr] = key
	return nil
}

// UnlockAll unlocks every account, stopping at the first that fails.
func (k *Keyring) UnlockAll() error {
	for _, acc := range k.Accounts() {
		if err := k.Unlock(acc.Addr); err != nil {
			return err
		}
	}
	return nil
}

// Lock forgets every key in memory.
func (k *Keyring) Lock() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = map[string]*ecdsa.PrivateKey{}
}

// LockAccount forgets addr's key, leaving the other accounts as they were.
func (k *Keyring) LockAccount(addr string) {
	if addr, err := Normalize(EVM, addr); err == nil {
		k.mu.Lock()
		defer k.mu.Unlock()
		delete(k.keys, addr)
	}
}

// Key returns addr's key while it is unlocked.
func (k *Keyring) Key(addr string) (*ecdsa.PrivateKey, bool) {
	addr, err := Normalize(EVM, addr)
	if err != nil {
		return nil, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	key, ok := k.keys[addr]
	return key, ok
}

func keySecret(addr string) string { return "key:" + strings.ToLower(addr) }
