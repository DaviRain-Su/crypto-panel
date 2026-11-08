// Package rpc is the JSON-RPC plumbing both chain families share: one
// request at a time per endpoint, a short cache, a size cap, a timeout and
// an audit log. The evm and solana packages pin their networks on top.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/davirain-su/crypto-panel/internal/version"
)

// Host is the endpoint's host, for logs and errors. A URL may carry an API
// key in its path or query, so the audit log never keeps the whole URL.
func Host(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}

// Options tune a Broker. The zero value is usable.
type Options struct {
	// Client makes the requests; nil uses http.DefaultClient.
	Client *http.Client
	// Timeout bounds one request (default 10s).
	Timeout time.Duration
	// CacheTTL keeps an answer for this long (default 15s); negative
	// disables caching.
	CacheTTL time.Duration
	// MinInterval spaces requests to one endpoint (default 50ms), so a
	// panel with many addresses does not burst a public endpoint;
	// negative sends every request at once.
	MinInterval time.Duration
	// Now is the clock, for tests; nil uses time.Now.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.CacheTTL < 0 {
		o.CacheTTL = 0
	} else if o.CacheTTL == 0 {
		o.CacheTTL = 15 * time.Second
	}
	if o.MinInterval < 0 {
		o.MinInterval = 0
	} else if o.MinInterval == 0 {
		o.MinInterval = 50 * time.Millisecond
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// AuditEntry is one request the broker made.
type AuditEntry struct {
	Time     time.Time
	Endpoint string
	Method   string
	Duration time.Duration
	Err      string
}

// Error is an error the endpoint answered with.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if len(e.Data) > 0 && string(e.Data) != "null" {
		return fmt.Sprintf("RPC error %d: %s: %s", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// Broker owns every request to one endpoint: it caches answers briefly,
// spaces requests, caps the answer size and keeps an audit log.
type Broker struct {
	opts Options

	mu    sync.Mutex
	cache map[string]cacheEntry
	last  map[string]time.Time
	audit []AuditEntry
}

const (
	maxResponseBytes = 8 << 20
	maxAuditEntries  = 200
	maxCacheEntries  = 1024
)

type cacheEntry struct {
	result  json.RawMessage
	expires time.Time
}

// NewBroker returns a broker with opts, or its defaults.
func NewBroker(opts Options) *Broker {
	return &Broker{
		opts:  opts.withDefaults(),
		cache: make(map[string]cacheEntry),
		last:  make(map[string]time.Time),
	}
}

// Call makes one JSON-RPC call to the endpoint at rawURL. Answers are
// cached for CacheTTL.
func (b *Broker) Call(ctx context.Context, rawURL, method string, params any) (json.RawMessage, error) {
	return b.CallWithTimeout(ctx, rawURL, method, params, 0)
}

// CallWithTimeout is Call with its own timeout, for a method that waits on
// the chain, such as a transaction broadcast. A zero timeout uses the
// broker's.
func (b *Broker) CallWithTimeout(ctx context.Context, rawURL, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if result, ok := b.cached(rawURL, method, params); ok {
		return result, nil
	}
	if err := b.wait(ctx, rawURL); err != nil {
		return nil, err
	}
	start := b.opts.Now()
	result, err := b.do(ctx, rawURL, method, params, timeout)
	b.record(rawURL, method, b.opts.Now().Sub(start), err)
	if err != nil {
		return nil, err
	}
	b.store(rawURL, method, params, result)
	return result, nil
}

// Audit returns the recent requests, oldest first.
func (b *Broker) Audit() []AuditEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]AuditEntry, len(b.audit))
	copy(out, b.audit)
	return out
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *Error          `json:"error"`
}

func (b *Broker) do(ctx context.Context, rawURL, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	payload, err := json.Marshal(request{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if timeout <= 0 {
		timeout = b.opts.Timeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Host(rawURL), err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-panel/"+version.Version)
	resp, err := b.opts.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", Host(rawURL), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s: reading the answer: %w", Host(rawURL), err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%s: answer larger than %d bytes", Host(rawURL), maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		// Some endpoints answer a refusal with a 4xx status and a JSON-RPC
		// error body; that body says more than the status line.
		var refusal response
		if err := json.Unmarshal(body, &refusal); err == nil && refusal.Error != nil {
			return nil, fmt.Errorf("%s: %s: %w", Host(rawURL), method, refusal.Error)
		}
		return nil, fmt.Errorf("%s: HTTP %s", Host(rawURL), resp.Status)
	}
	var out response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %s: unreadable answer: %w", Host(rawURL), method, err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s: %s: %w", Host(rawURL), method, out.Error)
	}
	if out.Result == nil {
		return nil, fmt.Errorf("%s: %s: empty result", Host(rawURL), method)
	}
	return out.Result, nil
}

func (b *Broker) wait(ctx context.Context, endpoint string) error {
	b.mu.Lock()
	now := b.opts.Now()
	next := b.last[endpoint]
	wait := time.Duration(0)
	if now.Before(next) {
		wait = next.Sub(now)
		b.last[endpoint] = next.Add(b.opts.MinInterval)
	} else {
		b.last[endpoint] = now.Add(b.opts.MinInterval)
	}
	b.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (b *Broker) cacheKey(rawURL, method string, params any) string {
	p, _ := json.Marshal(params)
	return rawURL + "\x00" + method + "\x00" + string(p)
}

func (b *Broker) cached(rawURL, method string, params any) (json.RawMessage, bool) {
	key := b.cacheKey(rawURL, method, params)
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.cache[key]
	if !ok {
		return nil, false
	}
	if !b.opts.Now().Before(entry.expires) {
		delete(b.cache, key)
		return nil, false
	}
	out := make(json.RawMessage, len(entry.result))
	copy(out, entry.result)
	return out, true
}

func (b *Broker) store(rawURL, method string, params any, result json.RawMessage) {
	key := b.cacheKey(rawURL, method, params)
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.cache) >= maxCacheEntries {
		now := b.opts.Now()
		for k, entry := range b.cache {
			if !now.Before(entry.expires) {
				delete(b.cache, k)
			}
		}
		if len(b.cache) >= maxCacheEntries {
			clear(b.cache)
		}
	}
	b.cache[key] = cacheEntry{result: result, expires: b.opts.Now().Add(b.opts.CacheTTL)}
}

func (b *Broker) record(rawURL, method string, d time.Duration, err error) {
	entry := AuditEntry{
		Time:     b.opts.Now(),
		Endpoint: Host(rawURL),
		Method:   method,
		Duration: d,
	}
	if err != nil {
		entry.Err = err.Error()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.audit = append(b.audit, entry)
	if len(b.audit) > maxAuditEntries {
		b.audit = append(b.audit[:0], b.audit[len(b.audit)-maxAuditEntries:]...)
	}
}
