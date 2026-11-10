// Package prices reads approximate USD prices from CoinGecko's public API.
//
// The app uses prices only to annotate balances: a balance is always shown
// in its own asset first, and a fiat value is a hint, never the number the
// user acts on. The client keeps a short cache so the refresh loop does not
// spend the public API's rate limit, and it never sends an address, a key or
// anything else about the user: only a list of asset ids.
package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultURL is CoinGecko's simple price endpoint. It needs no key.
const DefaultURL = "https://api.coingecko.com/api/v3/simple/price"

// DefaultTTL is how long one answer stays fresh. The app refreshes balances
// once a minute by default, and CoinGecko's public tier allows a few calls a
// minute, so one call per refresh is the budget.
const DefaultTTL = 60 * time.Second

// ids maps the symbols the app shows to CoinGecko's asset ids. A symbol
// that is not here has no fiat value, and the app says nothing rather than
// guessing at a lookalike token.
var ids = map[string]string{
	"ETH":   "ethereum",
	"WETH":  "weth",
	"BNB":   "binancecoin",
	"POL":   "polygon-ecosystem-token",
	"AVAX":  "avalanche-2",
	"SOL":   "solana",
	"NEAR":  "near",
	"USDC":  "usd-coin",
	"USDT":  "tether",
	"DAI":   "dai",
	"CBETH": "coinbase-wrapped-staked-eth",
	"JUP":   "jupiter-exchange-solana",
	"BONK":  "bonk",
}

// Client reads prices and caches them for TTL.
type Client struct {
	URL  string
	HTTP *http.Client
	TTL  time.Duration
	Now  func() time.Time

	mu    sync.Mutex
	cache map[string]cachedPrice
}

type cachedPrice struct {
	usd float64
	at  time.Time
}

// New returns a client for the public CoinGecko endpoint.
func New() *Client {
	return &Client{
		URL:  DefaultURL,
		HTTP: &http.Client{Timeout: 12 * time.Second},
		TTL:  DefaultTTL,
		Now:  time.Now,
	}
}

// Known reports whether the client can price symbol.
func Known(symbol string) bool {
	_, ok := ids[strings.ToUpper(strings.TrimSpace(symbol))]
	return ok
}

// Prices returns the USD price of each known symbol. Unknown symbols are
// left out; when none is known, it returns an empty map without a request.
// A cached answer younger than TTL is used without a request.
func (c *Client) Prices(ctx context.Context, symbols []string) (map[string]float64, error) {
	out := make(map[string]float64)
	byID := make(map[string][]string)
	now := c.now()
	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[string]cachedPrice)
	}
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		id, ok := ids[symbol]
		if !ok {
			continue
		}
		if cached, ok := c.cache[symbol]; ok && now.Sub(cached.at) < c.ttl() {
			out[symbol] = cached.usd
			continue
		}
		byID[id] = append(byID[id], symbol)
	}
	c.mu.Unlock()
	if len(byID) == 0 {
		return out, nil
	}
	assetIDs := make([]string, 0, len(byID))
	for id := range byID {
		assetIDs = append(assetIDs, id)
	}
	sort.Strings(assetIDs)

	raw, err := c.get(ctx, assetIDs)
	if err != nil {
		return out, err
	}
	c.mu.Lock()
	for id, symbols := range byID {
		price, ok := raw[id]["usd"]
		if !ok {
			continue
		}
		for _, symbol := range symbols {
			out[symbol] = price
			c.cache[symbol] = cachedPrice{usd: price, at: now}
		}
	}
	c.mu.Unlock()
	return out, nil
}

// PricesByID returns prices keyed by the requested price-feed asset IDs.
// Unlike Prices, it does not use ticker symbols, so distinct assets that
// share a symbol cannot borrow each other's value.
func (c *Client) PricesByID(ctx context.Context, assetIDs []string) (map[string]float64, error) {
	knownIDs := make(map[string]bool, len(ids))
	for _, id := range ids {
		knownIDs[id] = true
	}
	unique := make(map[string]bool)
	var wanted []string
	for _, id := range assetIDs {
		id = strings.TrimSpace(id)
		if knownIDs[id] && !unique[id] {
			unique[id] = true
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return map[string]float64{}, nil
	}
	sort.Strings(wanted)
	raw, err := c.get(ctx, wanted)
	if err != nil {
		return map[string]float64{}, err
	}
	out := make(map[string]float64, len(wanted))
	for _, id := range wanted {
		if price, ok := raw[id]["usd"]; ok {
			out[id] = price
		}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, assetIDs []string) (map[string]map[string]float64, error) {
	base := c.URL
	if base == "" {
		base = DefaultURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("prices: %w", err)
	}
	q := u.Query()
	q.Set("ids", strings.Join(assetIDs, ","))
	q.Set("vs_currencies", "usd")
	u.RawQuery = q.Encode()

	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("prices: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "lintel")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prices: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("prices: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prices: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out map[string]map[string]float64
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("prices: decoding response: %w", err)
	}
	if out == nil {
		return nil, errors.New("prices: empty response")
	}
	return out, nil
}

func (c *Client) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return DefaultTTL
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
