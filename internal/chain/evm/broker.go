// Package evm reads EVM networks: the broker that pins chain ids, the typed
// reads the panel shows, and the parsing of JSON-RPC quantities. Every
// eip155 network speaks this dialect.
package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"

	"github.com/davirain-su/crypto-panel/internal/chain/rpc"
)

// Endpoint is one RPC endpoint and the chain it must serve. ChainID is
// pinned: the broker asks the endpoint for eth_chainId the first time and
// refuses every later call if the answer is not this id (design §8).
type Endpoint struct {
	URL     string
	ChainID uint64
}

// Host is the endpoint's host, for logs and errors.
func (e Endpoint) Host() string {
	u, err := url.Parse(e.URL)
	if err != nil || u.Host == "" {
		return e.URL
	}
	return u.Host
}

// Options tune a Broker. The zero value is usable.
type Options = rpc.Options

// AuditEntry is one request the broker made.
type AuditEntry = rpc.AuditEntry

// RPCError is an error the endpoint answered with.
type RPCError = rpc.Error

// Broker owns every RPC request the app makes to one EVM network: it pins
// the endpoint's chain id, caches answers briefly, spaces requests, and
// keeps an audit log.
type Broker struct {
	inner *rpc.Broker

	mu       sync.Mutex
	verified map[string]uint64
}

// NewBroker returns a broker with opts, or its defaults.
func NewBroker(opts Options) *Broker {
	return &Broker{
		inner:    rpc.NewBroker(opts),
		verified: make(map[string]uint64),
	}
}

// Call makes one JSON-RPC call to ep, after checking that ep serves the
// chain it claims. Answers are cached for CacheTTL.
func (b *Broker) Call(ctx context.Context, ep Endpoint, method string, params any) (json.RawMessage, error) {
	if err := b.verify(ctx, ep); err != nil {
		return nil, err
	}
	return b.inner.Call(ctx, ep.URL, method, params)
}

// Audit returns the recent requests, oldest first.
func (b *Broker) Audit() []AuditEntry { return b.inner.Audit() }

// ChainMismatchError reports an endpoint that serves another chain than the
// one the user chose.
type ChainMismatchError struct {
	Endpoint string
	Got      uint64
	Want     uint64
}

func (e *ChainMismatchError) Error() string {
	return fmt.Sprintf("%s serves chain %d, but the panel is set to chain %d", e.Endpoint, e.Got, e.Want)
}

func (b *Broker) verify(ctx context.Context, ep Endpoint) error {
	if ep.ChainID == 0 {
		return nil
	}
	b.mu.Lock()
	got, ok := b.verified[ep.URL]
	b.mu.Unlock()
	if ok {
		if got != ep.ChainID {
			return &ChainMismatchError{Endpoint: ep.Host(), Got: got, Want: ep.ChainID}
		}
		return nil
	}
	raw, err := b.inner.Call(ctx, ep.URL, "eth_chainId", []any{})
	if err != nil {
		return fmt.Errorf("checking %s: %w", ep.Host(), err)
	}
	id, err := ParseQuantity(raw)
	if err != nil {
		return fmt.Errorf("checking %s: %w", ep.Host(), err)
	}
	got = id.Uint64()
	b.mu.Lock()
	b.verified[ep.URL] = got
	b.mu.Unlock()
	if got != ep.ChainID {
		return &ChainMismatchError{Endpoint: ep.Host(), Got: got, Want: ep.ChainID}
	}
	return nil
}
