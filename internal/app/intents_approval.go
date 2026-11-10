package app

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/intents"
)

// intentsApprovalRequest is one dry quote waiting for the user's answer.
type intentsApprovalRequest struct {
	quote       intents.Quote
	origin      intents.Token
	destination intents.Token
	refundTo    string
	recipient   string
}

// intentsApproval is the shell's window for a quote: what the user pays,
// the least they receive, by when, where a refund goes, and where the
// request came from. It is a dry quote, so accepting it signs nothing.
type intentsApproval struct {
	t        func(i18n.Key, ...any) string
	req      intentsApprovalRequest
	decided  bool
	onDecide func(bool)
	icons    *assetIconCache
}

func (ap *intentsApproval) decide(ok bool) {
	if ap.decided {
		return
	}
	ap.decided = true
	if ap.onDecide != nil {
		ap.onDecide(ok)
	}
}

func (ap *intentsApproval) view(c *ui.Context) {
	t := c.Theme()
	p := paletteFor(t)
	useAccent(t, p)
	c.Root().Background(p.bg)
	ap.loadIcons(c)
	ui.Column(c).Fill().Padding(24).Gap(14).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(34, 34).Radius(10).Background(p.accentSoft).Center().Children(func() {
				iconSwap.draw(c, 18, p.accent)
			})
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, ap.t(i18n.IntentsApproveKicker)).FontSize(10.5).FontWeight(700).
					TextColor(p.accent).LetterSpacing(0.8).MaxLines(1)
				ui.Text(c, ap.t(i18n.IntentsApproveTitle)).FontSize(21).FontWeight(700).
					LetterSpacing(-0.4).MaxLines(2)
			})
		})
		ui.Row(c).Gap(18).Children(func() {
			minimum := ap.req.quote.MinAmountOut
			if minimum == "" {
				minimum = ap.req.quote.AmountOut
			}
			ap.assetSummary(c, ap.req.origin, quoteAmount(ap.req.quote.AmountInText, ap.req.quote.AmountIn, ap.req.origin), p)
			ap.assetSummary(c, ap.req.destination, quoteAmount("", minimum, ap.req.destination), p)
		})
		ui.Column(c).Gap(9).Padding(16).Radius(12).Background(p.card).
			Border(1, p.hairline).Children(func() {
			for _, row := range ap.rows() {
				ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
					shown, tip := shortIfAddress(row.value)
					ui.Text(c, row.label).FontSize(12).FontWeight(600).TextColor(p.muted).Width(156)
					ui.Text(c, shown).Font("monospace").FontSize(12).Grow(1).MaxLines(3).Tooltip(tip)
				})
			}
		})
		ui.Row(c).Gap(8).Padding(10, 12).Radius(9).
			Background(p.dangerSoft).AlignItems(ui.Start).Children(func() {
			iconLock.draw(c, 14, p.danger)
			ui.Text(c, ap.t(i18n.IntentsApproveWarning)).FontSize(11).TextColor(p.danger).
				Grow(1).MaxLines(3)
		})
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(10).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			ui.Button(c, ap.t(i18n.ApproveReject)).OnClick(func() { ap.decide(false) })
			b := ui.ButtonBase(c).Height(34).PaddingX(14).Gap(7).Radius(9).
				AlignItems(ui.Center).FocusRing(false).Background(p.accent)
			b.Children(func() {
				ui.Text(c, ap.t(i18n.IntentsApproveAccept)).FontSize(12.5).
					FontWeight(600).TextColor(p.onAccent)
			})
			if b.Clicked() {
				ap.decide(true)
			}
		})
	})
}

// loadIcons starts public artwork requests without blocking approval.
func (ap *intentsApproval) loadIcons(c *ui.Context) {
	if ap.icons == nil {
		return
	}
	for _, token := range []intents.Token{ap.req.origin, ap.req.destination} {
		if token.IconURL == "" {
			continue
		}
		if _, ok := ap.icons.cached(token.IconURL); ok {
			continue
		}
		raw := token.IconURL
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, _ = ap.icons.load(ctx, raw)
			mygo.RunOnMain(c.Invalidate)
		}()
	}
}

func (ap *intentsApproval) assetSummary(c *ui.Context, token intents.Token, amount string, p palette) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Grow(1).Children(func() {
		loaded := false
		if icon := builtinAssetIcon(token); icon != nil {
			ui.Box(c).Size(32, 32).Radius(16).Clip().Children(func() {
				ui.Image(c, icon).Size(32, 32)
			})
			loaded = true
		} else if ap.icons != nil && token.IconURL != "" {
			if result, ok := ap.icons.cached(token.IconURL); ok && result.bitmap != nil {
				ui.Box(c).Size(32, 32).Radius(16).Clip().Children(func() {
					ui.Image(c, result.bitmap).Size(32, 32)
				})
				loaded = true
			}
		}
		if !loaded {
			ui.Box(c).Size(32, 32).Radius(16).Background(p.accentSoft).Center().Children(func() {
				ui.Text(c, assetMonogram(token.Symbol)).FontSize(13).FontWeight(700).TextColor(p.accent)
			})
		}
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, amount).FontSize(12.5).FontWeight(650).MaxLines(2)
			ui.Text(c, blockchainName(token.Blockchain)).FontSize(10.5).TextColor(p.muted)
		})
	})
}

// rows is the shell's decode of the quote, in the language of the result.
func (ap *intentsApproval) rows() []approvalRow {
	q := ap.req.quote
	minimum := q.MinAmountOut
	if minimum == "" {
		minimum = q.AmountOut
	}
	return []approvalRow{
		{ap.t(i18n.IntentsApprovePay), quoteAmount(q.AmountInText, q.AmountIn, ap.req.origin)},
		{ap.t(i18n.IntentsApproveReceive), quoteAmount("", minimum, ap.req.destination)},
		{ap.t(i18n.IntentsApproveDeadline), ap.deadline()},
		{ap.t(i18n.IntentsApproveDeposit), ap.deposit()},
		{ap.t(i18n.IntentsApproveRefund), ap.req.refundTo},
		{ap.t(i18n.IntentsApproveRecipient), ap.req.recipient},
		{ap.t(i18n.IntentsApproveIntent), ap.intentText()},
		{ap.t(i18n.IntentsApproveSource), ap.t(i18n.IntentsApproveSourceValue)},
	}
}

func (ap *intentsApproval) deadline() string {
	if ap.req.quote.Deadline.IsZero() {
		return ap.t(i18n.IntentsNoDeadline)
	}
	return ap.req.quote.Deadline.Local().Format("2006-01-02 15:04 MST")
}

func (ap *intentsApproval) deposit() string {
	if ap.req.quote.DepositAddress == "" {
		return ap.t(i18n.IntentsNoDeposit)
	}
	return ap.req.quote.DepositAddress
}

// intentText writes the quote as one sentence the user can check against the
// rows above.
func (ap *intentsApproval) intentText() string {
	q := ap.req.quote
	minimum := q.MinAmountOut
	if minimum == "" {
		minimum = q.AmountOut
	}
	return ap.t(i18n.IntentsIntentText,
		quoteNumber(q.AmountInText, q.AmountIn, ap.req.origin), ap.req.origin.Symbol,
		blockchainName(ap.req.origin.Blockchain),
		quoteNumber("", minimum, ap.req.destination), ap.req.destination.Symbol,
		blockchainName(ap.req.destination.Blockchain),
		ap.req.refundTo,
	)
}

// askIntentsApproval opens the window on the main thread and waits for the
// user. One approval shows at a time.
func (a *App) askIntentsApproval(req intentsApprovalRequest) bool {
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	decided := make(chan bool, 1)
	var win *mygo.Window
	mygo.RunOnMain(func() {
		if a.win == nil {
			decided <- false
			return
		}
		ap := &intentsApproval{
			t:        a.t,
			req:      req,
			onDecide: func(ok bool) { decided <- ok },
			icons:    a.assetIcons,
		}
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:           a.t(i18n.IntentsApproveWindowTitle),
			Width:           480,
			Height:          620,
			MinWidth:        430,
			MinHeight:       520,
			Parent:          a.win,
			Modal:           true,
			BackgroundColor: "light-dark(#f6f4ef, #141513)",
			Content:         ui.View(ap.view),
		})
		win.OnClose(func(*mygo.CloseEvent) { ap.decide(false) })
		win.Focus()
	})
	ok := <-decided
	mygo.RunOnMain(func() {
		if win != nil {
			win.Close()
		}
	})
	return ok
}
