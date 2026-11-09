package app

import (
	"errors"
	"strings"

	"github.com/egoist/mygo"

	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/neartx"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// The NEAR wallet's actions, the ed25519 counterpart of the EVM ones in
// wallet.go. A key enters only through Create or Import, goes straight to
// the OS keychain under near:key:<accountId>, and is read back only while
// the account is unlocked.

// nearDecimals is NEAR's 24 decimal places, for a NEAR card drawn while
// another network is selected.
const nearDecimals = 24

// nearAccounts returns the wallet's NEAR accounts in stored order.
func (a *App) nearAccounts() []wallet.NearAccount {
	return append([]wallet.NearAccount(nil), a.settings.NearWallet...)
}

// nearWalletName names a NEAR account in a sentence: its label, else its
// short account id.
func (a *App) nearWalletName(accountID string) string {
	for _, acc := range a.settings.NearWallet {
		if acc.AccountID == accountID && acc.Label != "" {
			return acc.Label
		}
	}
	return wallet.Short(accountID)
}

// createNearAccount makes a fresh ed25519 key and an implicit account, and
// shows the key once so the user can save it.
func (a *App) createNearAccount() {
	if a.nearKeyring == nil {
		a.nearErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	acc, err := a.nearKeyring.Create("")
	if err != nil {
		a.nearErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.placeNear(acc)
	a.settings = a.settings.Validate()
	a.save()
	a.nearErr = ""
	a.walletNotice = a.t(i18n.WalletNearCreated, wallet.Short(acc.AccountID))
	a.revealedKey, a.revealedAddr = "", ""
	if key, ok := a.nearKeyring.Key(acc.AccountID); ok {
		a.newKey = neartx.SecretKeyString(key)
		a.newKeyAddr = acc.AccountID
	}
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// importNearAccount adds the account of the key in the NEAR form. The
// account id may be left empty for an implicit account; a named account
// must be typed, because a key alone does not name it.
func (a *App) importNearAccount() {
	if a.nearKeyring == nil {
		a.nearErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	raw := strings.TrimSpace(a.draftNearKey)
	if raw == "" {
		return
	}
	acc, err := a.nearKeyring.Import(raw, a.draftNearAccount, a.draftNearLabel)
	if err != nil {
		a.nearErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.placeNear(acc)
	a.settings = a.settings.Validate()
	a.draftNearKey, a.draftNearAccount, a.draftNearLabel = "", "", ""
	a.save()
	a.nearErr = ""
	a.walletNotice = a.t(i18n.WalletNearImported, wallet.Short(acc.AccountID))
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// confirmRemoveNearAccount asks before deleting a key, then removes it.
func (a *App) confirmRemoveNearAccount(accountID string) {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              a.t(i18n.WalletRemoveTitle),
		Message:            a.t(i18n.WalletRemoveBody, a.nearWalletName(accountID)),
		Buttons:            []string{a.t(i18n.Remove), a.t(i18n.Cancel)},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	a.removeNearAccount(accountID)
}

// removeNearAccount deletes a NEAR account and its key.
func (a *App) removeNearAccount(accountID string) {
	a.cancelPasskeyOperations()
	if a.nearSendFrom == accountID {
		a.nearSendFrom = ""
	}
	name := a.nearWalletName(accountID)
	if a.nearKeyring != nil {
		if err := a.nearKeyring.Remove(accountID); err != nil && !errors.Is(err, wallet.ErrNoAccount) {
			a.nearErr = a.walletFailure(err)
			a.invalidate()
			return
		}
	}
	a.dropChainAccount(wallet.Near, accountID)
	a.settings = a.settings.Validate()
	a.clearShown(accountID)
	a.save()
	a.nearErr = ""
	a.walletNotice = a.t(i18n.WalletRemoved, name)
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// unlockNearAccount reads the account's key from the keychain into memory,
// after the system confirms the user when the wallet requires it.
func (a *App) unlockNearAccount(accountID string) {
	if a.nearKeyring == nil {
		a.nearErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.nearErr = ""
	a.authGate(a.t(i18n.AuthReasonUnlock, a.nearWalletName(accountID)), "near-unlock:"+accountID,
		func(msg string) { a.nearErr = msg },
		func() { a.finishNearUnlock(accountID) })
}

func (a *App) finishNearUnlock(accountID string) {
	if err := a.nearKeyring.Unlock(accountID); err != nil {
		a.nearErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.nearErr = ""
	a.invalidate()
}

// lockNearAccount forgets the account's key.
func (a *App) lockNearAccount(accountID string) {
	a.cancelPasskeyOperations()
	if a.nearKeyring == nil {
		return
	}
	a.nearKeyring.LockAccount(accountID)
	if a.revealedAddr == accountID {
		a.revealedKey, a.revealedAddr = "", ""
	}
	a.invalidate()
}

// revealNearKey shows the account's ed25519 key, after the system confirms
// the user. It leaves the screen when the page changes or the account is
// locked.
func (a *App) revealNearKey(accountID string) {
	if a.nearKeyring == nil {
		a.nearErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.nearErr = ""
	a.authGate(a.t(i18n.AuthReasonReveal, a.nearWalletName(accountID)), "near-reveal:"+accountID,
		func(msg string) { a.nearErr = msg },
		func() { a.finishNearReveal(accountID) })
}

func (a *App) finishNearReveal(accountID string) {
	if err := a.nearKeyring.Unlock(accountID); err != nil {
		a.nearErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	key, ok := a.nearKeyring.Key(accountID)
	if !ok {
		a.nearErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.revealedAddr = accountID
	a.revealedKey = neartx.SecretKeyString(key)
	a.newKey, a.newKeyAddr = "", ""
	a.shownKeys = nil
	a.nearErr = ""
	a.invalidate()
}
