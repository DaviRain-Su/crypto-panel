package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
)

// passkeyCard is the passkey wallet on the wallet page: one credential, the
// NEAR account it controls, the EVM smart account at the same address on
// every EVM chain, and the forms that create and use the NEAR account.
func (a *App) passkeyCard(c *ui.Context) {
	p := a.pal
	pk := a.settings.Passkey
	ui.Column(c).Key("passkey").Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 14, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(38, 38).Radius(11).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, iconFingerprint).Size(19, 19).TextColor(p.accent)
			})
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t(i18n.PasskeyTitle)).FontSize(13.5).FontWeight(700).MaxLines(1)
					ui.Text(c, a.t(i18n.PasskeyBadge)).FontSize(9.5).FontWeight(700).
						TextColor(p.accent).Padding(3, 6).Radius(99).Background(p.accentSoft)
				})
				ui.Text(c, a.t(i18n.PasskeyBody)).FontSize(11.5).TextColor(p.muted).MaxLines(3)
			})
			if pk != nil {
				a.iconButton(c, iconTrash, a.t(i18n.PasskeyForget), a.confirmRemovePasskey)
			}
		})
		if pk == nil {
			a.passkeyCreateRow(c)
			return
		}
		a.passkeyAccountRow(c, a.t(i18n.PasskeyNearAccount), pk.NEARAccount, a.passkeyNearBalance(), true)
		evm, evmCopyable := pk.EVMAddress, true
		if evm == "" {
			evm, evmCopyable = a.t(i18n.PasskeyDeriving), false
		}
		a.passkeyAccountRow(c, a.t(i18n.PasskeyEVMAccount), evm, a.passkeyEVMBalance(), evmCopyable)
		ui.Text(c, pk.PublicKey).Font("monospace").FontSize(9.5).
			TextColor(p.faint).MaxLines(1)
		if a.network().Family != chain.Near || a.network().ChainName != "mainnet" {
			ui.Text(c, a.t(i18n.PasskeyMainnetOnly)).FontSize(11).TextColor(p.muted).MaxLines(2)
		} else {
			a.passkeyActions(c)
		}
		a.passkeyMessages(c)
	})
}

// passkeyCreateRow is the card before a passkey exists.
func (a *App) passkeyCreateRow(c *ui.Context) {
	p := a.pal
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		a.primaryButton(c, a.t(i18n.PasskeyCreate), iconFingerprint, a.passkeyBusy, a.createPasskey)
		if a.passkeyBusy {
			ui.Spinner(c).Size(14, 14)
			ui.Text(c, a.t(i18n.PasskeyWaiting)).FontSize(11.5).
				TextColor(p.muted).MaxLines(1)
		}
	})
	ui.Text(c, a.t(i18n.PasskeyFine)).FontSize(10.5).TextColor(p.faint).MaxLines(2)
	a.passkeyMessages(c)
}

// passkeyAccountRow is one address the passkey controls, its balance on the
// selected network, and a copy button.
func (a *App) passkeyAccountRow(c *ui.Context, label, value, balance string, copyable bool) {
	p := a.pal
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(10.5).FontWeight(600).TextColor(p.muted).Width(96)
		ui.Text(c, value).Font("monospace").FontSize(10.5).Grow(1).MaxLines(1)
		if balance != "" {
			ui.Text(c, balance).FontSize(10.5).TextColor(p.muted).MaxLines(1)
		}
		if copyable {
			a.iconButton(c, iconCopy, a.t(i18n.WalletCopy), func() { a.copyPasskeyText(value) })
		}
	})
}

// passkeyNearBalance is the passkey NEAR account's balance while a NEAR
// network is selected.
func (a *App) passkeyNearBalance() string {
	if a.settings.Passkey == nil || a.network().Family != chain.Near {
		return ""
	}
	snap := a.accountSnapshot(a.settings.Passkey.NEARAccount)
	if snap == nil {
		return ""
	}
	return a.nearNativeText(snap)
}

// passkeyEVMBalance is the smart account's native balance while an EVM
// network is selected.
func (a *App) passkeyEVMBalance() string {
	if a.settings.Passkey == nil || a.settings.Passkey.EVMAddress == "" || a.network().Family != chain.EVM {
		return ""
	}
	snap := a.accountSnapshot(a.settings.Passkey.EVMAddress)
	if snap == nil {
		return ""
	}
	return a.nativeText(snap)
}

// passkeyActions deploys the NEAR account and sends from it. Both are
// relayed by the wallet's own unlocked NEAR account, and both show the
// shell's approval before anything is signed.
func (a *App) passkeyActions(c *ui.Context) {
	p := a.pal
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		a.softButton(c, a.t(i18n.PasskeyDeploy), iconShield, p.accent, a.deployPasskey)
		ui.Text(c, a.t(i18n.PasskeyDeployHint)).FontSize(10.5).
			TextColor(p.faint).Grow(1).MaxLines(2)
	})
	ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
		ui.Column(c).Gap(5).Grow(1).Children(func() {
			ui.TextInput(c.Key("passkey-send-to"), &a.passkeySendTo).
				Placeholder(a.t(i18n.WalletNearSendToPlaceholder)).
				Label(a.t(i18n.WalletNearSendTo))
		})
		ui.Column(c).Gap(5).Width(160).Children(func() {
			ui.TextInput(c.Key("passkey-send-amount"), &a.passkeySendAmount).
				Placeholder(a.t(i18n.WalletNearSendAmountHolder)).
				Label(a.t(i18n.WalletNearSendAmount)).
				OnSubmit(a.sendPasskeyNear)
		})
	})
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		a.primaryButton(c, a.t(i18n.PasskeySend), iconFingerprint, a.passkeySendBusy, a.sendPasskeyNear)
		if a.passkeySendBusy {
			ui.Spinner(c).Size(14, 14)
			ui.Text(c, a.t(i18n.PasskeyWaiting)).FontSize(11.5).
				TextColor(p.muted).MaxLines(1)
		}
	})
	if a.passkeySendErr != "" {
		ui.Text(c, a.passkeySendErr).FontSize(11).TextColor(p.danger).MaxLines(3)
	}
	if a.passkeySendNotice != "" {
		ui.Text(c, a.passkeySendNotice).FontSize(11).TextColor(p.success).MaxLines(2)
		if a.passkeySendHash != "" {
			a.softButton(c, a.t(i18n.WalletNearSendView), nil, p.accent, func() {
				a.openNearExplorer(a.passkeySendHash)
			})
		}
	}
}

// passkeyMessages shows the create ceremony's result or error.
func (a *App) passkeyMessages(c *ui.Context) {
	p := a.pal
	if a.passkeyErr != "" {
		ui.Text(c, a.passkeyErr).FontSize(11).TextColor(p.danger).MaxLines(3)
	}
	if a.passkeyNotice != "" {
		ui.Text(c, a.passkeyNotice).FontSize(11).TextColor(p.success).MaxLines(2)
	}
}

// passkeyRow is the settings page's passkey control.
func (a *App) passkeyRow(c *ui.Context) {
	p := a.pal
	ui.Column(c).Children(func() {
		ui.Row(c).Padding(13, 16).AlignItems(ui.Center).Gap(14).Children(func() {
			ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, iconFingerprint).Size(17, 17).TextColor(p.accent)
			})
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.t(i18n.PasskeySettingsTitle)).FontSize(12.5).FontWeight(600).MaxLines(1)
				body := a.t(i18n.PasskeySettingsBody)
				if a.settings.Passkey != nil {
					body = a.t(i18n.PasskeySettingsHave, a.settings.Passkey.NEARAccount)
				}
				ui.Text(c, body).FontSize(11).TextColor(p.muted).MaxLines(3)
			})
			switch {
			case a.passkeyBusy:
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, a.t(i18n.PasskeyWaiting)).FontSize(11).
					TextColor(p.muted).MaxLines(1)
			case a.settings.Passkey == nil:
				a.softButton(c, a.t(i18n.PasskeyCreate), iconFingerprint, p.accent, a.createPasskey)
			default:
				a.softButton(c, a.t(i18n.PasskeyForget), iconTrash, p.muted, a.confirmRemovePasskey)
			}
		})
		if a.passkeyErr != "" {
			ui.Text(c, a.passkeyErr).FontSize(11).TextColor(p.danger).
				Padding(0, 16, 12, 16).MaxLines(3)
		}
	})
}

// copyPasskeyText copies an address the passkey controls.
func (a *App) copyPasskeyText(s string) {
	mygo.Clipboard.WriteText(s)
	a.passkeyNotice = a.t(i18n.WalletCopied)
	a.invalidate()
}

// confirmRemovePasskey asks before the app forgets the passkey.
func (a *App) confirmRemovePasskey() {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              a.t(i18n.PasskeyForgetTitle),
		Message:            a.t(i18n.PasskeyForgetBody),
		Buttons:            []string{a.t(i18n.Remove), a.t(i18n.Cancel)},
		DefaultButton:      1,
		CancelButton:       1,
		DestructiveButtons: []int{0},
	})
	if err != nil || res.Button != 0 {
		return
	}
	a.removePasskey()
}
