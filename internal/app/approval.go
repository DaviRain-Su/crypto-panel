package app

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/evmtx"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/neartx"
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
	sourceKind  string // "" or "plugin" for a plugin, "dapp" for a web page
	description string
	network     chain.Network
	summary     *evmtx.Summary
	near        *neartx.Summary
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
				ui.Text(c, ap.kicker()).FontSize(10.5).FontWeight(700).
					TextColor(p.accent).LetterSpacing(0.8).MaxLines(1)
				ui.Text(c, ap.title()).FontSize(21).FontWeight(700).LetterSpacing(-0.4).MaxLines(2)
			})
		})
		ui.Column(c).Gap(9).Padding(16).Radius(12).Background(p.card).
			Border(1, p.hairline).Children(func() {
			for _, row := range ap.rows() {
				ap.row(c, p, row.label, row.value)
			}
		})
		if ap.description != "" {
			heading, note := i18n.ApprovePluginSays, i18n.ApprovePluginWords
			if ap.sourceKind == "dapp" {
				heading, note = i18n.ApproveSiteSays, i18n.ApproveSiteWords
			}
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, ap.t(heading)).FontSize(10.5).FontWeight(700).
					TextColor(p.muted).LetterSpacing(0.4)
				ui.Text(c, ap.description).FontSize(12).MaxLines(3)
				ui.Text(c, ap.t(note)).FontSize(10.5).
					TextColor(p.faint).MaxLines(2)
			})
		}
		ui.Row(c).Gap(8).Padding(10, 12).Radius(9).
			Background(p.dangerSoft).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconLock).Size(14, 14).TextColor(p.danger)
			ui.Text(c, ap.warning()).FontSize(11).TextColor(p.danger).
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

// kicker names where the request came from: a plugin, or a DApp's origin.
func (ap *approval) kicker() string {
	if ap.sourceKind == "dapp" {
		return ap.t(i18n.ApproveDappKicker, ap.pluginName)
	}
	return strings.ToUpper(ap.pluginName)
}

// warning is the sentence above the buttons. A web page's words are worth
// no more than a plugin's; a transfer the wallet itself started has no
// source words at all.
func (ap *approval) warning() string {
	switch ap.sourceKind {
	case "dapp":
		return ap.t(i18n.ApproveDappWarning)
	case "wallet":
		return ap.t(i18n.ApproveWalletWarning)
	}
	return ap.t(i18n.ApproveWarning)
}

// approvalRow is one label and value of the shell's decode.
type approvalRow struct{ label, value string }

// rows is the shell's own decode of the request: the EVM transaction or the
// NEAR actions, never the source's description.
func (ap *approval) rows() []approvalRow {
	if ap.near != nil {
		return ap.nearRows()
	}
	rows := []approvalRow{
		{ap.t(i18n.ApproveFrom), ap.fromFull()},
		{ap.t(i18n.ApproveTo), ap.summary.To.Hex()},
	}
	if ap.summary.Call != "" {
		rows = append(rows, approvalRow{ap.t(i18n.ApproveCall), ap.summary.Call})
	}
	rows = append(rows,
		approvalRow{ap.t(i18n.ApproveValue), ap.amount(ap.summary.Value)},
		approvalRow{ap.t(i18n.ApproveNetwork), ap.network.Name},
		approvalRow{ap.t(i18n.ApproveFee), ap.amount(ap.summary.Fee)},
	)
	return rows
}

// nearRows decodes each NEAR action: receiver, amount in NEAR and yoctoNEAR,
// method and arguments, and the signing key's own on-chain limit.
func (ap *approval) nearRows() []approvalRow {
	rows := []approvalRow{
		{ap.t(i18n.ApproveFrom), ap.fromFull()},
		{ap.t(i18n.ApproveNetwork), ap.network.Name},
		{ap.t(i18n.ApproveReceiver), ap.near.ReceiverID},
		{ap.t(i18n.ApproveSigningKey), ap.near.PublicKey},
	}
	if kp := ap.near.KeyPermission; kp != nil {
		rows = append(rows, approvalRow{ap.t(i18n.ApproveKeyLimit), ap.keyLimit(kp)})
	}
	for _, action := range ap.near.Actions {
		switch action.Kind {
		case neartx.ActionTransfer:
			rows = append(rows,
				approvalRow{ap.t(i18n.ApproveAmount), ap.nearAmount(action.Amount)},
				approvalRow{ap.t(i18n.ApproveYocto), yoctoString(action.Amount)},
			)
		case neartx.ActionFunctionCall:
			args := action.ArgsText
			if args == "" {
				args = "—"
			}
			rows = append(rows,
				approvalRow{ap.t(i18n.ApproveAction), ap.t(i18n.ApproveActionCall)},
				approvalRow{ap.t(i18n.ApproveMethod), action.MethodName},
				approvalRow{ap.t(i18n.ApproveArgs), args},
				approvalRow{ap.t(i18n.ApproveDeposit), ap.nearAmount(action.Amount)},
				approvalRow{ap.t(i18n.ApproveYocto), yoctoString(action.Amount)},
				approvalRow{ap.t(i18n.ApproveGas), tgas(action.Gas)},
			)
		case neartx.ActionAddKey:
			rows = append(rows,
				approvalRow{ap.t(i18n.ApproveAction), ap.t(i18n.ApproveActionAddKey)},
				approvalRow{ap.t(i18n.ApproveKey), action.PublicKey},
			)
			if action.KeyReceiver != "" {
				rows = append(rows,
					approvalRow{ap.t(i18n.ApproveKeyContract), action.KeyReceiver},
					approvalRow{ap.t(i18n.ApproveKeyMethods), ap.methods(action.MethodNames)},
					approvalRow{ap.t(i18n.ApproveKeyAllowance), ap.allowance(action.Allowance)},
				)
			} else {
				rows = append(rows, approvalRow{ap.t(i18n.ApproveKeyPermission), ap.t(i18n.ApproveFullAccess)})
			}
		case neartx.ActionDeleteKey:
			rows = append(rows,
				approvalRow{ap.t(i18n.ApproveAction), ap.t(i18n.ApproveActionDeleteKey)},
				approvalRow{ap.t(i18n.ApproveKey), action.PublicKey},
			)
		case neartx.ActionDeterministicStateInit:
			rows = append(rows,
				approvalRow{ap.t(i18n.ApproveAction), ap.t(i18n.ApproveActionStateInit)},
				approvalRow{ap.t(i18n.ApproveStateInitSize), fmt.Sprintf("%d bytes", action.StateInitSize)},
			)
		default:
			rows = append(rows, approvalRow{ap.t(i18n.ApproveAction), action.Kind.String()})
		}
	}
	return rows
}

// keyLimit says what the signer's access key may do on chain.
func (ap *approval) keyLimit(kp *neartx.KeyPermissionSummary) string {
	if kp.FullAccess {
		return ap.t(i18n.ApproveFullAccessKey)
	}
	parts := []string{}
	if kp.Allowance != nil {
		parts = append(parts, ap.t(i18n.ApproveAllowanceOf, ap.nearAmount(kp.Allowance)))
	}
	if kp.ReceiverID != "" {
		parts = append(parts, kp.ReceiverID)
	}
	parts = append(parts, ap.methods(kp.MethodNames))
	return strings.Join(parts, " · ")
}

func (ap *approval) methods(names []string) string {
	if len(names) == 0 {
		return ap.t(i18n.ApproveAnyMethod)
	}
	return strings.Join(names, ", ")
}

func (ap *approval) allowance(v *big.Int) string {
	if v == nil {
		return ap.t(i18n.ApproveNoAllowance)
	}
	return ap.nearAmount(v)
}

// title says what the transaction does, in the shell's words.
func (ap *approval) title() string {
	if ap.near != nil {
		return ap.nearTitle()
	}
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

func (ap *approval) nearTitle() string {
	if len(ap.near.Actions) == 1 {
		switch ap.near.Actions[0].Kind {
		case neartx.ActionTransfer:
			return ap.t(i18n.ApproveSendTitle, ap.network.Symbol)
		case neartx.ActionFunctionCall:
			return ap.t(i18n.ApproveNearCallTitle)
		case neartx.ActionAddKey:
			return ap.t(i18n.ApproveNearKeyTitle)
		case neartx.ActionDeleteKey:
			return ap.t(i18n.ApproveNearDeleteKeyTitle)
		}
	}
	return ap.t(i18n.ApproveNearTxTitle)
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

// nearAmount formats yoctoNEAR as NEAR. The exact yoctoNEAR value is shown
// on its own row, so a small amount is never rounded away unseen.
func (ap *approval) nearAmount(v *big.Int) string {
	if v == nil {
		v = new(big.Int)
	}
	return chain.FormatAmount(v, nearDecimals, 6) + " NEAR"
}

func yoctoString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

func tgas(gas uint64) string {
	return strconv.FormatFloat(float64(gas)/1e12, 'f', 1, 64) + " TGas"
}
