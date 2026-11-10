// Package intents talks to NEAR Intents through 1Click's public HTTP API.
//
// The calls here are hand-rolled rather than made through the official
// one-click-sdk-go: the SDK decodes responses with unknown fields
// disallowed, so any field 1Click adds — coingeckoId did — breaks the app
// until a new SDK release. Plain json.Unmarshal ignores fields it has not
// heard of, which is the right posture for a moving public API.
//
// This build only ever asks for dry quotes: a dry quote prices the swap but
// issues no deposit address, so no funds can move. A real swap needs a
// separate, deliberate path with the user's explicit consent, and is not
// part of this slice.
package intents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxTokenDecimals matches chain.MaxTokenDecimals. A larger value is not a
// token this client will hand to ParseUnits.
const maxTokenDecimals = 255

// maxResponseBytes is the most of a 1Click body this client will read.
const maxResponseBytes = 4 << 20

// clientTimeout is the HTTP client's own deadline, inside the caller's context.
const clientTimeout = 25 * time.Second

// DefaultBaseURL is 1Click's public API. It has no testnet.
const DefaultBaseURL = "https://1click.chaindefuser.com"

// DefaultSlippageBps is 1%, in basis points.
const DefaultSlippageBps = 100

// quoteLifetime is how long the user has to deposit before a wet quote
// would expire. A dry quote only carries it as a request condition.
const quoteLifetime = 30 * time.Minute

// Client is a 1Click client.
type Client struct {
	baseURL string
	http    *http.Client
	now     func() time.Time
	key     string
}

// New returns a client for the public 1Click API.
func New() *Client { return NewAt(DefaultBaseURL) }

// NewAt returns a client for another 1Click-compatible endpoint, as tests
// use. Alternate endpoints still have to use the production signing key.
func NewAt(baseURL string) *Client {
	return newClient(baseURL, time.Now, productionManagerPublicKey)
}

// Option supplies deterministic verifier configuration for integration tests.
// Verification is always enabled; no option can skip signature or freshness
// checks.
type Option func(*Client)

// WithClock sets the clock used for freshness checks and default deadlines.
// It is intended for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// WithVerifierKey sets the Ed25519 manager public key. It is intended for
// signed integration fixtures; production callers should use New or NewAt.
func WithVerifierKey(publicKey string) Option {
	return func(c *Client) {
		if publicKey != "" {
			c.key = publicKey
		}
	}
}

// NewAtWithOptions returns a client with explicit verifier configuration for
// integration tests. Omitting options is identical to NewAt.
func NewAtWithOptions(baseURL string, options ...Option) *Client {
	c := NewAt(baseURL)
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
	return c
}

func newClient(baseURL string, now func() time.Time, publicKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		now:     now,
		key:     publicKey,
		http: &http.Client{
			Timeout: clientTimeout,
			// A redirect would replay the quote, including recipient and
			// refundTo, at whatever host answered. Keep the response and
			// refuse it below.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Token is one asset 1Click can swap.
type Token struct {
	AssetID    string  `json:"assetId"`
	Symbol     string  `json:"symbol"`
	Blockchain string  `json:"blockchain"`
	Decimals   int     `json:"decimals"`
	Price      float64 `json:"price"`
	IconURL    string  `json:"icon"`
}

// QuoteRequest is one swap the user is asking about. Amount is in the
// origin asset's smallest unit.
type QuoteRequest struct {
	OriginAsset      string `json:"originAsset"`
	DestinationAsset string `json:"destinationAsset"`
	Amount           string `json:"amount"`
	Recipient        string `json:"recipient"`
	RefundTo         string `json:"refundTo"`
	SlippageBps      int    `json:"slippageBps"`
	// Deadline is when a deposit would stop being accepted. Zero means the
	// default, quoteLifetime from now.
	Deadline time.Time `json:"deadline"`
}

// Quote is 1Click's answer, in the shell's own shape.
type Quote struct {
	DepositAddress string       `json:"depositAddress,omitempty"`
	DepositMemo    string       `json:"depositMemo,omitempty"`
	AmountIn       string       `json:"amountIn"`
	AmountInText   string       `json:"amountInFormatted"`
	AmountInUSD    string       `json:"amountInUsd"`
	MinAmountIn    string       `json:"minAmountIn"`
	AmountOut      string       `json:"amountOut"`
	AmountOutText  string       `json:"amountOutFormatted"`
	AmountOutUSD   string       `json:"amountOutUsd"`
	MinAmountOut   string       `json:"minAmountOut"`
	Deadline       time.Time    `json:"deadline"`
	TimeEstimate   int          `json:"timeEstimate"`
	CorrelationID  string       `json:"correlationId"`
	Signature      string       `json:"signature"`
	Timestamp      time.Time    `json:"timestamp"`
	Verified       bool         `json:"verified"`
	Request        QuoteRequest `json:"quoteRequest"`
}

// wireToken is the part of a token on the wire this client reads. 1Click
// may send more fields — coingeckoId, contractAddress — and the decode
// ignores what it does not name.
type wireToken struct {
	AssetID    string          `json:"assetId"`
	Symbol     string          `json:"symbol"`
	Blockchain string          `json:"blockchain"`
	Decimals   json.RawMessage `json:"decimals"`
	Price      float64         `json:"price"`
	IconURL    string          `json:"icon"`
}

// Tokens lists the assets 1Click supports, sorted for display.
func (c *Client) Tokens(ctx context.Context) ([]Token, error) {
	var wire []wireToken
	if err := c.get(ctx, "/v0/tokens", &wire); err != nil {
		return nil, err
	}
	out := make([]Token, 0, len(wire))
	for _, t := range wire {
		tok, ok := acceptToken(t)
		if !ok {
			continue
		}
		out = append(out, tok)
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
	if req.SlippageBps < 0 {
		return Quote{}, errors.New("the slippage tolerance must not be negative")
	}
	if req.SlippageBps == 0 {
		req.SlippageBps = DefaultSlippageBps
	}
	if req.SlippageBps > 1000 {
		return Quote{}, errors.New("the slippage tolerance must be at most 10%")
	}
	if req.Deadline.IsZero() {
		req.Deadline = c.now().Add(quoteLifetime).UTC()
	}

	// Dry is always true here. A wet quote would issue a deposit address,
	// and this build has no path that could fund it.
	body := map[string]any{
		"dry":               true,
		"swapType":          "EXACT_INPUT",
		"slippageTolerance": req.SlippageBps,
		"originAsset":       req.OriginAsset,
		"depositType":       "ORIGIN_CHAIN",
		"destinationAsset":  req.DestinationAsset,
		"amount":            req.Amount,
		"refundTo":          req.RefundTo,
		"refundType":        "ORIGIN_CHAIN",
		"recipient":         req.Recipient,
		"recipientType":     "DESTINATION_CHAIN",
		"deadline":          req.Deadline,
	}
	var raw json.RawMessage
	if err := c.post(ctx, "/v0/quote", body, &raw); err != nil {
		return Quote{}, err
	}
	return decodeQuote(raw, req, c.key, c.now())
}

// acceptToken drops a token the shell must not price. A missing, null,
// negative, fractional, or oversized decimals value is not zero: using zero
// would turn "1" into a single base unit and still open the approval window.
func acceptToken(t wireToken) (Token, bool) {
	if strings.TrimSpace(t.AssetID) == "" || strings.TrimSpace(t.Symbol) == "" || strings.TrimSpace(t.Blockchain) == "" {
		return Token{}, false
	}
	decimals, ok := tokenDecimals(t.Decimals)
	if !ok {
		return Token{}, false
	}
	return Token{
		AssetID:    t.AssetID,
		Symbol:     t.Symbol,
		Blockchain: t.Blockchain,
		Decimals:   decimals,
		Price:      t.Price,
		IconURL:    strings.TrimSpace(t.IconURL),
	}, true
}

func tokenDecimals(raw json.RawMessage) (int, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	if len(raw) > 3 {
		return 0, false
	}
	n, err := strconv.Atoi(string(raw))
	if err != nil || n > maxTokenDecimals {
		return 0, false
	}
	return n, true
}

type wireQuote struct {
	DepositAddress     string     `json:"depositAddress"`
	DepositMemo        string     `json:"depositMemo"`
	AmountIn           string     `json:"amountIn"`
	AmountInFormatted  string     `json:"amountInFormatted"`
	AmountInUsd        string     `json:"amountInUsd"`
	MinAmountIn        string     `json:"minAmountIn"`
	AmountOut          string     `json:"amountOut"`
	AmountOutFormatted string     `json:"amountOutFormatted"`
	AmountOutUsd       string     `json:"amountOutUsd"`
	MinAmountOut       string     `json:"minAmountOut"`
	Deadline           *time.Time `json:"deadline"`
	TimeEstimate       float64    `json:"timeEstimate"`
}

// decodeQuote fails closed. A missing amount, an empty correlation id, a
// null quote, or a deposit address on a dry answer is an error, not a blank
// quote the approval window can show.
func decodeQuote(data []byte, req QuoteRequest, publicKey string, now time.Time) (Quote, error) {
	data = bytes.TrimSpace(data)
	if _, err := verifyQuoteResponse(data, publicKey, now); err != nil {
		return Quote{}, err
	}
	if len(data) == 0 || string(data) == "null" {
		return Quote{}, errors.New("1Click: the quote is missing")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return Quote{}, errors.New("1Click: the quote could not be read")
	}
	qraw, ok := top["quote"]
	if !ok || len(bytes.TrimSpace(qraw)) == 0 || string(bytes.TrimSpace(qraw)) == "null" {
		return Quote{}, errors.New("1Click: the quote is missing")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(qraw, &fields); err != nil || fields == nil {
		return Quote{}, errors.New("1Click: the quote could not be read")
	}
	amountIn, ok := positiveField(fields, "amountIn")
	if !ok {
		return Quote{}, errors.New("1Click: the quote amount is missing")
	}
	amountOut, ok := positiveField(fields, "amountOut")
	if !ok {
		return Quote{}, errors.New("1Click: the quote amount is missing")
	}
	minAmountOut, ok := positiveField(fields, "minAmountOut")
	if !ok {
		return Quote{}, errors.New("1Click: the quote amount is missing")
	}
	if raw, present := fields["depositAddress"]; present && depositAddressPresent(raw) {
		return Quote{}, errors.New("1Click: a dry quote must not include a deposit address")
	}
	correlationID, ok := jsonString(top["correlationId"])
	if !ok || strings.TrimSpace(correlationID) == "" {
		return Quote{}, errors.New("1Click: the quote has no correlation id")
	}
	if err := echoedRequestOK(top["quoteRequest"], req); err != nil {
		return Quote{}, err
	}
	var parsed wireQuote
	if err := json.Unmarshal(qraw, &parsed); err != nil {
		return Quote{}, errors.New("1Click: the quote could not be read")
	}
	timestamp, _ := time.Parse(time.RFC3339Nano, mustJSONString(top["timestamp"]))
	out := Quote{
		DepositMemo:   parsed.DepositMemo,
		AmountIn:      amountIn,
		AmountInText:  parsed.AmountInFormatted,
		AmountInUSD:   parsed.AmountInUsd,
		MinAmountIn:   parsed.MinAmountIn,
		AmountOut:     amountOut,
		AmountOutText: parsed.AmountOutFormatted,
		AmountOutUSD:  parsed.AmountOutUsd,
		MinAmountOut:  minAmountOut,
		TimeEstimate:  int(parsed.TimeEstimate),
		CorrelationID: correlationID,
		Timestamp:     timestamp,
		Verified:      true,
		Request:       req,
	}
	if sig, ok := jsonString(top["signature"]); ok {
		out.Signature = sig
	}
	if parsed.Deadline != nil {
		out.Deadline = *parsed.Deadline
	}
	return out, nil
}

func mustJSONString(raw json.RawMessage) string {
	value, _ := jsonString(raw)
	return value
}

func positiveField(fields map[string]json.RawMessage, key string) (string, bool) {
	s, ok := jsonString(fields[key])
	if !ok || !positiveInteger(s) {
		return "", false
	}
	return s, true
}

func jsonString(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func depositAddressPresent(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return false
	}
	s, ok := jsonString(raw)
	return !ok || s != ""
}

func echoedRequestOK(raw json.RawMessage, req QuoteRequest) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("1Click: the quote does not echo the request")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("1Click: the quote does not echo the request")
	}
	if string(bytes.TrimSpace(fields["dry"])) != "true" {
		return errors.New("1Click: the quote is not a dry quote")
	}
	want := []struct{ key, value string }{
		{"swapType", "EXACT_INPUT"},
		{"depositType", "ORIGIN_CHAIN"},
		{"refundType", "ORIGIN_CHAIN"},
		{"recipientType", "DESTINATION_CHAIN"},
		{"originAsset", req.OriginAsset},
		{"destinationAsset", req.DestinationAsset},
		{"amount", req.Amount},
		{"refundTo", req.RefundTo},
		{"recipient", req.Recipient},
	}
	for _, field := range want {
		got, ok := jsonString(fields[field.key])
		if !ok || got != field.value {
			return errors.New("1Click: the quote does not match the request")
		}
	}
	slipRaw := bytes.TrimSpace(fields["slippageTolerance"])
	var slip int
	if err := json.Unmarshal(slipRaw, &slip); err != nil || slip != req.SlippageBps {
		return errors.New("1Click: the quote does not match the request")
	}
	return nil
}

// get fetches path and decodes the JSON answer into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// post sends body as JSON and decodes the answer into out.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// The live quote route currently returns 201 Created even for dry quotes.
	// Keep all other success statuses rejected and still verify the signed body.
	return c.do(req, out, http.StatusCreated)
}

// do sends the request, keeps 1Click's own error message, and decodes a
// successful answer into out.
func (c *Client) do(req *http.Request, out any, additionalSuccess ...int) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "lintel")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	// 3xx is the redirect we refused to follow; arbitrary 2xx bodies are not
	// this API's success shape. A caller may name one observed route exception.
	success := res.StatusCode == http.StatusOK
	for _, status := range additionalSuccess {
		success = success || res.StatusCode == status
	}
	if !success {
		return apiError(res.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("1Click: %w", err)
	}
	return nil
}

// apiError keeps 1Click's own message, which says which asset or address it
// did not like, instead of only the HTTP status.
func apiError(status int, body []byte) error {
	var wire struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &wire) == nil && wire.Message != "" {
		return fmt.Errorf("1Click: %s", wire.Message)
	}
	return fmt.Errorf("1Click: HTTP %d", status)
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
