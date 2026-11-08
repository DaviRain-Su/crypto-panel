// Package solana reads Solana networks: the broker that pins an endpoint by
// its genesis hash, the typed reads the panel shows, and the parsing of
// lamports and SPL token accounts. Solana is the second chain family the
// panel speaks, beside EVM.
package solana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"

	"github.com/davirain-su/crypto-panel/internal/chain/rpc"
)

// Endpoint is one RPC endpoint and the cluster it must serve. Genesis is
// pinned: the broker asks the endpoint for getGenesisHash the first time
// and refuses every later call if the answer is not this hash (design §8).
type Endpoint struct {
	URL     string
	Genesis string
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

// Broker owns every RPC request the app makes to one Solana cluster: it
// pins the endpoint's genesis hash, caches answers briefly, spaces
// requests, and keeps an audit log.
type Broker struct {
	inner *rpc.Broker

	mu       sync.Mutex
	verified map[string]string
}

// NewBroker returns a broker with opts, or its defaults.
func NewBroker(opts Options) *Broker {
	return &Broker{
		inner:    rpc.NewBroker(opts),
		verified: make(map[string]string),
	}
}

// Call makes one JSON-RPC call to ep, after checking that ep serves the
// cluster it claims. Answers are cached for CacheTTL.
func (b *Broker) Call(ctx context.Context, ep Endpoint, method string, params any) (json.RawMessage, error) {
	if err := b.verify(ctx, ep); err != nil {
		return nil, err
	}
	return b.inner.Call(ctx, ep.URL, method, params)
}

// Audit returns the recent requests, oldest first.
func (b *Broker) Audit() []AuditEntry { return b.inner.Audit() }

// GenesisMismatchError reports an endpoint that serves another cluster than
// the one the user chose.
type GenesisMismatchError struct {
	Endpoint string
	Got      string
	Want     string
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("%s serves genesis %s, but the panel is set to %s", e.Endpoint, e.Got, e.Want)
}

func (b *Broker) verify(ctx context.Context, ep Endpoint) error {
	if ep.Genesis == "" {
		return nil
	}
	b.mu.Lock()
	got, ok := b.verified[ep.URL]
	b.mu.Unlock()
	if ok {
		if got != ep.Genesis {
			return &GenesisMismatchError{Endpoint: ep.Host(), Got: got, Want: ep.Genesis}
		}
		return nil
	}
	raw, err := b.inner.Call(ctx, ep.URL, "getGenesisHash", []any{})
	if err != nil {
		return fmt.Errorf("checking %s: %w", ep.Host(), err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("checking %s: unreadable genesis hash: %w", ep.Host(), err)
	}
	b.mu.Lock()
	b.verified[ep.URL] = got
	b.mu.Unlock()
	if got != ep.Genesis {
		return &GenesisMismatchError{Endpoint: ep.Host(), Got: got, Want: ep.Genesis}
	}
	return nil
}
