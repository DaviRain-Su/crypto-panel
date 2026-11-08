package app

import (
	"errors"
	"math/big"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/evmtx"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/passkey"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// approval is the shell's own window for one transaction: what the shell
// decoded, the plugin's words beside it, and two buttons. A plugin never
// draws this and never sees the key; a click here, confirmed by the system
// when the wallet requires it, is the only way a transaction is signed.
type approval struct {
	t           func(i18n.Key, ...any) string
	pluginName  string
	description string
	network     chain.Network
	summary     *evmtx.Summary
	fromAddr    string
	fromLabel   string

	// The system check, when the wallet requires one.
	authRequired bool
	authName     string
	authenticate func(string) error
	invalidate   func()
	busy         bool
	authErr      string

	decided  bool
	onDecide func(bool)
}

// decide records the user's answer once. The first call wins, so closing
// the window after a click is not a second answer.
func (ap *approval) decide(ok bool) {
	if ap.decided {
		return
	}
	ap.decided = true
	if ap.onDecide != nil {
		ap.onDecide(ok)
	}
}

// approve asks the system to confirm the user when the wallet requires it,
// and only then approves. The prompt runs on its own goroutine; the window
// keeps drawing and says what it is waiting for.
func (ap *approval) approve() {
	if ap.decided || ap.busy {
		return
	}
	if !ap.authRequired || ap.authenticate == nil {
		ap.decide(true)
		return
	}
	ap.busy = true
	ap.authErr = ""
	ap.invalidate()
	reason := ap.t(i18n.AuthReasonSign, ap.from())
	authenticate := ap.authenticate
	go func() {
		err := authenticate(reason)
		mygo.RunOnMain(func() {
			if ap.decided {
				return
			}
			ap.busy = false
			if err != nil {
				ap.authErr = ap.authFailure(err)
				ap.invalidate()
				return
			}
			ap.decide(true)
		})
	}()
}

func (ap *approval) authFailure(err error) string {
	if errors.Is(err, passkey.ErrCanceled) {
		return ap.t(i18n.AuthCanceled)
	}
	return ap.t(i18n.AuthFailed, err)
}

func (ap *approval) view(c *ui.Context) {
	t := c.Theme()
	p := paletteFor(t)
	useAccent(t, p)
	c.Root().Background(p.bg)
	ui.Column(c).Fill().Padding(24).Gap(14).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(34, 34).Radius(10).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, iconShield).Size(18, 18).TextColor(p.accent)
			})
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, strings.ToUpper(ap.pluginName)).FontSize(10.5).FontWeight(700).
					TextColor(p.accent).LetterSpacing(0.8).MaxLines(1)
				ui.Text(c, ap.title()).FontSize(21).FontWeight(700).LetterSpacing(-0.4).MaxLines(2)
			})
		})
		ui.Column(c).Gap(9).Padding(16).Radius(12).Background(p.card).
			Border(1, p.hairline).Children(func() {
			ap.row(c, p, ap.t(i18n.ApproveFrom), ap.fromFull())
			ap.row(c, p, ap.t(i18n.ApproveTo), ap.summary.To.Hex())
			if ap.summary.Call != "" {
				ap.row(c, p, ap.t(i18n.ApproveCall), ap.summary.Call)
			}
			ap.row(c, p, ap.t(i18n.ApproveValue), ap.amount(ap.summary.Value))
			ap.row(c, p, ap.t(i18n.ApproveNetwork), ap.network.Name)
			ap.row(c, p, ap.t(i18n.ApproveFee), ap.amount(ap.summary.Fee))
		})
		if ap.description != "" {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, ap.t(i18n.ApprovePluginSays)).FontSize(10.5).FontWeight(700).
					TextColor(p.muted).LetterSpacing(0.4)
				ui.Text(c, ap.description).FontSize(12).MaxLines(3)
				ui.Text(c, ap.t(i18n.ApprovePluginWords)).FontSize(10.5).
					TextColor(p.faint).MaxLines(2)
			})
		}
		ui.Row(c).Gap(8).Padding(10, 12).Radius(9).
			Background(p.dangerSoft).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconLock).Size(14, 14).TextColor(p.danger)
			ui.Text(c, ap.t(i18n.ApproveWarning)).FontSize(11).TextColor(p.danger).
				Grow(1).MaxLines(2)
		})
		if ap.authErr != "" {
			ui.Text(c, ap.authErr).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(10).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if ap.authRequired {
				ui.Text(c, ap.t(i18n.AuthProtected, ap.authName)).FontSize(10.5).
					TextColor(p.faint).Grow(1).MaxLines(1)
			}
			ui.Button(c, ap.t(i18n.ApproveReject)).
				Disabled(ap.busy).
				OnClick(func() { ap.decide(false) })
			switch {
			case ap.busy:
				ui.Row(c).Gap(7).Height(34).PaddingX(14).Radius(9).
					Background(p.accentSoft).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, ap.t(i18n.AuthWaiting, ap.authName)).FontSize(12).
						FontWeight(600).TextColor(p.accent)
				})
			default:
				label := ap.t(i18n.ApproveSign)
				var icon *ui.SVG
				if ap.authRequired {
					label = ap.t(i18n.AuthApprove, ap.authName)
					icon = iconFingerprint
				}
				ap.primaryButton(c, p, label, icon)
			}
		})
	})
}

// primaryButton is the window's one action: filled with the accent, with an
// icon when the system check is part of it.
func (ap *approval) primaryButton(c *ui.Context, p palette, label string, icon *ui.SVG) {
	b := ui.ButtonBase(c).Height(34).PaddingX(14).Gap(7).Radius(9).
		AlignItems(ui.Center).FocusRing(false).Background(p.accent)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(15, 15).TextColor(p.onAccent)
		}
		ui.Text(c, label).FontSize(12.5).FontWeight(600).TextColor(p.onAccent)
	})
	if b.Clicked() {
		ap.approve()
	}
}

func (ap *approval) row(c *ui.Context, p palette, label, value string) {
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.muted).Width(76)
		ui.Text(c, value).Font("monospace").FontSize(11.5).Grow(1).MaxLines(2)
	})
}

// title says what the transaction does, in the shell's words.
func (ap *approval) title() string {
	switch {
	case strings.HasPrefix(ap.summary.Call, "Transfer"):
		return ap.t(i18n.ApproveTransferTitle)
	case strings.HasPrefix(ap.summary.Call, "Approve"):
		return ap.t(i18n.ApproveAllowanceTitle)
	case len(ap.summary.Data) == 0 && ap.summary.Value.Sign() > 0:
		return ap.t(i18n.ApproveSendTitle, ap.network.Symbol)
	default:
		return ap.t(i18n.ApproveTxTitle)
	}
}

// from names the account briefly, for the system prompt's reason.
func (ap *approval) from() string {
	short := wallet.Short(ap.fromAddr)
	if ap.fromLabel != "" {
		return ap.fromLabel + " · " + short
	}
	return short
}

// fromFull names the account with its whole address: the window is where
// the user checks which account a transaction leaves.
func (ap *approval) fromFull() string {
	if ap.fromLabel != "" {
		return ap.fromLabel + " · " + ap.fromAddr
	}
	return ap.fromAddr
}

func (ap *approval) amount(v *big.Int) string {
	return chain.FormatAmount(v, ap.network.Decimals, 6) + " " + ap.network.Symbol
}
