package near

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davirain-su/crypto-panel/internal/chain"
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

// serve answers each JSON-RPC method with the body in answers.
func serve(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("request: %v", err)
		}
		answer, ok := answers[req.Method]
		if !ok {
			http.Error(w, "unexpected method "+req.Method, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, answer)
	}))
}

func TestAccount(t *testing.T) {
	srv := serve(t, map[string]string{
		"query": `{"jsonrpc":"2.0","id":1,"result":{"amount":"1500000000000000000000000","locked":"0","code_hash":"11111111111111111111111111111111","storage_usage":182}}`,
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
		"query": `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Server error","data":"account alice.testnet does not exist while viewing"}}`,
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
		"block": `{"jsonrpc":"2.0","id":1,"result":{"header":{"height":276791691}}}`,
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

func TestReaderInterface(t *testing.T) {
	var _ chain.Reader = NewClient(testnet, "")
}
