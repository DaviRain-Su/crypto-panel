package app

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/egoist/mygo"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/wallet"
)

// shownKey is one private key on the wallet page, labeled by the chain it
// controls. It lives only in memory, and only while the page is showing it.
type shownKey struct {
	Chain string
	Addr  string
	Key   string
}

func evmKeyHex(a *App, addr string) string {
	if a.keyring == nil {
		return ""
	}
	key, ok := a.keyring.Key(addr)
	if !ok {
		return ""
	}
	return hex.EncodeToString(ethcrypto.FromECDSA(key))
}

func nearKeyText(a *App, accountID string) string {
	if a.nearKeyring == nil {
		return ""
	}
	key, ok := a.nearKeyring.Key(accountID)
	if !ok {
		return ""
	}
	return neartx.SecretKeyString(key)
}

func solanaKeyText(a *App, addr string) string {
	if a.solanaKeyring == nil {
		return ""
	}
	key, ok := a.solanaKeyring.Key(addr)
	if !ok {
		return ""
	}
	return wallet.SolanaSecretString(key)
}

func (a *App) clearShown(addr string) {
	if a.revealedAddr == addr {
		a.revealedKey, a.revealedAddr = "", ""
	}
	if a.newKeyAddr == addr {
		a.newKey, a.newKeyAddr = "", ""
	}
	kept := a.shownKeys[:0]
	for _, key := range a.shownKeys {
		if key.Addr != addr {
			kept = append(kept, key)
		}
	}
	if len(kept) == 0 {
		a.shownKeys = nil
		return
	}
	a.shownKeys = kept
}

// activeKeyWallet is the container the page is showing, when that container
// holds keys. The passkey identity is not one of these.
func (a *App) activeKeyWallet() (wallet.Container, int, bool) {
	id := a.settings.ActiveWallet
	if id != "" && id != wallet.PasskeyContainerID {
		for i, w := range a.settings.Wallets {
			if w.ID == id {
				return w, i, true
			}
		}
	}
	if id == wallet.PasskeyContainerID || len(a.settings.Wallets) == 0 {
		return wallet.Container{}, -1, false
	}
	return a.settings.Wallets[0], 0, true
}

// viewingPasskey reports that the page should show the passkey's account
// for the selected chain rather than a key container.
func (a *App) viewingPasskey() bool {
	if a.settings.Passkey == nil {
		return false
	}
	if a.settings.ActiveWallet == wallet.PasskeyContainerID {
		return true
	}
	return len(a.settings.Wallets) == 0
}

func (a *App) selectWallet(id string) {
	if id == a.settings.ActiveWallet {
		return
	}
	if id == wallet.PasskeyContainerID {
		if a.settings.Passkey == nil {
			return
		}
	} else {
		found := false
		for _, w := range a.settings.Wallets {
			if w.ID == id {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	a.settings.ActiveWallet = id
	a.nearSendFrom = ""
	a.save()
	a.invalidate()
}

// showFamily switches the selected network to family, which is what makes
// the wallet page show that chain's account. Staying inside the current
// family keeps the network the user already picked.
func (a *App) showFamily(family chain.Family) {
	if a.network().Family == family {
		return
	}
	networks := chains.NetworksOf(family)
	if len(networks) == 0 {
		return
	}
	a.setNetwork(networks[0].ID)
}

// slot returns the container that should receive an account of kind. The
// active wallet is used when its slot is empty. Otherwise a new wallet is
// created, so a second key of the same chain is its own wallet.
func (a *App) slot(kind wallet.Kind, label string) int {
	if i, ok := a.activeKeyIndex(); ok && !a.settings.Wallets[i].Has(kind) {
		return i
	}
	name := strings.TrimSpace(label)
	if name == "" {
		name = a.t(i18n.WalletDefaultName, len(a.settings.Wallets)+1)
	}
	a.settings.Wallets = append(a.settings.Wallets, wallet.Container{
		ID:   wallet.NewID(),
		Name: name,
	})
	return len(a.settings.Wallets) - 1
}

func (a *App) activeKeyIndex() (int, bool) {
	_, i, ok := a.activeKeyWallet()
	return i, ok
}

func (a *App) placeEVM(acc wallet.Account) {
	i := a.slot(wallet.EVM, acc.Label)
	a.settings.Wallets[i].EVM = &acc
	a.settings.ActiveWallet = a.settings.Wallets[i].ID
}

func (a *App) placeNear(acc wallet.NearAccount) {
	i := a.slot(wallet.Near, acc.Label)
	a.settings.Wallets[i].Near = &acc
	a.settings.ActiveWallet = a.settings.Wallets[i].ID
}

func (a *App) placeSolana(acc wallet.SolanaAccount) {
	i := a.slot(wallet.Solana, acc.Label)
	a.settings.Wallets[i].Solana = &acc
	a.settings.ActiveWallet = a.settings.Wallets[i].ID
}

// dropChainAccount removes addr from whichever container holds it. An empty
// container is removed. The active wallet moves to another container when
// the one on screen goes away.
func (a *App) dropChainAccount(kind wallet.Kind, addr string) {
	src := append([]wallet.Container(nil), a.settings.Wallets...)
	kept := make([]wallet.Container, 0, len(src))
	for _, w := range src {
		switch kind {
		case wallet.EVM:
			if w.EVM != nil && w.EVM.Addr == addr {
				w.EVM = nil
			}
		case wallet.Near:
			if w.Near != nil && w.Near.AccountID == addr {
				w.Near = nil
			}
		case wallet.Solana:
			if w.Solana != nil && w.Solana.Address == addr {
				w.Solana = nil
			}
		}
		if w.Empty() {
			if a.settings.ActiveWallet == w.ID {
				a.settings.ActiveWallet = ""
			}
			continue
		}
		kept = append(kept, w)
	}
	if len(kept) == 0 {
		a.settings.Wallets = []wallet.Container{}
		return
	}
	a.settings.Wallets = kept
}

// importWalletKey imports the key in the form into the active wallet, for
// the chain the page is showing. The field format follows that chain.
func (a *App) importWalletKey() {
	switch a.network().Family {
	case chain.Near:
		a.draftNearKey = a.draftKey
		a.draftNearLabel = a.draftWalletLabel
		a.importNearAccount()
		if a.importErr != "" {
			return
		}
		a.draftKey, a.draftWalletLabel = "", ""
	case chain.Solana:
		a.importSolanaAccount()
	default:
		a.importWalletAccount()
	}
}

// toggleWalletImport opens the key form, or folds it. Folding drops the
// drafts, including a NEAR key copied aside on a failed import and the
// Solana secret, which lives in the same draft as an EVM key. Opening
// starts from an empty form too. Errors from other wallet actions stay.
func (a *App) toggleWalletImport() {
	if a.walletImportOpen {
		a.foldWalletImport()
		return
	}
	a.clearWalletImportDraft()
	a.walletImportOpen = true
	a.invalidate()
}

// foldWalletImport closes the key form and forgets what was typed.
func (a *App) foldWalletImport() {
	a.walletImportOpen = false
	a.clearWalletImportDraft()
	a.invalidate()
}

// clearWalletImportDraft drops every key-import field and the import
// form's own error. Solana has no separate draft: its secret is draftKey,
// the same field an EVM key uses. walletErr and nearErr belong to the
// other wallet actions and stay on the page.
func (a *App) clearWalletImportDraft() {
	a.draftKey = ""
	a.draftWalletLabel = ""
	a.draftNearKey = ""
	a.draftNearAccount = ""
	a.draftNearLabel = ""
	a.importErr = ""
}

// submitWalletImport runs the import and folds the form when it succeeded.
// An empty submit that reports an error, such as a missing keychain, stays
// open so that error can be read. A failed key stays in the field too.
func (a *App) submitWalletImport() {
	a.importWalletKey()
	if strings.TrimSpace(a.draftKey) != "" || a.importErr != "" {
		return
	}
	a.foldWalletImport()
}

func (a *App) importSolanaAccount() {
	if a.solanaKeyring == nil {
		a.importErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	raw := strings.TrimSpace(a.draftKey)
	if raw == "" {
		return
	}
	acc, err := a.solanaKeyring.Import(raw, a.draftWalletLabel)
	if err != nil {
		a.importErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.placeSolana(acc)
	a.settings = a.settings.Validate()
	a.draftKey, a.draftWalletLabel = "", ""
	a.save()
	a.importErr = ""
	a.walletNotice = a.t(i18n.WalletImported, wallet.Short(acc.Address))
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

func (a *App) confirmRemoveSolanaAccount(addr string) {
	name := a.solanaWalletName(addr)
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              a.t(i18n.WalletRemoveTitle),
		Message:            a.t(i18n.WalletRemoveBody, name),
		Buttons:            []string{a.t(i18n.Remove), a.t(i18n.Cancel)},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	a.removeSolanaAccount(addr)
}

func (a *App) solanaWalletName(addr string) string {
	for _, w := range a.settings.Wallets {
		if w.Solana != nil && w.Solana.Address == addr {
			if w.Solana.Label != "" {
				return w.Solana.Label
			}
			if w.Name != "" {
				return w.Name
			}
		}
	}
	return wallet.Short(addr)
}

func (a *App) removeSolanaAccount(addr string) {
	name := a.solanaWalletName(addr)
	if a.solanaKeyring != nil {
		if err := a.solanaKeyring.Remove(addr); err != nil && !errors.Is(err, wallet.ErrNoAccount) {
			a.walletErr = a.walletFailure(err)
			a.invalidate()
			return
		}
	}
	a.dropChainAccount(wallet.Solana, addr)
	a.settings = a.settings.Validate()
	a.clearShown(addr)
	a.save()
	a.walletErr = ""
	a.walletNotice = a.t(i18n.WalletRemoved, name)
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

func (a *App) unlockSolanaAccount(addr string) {
	if a.solanaKeyring == nil {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.walletErr = ""
	a.authGate(a.t(i18n.AuthReasonUnlock, a.solanaWalletName(addr)), "solana-unlock:"+addr,
		func(msg string) { a.walletErr = msg },
		func() { a.finishSolanaUnlock(addr) })
}

func (a *App) finishSolanaUnlock(addr string) {
	if err := a.solanaKeyring.Unlock(addr); err != nil {
		a.walletErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	a.walletErr = ""
	a.invalidate()
}

func (a *App) lockSolanaAccount(addr string) {
	if a.solanaKeyring == nil {
		return
	}
	a.solanaKeyring.LockAccount(addr)
	if a.revealedAddr == addr {
		a.revealedKey, a.revealedAddr = "", ""
	}
	a.invalidate()
}

func (a *App) revealSolanaKey(addr string) {
	if a.solanaKeyring == nil {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.walletErr = ""
	a.authGate(a.t(i18n.AuthReasonReveal, a.solanaWalletName(addr)), "solana-reveal:"+addr,
		func(msg string) { a.walletErr = msg },
		func() { a.finishSolanaReveal(addr) })
}

func (a *App) finishSolanaReveal(addr string) {
	if err := a.solanaKeyring.Unlock(addr); err != nil {
		a.walletErr = a.walletFailure(err)
		a.invalidate()
		return
	}
	key, ok := a.solanaKeyring.Key(addr)
	if !ok {
		a.walletErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	a.revealedAddr = addr
	a.revealedKey = wallet.SolanaSecretString(key)
	a.newKey, a.newKeyAddr = "", ""
	a.shownKeys = nil
	a.walletErr = ""
	a.invalidate()
}

// kindForFamily maps the selected network's family to the wallet slot it shows.
func kindForFamily(family chain.Family) wallet.Kind {
	switch family {
	case chain.Near:
		return wallet.Near
	case chain.Solana:
		return wallet.Solana
	default:
		return wallet.EVM
	}
}
