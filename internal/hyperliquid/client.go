// Package hyperliquid provides read-only access to Hyperliquid's public Info API.
package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://api.hyperliquid.xyz"
	maxResponseBytes = 4 << 20
	clientTimeout    = 25 * time.Second
)

var allowedOperations = map[string]struct{}{
	"meta":                   {},
	"spotMeta":               {},
	"metaAndAssetCtxs":       {},
	"spotMetaAndAssetCtxs":   {},
	"allMids":                {},
	"l2Book":                 {},
	"candleSnapshot":         {},
	"clearinghouseState":     {},
	"spotClearinghouseState": {},
	"portfolio":              {},
	"openOrders":             {},
	"userFillsByTime":        {},
}

// Client reads Hyperliquid through its fixed public Info endpoint.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for Hyperliquid mainnet.
func New() *Client { return NewAt(defaultBaseURL) }

// NewAt returns a client for a Hyperliquid-compatible endpoint, as tests use.
func NewAt(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: clientTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Info performs one allowlisted public read on the client's configured network.
func (c *Client) Info(ctx context.Context, operation string, params map[string]any) (json.RawMessage, error) {
	return c.info(ctx, c.baseURL, operation, params)
}

// InfoAt performs the same read on another fixed Hyperliquid environment.
// The host uses this only for the documented testnet, never a plugin URL.
func (c *Client) InfoAt(ctx context.Context, baseURL, operation string, params map[string]any) (json.RawMessage, error) {
	return c.info(ctx, strings.TrimRight(baseURL, "/"), operation, params)
}

// info sends the operation as the request's type field together with params.
// The response may be any JSON value.
func (c *Client) info(ctx context.Context, baseURL, operation string, params map[string]any) (json.RawMessage, error) {
	if _, ok := allowedOperations[operation]; !ok {
		return nil, fmt.Errorf("hyperliquid: unsupported info operation %q", operation)
	}
	if _, exists := params["type"]; exists {
		return nil, fmt.Errorf("hyperliquid: params must not contain type")
	}

	body := make(map[string]any, len(params)+1)
	for key, value := range params {
		body[key] = value
	}
	body["type"] = operation
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: encode %s request: %w", operation, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/info", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: create %s request: %w", operation, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: %s: %w", operation, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid: %s: unexpected HTTP status %s", operation, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: read %s response: %w", operation, err)
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("hyperliquid: %s response exceeds %d bytes", operation, maxResponseBytes)
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("hyperliquid: %s response is not valid JSON", operation)
	}
	return json.RawMessage(data), nil
}
