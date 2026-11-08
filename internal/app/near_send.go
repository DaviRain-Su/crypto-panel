package app

import (
	"strings"

	"github.com/egoist/mygo"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/nearid"
	"github.com/davirain-su/crypto-panel/internal/plugin"
)

// sendNear is the wallet's own transfer: it validates the form, asks the
// shell's approval window, and signs and broadcasts only what the user
// approved. The network work runs off the main thread.
func (a *App) sendNear() {
	if a.nearSendBusy {
		return
	}
	if a.network().Family != chain.Near {
		a.nearSendErr = a.t(i18n.WalletNearSendNoNetwork)
		a.invalidate()
		return
	}
	to, err := nearid.ParseAccountID(strings.TrimSpace(a.nearSendTo))
	if err != nil {
		a.nearSendErr = a.t(i18n.WalletNearSendInvalidTo)
		a.invalidate()
		return
	}
	amount, ok := chain.ParseUnits(a.nearSendAmount, nearDecimals)
	if !ok || amount.Sign() <= 0 {
		a.nearSendErr = a.t(i18n.WalletNearSendInvalidAmount)
		a.invalidate()
		return
	}
	from := a.nearSendFrom
	if from == "" && len(a.settings.NearWallet) > 0 {
		from = a.settings.NearWallet[0].AccountID
	}
	if from == "" {
		a.nearSendErr = a.t(i18n.WalletNearSendNoAccount)
		a.invalidate()
		return
	}
	if a.nearKeyring == nil || !a.nearKeyring.Unlocked(from) {
		a.nearSendErr = a.t(i18n.WalletNearSendLocked)
		a.invalidate()
		return
	}
	if a.reader == nil {
		a.nearSendErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	network := a.network()
	reader := a.reader
	a.nearSendBusy = true
	a.nearSendErr = ""
	a.nearSendNotice = ""
	a.nearSendHash = ""
	a.invalidate()
	a.goAsync(func() {
		res, err := signNearTransaction(nearSignDeps{
			sourceName: "Crypto Panel",
			sourceKind: "wallet",
			account:    a.nearSignAccount,
			key:        a.nearSignKey,
			approve:    a.approver(),
			rpc:        reader.RPC,
		}, network, plugin.NearSignRequest{
			ChainID:  network.ID,
			From:     from,
			Receiver: to,
			Actions:  []plugin.NearAction{{Type: "transfer", Amount: amount.String()}},
		})
		a.onMain(func() {
			a.nearSendBusy = false
			if err != nil {
				a.nearSendErr = err.Error()
				a.invalidate()
				return
			}
			a.nearSendHash = res.Hash
			a.nearSendNotice = a.t(i18n.WalletNearSendSent, res.Hash)
			a.nearSendTo = ""
			a.nearSendAmount = ""
			a.touch()
			a.requestRefresh()
			a.invalidate()
		})
	})
}

// nearExplorerURL is the selected network's explorer page for a transaction.
func (a *App) nearExplorerURL(hash string) string {
	base := strings.TrimRight(a.network().Explorer, "/")
	if base == "" || hash == "" {
		return ""
	}
	return base + "/txns/" + hash
}

func (a *App) openNearExplorer(hash string) {
	if u := a.nearExplorerURL(hash); u != "" {
		_ = mygo.Shell.OpenExternal(u)
	}
}
