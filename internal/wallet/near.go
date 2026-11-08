package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/davirain-su/crypto-panel/internal/neartx"
)

// NearAccount is one NEAR account of the wallet. Its key is not here: it is
// in the SecretStore under nearKeySecret(AccountID) while the account
// exists.
type NearAccount struct {
	AccountID string `json:"accountId"`
	PublicKey string `json:"publicKey"`
	Label     string `json:"label,omitempty"`
}

// NearKeyring holds the wallet's NEAR accounts and, while unlocked, their
// ed25519 keys. It is safe from any goroutine, as the EVM keyring is.
type NearKeyring struct {
	mu       sync.RWMutex
	store    SecretStore
	accounts []NearAccount
	keys     map[string]ed25519.PrivateKey
}

// NewNearKeyring returns a keyring over store, holding accounts. Accounts
// that are not valid NEAR accounts are dropped.
func NewNearKeyring(store SecretStore, accounts []NearAccount) *NearKeyring {
	k := &NearKeyring{store: store, keys: map[string]ed25519.PrivateKey{}}
	for _, acc := range accounts {
		accountID, err := neartx.ParseAccountID(acc.AccountID)
		if err != nil {
			continue
		}
		if _, err := neartx.ParsePublicKey(acc.PublicKey); err != nil {
			continue
		}
		acc.AccountID = accountID
		k.accounts = append(k.accounts, acc)
	}
	return k
}

// Accounts returns the wallet's NEAR accounts, in the order they were added.
func (k *NearKeyring) Accounts() []NearAccount {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return append([]NearAccount(nil), k.accounts...)
}

// Has reports whether accountID is an account of the wallet.
func (k *NearKeyring) Has(accountID string) bool {
	accountID, err := neartx.ParseAccountID(accountID)
	if err != nil {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.hasLocked(accountID)
}

func (k *NearKeyring) hasLocked(accountID string) bool {
	for _, acc := range k.accounts {
		if acc.AccountID == accountID {
			return true
		}
	}
	return false
}

// Unlocked reports whether the key of accountID is in memory.
func (k *NearKeyring) Unlocked(accountID string) bool {
	accountID, err := neartx.ParseAccountID(accountID)
	if err != nil {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	_, ok := k.keys[accountID]
	return ok
}

// Create makes a new implicit account with a fresh ed25519 key and returns
// it unlocked. The account id is the hex of its public key; the account
// exists on chain once it is funded.
func (k *NearKeyring) Create(label string) (NearAccount, error) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return NearAccount{}, fmt.Errorf("generating a key: %w", err)
	}
	accountID := hex.EncodeToString(public)
	return k.add(accountID, key, label)
}

// Import adds the account of an ed25519 secret key and returns it unlocked.
// The key may be near-cli's "ed25519:<base58>", 64 hex digits, or bare
// base58. When accountID is empty the implicit account id is used; a named
// account must be given, because a key alone does not name it.
func (k *NearKeyring) Import(secret, accountID, label string) (NearAccount, error) {
	key, err := neartx.ParseSecretKey(secret)
	if err != nil {
		return NearAccount{}, ErrInvalidKey
	}
	if strings.TrimSpace(accountID) == "" {
		accountID = hex.EncodeToString(key.Public().(ed25519.PublicKey))
	} else {
		accountID, err = neartx.ParseAccountID(accountID)
		if err != nil {
			return NearAccount{}, ErrInvalidAddress
		}
	}
	return k.add(accountID, key, label)
}

func (k *NearKeyring) add(accountID string, key ed25519.PrivateKey, label string) (NearAccount, error) {
	k.mu.RLock()
	duplicate := k.hasLocked(accountID)
	k.mu.RUnlock()
	if duplicate {
		return NearAccount{}, ErrDuplicateKey
	}
	// The store is written before the lock is taken: the keychain may ask
	// the user, and the wallet page must stay drawable meanwhile.
	if err := k.store.Set(nearKeySecret(accountID), []byte(key)); err != nil {
		return NearAccount{}, fmt.Errorf("saving the key: %w", err)
	}
	acc := NearAccount{
		AccountID: accountID,
		PublicKey: neartx.PublicKeyOf(key).String(),
		Label:     strings.TrimSpace(label),
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.hasLocked(accountID) {
		return NearAccount{}, ErrDuplicateKey
	}
	k.accounts = append(k.accounts, acc)
	k.keys[accountID] = key
	return acc, nil
}

// Remove deletes the account and its key. The account leaves memory first,
// so no signing call can read the key after the removal; a store that
// refuses the delete puts the account back.
func (k *NearKeyring) Remove(accountID string) error {
	accountID, err := neartx.ParseAccountID(accountID)
	if err != nil {
		return err
	}
	k.mu.Lock()
	idx := -1
	for i, acc := range k.accounts {
		if acc.AccountID == accountID {
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
	delete(k.keys, accountID)
	k.mu.Unlock()
	if err := k.store.Delete(nearKeySecret(accountID)); err != nil {
		k.mu.Lock()
		k.accounts = append(k.accounts, removed)
		k.mu.Unlock()
		return fmt.Errorf("deleting the key: %w", err)
	}
	return nil
}

// Unlock reads accountID's key from the store into memory.
func (k *NearKeyring) Unlock(accountID string) error {
	accountID, err := neartx.ParseAccountID(accountID)
	if err != nil {
		return err
	}
	k.mu.RLock()
	found := k.hasLocked(accountID)
	_, unlocked := k.keys[accountID]
	k.mu.RUnlock()
	if !found {
		return ErrNoAccount
	}
	if unlocked {
		return nil
	}
	raw, err := k.store.Get(nearKeySecret(accountID))
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
	k.keys[accountID] = key
	return nil
}

// Lock forgets every key in memory.
func (k *NearKeyring) Lock() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = map[string]ed25519.PrivateKey{}
}

// LockAccount forgets accountID's key, leaving the other accounts as they
// were.
func (k *NearKeyring) LockAccount(accountID string) {
	if accountID, err := neartx.ParseAccountID(accountID); err == nil {
		k.mu.Lock()
		defer k.mu.Unlock()
		delete(k.keys, accountID)
	}
}

// Key returns accountID's key while it is unlocked.
func (k *NearKeyring) Key(accountID string) (ed25519.PrivateKey, bool) {
	accountID, err := neartx.ParseAccountID(accountID)
	if err != nil {
		return nil, false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	key, ok := k.keys[accountID]
	if !ok {
		return nil, false
	}
	return append(ed25519.PrivateKey(nil), key...), true
}

// Reveal returns accountID's key as near-cli exports it, after unlocking it.
func (k *NearKeyring) Reveal(accountID string) (string, error) {
	if err := k.Unlock(accountID); err != nil {
		return "", err
	}
	key, ok := k.Key(accountID)
	if !ok {
		return "", ErrNoAccount
	}
	return neartx.SecretKeyString(key), nil
}

func nearKeySecret(accountID string) string { return "near:key:" + accountID }
