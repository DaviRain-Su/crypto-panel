package solana

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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
	testAddr    = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	testMint    = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	testMint2   = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"
	testGenesis = "5eykt4UsFv8P8NJdTREpY1vzqKqZKvdpKuc147dw2N9d"
)

// tokenAccount is one entry of a getTokenAccountsByOwner answer.
type tokenAccount struct {
	mint   string
	amount string
}

// fakeRPC answers the handful of methods the app uses.
type fakeRPC struct {
	mu            sync.Mutex
	genesis       string
	balances      map[string]uint64
	tokenAccounts map[string][]tokenAccount // keyed by token program
	programErr    map[string]*RPCError
	fail          map[string]*RPCError
	slot          uint64
	calls         map[string]int
}

func newFakeRPC() *fakeRPC {
	return &fakeRPC{
		genesis:       testGenesis,
		balances:      map[string]uint64{},
		tokenAccounts: map[string][]tokenAccount{},
		programErr:    map[string]*RPCError{},
		fail:          map[string]*RPCError{},
		slot:          100,
		calls:         map[string]int{},
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
	case "getGenesisHash":
		writeRPC(w, req.ID, f.genesis, nil)
	case "getSlot":
		writeRPC(w, req.ID, f.slot, nil)
	case "getBalance":
		var addr string
		_ = json.Unmarshal(req.Params[0], &addr)
		f.mu.Lock()
		value := f.balances[addr]
		f.mu.Unlock()
		writeRPC(w, req.ID, map[string]any{"context": map[string]any{"slot": 1}, "value": value}, nil)
	case "getTokenAccountsByOwner":
		var opts struct {
			ProgramID string `json:"programId"`
		}
		_ = json.Unmarshal(req.Params[1], &opts)
		f.mu.Lock()
		rpcErr := f.programErr[opts.ProgramID]
		accounts := f.tokenAccounts[opts.ProgramID]
		f.mu.Unlock()
		if rpcErr != nil {
			writeRPC(w, req.ID, nil, rpcErr)
			return
		}
		value := make([]any, 0, len(accounts))
		for _, a := range accounts {
			value = append(value, map[string]any{
				"pubkey": "account",
				"account": map[string]any{
					"data": map[string]any{
						"parsed": map[string]any{
							"info": map[string]any{
								"mint":        a.mint,
								"tokenAmount": map[string]any{"amount": a.amount, "decimals": 6},
							},
						},
					},
				},
			})
		}
		writeRPC(w, req.ID, map[string]any{
			"context": map[string]any{"slot": 1},
			"value":   value,
		}, nil)
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

func TestCallPinsGenesis(t *testing.T) {
	fake := newFakeRPC()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	ep := Endpoint{URL: srv.URL, Genesis: "another-genesis"}
	_, err := b.NativeBalance(context.Background(), ep, testAddr)
	var mismatch *GenesisMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want GenesisMismatchError", err)
	}
	if mismatch.Got != testGenesis || mismatch.Want != "another-genesis" {
		t.Fatalf("mismatch = %+v", mismatch)
	}
	if got := fake.count("getGenesisHash"); got != 1 {
		t.Fatalf("getGenesisHash calls = %d, want 1", got)
	}
	// The answer is remembered: the next call fails without asking again.
	_, err = b.NativeBalance(context.Background(), ep, testAddr)
	if !errors.As(err, &mismatch) {
		t.Fatalf("second error = %v, want GenesisMismatchError", err)
	}
	if got := fake.count("getGenesisHash"); got != 1 {
		t.Fatalf("getGenesisHash calls after mismatch = %d, want 1", got)
	}
	if got := fake.count("getBalance"); got != 0 {
		t.Fatalf("getBalance calls = %d, want 0", got)
	}
}

func TestNativeBalance(t *testing.T) {
	fake := newFakeRPC()
	fake.balances[testAddr] = 2_500_000_000
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	got, err := b.NativeBalance(context.Background(), Endpoint{URL: srv.URL, Genesis: testGenesis}, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "2500000000" {
		t.Fatalf("balance = %s", got)
	}
}

func TestAccountSumsMintsAcrossPrograms(t *testing.T) {
	fake := newFakeRPC()
	fake.balances[testAddr] = 1_000_000_000
	// Two classic token accounts for the same mint, plus one Token-2022
	// account for another mint.
	fake.tokenAccounts[TokenProgram] = []tokenAccount{
		{mint: testMint, amount: "1200000"},
		{mint: testMint, amount: "300000"},
		{mint: testMint2, amount: "5000000"},
	}
	fake.tokenAccounts[Token2022Program] = []tokenAccount{
		{mint: testMint, amount: "500000"},
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	network := chain.Network{ID: "solana:test", Family: chain.Solana, Tokens: []chain.Token{
		{Symbol: "USDC", Address: testMint, Decimals: 6},
		{Symbol: "USDT", Address: testMint2, Decimals: 6},
		{Symbol: "NOPE", Address: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263", Decimals: 5},
	}}
	b := testBroker()
	acct, err := b.Account(context.Background(), Endpoint{URL: srv.URL, Genesis: testGenesis}, network, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Native.String() != "1000000000" {
		t.Fatalf("native = %s", acct.Native)
	}
	if len(acct.Tokens) != 2 {
		t.Fatalf("tokens = %+v, want the two held mints", acct.Tokens)
	}
	if acct.Tokens[0].Token.Symbol != "USDC" || acct.Tokens[0].Amount.String() != "2000000" {
		t.Errorf("USDC = %+v", acct.Tokens[0])
	}
	if acct.Tokens[1].Token.Symbol != "USDT" || acct.Tokens[1].Amount.String() != "5000000" {
		t.Errorf("USDT = %+v", acct.Tokens[1])
	}
	if len(acct.Warnings) != 0 {
		t.Errorf("warnings = %v", acct.Warnings)
	}
}

func TestAccountWarnsWhenOneProgramFails(t *testing.T) {
	fake := newFakeRPC()
	fake.balances[testAddr] = 1
	fake.tokenAccounts[TokenProgram] = []tokenAccount{{mint: testMint, amount: "7"}}
	fake.programErr[Token2022Program] = &RPCError{Code: -32000, Message: "boom"}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	network := chain.Network{ID: "solana:test", Family: chain.Solana, Tokens: []chain.Token{
		{Symbol: "USDC", Address: testMint, Decimals: 6},
	}}
	b := testBroker()
	acct, err := b.Account(context.Background(), Endpoint{URL: srv.URL, Genesis: testGenesis}, network, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(acct.Tokens) != 1 || acct.Tokens[0].Amount.String() != "7" {
		t.Fatalf("tokens = %+v", acct.Tokens)
	}
	if len(acct.Warnings) != 1 || !strings.Contains(acct.Warnings[0], "boom") {
		t.Fatalf("warnings = %v", acct.Warnings)
	}
}

func TestAccountExplainsIndexedRefusal(t *testing.T) {
	fake := newFakeRPC()
	fake.balances[testAddr] = 1
	fake.programErr[TokenProgram] = &RPCError{Code: -32602, Message: "Indexed requests require a personal token"}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	network := chain.Network{ID: "solana:test", Family: chain.Solana, Tokens: []chain.Token{
		{Symbol: "USDC", Address: testMint, Decimals: 6},
	}}
	b := testBroker()
	acct, err := b.Account(context.Background(), Endpoint{URL: srv.URL, Genesis: testGenesis}, network, testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(acct.Warnings) != 1 || !strings.Contains(acct.Warnings[0], "custom RPC") {
		t.Fatalf("warnings = %v, want one hint about a custom RPC", acct.Warnings)
	}
	// The second program is not asked once the endpoint has refused the
	// first: the answer would be the same.
	if got := fake.count("getTokenAccountsByOwner"); got != 1 {
		t.Fatalf("getTokenAccountsByOwner calls = %d, want 1", got)
	}
}

func TestSlot(t *testing.T) {
	fake := newFakeRPC()
	fake.slot = 42
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	slot, err := b.Slot(context.Background(), Endpoint{URL: srv.URL, Genesis: testGenesis})
	if err != nil {
		t.Fatal(err)
	}
	if slot != 42 {
		t.Fatalf("slot = %d, want 42", slot)
	}
}

func TestClientIsAReader(t *testing.T) {
	fake := newFakeRPC()
	fake.balances[testAddr] = 5
	srv := httptest.NewServer(fake)
	defer srv.Close()

	network := chain.Network{ID: "solana:test", Family: chain.Solana, Genesis: testGenesis, RPCURL: srv.URL}
	var reader chain.Reader = NewClient(network, "")
	acct, err := reader.Account(context.Background(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Native.String() != "5" {
		t.Fatalf("native = %s", acct.Native)
	}
	if _, err := reader.Block(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCacheServesRepeatedReads(t *testing.T) {
	fake := newFakeRPC()
	fake.balances[testAddr] = 1
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := NewBroker(Options{MinInterval: -1, CacheTTL: time.Minute})
	ep := Endpoint{URL: srv.URL, Genesis: testGenesis}
	for range 3 {
		if _, err := b.NativeBalance(context.Background(), ep, testAddr); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.count("getBalance"); got != 1 {
		t.Fatalf("getBalance calls = %d, want 1", got)
	}
	if got := fake.count("getGenesisHash"); got != 1 {
		t.Fatalf("getGenesisHash calls = %d, want 1", got)
	}
}

func TestRPCErrorIsReported(t *testing.T) {
	fake := newFakeRPC()
	fake.fail["getBalance"] = &RPCError{Code: -32000, Message: "boom"}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	_, err := b.NativeBalance(context.Background(), Endpoint{URL: srv.URL, Genesis: testGenesis}, testAddr)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want it to mention boom", err)
	}
}

func TestAuditKeepsHostOnly(t *testing.T) {
	fake := newFakeRPC()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b := testBroker()
	ep := Endpoint{URL: srv.URL + "/secret-path?api-key=abc", Genesis: testGenesis}
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

// TestLiveEndpoint checks the default endpoint and the typed reads against
// the real cluster. It is opt-in: go test ./internal/chain/solana -run TestLiveEndpoint -live.
func TestLiveEndpoint(t *testing.T) {
	if !*live {
		t.Skip("pass -live to hit the public endpoint")
	}
	b := NewBroker(Options{})
	ep := Endpoint{URL: "https://solana-rpc.publicnode.com", Genesis: testGenesis}
	slot, err := b.Slot(context.Background(), ep)
	if err != nil {
		t.Fatal(err)
	}
	if slot == 0 {
		t.Fatal("slot = 0")
	}
	// A busy exchange wallet, so the native read is against real data.
	const binance = "5tzFkiKscXHK5ZXCGbXZxdw7gTjjD1mBwuoFbhUvuAi9"
	native, err := b.NativeBalance(context.Background(), ep, binance)
	if err != nil {
		t.Fatal(err)
	}
	if native.Sign() <= 0 {
		t.Fatalf("native = %s, want a positive balance", native)
	}
	t.Logf("slot = %d, native = %s lamports", slot, native)
}
