// Package intents talks to NEAR Intents through the official 1Click Go SDK.
//
// This build only ever asks for dry quotes: a dry quote prices the swap but
// issues no deposit address, so no funds can move. A real swap needs a
// separate, deliberate path with the user's explicit consent, and is not
// part of this slice.
package intents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	sdk "github.com/defuse-protocol/one-click-sdk-go"
)

// DefaultBaseURL is 1Click's public API. It has no testnet.
const DefaultBaseURL = "https://1click.chaindefuser.com"

// DefaultSlippageBps is 1%, in basis points.
const DefaultSlippageBps = 100

// quoteLifetime is how long the user has to deposit before a wet quote
// would expire. A dry quote only carries it as a request condition.
const quoteLifetime = 30 * time.Minute

// Client is a 1Click client.
type Client struct {
	api *sdk.APIClient
}

// New returns a client for the public 1Click API.
func New() *Client { return NewAt(DefaultBaseURL) }

// NewAt returns a client for another 1Click-compatible endpoint, as tests
// use.
func NewAt(baseURL string) *Client {
	cfg := sdk.NewConfiguration()
	cfg.Servers = sdk.ServerConfigurations{{URL: strings.TrimRight(baseURL, "/")}}
	cfg.HTTPClient = &http.Client{Timeout: 25 * time.Second}
	cfg.UserAgent = "lintel"
	return &Client{api: sdk.NewAPIClient(cfg)}
}

// Token is one asset 1Click can swap.
type Token struct {
	AssetID    string
	Symbol     string
	Blockchain string
	Decimals   int
	Price      float64
}

// QuoteRequest is one swap the user is asking about. Amount is in the
// origin asset's smallest unit.
type QuoteRequest struct {
	OriginAsset      string
	DestinationAsset string
	Amount           string
	Recipient        string
	RefundTo         string
	SlippageBps      int
	// Deadline is when a deposit would stop being accepted. Zero means the
	// default, quoteLifetime from now.
	Deadline time.Time
}

// Quote is 1Click's answer, in the shell's own shape.
type Quote struct {
	DepositAddress string
	DepositMemo    string
	AmountIn       string
	AmountInText   string
	AmountInUSD    string
	MinAmountIn    string
	AmountOut      string
	AmountOutText  string
	AmountOutUSD   string
	MinAmountOut   string
	Deadline       time.Time
	TimeEstimate   int
	CorrelationID  string
	Signature      string
	Request        QuoteRequest
}

// Tokens lists the assets 1Click supports, sorted for display.
func (c *Client) Tokens(ctx context.Context) ([]Token, error) {
	list, _, err := c.api.OneClickAPI.GetTokens(ctx).Execute()
	if err != nil {
		return nil, apiError(err)
	}
	out := make([]Token, 0, len(list))
	for _, t := range list {
		if strings.TrimSpace(t.AssetId) == "" || strings.TrimSpace(t.Symbol) == "" {
			continue
		}
		out = append(out, Token{
			AssetID:    t.AssetId,
			Symbol:     t.Symbol,
			Blockchain: t.Blockchain,
			Decimals:   int(t.Decimals),
			Price:      float64(t.Price),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Blockchain < out[j].Blockchain
	})
	return out, nil
}

// Quote asks for a dry quote. It never sends dry: false.
func (c *Client) Quote(ctx context.Context, req QuoteRequest) (Quote, error) {
	req.OriginAsset = strings.TrimSpace(req.OriginAsset)
	req.DestinationAsset = strings.TrimSpace(req.DestinationAsset)
	req.Amount = strings.TrimSpace(req.Amount)
	req.Recipient = strings.TrimSpace(req.Recipient)
	req.RefundTo = strings.TrimSpace(req.RefundTo)
	switch {
	case req.OriginAsset == "":
		return Quote{}, errors.New("the asset to pay with is required")
	case req.DestinationAsset == "":
		return Quote{}, errors.New("the asset to receive is required")
	case req.OriginAsset == req.DestinationAsset:
		return Quote{}, errors.New("the two assets are the same")
	case !positiveInteger(req.Amount):
		return Quote{}, errors.New("the amount must be a positive whole number of the asset's smallest unit")
	case req.Recipient == "":
		return Quote{}, errors.New("the recipient address is required")
	case req.RefundTo == "":
		return Quote{}, errors.New("the refund address is required")
	}
	if req.SlippageBps <= 0 {
		req.SlippageBps = DefaultSlippageBps
	}
	if req.SlippageBps > 1000 {
		return Quote{}, errors.New("the slippage tolerance must be at most 10%")
	}
	if req.Deadline.IsZero() {
		req.Deadline = time.Now().Add(quoteLifetime).UTC()
	}

	// Dry is always true here. A wet quote would issue a deposit address,
	// and this build has no path that could fund it.
	res, _, err := c.api.OneClickAPI.GetQuote(ctx).QuoteRequest(sdk.QuoteRequest{
		Dry:               true,
		SwapType:          "EXACT_INPUT",
		SlippageTolerance: float32(req.SlippageBps),
		OriginAsset:       req.OriginAsset,
		DepositType:       "ORIGIN_CHAIN",
		DestinationAsset:  req.DestinationAsset,
		Amount:            req.Amount,
		RefundTo:          req.RefundTo,
		RefundType:        "ORIGIN_CHAIN",
		Recipient:         req.Recipient,
		RecipientType:     "DESTINATION_CHAIN",
		Deadline:          req.Deadline,
	}).Execute()
	if err != nil {
		return Quote{}, apiError(err)
	}
	if res == nil {
		return Quote{}, errors.New("1Click returned no quote")
	}
	q := res.Quote
	out := Quote{
		DepositAddress: stringOf(q.DepositAddress),
		DepositMemo:    stringOf(q.DepositMemo),
		AmountIn:       q.AmountIn,
		AmountInText:   q.AmountInFormatted,
		AmountInUSD:    q.AmountInUsd,
		MinAmountIn:    q.MinAmountIn,
		AmountOut:      q.AmountOut,
		AmountOutText:  q.AmountOutFormatted,
		AmountOutUSD:   q.AmountOutUsd,
		MinAmountOut:   q.MinAmountOut,
		TimeEstimate:   int(q.TimeEstimate),
		CorrelationID:  res.CorrelationId,
		Signature:      res.Signature,
		Request:        req,
	}
	if q.Deadline != nil {
		out.Deadline = *q.Deadline
	}
	return out, nil
}

// apiError keeps 1Click's own message, which says which asset or address it
// did not like, instead of only the HTTP status.
func apiError(err error) error {
	var apiErr *sdk.GenericOpenAPIError
	if errors.As(err, &apiErr) {
		var body struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(apiErr.Body(), &body) == nil && body.Message != "" {
			return fmt.Errorf("1Click: %s", body.Message)
		}
	}
	return fmt.Errorf("1Click: %w", err)
}

func stringOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// positiveInteger reports whether s is a whole decimal number above zero,
// with no sign and no fraction.
func positiveInteger(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	return ok && v.Sign() > 0
}
