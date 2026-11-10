package app

import (
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/wallet"
)

// passkeyPreparedCard is the one-time list of every network's address after
// a passkey is created. It holds no secret: the private key never entered
// the app. Dismissing it leaves the page on the selected network only.
func (a *App) passkeyPreparedCard(c *ui.Context) {
	p := a.pal
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
	ui.Column(c).Key("passkey-prepared").Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 14, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(38, 38).Radius(11).Background(p.accentSoft).Center().Children(func() {
				iconFingerprint.draw(c, 19, p.accent)
			})
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, a.t(i18n.PasskeyPreparedTitle)).FontSize(13.5).FontWeight(700).MaxLines(1)
				ui.Text(c, a.t(i18n.PasskeyPreparedBody)).FontSize(11.5).TextColor(p.muted).MaxLines(3)
			})
		})
		for _, network := range chains.All() {
			addr, copyable := a.passkeyAddress(network)
			a.passkeyAccountRow(c, network.Name, addr, "", copyable)
		}
		ui.Text(c, a.t(i18n.PasskeyPreparedFoot)).FontSize(11).TextColor(p.muted).MaxLines(4)
		a.softButton(c, a.t(i18n.WalletHide), nil, p.muted, a.dismissPasskeyPrepared)
		a.passkeyMessages(c)
	})
}

// passkeyAddress is the account this passkey controls on network. Ethereum
// and Base share the EVM smart-account address. The two NEAR networks share
// the NEAR account id. Solana is the LazorKit vault.
func (a *App) passkeyAddress(network chain.Network) (string, bool) {
	pk := a.settings.Passkey
	if pk == nil {
		return "", false
	}
	switch network.Family {
	case chain.Near:
		return pk.NEARAccount, pk.NEARAccount != ""
	case chain.Solana:
		return pk.SolanaAddress, pk.SolanaAddress != ""
	default:
		if pk.EVMAddress == "" {
			return a.t(i18n.PasskeyDeriving), false
		}
		return wallet.Display(wallet.EVM, pk.EVMAddress), true
	}
}

func (a *App) dismissPasskeyPrepared() {
	a.passkeyPrepared = false
	a.invalidate()
}

// passkeyChainCard is the passkey identity for the chain the page is showing.
// One credential still controls the NEAR account, the EVM smart account and
// the Solana vault; switching chain changes which of those is on screen.
func (a *App) passkeyChainCard(c *ui.Context) {
	p := a.pal
	pk := a.settings.Passkey
	if pk == nil {
		a.passkeyCard(c)
		return
	}
	ui.Column(c).Key("passkey").Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 14, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(38, 38).Radius(11).Background(p.accentSoft).Center().Children(func() {
				iconFingerprint.draw(c, 19, p.accent)
			})
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t(i18n.PasskeyTitle)).FontSize(13.5).FontWeight(700).MaxLines(1)
					ui.Text(c, a.t(i18n.PasskeyBadge)).FontSize(9.5).FontWeight(700).
						TextColor(p.accent).Padding(3, 6).Radius(99).Background(p.accentSoft)
				})
				ui.Text(c, a.t(i18n.PasskeyBody)).FontSize(11.5).TextColor(p.muted).MaxLines(4)
			})
			a.iconButton(c, iconTrash, a.t(i18n.PasskeyForget), a.confirmRemovePasskey)
		})
		switch a.network().Family {
		case chain.Near:
			a.passkeyAccountRow(c, a.t(i18n.PasskeyNearAccount), pk.NEARAccount, a.passkeyNearBalance(), true)
			if a.network().ChainName != "mainnet" {
				ui.Text(c, a.t(i18n.PasskeyMainnetOnly)).FontSize(11).TextColor(p.muted).MaxLines(2)
			} else {
				a.passkeyActions(c)
			}
		case chain.Solana:
			a.passkeyAccountRow(c, a.t(i18n.PasskeySolanaAccount), pk.SolanaAddress, a.passkeySolanaBalance(), pk.SolanaAddress != "")
			if a.passkeyVaultExists() {
				// The refresh found an account at the derived address. The
				// app did not create it, so it may be a squatter's.
				ui.Text(c, a.t(i18n.PasskeySolanaClaimed)).FontSize(11).TextColor(p.danger).MaxLines(3)
			}
			ui.Text(c, a.t(i18n.PasskeySolanaNote)).FontSize(11).TextColor(p.muted).MaxLines(4)
		default:
			evm, copyable := pk.EVMAddress, pk.EVMAddress != ""
			if evm == "" {
				evm, copyable = a.t(i18n.PasskeyDeriving), false
			}
			a.passkeyAccountRow(c, a.t(i18n.PasskeyEVMAccount), evm, a.passkeyEVMBalance(), copyable)
			ui.Text(c, a.t(i18n.PasskeyEVMNote)).FontSize(11).TextColor(p.muted).MaxLines(3)
		}
		ui.Text(c, pk.PublicKey).Font("monospace").FontSize(9.5).
			TextColor(p.faint).MaxLines(1)
		a.softButton(c, a.t(i18n.PasskeyExport), iconCopy, p.accent, func() { a.exportPasskeyRecovery() })
		ui.Text(c, a.t(i18n.PasskeyRecoveryHint)).FontSize(10.5).TextColor(p.muted).MaxLines(4)
		a.passkeyMessages(c)
	})
}

func (a *App) passkeySolanaBalance() string {
	if a.settings.Passkey == nil || a.settings.Passkey.SolanaAddress == "" || a.network().Family != chain.Solana {
		return ""
	}
	snap := a.accountSnapshot(a.settings.Passkey.SolanaAddress)
	if snap == nil {
		return ""
	}
	if a.snap.vault == vaultExists {
		// The account is there, but the app did not create it and cannot
		// tell whose it is: no balance that could be someone else's.
		return ""
	}
	return a.nativeText(snap)
}

// passkeyVaultExists reports what the last refresh learned about the
// passkey's Solana vault: whether the derived address already has an
// account on the selected network. It never asks the network here; the
// refresh loop owns that question.
func (a *App) passkeyVaultExists() bool {
	if a.settings.Passkey == nil || a.settings.Passkey.SolanaAddress == "" || a.network().Family != chain.Solana {
		return false
	}
	snap := a.accountSnapshot(a.settings.Passkey.SolanaAddress)
	if snap == nil {
		return false
	}
	return a.snap.vault == vaultExists
}

// passkeyCard is the passkey wallet before a credential exists. Once one
// exists, the chain card shows that network's account and the recovery entry.
func (a *App) passkeyCard(c *ui.Context) {
	if a.settings.Passkey != nil {
		a.passkeyChainCard(c)
		return
	}
	p := a.pal
	ui.Column(c).Key("passkey").Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).
		Margin(0, 0, 14, 0).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(38, 38).Radius(11).Background(p.accentSoft).Center().Children(func() {
				iconFingerprint.draw(c, 19, p.accent)
			})
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.t(i18n.PasskeyTitle)).FontSize(13.5).FontWeight(700).MaxLines(1)
					ui.Text(c, a.t(i18n.PasskeyBadge)).FontSize(9.5).FontWeight(700).
						TextColor(p.accent).Padding(3, 6).Radius(99).Background(p.accentSoft)
				})
			})
		})
		a.passkeyCreateRow(c)
	})
}

// passkeyCreateRow is one line until the reader asks for the recovery
// explanation. The long copy used to sit above the accounts.
func (a *App) passkeyCreateRow(c *ui.Context) {
	p := a.pal
	ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
		a.primaryButton(c, a.t(i18n.PasskeyCreate), iconFingerprint, a.passkeyBusy, a.createPasskey)
		if a.passkeyBusy {
			ui.Spinner(c).Size(14, 14)
			ui.Text(c, a.t(i18n.PasskeyWaiting)).FontSize(11.5).
				TextColor(p.muted).MaxLines(1)
			a.softButton(c, a.t(i18n.Cancel), nil, p.muted, a.cancelPasskeyOperations)
		} else {
			a.softButton(c, a.t(i18n.PasskeyRestore), iconFingerprint, p.accent, a.choosePasskeyRecovery)
			a.softButton(c, a.t(i18n.PasskeyDetails), nil, p.muted, func() {
				a.passkeyDetails = !a.passkeyDetails
				a.invalidate()
			})
		}
	})
	if a.passkeyDetails {
		ui.Text(c, a.t(i18n.PasskeyBody)).FontSize(12).TextColor(p.muted).MaxLines(4)
		ui.Text(c, a.t(i18n.PasskeyFine)).FontSize(12).TextColor(p.muted).MaxLines(4)
		ui.Text(c, a.t(i18n.PasskeyRecoveryHint)).FontSize(12).TextColor(p.muted).MaxLines(4)
	}
	a.passkeyMessages(c)
}

// passkeyAccountRow is one address the passkey controls, its balance on the
// selected network, and a copy button.
func (a *App) passkeyAccountRow(c *ui.Context, label, value, balance string, copyable bool) {
	p := a.pal
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(p.muted).Width(152)
		ui.Text(c, value).Font("monospace").FontSize(12).Grow(1).MaxLines(1)
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
			a.softButton(c, a.t(i18n.Cancel), nil, p.muted, a.cancelPasskeyOperations)
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
		ui.Text(c, a.passkeyNotice).FontSize(11).TextColor(p.success).MaxLines(4)
	}
}

// passkeyRow is the settings page's passkey control.
func (a *App) passkeyRow(c *ui.Context) {
	p := a.pal
	ui.Column(c).Children(func() {
		ui.Row(c).Padding(13, 16).AlignItems(ui.Center).Gap(14).Children(func() {
			ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
				iconFingerprint.draw(c, 17, p.accent)
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
				a.softButton(c, a.t(i18n.Cancel), nil, p.muted, a.cancelPasskeyOperations)
			case a.settings.Passkey == nil:
				a.softButton(c, a.t(i18n.PasskeyCreate), iconFingerprint, p.accent, a.createPasskey)
				a.softButton(c, a.t(i18n.PasskeyRestore), iconFingerprint, p.accent, a.choosePasskeyRecovery)
			default:
				a.softButton(c, a.t(i18n.PasskeyExport), iconCopy, p.accent, func() { a.exportPasskeyRecovery() })
				a.softButton(c, a.t(i18n.PasskeyForget), iconTrash, p.muted, a.confirmRemovePasskey)
			}
		})
		ui.Text(c, a.t(i18n.PasskeyRecoveryHint)).FontSize(10.5).TextColor(p.muted).
			Padding(0, 16, 12, 16).MaxLines(4)
		if a.passkeyErr != "" {
			ui.Text(c, a.passkeyErr).FontSize(11).TextColor(p.danger).
				Padding(0, 16, 12, 16).MaxLines(3)
		}
		if a.passkeyNotice != "" {
			ui.Text(c, a.passkeyNotice).FontSize(11).TextColor(p.success).
				Padding(0, 16, 12, 16).MaxLines(4)
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
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:             a.win,
		Type:               mygo.MessageWarning,
		Title:              a.t(i18n.PasskeyForgetTitle),
		Message:            a.t(i18n.PasskeyForgetBody),
		Buttons:            []string{a.t(i18n.PasskeyExportForget), a.t(i18n.Remove), a.t(i18n.Cancel)},
		DefaultButton:      0,
		CancelButton:       2,
		DestructiveButtons: []int{1},
	})
	if err != nil || res.Button < 0 || res.Button > 1 || a.settings.Passkey != pk {
		return
	}
	if res.Button == 0 && !a.exportPasskeyRecovery() {
		return
	}
	if a.settings.Passkey == pk {
		a.removePasskey()
	}
}

func (a *App) exportPasskeyRecovery() bool {
	pk := a.settings.Passkey
	data, err := passkeyRecoveryJSON(pk)
	if err != nil {
		a.passkeyErr = a.t(i18n.PasskeyRecoveryFailed, err.Error())
		a.invalidate()
		return false
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: a.win, Title: a.t(i18n.PasskeyExportTitle),
		DefaultPath: "crypto-panel-passkey-recovery.json", Message: a.t(i18n.PasskeyRecoveryHint),
		Filters: []mygo.FileFilter{{Name: a.t(i18n.PasskeyRecoveryFile), Extensions: []string{"json"}}},
	})
	if err == nil && path == "" {
		return false
	}
	if err == nil && a.settings.Passkey != pk {
		return false
	}
	if err == nil {
		err = os.WriteFile(path, data, 0600)
	}
	if err != nil {
		a.passkeyErr = a.t(i18n.PasskeyRecoveryFailed, err.Error())
		a.invalidate()
		return false
	}
	a.passkeyErr = ""
	a.passkeyNotice = a.t(i18n.PasskeyExported)
	a.invalidate()
	return true
}

func (a *App) choosePasskeyRecovery() {
	if a.passkeyOp != nil || a.settings.Passkey != nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent: a.win, Title: a.t(i18n.PasskeyRestoreTitle), Message: a.t(i18n.PasskeyRecoveryHint),
		Filters: []mygo.FileFilter{{Name: a.t(i18n.PasskeyRecoveryFile), Extensions: []string{"json"}}},
	})
	if err == nil && len(paths) == 0 {
		return
	}
	var data []byte
	if err == nil {
		data, err = readPasskeyRecoveryFile(paths[0])
	}
	if err != nil {
		a.passkeyErr = a.t(i18n.PasskeyRecoveryFailed, err.Error())
		a.invalidate()
		return
	}
	a.restorePasskey(data)
}
