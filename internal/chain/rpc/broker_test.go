package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testBroker() *Broker {
	return NewBroker(Options{MinInterval: -1, CacheTTL: -1, Timeout: 5 * time.Second})
}

func TestCallUnwrapsARefusalBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Request blocked"}}`))
	}))
	defer srv.Close()

	_, err := testBroker().Call(context.Background(), srv.URL, "getTokenAccountsByOwner", []any{})
	if err == nil || !strings.Contains(err.Error(), "Request blocked") {
		t.Fatalf("error = %v, want the body's message", err)
	}
}

func TestCallReportsABareStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := testBroker().Call(context.Background(), srv.URL, "getSlot", []any{})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %v, want the status", err)
	}
}

func TestHostKeepsOnlyTheHost(t *testing.T) {
	if got := Host("https://example.com/secret?api-key=1"); got != "example.com" {
		t.Fatalf("Host = %q", got)
	}
	if got := Host("not a url"); got != "not a url" {
		t.Fatalf("Host(bad) = %q", got)
	}
}
