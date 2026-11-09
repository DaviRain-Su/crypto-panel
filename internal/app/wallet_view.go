package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/passkey"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// walletView is the native wallet's page: the accounts it holds, each with
// its balance and lock state, the system check that guards them, and the
// forms that add accounts. A key is shown only when it was just created or
// the user asked to see it.
func (a *App) walletView(c *ui.Context) {
	p := a.pal
	a.walletKeyCard(c)
	if a.walletNotice != "" {
		ui.Text(c, a.walletNotice).FontSize(11.5).TextColor(p.success).
			Margin(0, 0, 10, 0).MaxLines(2)
	}
	a.walletSecurityCard(c)
	a.passkeyCard(c)
	if a.network().Family == chain.Solana {
		ui.Text(c, a.t(i18n.WalletEVMOnly)).FontSize(11.5).TextColor(p.muted).
			Margin(12, 0, 0, 0).MaxLines(2)
	}
	evm := a.walletAccounts()
	near := a.nearAccounts()
	if len(evm)+len(near) == 0 {
		// The passkey card is its own wallet; it does not need the
		// add-an-account prompt.
		if a.settings.Passkey == nil {
			a.emptyWallet(c)
		}
	} else {
		if len(evm) > 0 {
			a.sectionTitle(c, a.t(i18n.WalletEVMSection))
			ui.Column(c).Gap(10).Children(func() {
				for _, acc := range evm {
					a.walletAccountCard(c, acc)
				}
			})
		}
		if len(near) > 0 {
			a.sectionTitle(c, a.t(i18n.WalletNearSection))
			ui.Column(c).Gap(10).Children(func() {
				for _, acc := range near {
					a.nearAccountCard(c, acc)
				}
			})
			a.nearSendForm(c)
		}
	}
	a.walletImportForm(c)
	a.nearImportForm(c)
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
	ui.Column(c).Gap(10).Padding(16).Radius(14).Background(p.dangerSoft).
		Border(1, p.hairline).Margin(0, 0, 14, 0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconKey).Size(15, 15).TextColor(p.danger)
			ui.Text(c, title).FontSize(13).FontWeight(700).MaxLines(1)
		})
		ui.Text(c, key).Font("monospace").FontSize(12).TextColor(p.text).MaxLines(2)
		ui.Text(c, warning).FontSize(11).TextColor(p.danger).MaxLines(2)
		ui.Row(c).Gap(8).Children(func() {
			a.softButton(c, a.t(i18n.WalletCopy), iconCopy, p.text, func() { a.copyKey(key) })
			a.softButton(c, a.t(i18n.WalletHide), nil, p.muted, a.hideKey)
		})
	})
}

// walletSecurityCard says where the keys live and whether the system
// confirms the user before they are used.
func (a *App) walletSecurityCard(c *ui.Context) {
	p := a.pal
	title := a.t(i18n.WalletSecurityTitle)
	if a.presenceRequired() {
		title = a.t(i18n.AuthProtected, a.authName())
	}
	body := a.t(i18n.WalletSecurityBody)
	switch {
	case a.passkeyKind == passkey.KindNone:
		body = a.t(i18n.AuthUnavailable)
	case a.presenceRequired():
		body = a.t(i18n.AuthBody, a.authName())
	}
	ui.Row(c).Gap(14).Padding(16).Radius(14).Background(p.card).
		Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(38, 38).Radius(11).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconShield).Size(19, 19).TextColor(p.accent)
		})
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600).MaxLines(1)
			ui.Text(c, body).FontSize(11.5).TextColor(p.muted).MaxLines(3)
		})
		if a.passkeyKind != passkey.KindNone {
			on := !a.settings.DisablePresence
			if ui.Switch(c, &on).Changed() {
				a.setPresenceRequired(on)
			}
		}
	})
}

func (a *App) emptyWallet(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(30).Center().Radius(14).
		Background(p.card).Border(1, p.hairline).Margin(24, 0, 0, 0).Children(func() {
		ui.Box(c).Size(46, 46).Radius(13).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconWallet).Size(22, 22).TextColor(p.accent)
		})
		ui.Text(c, a.t(i18n.WalletEmpty)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.WalletEmptyWhy)).FontSize(12.5).TextColor(p.muted).
			TextAlign(ui.Center).MaxLines(3)
		ui.Row(c).Gap(8).Children(func() {
			a.primaryButton(c, a.t(i18n.WalletNew), nil, false, a.createWalletAccount)
			a.primaryButton(c, a.t(i18n.WalletNearNew), nil, false, a.createNearAccount)
		})
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
	busy := a.authBusy == "unlock:"+acc.Addr || a.authBusy == "reveal:"+acc.Addr
	ui.Column(c).Key("wallet:"+acc.Addr).Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			a.identicon(c, acc.Addr)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13.5).FontWeight(600).MaxLines(1)
					badge, color, soft := a.t(i18n.WalletLocked), p.muted, p.inset
					if unlocked {
						badge, color, soft = a.t(i18n.WalletUnlocked), p.success, p.successSoft
					}
					ui.Row(c).Gap(4).Padding(3, 7).Radius(99).Background(soft).
						AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(5, 5).Radius(2.5).Background(color)
						ui.Text(c, badge).FontSize(9.5).FontWeight(700).TextColor(color)
					})
				})
				ui.Text(c, a.displayAddress(acc.Addr)).Font("monospace").FontSize(10.5).
					TextColor(p.muted).MaxLines(1)
			})
			ui.Column(c).AlignItems(ui.End).Gap(1).Children(func() {
				ui.Text(c, a.nativeText(snap)).FontSize(15).FontWeight(700)
				ui.Text(c, "ETH").FontSize(10).TextColor(p.faint)
			})
			a.iconButton(c, iconTrash, a.t(i18n.Remove), func() {
				a.confirmRemoveWalletAccount(acc.Addr)
			})
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case busy:
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, a.t(i18n.AuthWaiting, a.authName())).FontSize(11.5).
						TextColor(p.muted).MaxLines(1)
				})
			case unlocked:
				a.softButton(c, a.t(i18n.WalletLock), iconLock, p.text, func() { a.lockAccount(acc.Addr) })
				a.softButton(c, a.t(i18n.WalletReveal), iconKey, p.muted, func() { a.revealKey(acc.Addr) })
			default:
				icon := iconLockOpen
				if a.presenceRequired() {
					icon = iconFingerprint
				}
				a.softButton(c, a.t(i18n.WalletUnlock), icon, p.accent, func() { a.unlockAccount(acc.Addr) })
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
	ui.Column(c).Gap(10).Padding(16).Radius(14).MaxWidth(600).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.TextInput(c.Key("wallet-key"), &a.draftKey).
			Password().
			Placeholder(a.t(i18n.WalletKeyPlaceholder)).
			Label(a.t(i18n.WalletKeyLabel)).
			OnSubmit(a.importWalletAccount)
		ui.TextInput(c.Key("wallet-label"), &a.draftWalletLabel).
			Placeholder(a.t(i18n.LabelPlaceholder)).
			Label(a.t(i18n.LabelLabel))
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			a.primaryButton(c, a.t(i18n.WalletImportButton), nil,
				strings.TrimSpace(a.draftKey) == "", a.importWalletAccount)
			if a.walletErr != "" {
				ui.Text(c, a.walletErr).FontSize(11).TextColor(p.danger).Grow(1).MaxLines(2)
			}
		})
		ui.Text(c, a.t(i18n.WalletImportNote)).FontSize(11).TextColor(p.muted).MaxLines(2)
	})
}

// nearAccountCard is one NEAR account: its ed25519 public key, its balance
// on the selected NEAR network, and the lock actions.
func (a *App) nearAccountCard(c *ui.Context, acc wallet.NearAccount) {
	p := a.pal
	snap := a.accountSnapshot(acc.AccountID)
	unlocked := a.nearKeyring != nil && a.nearKeyring.Unlocked(acc.AccountID)
	name := acc.Label
	if name == "" {
		name = wallet.Short(acc.AccountID)
	}
	busy := a.authBusy == "near-unlock:"+acc.AccountID || a.authBusy == "near-reveal:"+acc.AccountID
	ui.Column(c).Key("near:"+acc.AccountID).Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			a.identicon(c, acc.AccountID)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13.5).FontWeight(600).MaxLines(1)
					ui.Text(c, a.t(i18n.WalletNearBadge)).FontSize(9.5).FontWeight(700).
						TextColor(p.accent).Padding(3, 6).Radius(99).Background(p.accentSoft)
					badge, color, soft := a.t(i18n.WalletLocked), p.muted, p.inset
					if unlocked {
						badge, color, soft = a.t(i18n.WalletUnlocked), p.success, p.successSoft
					}
					ui.Row(c).Gap(4).Padding(3, 7).Radius(99).Background(soft).
						AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(5, 5).Radius(2.5).Background(color)
						ui.Text(c, badge).FontSize(9.5).FontWeight(700).TextColor(color)
					})
				})
				ui.Text(c, acc.AccountID).Font("monospace").FontSize(10.5).
					TextColor(p.muted).MaxLines(1)
				ui.Text(c, acc.PublicKey).Font("monospace").FontSize(9.5).
					TextColor(p.faint).MaxLines(1)
			})
			ui.Column(c).AlignItems(ui.End).Gap(1).Children(func() {
				ui.Text(c, a.nearNativeText(snap)).FontSize(15).FontWeight(700)
				ui.Text(c, "NEAR").FontSize(10).TextColor(p.faint)
			})
			a.iconButton(c, iconTrash, a.t(i18n.Remove), func() {
				a.confirmRemoveNearAccount(acc.AccountID)
			})
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case busy:
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, a.t(i18n.AuthWaiting, a.authName())).FontSize(11.5).
						TextColor(p.muted).MaxLines(1)
				})
			case unlocked:
				a.softButton(c, a.t(i18n.WalletLock), iconLock, p.text, func() { a.lockNearAccount(acc.AccountID) })
				a.softButton(c, a.t(i18n.WalletReveal), iconKey, p.muted, func() { a.revealNearKey(acc.AccountID) })
			default:
				icon := iconLockOpen
				if a.presenceRequired() {
					icon = iconFingerprint
				}
				a.softButton(c, a.t(i18n.WalletUnlock), icon, p.accent, func() { a.unlockNearAccount(acc.AccountID) })
			}
		})
		if snap != nil && snap.err != "" {
			ui.Text(c, snap.err).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
	})
}

// nearSendForm sends NEAR from one of the wallet's own accounts. The
// approval window it opens is the shell's, and the hash it returns links to
// the selected network's explorer.
func (a *App) nearSendForm(c *ui.Context) {
	p := a.pal
	accounts := a.nearAccounts()
	if len(accounts) == 0 {
		return
	}
	ui.Column(c).Gap(10).Padding(16).Radius(14).MaxWidth(600).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(12, 0, 0, 0).Children(func() {
		ui.Text(c, a.t(i18n.WalletNearSendTitle)).FontSize(13).FontWeight(700)
		if a.network().Family != chain.Near {
			ui.Text(c, a.t(i18n.WalletNearSendNoNetwork)).FontSize(11.5).
				TextColor(p.muted).MaxLines(2)
		}
		ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
			ui.Column(c).Gap(5).Grow(1).Children(func() {
				ui.Text(c, a.t(i18n.WalletNearSendFrom)).FontSize(11).FontWeight(600).
					TextColor(p.muted).MaxLines(1)
				labels := make([]string, len(accounts))
				idx := 0
				for i, acc := range accounts {
					short := wallet.Short(acc.AccountID)
					labels[i] = a.nearWalletName(acc.AccountID)
					if labels[i] != short {
						labels[i] += " · " + short
					}
					if acc.AccountID == a.nearSendFrom {
						idx = i
					}
				}
				selected := labels[idx]
				if ui.Select(c.Key("near-send-from"), &selected, labels).Changed() {
					for i, l := range labels {
						if l == selected {
							a.nearSendFrom = accounts[i].AccountID
							break
						}
					}
				}
			})
			ui.Column(c).Gap(5).Grow(1).Children(func() {
				ui.Text(c, a.t(i18n.WalletNearSendAmount)).FontSize(11).FontWeight(600).
					TextColor(p.muted).MaxLines(1)
				ui.TextInput(c.Key("near-send-amount"), &a.nearSendAmount).
					Placeholder(a.t(i18n.WalletNearSendAmountHolder)).
					Label(a.t(i18n.WalletNearSendAmount)).
					OnSubmit(a.sendNear)
			})
		})
		ui.TextInput(c.Key("near-send-to"), &a.nearSendTo).
			Placeholder(a.t(i18n.WalletNearSendToPlaceholder)).
			Label(a.t(i18n.WalletNearSendTo))
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.primaryButton(c, a.t(i18n.WalletNearSend), nil,
				a.nearSendBusy || a.network().Family != chain.Near, a.sendNear)
			if a.nearSendBusy {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.t(i18n.AuthWaiting, a.authName())).FontSize(11.5).
					TextColor(p.muted).MaxLines(1)
			}
		})
		if a.nearSendErr != "" {
			ui.Text(c, a.nearSendErr).FontSize(11).TextColor(p.danger).MaxLines(3)
		}
		if a.nearSendNotice != "" {
			ui.Text(c, a.nearSendNotice).FontSize(11).TextColor(p.success).MaxLines(2)
			if a.nearSendHash != "" {
				a.softButton(c, a.t(i18n.WalletNearSendView), nil, p.accent, func() {
					a.openNearExplorer(a.nearSendHash)
				})
			}
		}
	})
}

// nearNativeText formats a NEAR balance, whether or not a NEAR network is
// the one selected.
func (a *App) nearNativeText(snap *accountSnapshot) string {
	if snap == nil || snap.err != "" || snap.native == nil {
		return "—"
	}
	return chain.FormatAmount(snap.native, nearDecimals, 4) + " NEAR"
}

// nearImportForm adds a NEAR account from a key. The account id is optional
// for an implicit account and required for a named one.
func (a *App) nearImportForm(c *ui.Context) {
	p := a.pal
	a.sectionTitle(c, a.t(i18n.WalletNearImport))
	ui.Column(c).Gap(10).Padding(16).Radius(14).MaxWidth(600).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.TextInput(c.Key("near-account"), &a.draftNearAccount).
			Placeholder(a.t(i18n.WalletNearAccountPlaceholder)).
			Label(a.t(i18n.WalletNearAccountLabel))
		ui.TextInput(c.Key("near-key"), &a.draftNearKey).
			Password().
			Placeholder(a.t(i18n.WalletNearKeyPlaceholder)).
			Label(a.t(i18n.WalletNearKeyLabel)).
			OnSubmit(a.importNearAccount)
		ui.TextInput(c.Key("near-label"), &a.draftNearLabel).
			Placeholder(a.t(i18n.LabelPlaceholder)).
			Label(a.t(i18n.LabelLabel))
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			a.primaryButton(c, a.t(i18n.WalletImportButton), nil,
				strings.TrimSpace(a.draftNearKey) == "", a.importNearAccount)
			if a.nearErr != "" {
				ui.Text(c, a.nearErr).FontSize(11).TextColor(p.danger).Grow(1).MaxLines(2)
			}
		})
		ui.Text(c, a.t(i18n.WalletNearImportNote)).FontSize(11).TextColor(p.muted).MaxLines(3)
	})
}
