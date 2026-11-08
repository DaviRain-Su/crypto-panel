package app

import (
	"math/big"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/evmtx"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// approval is the shell's own window for one transaction: what the shell
// decoded, the plugin's words beside it, and two buttons. A plugin never
// draws this and never sees the key; a click here is the only way a
// transaction is signed.
type approval struct {
	t           func(i18n.Key, ...any) string
	pluginName  string
	description string
	network     chain.Network
	summary     *evmtx.Summary
	fromAddr    string
	fromLabel   string

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

func (ap *approval) view(c *ui.Context) {
	t := c.Theme()
	p := paletteFor(t)
	useAccent(t, p)
	c.Root().Background(p.bg)
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		ui.Text(c, strings.ToUpper(ap.pluginName)).FontSize(11).FontWeight(600).
			TextColor(p.accent).LetterSpacing(0.6).MaxLines(1)
		ui.Text(c, ap.title()).FontSize(22).FontWeight(700).LetterSpacing(-0.4).MaxLines(2)
		ui.Column(c).Gap(8).Padding(14).Radius(10).Background(p.card).
			Border(1, p.hairline).Children(func() {
			ap.row(c, p, ap.t(i18n.ApproveFrom), ap.from())
			ap.row(c, p, ap.t(i18n.ApproveTo), wallet.Short(ap.summary.To.Hex()))
			if ap.summary.Call != "" {
				ap.row(c, p, ap.t(i18n.ApproveCall), ap.summary.Call)
			}
			ap.row(c, p, ap.t(i18n.ApproveValue), ap.amount(ap.summary.Value))
			ap.row(c, p, ap.t(i18n.ApproveNetwork), ap.network.Name)
			ap.row(c, p, ap.t(i18n.ApproveFee), ap.amount(ap.summary.Fee))
		})
		if ap.description != "" {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, ap.t(i18n.ApprovePluginSays)).FontSize(11).FontWeight(600).
					TextColor(p.muted)
				ui.Text(c, ap.description).FontSize(12).MaxLines(3)
				ui.Text(c, ap.t(i18n.ApprovePluginWords)).FontSize(10.5).
					TextColor(p.faint).MaxLines(2)
			})
		}
		ui.Text(c, ap.t(i18n.ApproveWarning)).FontSize(11).TextColor(p.danger).MaxLines(2)
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			ui.Button(c, ap.t(i18n.ApproveReject)).OnClick(func() { ap.decide(false) })
			ui.Button(c, ap.t(i18n.ApproveSign)).OnClick(func() { ap.decide(true) })
		})
	})
}

func (ap *approval) row(c *ui.Context, p palette, label, value string) {
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.muted).Width(76)
		ui.Text(c, value).FontSize(12.5).Grow(1).MaxLines(2)
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

// from names the account, with its label when it has one.
func (ap *approval) from() string {
	short := wallet.Short(ap.fromAddr)
	if ap.fromLabel != "" {
		return ap.fromLabel + " · " + short
	}
	return short
}

func (ap *approval) amount(v *big.Int) string {
	return chain.FormatAmount(v, ap.network.Decimals, 6) + " " + ap.network.Symbol
}
