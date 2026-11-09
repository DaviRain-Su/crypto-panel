// Package near reads a NEAR network through its JSON-RPC. It is the
// chain.Reader the app builds for a near network: balances, the latest
// block, and the raw calls a plugin's host.rpc may make. Signing lives in
// internal/neartx, so a reader never touches a key.
package near

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chain/rpc"
	"github.com/davirain-su/crypto-panel/internal/neartx"
)

// broadcastTimeout bounds a transaction broadcast, which waits for the
// transaction's final outcome on chain.
const broadcastTimeout = 90 * time.Second

// Client reads one NEAR network through a Broker.
type Client struct {
	network  chain.Network
	endpoint string
	broker   *rpc.Broker
}

// NewClient returns a reader for network at rpcURL. An empty rpcURL uses the
// network's default endpoint.
func NewClient(network chain.Network, rpcURL string) *Client {
	if rpcURL == "" {
		rpcURL = network.RPCURL
	}
	return &Client{
		network:  network,
		endpoint: rpcURL,
		// nonce、近期区块和广播结果均不能复用缓存。
		broker: rpc.NewBroker(rpc.Options{CacheTTL: -1}),
	}
}

// Network returns the network this client reads.
func (c *Client) Network() chain.Network { return c.network }

// Endpoint returns the endpoint in use.
func (c *Client) Endpoint() string { return c.endpoint }

// Broker returns the broker behind the client, for its audit log.
func (c *Client) Broker() *rpc.Broker { return c.broker }

// RPC 执行经过网络校验、限流和审计且不使用缓存的调用。
// 广播方法使用较长超时，等待链上交易结果。
func (c *Client) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return c.RPCWithTimeout(ctx, method, params, 0)
}

// RPCWithTimeout 允许为等待链上结果的方法指定超时；非正值使用该方法的默认超时。
func (c *Client) RPCWithTimeout(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	// status 的响应本身用于校验，避免它再次调用 Verify 形成递归。
	if method != "status" {
		if err := c.Verify(ctx); err != nil {
			return nil, err
		}
	}
	if timeout <= 0 {
		switch method {
		case "broadcast_tx_commit", "broadcast_tx_async", "send_tx":
			timeout = broadcastTimeout
		}
	}
	raw, err := c.broker.CallWithTimeout(ctx, c.endpoint, method, params, timeout)
	if err != nil {
		return nil, err
	}
	if method == "status" {
		if err := c.verifyStatus(raw); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// Block reads the latest final block's height.
func (c *Client) Block(ctx context.Context) (uint64, error) {
	raw, err := c.RPC(ctx, "block", map[string]any{"finality": "final"})
	if err != nil {
		return 0, err
	}
	var out struct {
		Header struct {
			Height uint64 `json:"height"`
		} `json:"header"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("%s: block: unreadable block: %w", rpc.Host(c.endpoint), err)
	}
	return out.Header.Height, nil
}

// Account reads an account's native NEAR balance. An implicit account that
// has not been funded yet does not exist on chain; that is a zero balance
// with a warning, not a failed refresh.
func (c *Client) Account(ctx context.Context, addr string) (chain.AccountBalances, error) {
	out := chain.AccountBalances{Address: addr}
	raw, err := c.RPC(ctx, "query", map[string]any{
		"request_type": "view_account",
		"finality":     "final",
		"account_id":   addr,
	})
	if err != nil {
		if unknownAccount(err) {
			out.Native = new(big.Int)
			out.Warnings = append(out.Warnings, "this account is not on chain yet; fund it to activate it")
			return out, nil
		}
		return out, err
	}
	var view struct {
		Amount       string `json:"amount"`
		Locked       string `json:"locked"`
		CodeHash     string `json:"code_hash"`
		StorageUsage uint64 `json:"storage_usage"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		return out, fmt.Errorf("%s: view_account: unreadable account: %w", rpc.Host(c.endpoint), err)
	}
	amount, ok := new(big.Int).SetString(view.Amount, 10)
	if !ok {
		return out, fmt.Errorf("%s: view_account: unreadable balance %q", rpc.Host(c.endpoint), view.Amount)
	}
	out.Native = amount
	return out, nil
}

// Verify checks that the endpoint serves the network the settings name.
func (c *Client) Verify(ctx context.Context) error {
	// 直接经 Broker 查询，不缓存校验结果；失败或取消后，下次仍会重新校验。
	raw, err := c.broker.Call(ctx, c.endpoint, "status", []any{})
	if err != nil {
		return err
	}
	return c.verifyStatus(raw)
}

func (c *Client) verifyStatus(raw json.RawMessage) error {
	var status struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("%s: status: unreadable chain id: %w", rpc.Host(c.endpoint), err)
	}
	if c.network.ChainName == "" {
		return fmt.Errorf("%s: the selected NEAR network has no chain id", rpc.Host(c.endpoint))
	}
	if status.ChainID != c.network.ChainName {
		return fmt.Errorf("%s serves NEAR %q, but %s (%s) is selected", rpc.Host(c.endpoint), status.ChainID, c.network.Name, c.network.ChainName)
	}
	return nil
}

// Send broadcasts a signed transaction and waits for its final outcome.
func (c *Client) Send(ctx context.Context, signed []byte, expected [32]byte) (string, error) {
	return neartx.Send(ctx, c.RPC, signed, expected)
}

// unknownAccount reports the RPC error a view_account call answers for an
// account that has never been funded.
func unknownAccount(err error) bool {
	s := err.Error()
	return strings.Contains(s, "UNKNOWN_ACCOUNT") || strings.Contains(s, "does not exist while viewing")
}

var _ chain.Reader = (*Client)(nil)
