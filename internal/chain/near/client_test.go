package near

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davirain-su/crypto-panel/internal/base58"
	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chain/rpc"
	"github.com/davirain-su/crypto-panel/internal/neartx"
)

// The test networks are written out here rather than taken from presets:
// internal/chains builds a near.Client, so importing it here would be a
// cycle.
var (
	testnet = chain.Network{
		ID: "near:testnet", Family: chain.Near, Name: "NEAR Testnet",
		Symbol: "NEAR", Decimals: 24, ChainName: "testnet",
	}
	mainnet = chain.Network{
		ID: "near:mainnet", Family: chain.Near, Name: "NEAR",
		Symbol: "NEAR", Decimals: 24, ChainName: "mainnet",
	}
)

const testnetStatus = `{"jsonrpc":"2.0","id":1,"result":{"chain_id":"testnet"}}`

// serve answers each JSON-RPC method with the body in answers.
func serve(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	return serveRPC(t, func(method string) string { return answers[method] })
}

func serveRPC(t *testing.T, answer func(method string) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		raw := answer(req.Method)
		if raw == "" {
			t.Errorf("unexpected method %q", req.Method)
			http.Error(w, "unexpected method "+req.Method, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, raw)
	}))
}

func TestAccount(t *testing.T) {
	srv := serve(t, map[string]string{
		"status": testnetStatus,
		"query":  `{"jsonrpc":"2.0","id":1,"result":{"amount":"1500000000000000000000000","locked":"0","code_hash":"11111111111111111111111111111111","storage_usage":182}}`,
	})
	defer srv.Close()
	c := NewClient(testnet, srv.URL)
	got, err := c.Account(context.Background(), "alice.testnet")
	if err != nil {
		t.Fatal(err)
	}
	if got.Native.String() != "1500000000000000000000000" {
		t.Fatalf("balance = %s", got.Native)
	}
}

func TestAccountUnknown(t *testing.T) {
	srv := serve(t, map[string]string{
		"status": testnetStatus,
		"query":  `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Server error","data":"account alice.testnet does not exist while viewing"}}`,
	})
	defer srv.Close()
	c := NewClient(testnet, srv.URL)
	got, err := c.Account(context.Background(), "alice.testnet")
	if err != nil {
		t.Fatal(err)
	}
	if got.Native.Sign() != 0 {
		t.Fatalf("balance = %s", got.Native)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "fund") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestBlock(t *testing.T) {
	srv := serve(t, map[string]string{
		"status": testnetStatus,
		"block":  `{"jsonrpc":"2.0","id":1,"result":{"header":{"height":276791691}}}`,
	})
	defer srv.Close()
	c := NewClient(testnet, srv.URL)
	height, err := c.Block(context.Background())
	if err != nil || height != 276791691 {
		t.Fatalf("height = %d, %v", height, err)
	}
}

func TestVerify(t *testing.T) {
	srv := serve(t, map[string]string{
		"status": `{"jsonrpc":"2.0","id":1,"result":{"chain_id":"testnet"}}`,
	})
	defer srv.Close()
	if err := NewClient(testnet, srv.URL).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := NewClient(mainnet, srv.URL).Verify(context.Background()); err == nil {
		t.Fatal("a testnet endpoint passed as mainnet")
	}
}

func TestRPCRejectsWrongNetworkBeforeReadingOrSending(t *testing.T) {
	srv := serve(t, map[string]string{
		"status": `{"jsonrpc":"2.0","id":1,"result":{"chain_id":"mainnet"}}`,
	})
	defer srv.Close()
	for _, method := range []string{"status", "query", "block", "broadcast_tx_commit", "broadcast_tx_async", "send_tx"} {
		for _, withTimeout := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/timeout=%t", method, withTimeout), func(t *testing.T) {
				c := NewClient(testnet, srv.URL)
				var err error
				if withTimeout {
					_, err = c.RPCWithTimeout(context.Background(), method, nil, time.Second)
				} else {
					_, err = c.RPC(context.Background(), method, nil)
				}
				if err == nil || !strings.Contains(err.Error(), `serves NEAR "mainnet"`) {
					t.Fatalf("wrong network was not refused: %v", err)
				}
				if audit := c.Broker().Audit(); len(audit) != 1 || audit[0].Method != "status" {
					t.Fatalf("an unchecked request reached the endpoint: %+v", audit)
				}
			})
		}
	}
	for name, call := range map[string]func(*Client) error{
		"prepare": func(c *Client) error {
			_, _, err := neartx.Prepare(context.Background(), c.RPC, neartx.Signer{AccountID: "alice.testnet"}, neartx.Request{
				ReceiverID: "bob.testnet", Actions: []neartx.Action{neartx.Transfer(big.NewInt(1))},
			})
			return err
		},
		"send": func(c *Client) error {
			_, err := c.Send(context.Background(), []byte("fake signed transaction"), [32]byte{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewClient(testnet, srv.URL)
			if err := call(c); err == nil || !strings.Contains(err.Error(), `serves NEAR "mainnet"`) {
				t.Fatalf("wrong network was not refused: %v", err)
			}
			if audit := c.Broker().Audit(); len(audit) != 1 || audit[0].Method != "status" {
				t.Fatalf("an unchecked signing request reached the endpoint: %+v", audit)
			}
		})
	}
}

func TestRPCRejectsUnverifiableNetwork(t *testing.T) {
	for _, tt := range []struct {
		name, result, chainName string
	}{
		{"missing_chain_id", `{}`, "testnet"},
		{"empty_chain_id", `{"chain_id":""}`, "testnet"},
		{"invalid_chain_id", `{"chain_id":42}`, "testnet"},
		{"null_status", `null`, "testnet"},
		{"no_network_pin", `{"chain_id":"testnet"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, map[string]string{
				"status": fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":%s}`, tt.result),
			})
			defer srv.Close()
			network := testnet
			network.ChainName = tt.chainName
			c := NewClient(network, srv.URL)
			for _, method := range []string{"query", "status"} {
				if _, err := c.RPC(context.Background(), method, nil); err == nil {
					t.Fatalf("%s accepted an unverifiable network", method)
				}
			}
		})
	}
}

func TestRPCRechecksNetworkAfterMismatch(t *testing.T) {
	var statuses, queries atomic.Int32
	srv := serveRPC(t, func(method string) string {
		switch method {
		case "status":
			if statuses.Add(1) == 2 {
				return `{"jsonrpc":"2.0","id":1,"result":{"chain_id":"mainnet"}}`
			}
			return testnetStatus
		case "query":
			queries.Add(1)
			return `{"jsonrpc":"2.0","id":1,"result":{"amount":"0"}}`
		}
		return ""
	})
	defer srv.Close()
	c := NewClient(testnet, srv.URL)
	for i := 0; i < 3; i++ {
		_, err := c.RPC(context.Background(), "query", nil)
		if (err != nil) != (i == 1) {
			t.Fatalf("request %d: error = %v", i+1, err)
		}
	}
	if statuses.Load() != 3 || queries.Load() != 2 {
		t.Fatalf("status = %d, query = %d", statuses.Load(), queries.Load())
	}
}

func TestRPCCancelledVerificationCanRetry(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var statuses, queries atomic.Int32
	srv := serveRPC(t, func(method string) string {
		switch method {
		case "status":
			if statuses.Add(1) == 1 {
				close(started)
				<-release
			}
			return testnetStatus
		case "query":
			queries.Add(1)
			return `{"jsonrpc":"2.0","id":1,"result":{}}`
		}
		return ""
	})
	defer srv.Close()
	defer close(release)
	c := NewClient(testnet, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.RPC(ctx, "query", nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("network verification did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("verification error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled verification did not return")
	}
	if _, err := c.RPC(context.Background(), "query", nil); err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
	if statuses.Load() != 2 || queries.Load() != 1 {
		t.Fatalf("status = %d, query = %d", statuses.Load(), queries.Load())
	}
}

func TestPrepareAndBroadcastDoNotReuseResults(t *testing.T) {
	calls := map[string]*atomic.Int32{}
	for _, method := range []string{"query", "block", "broadcast_tx_commit", "broadcast_tx_async", "send_tx"} {
		calls[method] = new(atomic.Int32)
	}
	srv := serveRPC(t, func(method string) string {
		if method == "status" {
			return testnetStatus
		}
		counter := calls[method]
		if counter == nil {
			return ""
		}
		n := counter.Add(1)
		switch method {
		case "query":
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"nonce":%d,"permission":"FullAccess"}}`, n)
		case "block":
			hash := [32]byte{byte(n)}
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"header":{"hash":%q}}}`, base58.Encode(hash[:]))
		default:
			return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"attempt":%d}}`, n)
		}
	})
	defer srv.Close()
	c := NewClient(testnet, srv.URL)
	for i := 1; i <= 2; i++ {
		tx, _, err := neartx.Prepare(context.Background(), c.RPC, neartx.Signer{AccountID: "alice.testnet"}, neartx.Request{
			ReceiverID: "bob.testnet", Actions: []neartx.Action{neartx.Transfer(big.NewInt(1))},
		})
		if err != nil {
			t.Fatal(err)
		}
		if tx.Nonce != uint64(i+1) || tx.BlockHash != ([32]byte{byte(i)}) {
			t.Fatalf("prepare %d reused stale data: nonce = %d, block = %x", i, tx.Nonce, tx.BlockHash)
		}
	}
	for _, method := range []string{"broadcast_tx_commit", "broadcast_tx_async", "send_tx"} {
		params := []any{"fake signed transaction"}
		first, err := c.RPC(context.Background(), method, params)
		if err != nil {
			t.Fatal(err)
		}
		second, err := c.RPCWithTimeout(context.Background(), method, params, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) == string(second) || calls[method].Load() != 2 {
			t.Fatalf("%s reused a broadcast result: %s, %s", method, first, second)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRPCTimeouts(t *testing.T) {
	outcome := `{"jsonrpc":"2.0","id":1,"result":{"status":{"SuccessValue":""},"transaction_outcome":{"id":"11111111111111111111111111111111"}}}`
	srv := serve(t, map[string]string{
		"status": testnetStatus, "query": `{"jsonrpc":"2.0","id":1,"result":{}}`,
		"broadcast_tx_commit": outcome, "broadcast_tx_async": outcome, "send_tx": outcome,
	})
	defer srv.Close()
	for _, tt := range []struct {
		name, method, send    string
		timeout, parent, want time.Duration
	}{
		{name: "commit", method: "broadcast_tx_commit", want: 90 * time.Second},
		{name: "async", method: "broadcast_tx_async", want: 90 * time.Second},
		{name: "send_tx", method: "send_tx", want: 90 * time.Second},
		{name: "default_timeout", method: "broadcast_tx_commit", timeout: -1, want: 90 * time.Second},
		{name: "explicit_timeout", method: "broadcast_tx_commit", timeout: 2 * time.Second, want: 2 * time.Second},
		{name: "parent_deadline", method: "broadcast_tx_commit", parent: 3 * time.Second, want: 3 * time.Second},
		{name: "reader_send", method: "broadcast_tx_commit", send: "reader", want: 90 * time.Second},
		{name: "client_send", method: "broadcast_tx_commit", send: "client", want: 90 * time.Second},
		{name: "read", method: "query", want: time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deadlines := map[string]time.Time{}
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				defer body.Close()
				var payload struct {
					Method string `json:"method"`
				}
				if err := json.NewDecoder(body).Decode(&payload); err != nil {
					return nil, err
				}
				deadline, ok := req.Context().Deadline()
				if !ok {
					return nil, errors.New("RPC request has no deadline")
				}
				deadlines[payload.Method] = deadline
				return http.DefaultTransport.RoundTrip(req)
			})
			c := NewClient(testnet, srv.URL)
			c.broker = rpc.NewBroker(rpc.Options{
				Client: &http.Client{Transport: transport}, Timeout: time.Second, CacheTTL: -1, MinInterval: -1,
			})
			ctx := context.Background()
			cancel := func() {}
			if tt.parent != 0 {
				ctx, cancel = context.WithTimeout(ctx, tt.parent)
			}
			defer cancel()
			start := time.Now()
			var err error
			switch tt.send {
			case "reader":
				_, err = neartx.Send(ctx, c.RPC, []byte("fake signed transaction"), [32]byte{})
			case "client":
				_, err = c.Send(ctx, []byte("fake signed transaction"), [32]byte{})
			default:
				if tt.timeout != 0 {
					_, err = c.RPCWithTimeout(ctx, tt.method, nil, tt.timeout)
				} else {
					_, err = c.RPC(ctx, tt.method, nil)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			deadline, ok := deadlines[tt.method]
			if !ok {
				t.Fatalf("%s never reached HTTP", tt.method)
			}
			if got := deadline.Sub(start); got < tt.want-250*time.Millisecond || got > tt.want+250*time.Millisecond {
				t.Fatalf("%s timeout = %s, want %s", tt.method, got, tt.want)
			}
		})
	}
}

func TestReaderInterface(t *testing.T) {
	var _ chain.Reader = NewClient(testnet, "")
}
