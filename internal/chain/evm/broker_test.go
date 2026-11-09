package evm

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/chain"
)

var live = flag.Bool("live", false, "hit the public endpoint in TestLiveEndpoint")

const (
	testAddr   = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	testToken  = "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
	testToken2 = "0xdac17f958d2ee523a2206206994597c13d831ec7"
)

// fakeRPC answers the handful of methods the app uses.
type fakeRPC struct {
	mu       sync.Mutex
	chainID  string
	balances map[string]string
	tokens   map[string]map[string]string
	tokenErr map[string]*RPCError
	fail     map[string]*RPCError
	block    string
	calls    map[string]int
}

func newFakeRPC(chainID uint64) *fakeRPC {
	return &fakeRPC{
		chainID:  fmt.Sprintf("0x%x", chainID),
		balances: map[string]string{},
		tokens:   map[string]map[string]string{},
		tokenErr: map[string]*RPCError{},
		fail:     map[string]*RPCError{},
		block:    "0x10",
		calls:    map[string]int{},
	}
}

func (f *fakeRPC) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *fakeRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int               `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.calls[req.Method]++
	fail := f.fail[req.Method]
	f.mu.Unlock()
	if fail != nil {
		writeRPC(w, req.ID, nil, fail)
		return
	}
	switch req.Method {
	case "eth_chainId":
		writeRPC(w, req.ID, f.chainID, nil)
	case "eth_blockNumber":
		writeRPC(w, req.ID, f.block, nil)
	case "eth_getBalance":
		var addr string
		_ = json.Unmarshal(req.Params[0], &addr)
		f.mu.Lock()
		result, ok := f.balances[addr]
		f.mu.Unlock()
		if !ok {
			result = "0x0"
		}
		writeRPC(w, req.ID, result, nil)
	case "eth_call":
		var call struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		_ = json.Unmarshal(req.Params[0], &call)
		f.mu.Lock()
		rpcErr := f.tokenErr[call.To]
		f.mu.Unlock()
		if rpcErr != nil {
			writeRPC(w, req.ID, nil, rpcErr)
			return
		}
		holder := "0x" + call.Data[len(call.Data)-40:]
		f.mu.Lock()
		result, ok := f.tokens[call.To][holder]
		f.mu.Unlock()
		if !ok {
			result = "0x" + strings.Repeat("0", 64)
		}
		writeRPC(w, req.ID, result, nil)
	default:
		writeRPC(w, req.ID, nil, &RPCError{Code: -32601, Message: "method not found"})
	}
}

func writeRPC(w http.ResponseWriter, id int, result any, rpcErr *RPCError) {
	body := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		body["error"] = rpcErr
	} else {
		body["result"] = result
	}
	_ = json.NewEncoder(w).Encode(body)
}

func testBroker() *Broker {
	return NewBroker(Options{MinInterval: -1, CacheTTL: -1, Timeout: 5 * time.Second})
}

func TestCallPinsChainID(t *testing.T) {
	fake := newFakeRPC(1)
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	ep := Endpoint{URL: srv.URL, ChainID: 2}
	_, err := b.NativeBalance(context.Background(), ep, testAddr)
	var mismatch *ChainMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want ChainMismatchError", err)
	}
	if mismatch.Got != 1 || mismatch.Want != 2 {
		t.Fatalf("mismatch = %+v", mismatch)
	}
	if got := fake.count("eth_chainId"); got != 1 {
		t.Fatalf("eth_chainId calls = %d, want 1", got)
	}
	// The answer is remembered: the next call fails without asking again.
	_, err = b.NativeBalance(context.Background(), ep, testAddr)
	if !errors.As(err, &mismatch) {
		t.Fatalf("second error = %v, want ChainMismatchError", err)
	}
	if got := fake.count("eth_chainId"); got != 1 {
		t.Fatalf("eth_chainId calls after mismatch = %d, want 1", got)
	}
	if got := fake.count("eth_getBalance"); got != 0 {
		t.Fatalf("eth_getBalance calls = %d, want 0", got)
	}
}

func TestNativeBalance(t *testing.T) {
	fake := newFakeRPC(1)
	fake.balances[testAddr] = "0x1bc16d674ec80000" // 2e18
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	got, err := b.NativeBalance(context.Background(), Endpoint{URL: srv.URL, ChainID: 1}, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "2000000000000000000" {
		t.Fatalf("balance = %s", got)
	}
}

func TestTokenBalance(t *testing.T) {
	fake := newFakeRPC(1)
	fake.tokens[testToken] = map[string]string{testAddr: "0x" + fmt.Sprintf("%064x", 1_200_000_000)}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	got, err := b.TokenBalance(context.Background(), Endpoint{URL: srv.URL, ChainID: 1}, testToken, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "1200000000" {
		t.Fatalf("token balance = %s", got)
	}
}

func TestAccountKeepsGoingAfterOneTokenFails(t *testing.T) {
	fake := newFakeRPC(1)
	fake.balances[testAddr] = "0xde0b6b3a7640000" // 1e18
	fake.tokens[testToken] = map[string]string{testAddr: "0x" + fmt.Sprintf("%064x", 42)}
	fake.tokenErr[testToken2] = &RPCError{Code: -32000, Message: "execution reverted"}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	network := chain.Network{ID: "eip155:1", Family: chain.EVM, Tokens: []chain.Token{
		{Symbol: "USDC", Address: testToken, Decimals: 6},
		{Symbol: "USDT", Address: testToken2, Decimals: 6},
	}}
	b := testBroker()
	acct, err := b.Account(context.Background(), Endpoint{URL: srv.URL, ChainID: 1}, network, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Native.String() != "1000000000000000000" {
		t.Fatalf("native = %s", acct.Native)
	}
	if len(acct.Tokens) != 1 || acct.Tokens[0].Token.Symbol != "USDC" {
		t.Fatalf("tokens = %+v", acct.Tokens)
	}
	if len(acct.Warnings) != 1 || !strings.Contains(acct.Warnings[0], "USDT") {
		t.Fatalf("warnings = %v", acct.Warnings)
	}
}

func TestClientIsAReader(t *testing.T) {
	fake := newFakeRPC(8453)
	fake.balances[testAddr] = "0x1"
	srv := httptest.NewServer(fake)
	defer srv.Close()

	network := chain.Network{ID: "eip155:8453", Family: chain.EVM, ChainID: 8453, RPCURL: srv.URL}
	var reader chain.Reader = NewClient(network, "")
	acct, err := reader.Account(context.Background(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Native.String() != "1" {
		t.Fatalf("native = %s", acct.Native)
	}
	if _, err := reader.Block(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCacheServesRepeatedReads(t *testing.T) {
	fake := newFakeRPC(1)
	fake.balances[testAddr] = "0x1"
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := NewBroker(Options{MinInterval: -1, CacheTTL: time.Minute})
	ep := Endpoint{URL: srv.URL, ChainID: 1}
	for range 3 {
		if _, err := b.NativeBalance(context.Background(), ep, testAddr); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.count("eth_getBalance"); got != 1 {
		t.Fatalf("eth_getBalance calls = %d, want 1", got)
	}
	if got := fake.count("eth_chainId"); got != 1 {
		t.Fatalf("eth_chainId calls = %d, want 1", got)
	}
}

func TestRPCErrorIsReported(t *testing.T) {
	fake := newFakeRPC(1)
	fake.fail["eth_getBalance"] = &RPCError{Code: -32000, Message: "boom"}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	_, err := b.NativeBalance(context.Background(), Endpoint{URL: srv.URL, ChainID: 1}, testAddr)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want it to mention boom", err)
	}
}

func TestAuditKeepsHostOnly(t *testing.T) {
	fake := newFakeRPC(1)
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	ep := Endpoint{URL: srv.URL + "/secret-path?api-key=abc", ChainID: 1}
	if _, err := b.NativeBalance(context.Background(), ep, testAddr); err != nil {
		t.Fatal(err)
	}
	audit := b.Audit()
	if len(audit) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(audit))
	}
	for _, entry := range audit {
		if strings.Contains(entry.Endpoint, "secret") || strings.Contains(entry.Endpoint, "api-key") {
			t.Fatalf("audit leaked the URL: %+v", entry)
		}
	}
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	b := NewBroker(Options{MinInterval: -1, CacheTTL: -1, Timeout: 50 * time.Millisecond})
	_, err := b.NativeBalance(context.Background(), Endpoint{URL: srv.URL}, testAddr)
	if err == nil {
		t.Fatal("want a timeout error")
	}
}

func TestBlockNumber(t *testing.T) {
	fake := newFakeRPC(1)
	fake.block = "0x2a"
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	block, err := b.BlockNumber(context.Background(), Endpoint{URL: srv.URL, ChainID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if block != 42 {
		t.Fatalf("block = %d, want 42", block)
	}
}

func TestParseQuantity(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{`"0x0"`, "0"},
		{`"0x"`, "0"},
		{`"0x1bc16d674ec80000"`, "2000000000000000000"},
		{`"0x10"`, "16"},
	} {
		got, err := ParseQuantity([]byte(tc.raw))
		if err != nil {
			t.Fatalf("ParseQuantity(%s): %v", tc.raw, err)
		}
		if got.String() != tc.want {
			t.Errorf("ParseQuantity(%s) = %s, want %s", tc.raw, got, tc.want)
		}
	}
	if _, err := ParseQuantity([]byte(`"0xzz"`)); err == nil {
		t.Error("ParseQuantity(0xzz) must fail")
	}
}

func TestParseUint256(t *testing.T) {
	got, err := ParseUint256([]byte(`"0x00000000000000000000000000000000000000000000000000000000000004d2"`))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "1234" {
		t.Errorf("ParseUint256 = %s, want 1234", got)
	}
	if _, err := ParseUint256([]byte(`"0x` + "11" + `"`)); err != nil {
		t.Errorf("short data: %v", err)
	}
	if _, err := ParseUint256([]byte(`"0x` + strings.Repeat("11", 33) + `"`)); err == nil {
		t.Error("more than 32 bytes must fail")
	}
}

// TestLiveEndpoint checks the default endpoint and the typed reads against
// the real chain. It is opt-in: go test ./internal/chain/evm -run TestLiveEndpoint -live.
func TestLiveEndpoint(t *testing.T) {
	if !*live {
		t.Skip("pass -live to hit the public endpoint")
	}
	const vitalik = "0xd8da6bf26964af9d7eed9e03e53415d37aa96045"
	b := NewBroker(Options{})
	ep := Endpoint{URL: "https://ethereum-rpc.publicnode.com", ChainID: 1}
	balance, err := b.NativeBalance(context.Background(), ep, vitalik)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Sign() <= 0 {
		t.Fatalf("balance = %s, want a positive balance", balance)
	}
	block, err := b.BlockNumber(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	if block == 0 {
		t.Fatal("block = 0")
	}
	t.Logf("balance = %s wei, block = %d", balance, block)
}
