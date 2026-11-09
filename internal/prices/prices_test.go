package prices

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPricesReadsKnownSymbols(t *testing.T) {
	var calls atomic.Int32
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		query = r.URL.Query().Get("ids")
		if got := r.URL.Query().Get("vs_currencies"); got != "usd" {
			t.Errorf("vs_currencies = %q", got)
		}
		if ua := r.Header.Get("User-Agent"); ua != "lintel" {
			t.Errorf("User-Agent = %q", ua)
		}
		_, _ = w.Write([]byte(`{"ethereum":{"usd":3120.5},"near":{"usd":4.25}}`))
	}))
	defer srv.Close()

	c := New()
	c.URL = srv.URL
	got, err := c.Prices(context.Background(), []string{"ETH", "near", "NOPE"})
	if err != nil {
		t.Fatal(err)
	}
	if got["ETH"] != 3120.5 || got["NEAR"] != 4.25 {
		t.Fatalf("prices = %v", got)
	}
	if _, ok := got["NOPE"]; ok {
		t.Error("an unknown symbol was priced")
	}
	if query != "ethereum,near" {
		t.Errorf("ids = %q", query)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestPricesCachesForTTL(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"solana":{"usd":150}}`))
	}))
	defer srv.Close()

	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	c := New()
	c.URL = srv.URL
	c.Now = func() time.Time { return now }
	if _, err := c.Prices(context.Background(), []string{"SOL"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if _, err := c.Prices(context.Background(), []string{"SOL"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls inside the TTL = %d", calls.Load())
	}
	now = now.Add(DefaultTTL)
	if _, err := c.Prices(context.Background(), []string{"SOL"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls after the TTL = %d", calls.Load())
	}
}

func TestPricesReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New()
	c.URL = srv.URL
	_, err := c.Prices(context.Background(), []string{"ETH"})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v", err)
	}
}

func TestPricesSkipsRequestForUnknownSymbols(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()

	c := New()
	c.URL = srv.URL
	got, err := c.Prices(context.Background(), []string{"DOGE", "SHIB"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || calls.Load() != 0 {
		t.Fatalf("got %v with %d calls", got, calls.Load())
	}
}
