package app

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/egoist/mygo"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// The wallet's actions. Every one runs on the main thread, from a click. A
// key enters the app only through Import or Create, goes straight to the OS
// keychain, and is read back only while its account is unlocked.

// walletAccounts returns the wallet's accounts in stored order.
func (a *App) walletAccounts() []wallet.Account {
	return append([]wallet.Account(nil), a.settings.Wallet...)
}

// walletAddresses lists the wallet's accounts as watched addresses, for
// host.accounts.
func (a *App) walletAddresses() []wallet.Address {
	out := make([]wallet.Address, 0, len(a.settings.Wallet)+len(a.settings.NearWallet))
	for _, acc := range a.settings.Wallet {
		out = append(out, wallet.Address{Addr: acc.Addr, Label: acc.Label})
	}
	for _, acc := range a.settings.NearWallet {
		out = append(out, wallet.Address{Addr: acc.AccountID, Label: acc.Label})
	}
	return out
}

// walletName is how an account is named in a sentence: its label, else its
// short address.
func (a *App) walletName(addr string) string {
	for _, acc := range a.settings.Wallet {
		if acc.Addr == addr && acc.Label != "" {
			return acc.Label
		}
	}
	return wallet.Short(addr)
}

// walletUnlocked counts the accounts whose keys are in memory.
func (a *App) walletUnlocked() int {
	n := 0
	if a.keyring != nil {
		for _, acc := range a.settings.Wallet {
			if a.keyring.Unlocked(acc.Addr) {
				n++
			}
		}
	}
	if a.nearKeyring != nil {
		for _, acc := range a.settings.NearWallet {
			if a.nearKeyring.Unlocked(acc.AccountID) {
				n++
			}
		}
	}
	return n
}

// walletSummary heads the wallet page. The passkey counts as an account
// that is always ready: its key is in the authenticator, not in memory.
func (a *App) walletSummary() string {
	n := len(a.settings.Wallet) + len(a.settings.NearWallet)
	unlocked := a.walletUnlocked()
	if a.settings.Passkey != nil {
		n++
		unlocked++
	}
	switch n {
	case 0:
		return a.t(i18n.WalletEmptySummary)
	case 1:
		return a.t(i18n.WalletSummaryOne, unlocked)
	default:
		return a.t(i18n.WalletSummary, n, unlocked)
	}
}

// walletFailure turns a keyring error into the sentence the page shows.
func (a *App) walletFailure(err error) string {
	switch {
	case errors.Is(err, wallet.ErrInvalidKey):
		return a.t(i18n.WalletInvalidKey)
	case errors.Is(err, wallet.ErrInvalidAddress):
		return a.t(i18n.InvalidAddress)
	case errors.Is(err, wallet.ErrDuplicateKey):
		return a.t(i18n.WalletDuplicate)
	case errors.Is(err, wallet.ErrNoAccount):
		return a.t(i18n.WalletNoAccount)
	default:
		return a.t(i18n.WalletKeychainFailed, err)
	}
}

// createWalletAccount makes a new key and shows it once, so the user can
// write it down. The key is already in the keychain by then.
func (a *App) createWalletAccount() {
	if a.keyring == nil {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	acc, err := a.keyring.Create("")
	if err != nil {
		a.walletErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.settings.Wallet = append(a.settings.Wallet, acc)
	a.settings = a.settings.Validate()
	a.save()
	a.walletErr = ""
	a.walletNotice = a.t(i18n.WalletCreated, wallet.Short(acc.Addr))
	a.revealedKey, a.revealedAddr = "", ""
	if key, ok := a.keyring.Key(acc.Addr); ok {
		a.newKey = hex.EncodeToString(ethcrypto.FromECDSA(key))
		a.newKeyAddr = acc.Addr
	}
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// importWalletAccount adds the account of the key in the form.
func (a *App) importWalletAccount() {
	if a.keyring == nil {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	raw := strings.TrimSpace(a.draftKey)
	if raw == "" {
		return
	}
	acc, err := a.keyring.Import(raw, a.draftWalletLabel)
	if err != nil {
		a.walletErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.settings.Wallet = append(a.settings.Wallet, acc)
	a.settings = a.settings.Validate()
	a.draftKey, a.draftWalletLabel = "", ""
	a.save()
	a.walletErr = ""
	a.walletNotice = a.t(i18n.WalletImported, wallet.Short(acc.Addr))
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// confirmRemoveWalletAccount asks before deleting a key, then removes it.
func (a *App) confirmRemoveWalletAccount(addr string) {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              a.t(i18n.WalletRemoveTitle),
		Message:            a.t(i18n.WalletRemoveBody, a.walletName(addr)),
		Buttons:            []string{a.t(i18n.Remove), a.t(i18n.Cancel)},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	a.removeWalletAccount(addr)
}

// removeWalletAccount deletes an account and its key.
func (a *App) removeWalletAccount(addr string) {
	name := a.walletName(addr)
	if a.keyring != nil {
		if err := a.keyring.Remove(addr); err != nil && !errors.Is(err, wallet.ErrNoAccount) {
			a.walletErr = a.walletFailure(err)
			a.invalidate()
			return
		}
	}
	kept := a.settings.Wallet[:0]
	for _, acc := range a.settings.Wallet {
		if acc.Addr != addr {
			kept = append(kept, acc)
		}
	}
	a.settings.Wallet = kept
	a.settings = a.settings.Validate()
	if a.revealedAddr == addr {
		a.revealedKey, a.revealedAddr = "", ""
	}
	if a.newKeyAddr == addr {
		a.newKey, a.newKeyAddr = "", ""
	}
	a.save()
	a.walletErr = ""
	a.walletNotice = a.t(i18n.WalletRemoved, name)
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// unlockAccount reads addr's key from the keychain into memory, after the
// system confirms the user when the wallet requires it.
func (a *App) unlockAccount(addr string) {
	if a.keyring == nil {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.walletErr = ""
	a.authGate(a.t(i18n.AuthReasonUnlock, a.walletName(addr)), "unlock:"+addr,
		func(msg string) { a.walletErr = msg },
		func() { a.finishUnlock(addr) })
}

func (a *App) finishUnlock(addr string) {
	if err := a.keyring.Unlock(addr); err != nil {
		a.walletErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.walletErr = ""
	a.invalidate()
}

// lockAccount forgets addr's key.
func (a *App) lockAccount(addr string) {
	if a.keyring == nil {
		return
	}
	a.keyring.LockAccount(addr)
	if a.revealedAddr == addr {
		a.revealedKey, a.revealedAddr = "", ""
	}
	a.invalidate()
}

// lockAll forgets every key in memory.
func (a *App) lockAll() {
	if a.keyring != nil {
		a.keyring.Lock()
	}
	if a.nearKeyring != nil {
		a.nearKeyring.Lock()
	}
	a.authBusy = ""
	a.revealedKey, a.revealedAddr = "", ""
	a.newKey, a.newKeyAddr = "", ""
	a.walletErr = ""
	a.invalidate()
}

// revealKey shows addr's key, for backing it up, after the system confirms
// the user. The key stays on screen only until the page changes or the
// account is locked.
func (a *App) revealKey(addr string) {
	if a.keyring == nil {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.walletErr = ""
	a.authGate(a.t(i18n.AuthReasonReveal, a.walletName(addr)), "reveal:"+addr,
		func(msg string) { a.walletErr = msg },
		func() { a.finishReveal(addr) })
}

func (a *App) finishReveal(addr string) {
	if err := a.keyring.Unlock(addr); err != nil {
		a.walletErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	key, ok := a.keyring.Key(addr)
	if !ok {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.revealedAddr = addr
	a.revealedKey = hex.EncodeToString(ethcrypto.FromECDSA(key))
	a.newKey, a.newKeyAddr = "", ""
	a.walletErr = ""
	a.invalidate()
}

// hideKey takes a shown key off the screen.
func (a *App) hideKey() {
	a.revealedKey, a.revealedAddr = "", ""
	a.newKey, a.newKeyAddr = "", ""
	a.invalidate()
}

// copyKey puts a shown key on the clipboard and says so.
func (a *App) copyKey(key string) {
	mygo.Clipboard.WriteText(key)
	a.walletNotice = a.t(i18n.WalletCopied)
	a.invalidate()
}
