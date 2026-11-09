package evm

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"

	"github.com/davirain-su/lintel/internal/chain"
)

// balanceOf(address) selector: the first four bytes of its Keccak-256 hash.
const balanceOfSelector = "0x70a08231"

// Client reads one EVM network through a Broker. It is the chain.Reader the
// app builds for an eip155 network.
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
		endpoint: Endpoint{URL: rpcURL, ChainID: network.ChainID},
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

// Block reads the endpoint's latest block number.
func (c *Client) Block(ctx context.Context) (uint64, error) {
	return c.broker.BlockNumber(ctx, c.endpoint)
}

// Account reads an address's native balance and every token in the
// network's list. A token that fails is a warning, not a failure of the
// account: one misbehaving contract must not hide the rest.
func (c *Client) Account(ctx context.Context, addr string) (chain.AccountBalances, error) {
	return c.broker.Account(ctx, c.endpoint, c.network, addr)
}

// NativeBalance reads the network's own coin for addr, in wei.
func (b *Broker) NativeBalance(ctx context.Context, ep Endpoint, addr string) (*big.Int, error) {
	raw, err := b.Call(ctx, ep, "eth_getBalance", []any{addr, "latest"})
	if err != nil {
		return nil, err
	}
	return ParseQuantity(raw)
}

// TokenBalance reads an ERC-20's balanceOf(holder).
func (b *Broker) TokenBalance(ctx context.Context, ep Endpoint, token, holder string) (*big.Int, error) {
	holder = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(holder)), "0x")
	data := balanceOfSelector + strings.Repeat("0", 24) + holder
	raw, err := b.Call(ctx, ep, "eth_call", []any{
		map[string]string{"to": strings.ToLower(token), "data": data},
		"latest",
	})
	if err != nil {
		return nil, err
	}
	return ParseUint256(raw)
}

// BlockNumber reads the endpoint's latest block.
func (b *Broker) BlockNumber(ctx context.Context, ep Endpoint) (uint64, error) {
	raw, err := b.Call(ctx, ep, "eth_blockNumber", []any{})
	if err != nil {
		return 0, err
	}
	n, err := ParseQuantity(raw)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
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
	for _, token := range network.Tokens {
		amount, err := b.TokenBalance(ctx, ep, token.Address, addr)
		if err != nil {
			out.Warnings = append(out.Warnings, token.Symbol+": "+err.Error())
			continue
		}
		out.Tokens = append(out.Tokens, chain.TokenBalance{Token: token, Amount: amount})
	}
	return out, nil
}
