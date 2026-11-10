package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/davirain-su/lintel/internal/chain"
)

// The two token programs whose accounts the app reads. Token-2022 mints
// are as common as classic SPL ones now, so both are asked.
const (
	TokenProgram     = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	Token2022Program = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
)

// Client reads one Solana cluster through a Broker. It is the chain.Reader
// the app builds for a solana network.
type Client struct {
	network  chain.Network
	endpoint Endpoint
	broker   *Broker
}

// NewClient returns a reader for network at rpcURL. An empty rpcURL uses
// the network's default endpoint.
func NewClient(network chain.Network, rpcURL string) *Client {
	if rpcURL == "" {
		rpcURL = network.RPCURL
	}
	return &Client{
		network:  network,
		endpoint: Endpoint{URL: rpcURL, Genesis: network.Genesis},
		broker:   NewBroker(Options{}),
	}
}

// Network returns the network this client reads.
func (c *Client) Network() chain.Network { return c.network }

// Endpoint returns the pinned endpoint in use.
func (c *Client) Endpoint() Endpoint { return c.endpoint }

// Broker returns the broker behind the client, for its audit log.
func (c *Client) Broker() *Broker { return c.broker }

// RPC makes one pinned, cached, limited and audited call.
func (c *Client) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.broker.Call(ctx, c.endpoint, method, params)
}

// Block reads the endpoint's latest slot.
func (c *Client) Block(ctx context.Context) (uint64, error) {
	return c.broker.Slot(ctx, c.endpoint)
}

// Account reads an address's native balance and the network's default
// tokens. A token program that fails is a warning, not a failure of the
// account: one misbehaving endpoint must not hide the rest.
func (c *Client) Account(ctx context.Context, addr string) (chain.AccountBalances, error) {
	return c.broker.Account(ctx, c.endpoint, c.network, addr)
}

// AccountExists reports whether an account exists on the cluster. An
// address the program has never seen answers value null; that is not an
// error, it is an account that is not there yet.
func (c *Client) AccountExists(ctx context.Context, addr string) (bool, error) {
	return c.broker.AccountExists(ctx, c.endpoint, addr)
}

// AccountExists reports whether an account exists on the cluster.
func (b *Broker) AccountExists(ctx context.Context, ep Endpoint, addr string) (bool, error) {
	raw, err := b.Call(ctx, ep, "getAccountInfo", []any{
		addr,
		map[string]string{"encoding": "base64", "commitment": "confirmed"},
	})
	if err != nil {
		return false, err
	}
	// A missing account is {"value": null}. A null result, an empty object,
	// or an object without value is not that answer: the check failed.
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return false, fmt.Errorf("%s: getAccountInfo: missing account value", ep.Host())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil || fields == nil {
		return false, fmt.Errorf("%s: getAccountInfo: unreadable answer", ep.Host())
	}
	value, ok := fields["value"]
	if !ok || len(bytes.TrimSpace(value)) == 0 {
		return false, fmt.Errorf("%s: getAccountInfo: missing account value", ep.Host())
	}
	if string(bytes.TrimSpace(value)) == "null" {
		return false, nil
	}
	return true, nil
}

// Slot reads the endpoint's latest slot.
func (b *Broker) Slot(ctx context.Context, ep Endpoint) (uint64, error) {
	raw, err := b.Call(ctx, ep, "getSlot", []any{map[string]string{"commitment": "confirmed"}})
	if err != nil {
		return 0, err
	}
	var slot uint64
	if err := json.Unmarshal(raw, &slot); err != nil {
		return 0, fmt.Errorf("%s: getSlot: unreadable slot: %w", ep.Host(), err)
	}
	return slot, nil
}

// NativeBalance reads an address's lamport balance.
func (b *Broker) NativeBalance(ctx context.Context, ep Endpoint, addr string) (*big.Int, error) {
	raw, err := b.Call(ctx, ep, "getBalance", []any{addr, map[string]string{"commitment": "confirmed"}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: getBalance: unreadable balance: %w", ep.Host(), err)
	}
	return new(big.Int).SetUint64(out.Value), nil
}

// tokenAccounts is the jsonParsed answer of getTokenAccountsByOwner, cut
// down to the fields the app reads.
type tokenAccounts struct {
	Value []struct {
		Account struct {
			Data struct {
				Parsed struct {
					Info struct {
						Mint        string `json:"mint"`
						TokenAmount struct {
							Amount string `json:"amount"`
						} `json:"tokenAmount"`
					} `json:"info"`
				} `json:"parsed"`
			} `json:"data"`
		} `json:"account"`
	} `json:"value"`
}

// tokenBalances reads every token account an address owns under one token
// program and sums the amounts by mint.
func (b *Broker) tokenBalances(ctx context.Context, ep Endpoint, program, owner string) (map[string]*big.Int, error) {
	raw, err := b.Call(ctx, ep, "getTokenAccountsByOwner", []any{
		owner,
		map[string]string{"programId": program},
		map[string]string{"encoding": "jsonParsed", "commitment": "confirmed"},
	})
	if err != nil {
		return nil, err
	}
	var out tokenAccounts
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: getTokenAccountsByOwner: unreadable accounts: %w", ep.Host(), err)
	}
	byMint := make(map[string]*big.Int, len(out.Value))
	for _, entry := range out.Value {
		info := entry.Account.Data.Parsed.Info
		// A Token-2022 account with an extension this endpoint cannot parse
		// comes back without parsed info; skip it rather than lose the rest.
		if info.Mint == "" || info.TokenAmount.Amount == "" {
			continue
		}
		amount, ok := new(big.Int).SetString(info.TokenAmount.Amount, 10)
		if !ok {
			continue
		}
		if held, ok := byMint[info.Mint]; ok {
			held.Add(held, amount)
		} else {
			byMint[info.Mint] = amount
		}
	}
	return byMint, nil
}

// Account reads an address's native balance and every token in the
// network's list.
func (b *Broker) Account(ctx context.Context, ep Endpoint, network chain.Network, addr string) (chain.AccountBalances, error) {
	out := chain.AccountBalances{Address: addr}
	native, err := b.NativeBalance(ctx, ep, addr)
	if err != nil {
		return out, err
	}
	out.Native = native

	held := make(map[string]*big.Int)
	for _, program := range []string{TokenProgram, Token2022Program} {
		amounts, err := b.tokenBalances(ctx, ep, program, addr)
		if err != nil {
			if indexedRequest(err) {
				// Public endpoints often refuse token-account queries as
				// "indexed requests"; say so once, in words that suggest the
				// fix, instead of repeating the provider's message twice.
				out.Warnings = append(out.Warnings, "this endpoint does not serve token accounts; set a custom RPC to see token balances")
				break
			}
			out.Warnings = append(out.Warnings, err.Error())
			continue
		}
		for mint, amount := range amounts {
			if sum, ok := held[mint]; ok {
				sum.Add(sum, amount)
			} else {
				held[mint] = amount
			}
		}
	}
	for _, token := range network.Tokens {
		if amount, ok := held[token.Address]; ok {
			out.Tokens = append(out.Tokens, chain.TokenBalance{Token: token, Amount: amount})
		}
	}
	return out, nil
}

// indexedRequest reports an endpoint that refuses token-account queries as
// an indexed request, which free public endpoints commonly do.
func indexedRequest(err error) bool {
	s := err.Error()
	return strings.Contains(s, "Indexed requests require") || strings.Contains(s, "Request blocked")
}
