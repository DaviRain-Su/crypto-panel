package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/passkey"
	"github.com/davirain-su/lintel/internal/wallet"
)

// walletView is the native wallet's page. A wallet is a named container.
// The network menu chooses which of that container's accounts is on
// screen: balance, lock, and send when that network's family already has
// it. Import writes a key into the wallet that is open, in the format of
// the network that is open.
func (a *App) walletView(c *ui.Context) {
	p := a.pal
	a.walletKeyCard(c)
	if a.walletNotice != "" {
		ui.Text(c, a.walletNotice).FontSize(11.5).TextColor(p.success).
			Margin(0, 0, 10, 0).MaxLines(2)
	}
	// Accounts come before the security essay, so the balances are the
	// first thing on the page. The header already offers New wallet, so
	// the empty state does not repeat it.
	hasAccounts := len(a.settings.Wallets) > 0 || a.viewingPasskey()
	if hasAccounts {
		a.walletPickers(c)
	}
	switch {
	case a.passkeyPrepared && a.settings.Passkey != nil:
		a.passkeyPreparedCard(c)
	case len(a.settings.Wallets) == 0 && !a.viewingPasskey():
		a.emptyWallet(c)
		a.walletImportForm(c)
	case a.viewingPasskey():
		a.passkeyChainCard(c)
		a.walletImportForm(c)
	default:
		w, _, ok := a.activeKeyWallet()
		if !ok {
			a.emptyWallet(c)
			a.walletImportForm(c)
			break
		}
		kind := kindForFamily(a.network().Family)
		if !w.Has(kind) {
			a.missingChain(c, w)
		} else {
			switch kind {
			case wallet.Near:
				a.nearAccountCard(c, *w.Near)
				a.nearSendForm(c)
			case wallet.Solana:
				a.solanaAccountCard(c, *w.Solana)
			default:
				a.walletAccountCard(c, *w.EVM)
			}
		}
		a.walletImportForm(c)
	}
	a.walletSecurityCard(c)
	if a.settings.Passkey == nil {
		a.passkeyCard(c)
	}
}

// walletKeyCard shows a key that must not be missed: a new account's key,
// which is shown once, or one the user asked to see.
func (a *App) walletKeyCard(c *ui.Context) {
	p := a.pal
	if len(a.shownKeys) > 0 {
		ui.Column(c).Gap(10).Padding(16).Radius(14).Background(p.dangerSoft).
			Border(1, p.hairline).Margin(0, 0, 14, 0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				iconKey.draw(c, 15, p.danger)
				title := a.t(i18n.WalletNewTitle)
				if len(a.shownKeys) > 1 {
					title = a.t(i18n.WalletNewTitleMany)
				}
				ui.Text(c, title).FontSize(13).FontWeight(700).MaxLines(1)
			})
			for _, key := range a.shownKeys {
				key := key
				ui.Text(c, key.Chain+" · "+wallet.Short(key.Addr)).FontSize(11).
					FontWeight(600).TextColor(p.danger).MaxLines(1)
				ui.Text(c, key.Key).Font("monospace").FontSize(12).TextColor(p.text).MaxLines(3)
				a.softButton(c, a.t(i18n.WalletCopy), iconCopy, p.text, func() { a.copyKey(key.Key) })
			}
			ui.Text(c, a.t(i18n.WalletKeyWarning)).FontSize(11).TextColor(p.danger).MaxLines(2)
			a.softButton(c, a.t(i18n.WalletHide), nil, p.muted, a.hideKey)
		})
		return
	}
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
			iconKey.draw(c, 15, p.danger)
			ui.Text(c, title).FontSize(13).FontWeight(700).MaxLines(1)
		})
		ui.Text(c, key).Font("monospace").FontSize(12).TextColor(p.text).MaxLines(3)
		ui.Text(c, warning).FontSize(11).TextColor(p.danger).MaxLines(2)
		ui.Row(c).Gap(8).Children(func() {
			a.softButton(c, a.t(i18n.WalletCopy), iconCopy, p.text, func() { a.copyKey(key) })
			a.softButton(c, a.t(i18n.WalletHide), nil, p.muted, a.hideKey)
		})
	})
}

// walletPickers is the wallet menu and the network menu. The network menu
// lists every preset, not a family tab: Ethereum and Base are separate
// choices that share one EVM account, and the two NEAR networks share one
// NEAR account.
func (a *App) walletPickers(c *ui.Context) {
	p := a.pal
	type choice struct {
		id, label string
	}
	var wallets []choice
	for _, w := range a.settings.Wallets {
		label := w.Name
		if label == "" {
			label = wallet.Short(w.ID)
		}
		wallets = append(wallets, choice{id: w.ID, label: label})
	}
	if a.settings.Passkey != nil {
		wallets = append(wallets, choice{id: wallet.PasskeyContainerID, label: a.t(i18n.PasskeyTitle)})
	}
	network := a.network().Name
	ui.Row(c).Gap(16).Margin(14, 0, 12, 0).Children(func() {
		if len(wallets) >= 2 {
			labels := make([]string, len(wallets))
			idx := 0
			active := a.settings.ActiveWallet
			if active == "" && len(a.settings.Wallets) > 0 {
				active = a.settings.Wallets[0].ID
			}
			if a.viewingPasskey() {
				active = wallet.PasskeyContainerID
			}
			for i, choice := range wallets {
				labels[i] = choice.label
				if choice.id == active {
					idx = i
				}
			}
			selected := labels[idx]
			ui.Column(c).Gap(6).Grow(1).MaxWidth(360).Children(func() {
				ui.Text(c, a.t(i18n.WalletSwitch)).FontSize(11).FontWeight(600).
					TextColor(p.muted).MaxLines(1)
				if ui.Select(c.Key("wallet-switch"), &selected, labels).Changed() {
					for i, label := range labels {
						if label == selected {
							a.selectWallet(wallets[i].id)
							break
						}
					}
				}
			})
		}
		ui.Column(c).Gap(6).Grow(1).MaxWidth(360).Children(func() {
			ui.Text(c, a.t(i18n.WalletNetwork)).FontSize(11).FontWeight(600).
				TextColor(p.muted).MaxLines(1)
			// Grouped by family. MyGo's Select is a flat list and always
			// opens downward, so this is a menu the shell draws itself.
			ui.PopoverMenuButton(c, network, func(m *ui.Menu) {
				first := true
				for _, family := range chains.Families() {
					group := chains.NetworksOf(family)
					if len(group) == 0 {
						continue
					}
					if !first {
						m.Separator()
					}
					first = false
					m.Item(familyName(family)).Disabled(true)
					for _, n := range group {
						n := n
						if m.Item(n.Name).Checked(n.ID == a.settings.NetworkID).Chosen() {
							a.setNetwork(n.ID)
						}
					}
				}
			}).FillWidth().Justify(ui.SpaceBetween)
		})
	})
}

func (a *App) missingChain(c *ui.Context, w wallet.Container) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(22).Center().Radius(14).
		Background(p.card).Border(1, p.hairline).Margin(0, 0, 12, 0).Children(func() {
		ui.Text(c, a.t(i18n.WalletNoChain, w.Name, a.network().Name)).
			FontSize(13).FontWeight(600).TextAlign(ui.Center).MaxLines(3)
		ui.Text(c, a.t(i18n.WalletNoChainWhy)).FontSize(12).TextColor(p.muted).
			TextAlign(ui.Center).MaxLines(3)
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
		Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).AlignItems(ui.Center).
		Margin(0, 0, 16, 0).Children(func() {
		ui.Box(c).Size(38, 38).Radius(11).Background(p.accentSoft).Center().Children(func() {
			iconShield.draw(c, 19, p.accent)
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
			iconWallet.draw(c, 22, p.accent)
		})
		ui.Text(c, a.t(i18n.WalletEmpty)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.WalletEmptyWhy)).FontSize(12.5).TextColor(p.muted).
			TextAlign(ui.Center).MaxLines(4)
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
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 16, 0).Children(func() {
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
				ui.Text(c, a.displayAddress(acc.Addr)).Font("monospace").FontSize(12).
					TextColor(p.muted).MaxLines(1)
			})
			ui.Column(c).AlignItems(ui.End).Gap(1).Children(func() {
				ui.Text(c, a.nativeText(snap)).FontSize(15).FontWeight(700)
				ui.Text(c, a.network().Symbol).FontSize(12).TextColor(p.muted)
			})
			a.iconButton(c, iconCopy, a.t(i18n.WalletCopy), func() { a.copyKey(a.displayAddress(acc.Addr)) })
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
	title := a.t(i18n.WalletImport)
	if w, _, ok := a.activeKeyWallet(); ok {
		title = a.t(i18n.WalletImportInto, w.Name)
	}
	family := a.network().Family
	a.sectionTitle(c, title)
	ui.Column(c).Gap(10).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 16, 0).Children(func() {
		ui.Text(c, a.t(i18n.WalletImportChain, a.network().Name)).FontSize(12).
			TextColor(p.muted).MaxLines(2)
		if family == chain.Near {
			ui.TextInput(c.Key("near-account"), &a.draftNearAccount).
				Placeholder(a.t(i18n.WalletNearAccountPlaceholder)).
				Label(a.t(i18n.WalletNearAccountLabel))
		}
		ui.TextInput(c.Key("wallet-key"), &a.draftKey).
			Password().
			Placeholder(a.importKeyPlaceholder()).
			Label(a.importKeyLabel()).
			OnSubmit(a.importWalletKey)
		ui.TextInput(c.Key("wallet-label"), &a.draftWalletLabel).
			Placeholder(a.t(i18n.LabelPlaceholder)).
			Label(a.t(i18n.LabelLabel))
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			a.primaryButton(c, a.t(i18n.WalletImportButton), nil,
				strings.TrimSpace(a.draftKey) == "", a.importWalletKey)
			errText := a.walletErr
			if family == chain.Near && a.nearErr != "" {
				errText = a.nearErr
			}
			if errText != "" {
				ui.Text(c, errText).FontSize(11).TextColor(p.danger).Grow(1).MaxLines(3)
			}
		})
		ui.Text(c, a.importKeyNote()).FontSize(11).TextColor(p.muted).MaxLines(3)
	})
}

func (a *App) importKeyPlaceholder() string {
	switch a.network().Family {
	case chain.Near:
		return a.t(i18n.WalletNearKeyPlaceholder)
	case chain.Solana:
		return a.t(i18n.WalletSolanaKeyPlaceholder)
	default:
		return a.t(i18n.WalletKeyPlaceholder)
	}
}

func (a *App) importKeyLabel() string {
	switch a.network().Family {
	case chain.Near:
		return a.t(i18n.WalletNearKeyLabel)
	case chain.Solana:
		return a.t(i18n.WalletSolanaKeyLabel)
	default:
		return a.t(i18n.WalletKeyLabel)
	}
}

func (a *App) importKeyNote() string {
	switch a.network().Family {
	case chain.Near:
		return a.t(i18n.WalletNearImportNote)
	case chain.Solana:
		return a.t(i18n.WalletSolanaImportNote)
	default:
		return a.t(i18n.WalletImportNote)
	}
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
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 16, 0).Children(func() {
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
			a.iconButton(c, iconCopy, a.t(i18n.WalletCopy), func() { a.copyKey(acc.AccountID) })
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
	w, _, ok := a.activeKeyWallet()
	if !ok || w.Near == nil {
		return
	}
	accounts := []wallet.NearAccount{*w.Near}
	if a.nearSendFrom != w.Near.AccountID {
		a.nearSendFrom = w.Near.AccountID
	}
	ui.Column(c).Gap(10).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 16, 0).Children(func() {
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

// solanaAccountCard is the open wallet's Solana account. The key can be
// unlocked and revealed. Sending a Solana transaction is not wired.
func (a *App) solanaAccountCard(c *ui.Context, acc wallet.SolanaAccount) {
	p := a.pal
	snap := a.accountSnapshot(acc.Address)
	unlocked := a.solanaKeyring != nil && a.solanaKeyring.Unlocked(acc.Address)
	name := acc.Label
	if name == "" {
		if w, _, ok := a.activeKeyWallet(); ok && w.Name != "" {
			name = w.Name
		} else {
			name = wallet.Short(acc.Address)
		}
	}
	busy := a.authBusy == "solana-unlock:"+acc.Address || a.authBusy == "solana-reveal:"+acc.Address
	ui.Column(c).Key("solana:"+acc.Address).Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 16, 0).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			a.identicon(c, acc.Address)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13.5).FontWeight(600).MaxLines(1)
					ui.Text(c, "Solana").FontSize(9.5).FontWeight(700).
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
				ui.Text(c, acc.Address).Font("monospace").FontSize(10.5).
					TextColor(p.muted).MaxLines(1)
			})
			ui.Column(c).AlignItems(ui.End).Gap(1).Children(func() {
				ui.Text(c, a.nativeText(snap)).FontSize(15).FontWeight(700)
				ui.Text(c, "SOL").FontSize(10).TextColor(p.faint)
			})
			a.iconButton(c, iconCopy, a.t(i18n.WalletCopy), func() { a.copyKey(acc.Address) })
			a.iconButton(c, iconTrash, a.t(i18n.Remove), func() {
				a.confirmRemoveSolanaAccount(acc.Address)
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
				a.softButton(c, a.t(i18n.WalletLock), iconLock, p.text, func() { a.lockSolanaAccount(acc.Address) })
				a.softButton(c, a.t(i18n.WalletReveal), iconKey, p.muted, func() { a.revealSolanaKey(acc.Address) })
			default:
				icon := iconLockOpen
				if a.presenceRequired() {
					icon = iconFingerprint
				}
				a.softButton(c, a.t(i18n.WalletUnlock), icon, p.accent, func() { a.unlockSolanaAccount(acc.Address) })
			}
		})
		ui.Text(c, a.t(i18n.WalletSolanaNote)).FontSize(11).TextColor(p.muted).MaxLines(3)
		if snap != nil && snap.err != "" {
			ui.Text(c, snap.err).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
	})
}
