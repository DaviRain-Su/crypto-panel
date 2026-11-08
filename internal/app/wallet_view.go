package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// walletView is the native wallet's page: the accounts it holds, each with
// its balance and lock state, and the forms that add accounts. A key is
// shown only when it was just created or the user asked to see it.
func (a *App) walletView(c *ui.Context) {
	p := a.pal
	a.walletKeyCard(c)
	if a.walletNotice != "" {
		ui.Text(c, a.walletNotice).FontSize(11.5).TextColor(p.success).Margin(0, 0, 10, 0).MaxLines(2)
	}
	a.walletSecurityCard(c)
	if a.network().Family != chain.EVM {
		ui.Text(c, a.t(i18n.WalletEVMOnly)).FontSize(11.5).TextColor(p.muted).
			Margin(12, 0, 0, 0).MaxLines(2)
	}
	accounts := a.walletAccounts()
	if len(accounts) == 0 {
		a.emptyWallet(c)
	} else {
		a.sectionTitle(c, a.t(i18n.AccountsTitle))
		ui.Column(c).Gap(10).Children(func() {
			for _, acc := range accounts {
				a.walletAccountCard(c, acc)
			}
		})
	}
	a.walletImportForm(c)
}

// walletKeyCard shows a key that must not be missed: a new account's key,
// which is shown once, or one the user asked to see.
func (a *App) walletKeyCard(c *ui.Context) {
	p := a.pal
	var key, title, warning string
	switch {
	case a.newKey != "":
		key, title = a.newKey, a.t(i18n.WalletNewTitle)
		warning = a.t(i18n.WalletKeyWarning)
	case a.revealedKey != "":
		key, title = a.revealedKey, a.t(i18n.WalletRevealTitle, wallet.Short(a.revealedAddr))
		warning = a.t(i18n.WalletRevealWarning)
	default:
		return
	}
	ui.Column(c).Gap(8).Padding(16).Radius(12).Background(p.card).Border(1, p.hairline).
		Margin(0, 0, 14, 0).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(700)
		ui.Text(c, key).Font("monospace").FontSize(12).TextColor(p.text).MaxLines(2)
		ui.Text(c, warning).FontSize(11).TextColor(p.danger).MaxLines(2)
		ui.Row(c).Gap(8).Children(func() {
			a.softButton(c, a.t(i18n.WalletCopy), nil, p.text, func() { a.copyKey(key) })
			a.softButton(c, a.t(i18n.WalletHide), nil, p.muted, a.hideKey)
		})
	})
}

// walletSecurityCard says where the keys live, once, at the top of the page.
func (a *App) walletSecurityCard(c *ui.Context) {
	p := a.pal
	ui.Row(c).Gap(12).Padding(14).Radius(12).Background(p.card).Border(1, p.hairline).
		AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconLock).Size(16, 16).TextColor(p.accent)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, a.t(i18n.WalletSecurityTitle)).FontSize(12.5).FontWeight(600).MaxLines(1)
			ui.Text(c, a.t(i18n.WalletSecurityBody)).FontSize(11.5).TextColor(p.muted).MaxLines(3)
		})
	})
}

func (a *App) emptyWallet(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(28).Center().Radius(12).
		Background(p.card).Border(1, p.hairline).Margin(20, 0, 0, 0).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconWallet).Size(22, 22).TextColor(p.accent)
		})
		ui.Text(c, a.t(i18n.WalletEmpty)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.WalletEmptyWhy)).FontSize(12.5).TextColor(p.muted).TextAlign(ui.Center)
	})
}

func (a *App) walletAccountCard(c *ui.Context, acc wallet.Account) {
	p := a.pal
	snap := a.accountSnapshot(acc.Addr)
	unlocked := a.keyring != nil && a.keyring.Unlocked(acc.Addr)
	name := acc.Label
	if name == "" {
		name = wallet.Short(acc.Addr)
	}
	ui.Column(c).Key("wallet:"+acc.Addr).Gap(10).Padding(16).Radius(12).
		Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(14).FontWeight(600).MaxLines(1)
					badge, color := a.t(i18n.WalletLocked), p.muted
					if unlocked {
						badge, color = a.t(i18n.WalletUnlocked), p.success
					}
					ui.Text(c, badge).FontSize(10.5).FontWeight(600).TextColor(color)
				})
				ui.Text(c, a.displayAddress(acc.Addr)).FontSize(11).TextColor(p.muted).MaxLines(1)
			})
			ui.Text(c, a.nativeText(snap)).FontSize(15).FontWeight(700)
			a.iconButton(c, iconTrash, a.t(i18n.Remove), func() {
				a.confirmRemoveWalletAccount(acc.Addr)
			})
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if unlocked {
				a.softButton(c, a.t(i18n.WalletLock), nil, p.text, func() { a.lockAccount(acc.Addr) })
				a.softButton(c, a.t(i18n.WalletReveal), nil, p.muted, func() { a.revealKey(acc.Addr) })
			} else {
				a.softButton(c, a.t(i18n.WalletUnlock), nil, p.text, func() { a.unlockAccount(acc.Addr) })
			}
		})
		a.tokenRows(c, snap)
		if snap != nil && snap.err != "" {
			ui.Text(c, snap.err).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
	})
}

func (a *App) walletImportForm(c *ui.Context) {
	p := a.pal
	a.sectionTitle(c, a.t(i18n.WalletImport))
	ui.Column(c).Gap(10).Padding(16).Radius(12).MaxWidth(560).
		Background(p.card).Border(1, p.hairline).Children(func() {
		ui.TextInput(c.Key("wallet-key"), &a.draftKey).
			Password().
			Placeholder(a.t(i18n.WalletKeyPlaceholder)).
			Label(a.t(i18n.WalletKeyLabel)).
			OnSubmit(a.importWalletAccount)
		ui.TextInput(c.Key("wallet-label"), &a.draftWalletLabel).
			Placeholder(a.t(i18n.LabelPlaceholder)).
			Label(a.t(i18n.LabelLabel))
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Button(c, a.t(i18n.WalletImportButton)).
				Disabled(strings.TrimSpace(a.draftKey) == "").
				OnClick(a.importWalletAccount)
			if a.walletErr != "" {
				ui.Text(c, a.walletErr).FontSize(11).TextColor(p.danger).Grow(1).MaxLines(2)
			}
		})
		ui.Text(c, a.t(i18n.WalletImportNote)).FontSize(11).TextColor(p.muted).MaxLines(2)
	})
}
