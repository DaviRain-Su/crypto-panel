package app

import (
	"context"
	"math/big"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/intents"
)

// The Intents page asks 1Click for a dry quote and shows its conditions in
// the shell's approval window, in the language of the result: pay this, get
// at least that, by then, refund here. A dry quote issues no deposit
// address, so nothing can move and nothing is signed; a real swap is out of
// this build's scope.

// loadIntentsTokens reads the assets 1Click supports. It runs off the main
// thread and comes back through onMain.
func (a *App) loadIntentsTokens() {
	if a.intentsTokensLoading {
		return
	}
	if a.intentsClient == nil {
		a.intentsTokensErr = a.t(i18n.IntentsNoClient)
		a.invalidate()
		return
	}
	a.intentsTokensLoading = true
	a.intentsTokensErr = ""
	a.invalidate()
	client := a.intentsClient
	a.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tokens, err := client.Tokens(ctx)
		a.onMain(func() {
			a.intentsTokensLoading = false
			if err != nil {
				a.intentsTokensErr = err.Error()
				a.invalidate()
				return
			}
			a.intentsTokens = tokens
			a.intentsTokensLoaded = true
			a.applyIntentsDefaults()
			a.invalidate()
		})
	})
}

// applyIntentsDefaults picks a first pair and a refund address once the asset
// list arrives, so the page is usable without typing an asset ID.
func (a *App) applyIntentsDefaults() {
	if a.intentsOrigin == "" {
		a.intentsOrigin = pickAsset(a.intentsTokens, "wNEAR", "near")
	}
	if a.intentsDestination == "" {
		a.intentsDestination = pickAsset(a.intentsTokens, "USDC", "eth")
	}
	if a.intentsOrigin == "" && len(a.intentsTokens) > 0 {
		a.intentsOrigin = a.intentsTokens[0].AssetID
	}
	if a.intentsDestination == "" && len(a.intentsTokens) > 1 {
		a.intentsDestination = a.intentsTokens[1].AssetID
	}
	if a.intentsRefund == "" {
		if tok, ok := a.intentsToken(a.intentsOrigin); ok {
			a.intentsRefund = a.defaultRefund(tok)
		}
	}
}

// pickAsset finds a symbol on a blockchain, or "" when 1Click does not list
// it.
func pickAsset(tokens []intents.Token, symbol, blockchain string) string {
	for _, tok := range tokens {
		if tok.Symbol == symbol && tok.Blockchain == blockchain {
			return tok.AssetID
		}
	}
	return ""
}

func (a *App) intentsToken(assetID string) (intents.Token, bool) {
	for _, tok := range a.intentsTokens {
		if tok.AssetID == assetID {
			return tok, true
		}
	}
	return intents.Token{}, false
}

// defaultRefund is the wallet's own address on the origin chain, when it has
// one. A refund to an address the user does not hold would lose the funds.
func (a *App) defaultRefund(origin intents.Token) string {
	switch origin.Blockchain {
	case "near":
		if len(a.settings.NearWallet) > 0 {
			return a.settings.NearWallet[0].AccountID
		}
	case "eth", "arb", "base", "pol", "bsc", "op", "avax", "gnosis":
		if len(a.settings.Wallet) > 0 {
			return a.settings.Wallet[0].Addr
		}
	}
	return ""
}

// requestIntentsQuote validates the form, asks 1Click for a dry quote, and
// shows the approval window. The network work and the window run off the
// main thread.
func (a *App) requestIntentsQuote() {
	if a.intentsBusy {
		return
	}
	origin, ok := a.intentsToken(a.intentsOrigin)
	if !ok {
		a.intentsErr = a.t(i18n.IntentsPickAssets)
		a.invalidate()
		return
	}
	destination, ok := a.intentsToken(a.intentsDestination)
	if !ok {
		a.intentsErr = a.t(i18n.IntentsPickAssets)
		a.invalidate()
		return
	}
	amount, ok := chain.ParseUnits(a.intentsAmount, origin.Decimals)
	if !ok || amount.Sign() <= 0 {
		a.intentsErr = a.t(i18n.IntentsAmountInvalid, origin.Decimals)
		a.invalidate()
		return
	}
	recipient := strings.TrimSpace(a.intentsRecipient)
	if recipient == "" {
		a.intentsErr = a.t(i18n.IntentsRecipientRequired)
		a.invalidate()
		return
	}
	refund := strings.TrimSpace(a.intentsRefund)
	if refund == "" {
		refund = a.defaultRefund(origin)
	}
	if refund == "" {
		a.intentsErr = a.t(i18n.IntentsRefundRequired)
		a.invalidate()
		return
	}
	if a.intentsClient == nil {
		a.intentsErr = a.t(i18n.IntentsNoClient)
		a.invalidate()
		return
	}
	req := intents.QuoteRequest{
		OriginAsset:      origin.AssetID,
		DestinationAsset: destination.AssetID,
		Amount:           amount.String(),
		Recipient:        recipient,
		RefundTo:         refund,
	}
	a.intentsBusy = true
	a.intentsErr = ""
	a.intentsNotice = ""
	a.intentsQuote = nil
	a.intentsAccepted = false
	a.invalidate()
	client := a.intentsClient
	a.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		quote, err := client.Quote(ctx, req)
		if err != nil {
			a.onMain(func() {
				a.intentsBusy = false
				a.intentsErr = err.Error()
				a.invalidate()
			})
			return
		}
		a.showIntentsApproval(quote, origin, destination)
	})
}

// showIntentsApproval opens the approval window and records the answer. It
// runs on the quote's goroutine; the window is created on the main thread.
func (a *App) showIntentsApproval(quote intents.Quote, origin, destination intents.Token) {
	ok := a.intentsApprover()(intentsApprovalRequest{
		quote:       quote,
		origin:      origin,
		destination: destination,
		refundTo:    quote.Request.RefundTo,
		recipient:   quote.Request.Recipient,
	})
	a.onMain(func() {
		a.intentsBusy = false
		a.intentsQuote = &quote
		a.intentsAccepted = ok
		if ok {
			a.intentsNotice = a.t(i18n.IntentsAccepted)
		} else {
			a.intentsNotice = a.t(i18n.IntentsRejected)
		}
		a.invalidate()
	})
}

// reviewIntentsQuote reopens the approval window for the quote already on
// screen, without asking 1Click again.
func (a *App) reviewIntentsQuote() {
	if a.intentsQuote == nil || a.intentsBusy {
		return
	}
	quote := *a.intentsQuote
	origin, _ := a.intentsToken(quote.Request.OriginAsset)
	destination, _ := a.intentsToken(quote.Request.DestinationAsset)
	a.intentsBusy = true
	a.intentsNotice = ""
	a.invalidate()
	a.goAsync(func() { a.showIntentsApproval(quote, origin, destination) })
}

func (a *App) intentsApprover() func(intentsApprovalRequest) bool {
	if a.approveIntents != nil {
		return a.approveIntents
	}
	return a.askIntentsApproval
}

// intentsBody is the Intents page: the swap form, the last quote, and the
// state of the asset list.
func (a *App) intentsBody(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(18).Children(func() {
		ui.Column(c).Gap(12).Padding(16).Radius(14).Background(p.card).
			Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
			ui.Row(c).Gap(12).Children(func() {
				a.intentsAssetField(c, "intents-origin", a.t(i18n.IntentsPayWith), &a.intentsOrigin)
				a.intentsAssetField(c, "intents-destination", a.t(i18n.IntentsReceive), &a.intentsDestination)
			})
			ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
				label := a.t(i18n.IntentsAmount)
				if tok, ok := a.intentsToken(a.intentsOrigin); ok {
					label += " · " + tok.Symbol
				}
				a.intentsField(c, label, 1, func() {
					ui.TextInput(c.Key("intents-amount"), &a.intentsAmount).
						Placeholder(a.t(i18n.IntentsAmountPlaceholder)).
						Label(a.t(i18n.IntentsAmount)).
						OnSubmit(a.requestIntentsQuote)
				})
				a.intentsField(c, a.t(i18n.IntentsRecipient), 2, func() {
					ui.TextInput(c.Key("intents-recipient"), &a.intentsRecipient).
						Placeholder(a.t(i18n.IntentsRecipientPlaceholder)).
						Label(a.t(i18n.IntentsRecipient))
				})
			})
			ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
				a.intentsField(c, a.t(i18n.IntentsRefund), 2, func() {
					ui.TextInput(c.Key("intents-refund"), &a.intentsRefund).
						Placeholder(a.t(i18n.IntentsRefundPlaceholder)).
						Label(a.t(i18n.IntentsRefund))
				})
				a.primaryButton(c, a.t(i18n.IntentsQuote), nil,
					a.intentsBusy || !a.intentsTokensLoaded, a.requestIntentsQuote)
			})
			if a.intentsBusy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, a.t(i18n.IntentsQuoting)).FontSize(11.5).TextColor(p.muted)
				})
			}
			if a.intentsErr != "" {
				ui.Text(c, a.intentsErr).FontSize(11.5).TextColor(p.danger).MaxLines(3)
			}
			if a.intentsNotice != "" {
				ui.Text(c, a.intentsNotice).FontSize(11.5).TextColor(p.muted).MaxLines(2)
			}
		})
		if a.intentsQuote != nil {
			a.intentsQuoteCard(c)
		}
		a.intentsAssetsState(c)
	})
}

// intentsField is a labelled control of the swap form.
func (a *App) intentsField(c *ui.Context, label string, grow float32, control func()) {
	ui.Column(c).Gap(5).Grow(grow).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(a.pal.muted).MaxLines(1)
		control()
	})
}

// intentsAssetField is one of the two asset selects.
func (a *App) intentsAssetField(c *ui.Context, key, label string, current *string) {
	p := a.pal
	ui.Column(c).Gap(5).Grow(1).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.muted).MaxLines(1)
		if len(a.intentsTokens) == 0 {
			ui.Text(c, a.t(i18n.IntentsNoAssets)).FontSize(12).TextColor(p.faint).MaxLines(1)
			return
		}
		labels := make([]string, len(a.intentsTokens))
		idx := 0
		for i, tok := range a.intentsTokens {
			labels[i] = tokenLabel(tok)
			if tok.AssetID == *current {
				idx = i
			}
		}
		selected := labels[idx]
		if ui.Select(c.Key(key), &selected, labels).Changed() {
			previous := *current
			for i, l := range labels {
				if l == selected {
					*current = a.intentsTokens[i].AssetID
					break
				}
			}
			// A refund address that was only the default follows the origin
			// chain; one the user typed is left alone.
			if key == "intents-origin" && *current != previous {
				oldToken, _ := a.intentsToken(previous)
				newToken, _ := a.intentsToken(*current)
				if strings.TrimSpace(a.intentsRefund) == a.defaultRefund(oldToken) {
					a.intentsRefund = a.defaultRefund(newToken)
				}
			}
		}
	})
}

// intentsAssetsState says whether the asset list is loading, failed, or
// ready.
func (a *App) intentsAssetsState(c *ui.Context) {
	p := a.pal
	switch {
	case a.intentsTokensLoading:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(14, 14)
			ui.Text(c, a.t(i18n.IntentsLoadingAssets)).FontSize(11.5).TextColor(p.muted)
		})
	case a.intentsTokensErr != "":
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.intentsTokensErr).FontSize(11.5).TextColor(p.danger).
				Grow(1).MaxLines(2)
			a.softButton(c, a.t(i18n.IntentsRetry), nil, p.accent, a.loadIntentsTokens)
		})
	case a.intentsTokensLoaded:
		ui.Text(c, a.t(i18n.IntentsAssetsNote, len(a.intentsTokens))).FontSize(11).
			TextColor(p.faint).MaxLines(3)
	default:
		a.softButton(c, a.t(i18n.IntentsLoadAssets), nil, p.accent, a.loadIntentsTokens)
	}
}

// intentsQuoteCard shows the last quote in the shell's own words.
func (a *App) intentsQuoteCard(c *ui.Context) {
	p := a.pal
	q := a.intentsQuote
	origin, _ := a.intentsToken(q.Request.OriginAsset)
	destination, _ := a.intentsToken(q.Request.DestinationAsset)
	minimum := q.MinAmountOut
	if minimum == "" {
		minimum = q.AmountOut
	}
	ui.Column(c).Gap(9).Padding(16).Radius(14).Background(p.card).
		Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.t(i18n.IntentsQuoteTitle)).FontSize(14).FontWeight(700).Grow(1)
			ui.Badge(c, a.t(i18n.IntentsDryBadge))
		})
		a.intentsRow(c, p, a.t(i18n.IntentsYouPay), quoteAmount(q.AmountInText, q.AmountIn, origin))
		a.intentsRow(c, p, a.t(i18n.IntentsYouReceive), quoteAmount("", minimum, destination))
		if q.AmountOutText != "" && q.AmountOutText != minimum {
			a.intentsRow(c, p, a.t(i18n.IntentsEstimated), quoteAmount(q.AmountOutText, q.AmountOut, destination))
		}
		a.intentsRow(c, p, a.t(i18n.IntentsDeadline), a.intentsDeadlineText(q.Deadline))
		a.intentsRow(c, p, a.t(i18n.IntentsDepositAddress), a.intentsDepositText(q.DepositAddress))
		a.intentsRow(c, p, a.t(i18n.IntentsRefundRow), q.Request.RefundTo)
		a.intentsRow(c, p, a.t(i18n.IntentsRecipientRow), q.Request.Recipient)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.softButton(c, a.t(i18n.IntentsReview), nil, p.accent, a.reviewIntentsQuote)
			ui.Text(c, a.t(i18n.IntentsQuotedBy, q.CorrelationID)).FontSize(10.5).
				TextColor(p.faint).Grow(1).MaxLines(1)
		})
	})
}

func (a *App) intentsRow(c *ui.Context, p palette, label, value string) {
	ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(p.muted).Width(128)
		ui.Text(c, value).Font("monospace").FontSize(11.5).Grow(1).MaxLines(2)
	})
}

// quoteNumber writes an amount in the asset's own units, preferring
// 1Click's formatted text and falling back to the smallest-unit value.
func quoteNumber(text, base string, tok intents.Token) string {
	if text == "" && base != "" {
		if v, ok := new(big.Int).SetString(base, 10); ok {
			text = chain.FormatUnits(v, tok.Decimals)
		}
	}
	if text == "" {
		return "—"
	}
	return text
}

// quoteAmount is quoteNumber with the asset's symbol.
func quoteAmount(text, base string, tok intents.Token) string {
	number := quoteNumber(text, base, tok)
	if tok.Symbol == "" {
		return number
	}
	return number + " " + tok.Symbol
}

func (a *App) intentsDeadlineText(t time.Time) string {
	if t.IsZero() {
		return a.t(i18n.IntentsNoDeadline)
	}
	return t.Local().Format("2006-01-02 15:04 MST")
}

func (a *App) intentsDepositText(address string) string {
	if address == "" {
		return a.t(i18n.IntentsNoDeposit)
	}
	return address
}

// tokenLabel names an asset in a select: its symbol and the chain it lives
// on.
func tokenLabel(tok intents.Token) string {
	return tok.Symbol + " · " + blockchainName(tok.Blockchain)
}

// blockchainName writes 1Click's chain codes as people read them.
func blockchainName(id string) string {
	switch id {
	case "near":
		return "NEAR"
	case "eth":
		return "Ethereum"
	case "arb":
		return "Arbitrum"
	case "base":
		return "Base"
	case "sol":
		return "Solana"
	case "btc":
		return "Bitcoin"
	case "ton":
		return "TON"
	case "doge":
		return "Dogecoin"
	case "xrp":
		return "XRP"
	case "zec":
		return "Zcash"
	case "bsc":
		return "BNB Chain"
	case "pol":
		return "Polygon"
	case "tron":
		return "Tron"
	case "sui":
		return "Sui"
	case "op":
		return "Optimism"
	case "avax":
		return "Avalanche"
	case "ltc":
		return "Litecoin"
	case "bch":
		return "Bitcoin Cash"
	}
	return strings.ToUpper(id)
}
