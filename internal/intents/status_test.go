package intents

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/base58"
)

func newSignedTestClient(baseURL string, key ed25519.PublicKey, now time.Time) *Client {
	return NewAtWithOptions(baseURL, WithClock(func() time.Time { return now }), WithVerifierKey("ed25519:"+base58.Encode(key)))
}

func TestStatusReadsWithAddressAndExactMemo(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	quote := signedStatusQuote(t, key, now, false)
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v0/status" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"status":"PROCESSING",
			"correlationId":"outer-id",
			"updatedAt":"2026-01-02T03:04:06Z",
			"quoteResponse":`+quote+`,
			"swapDetails":{"amountIn":"1000","hashes":["a"]}
		}`)
	}))
	defer srv.Close()
	c := newSignedTestClient(srv.URL, key.Public().(ed25519.PublicKey), now)
	got, err := c.Status(context.Background(), "  deposit address/+?  ", " memo /+? ")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "depositAddress=deposit+address%2F%2B%3F&depositMemo=+memo+%2F%2B%3F+" {
		t.Fatalf("query = %q", gotQuery)
	}
	if got.Status != StatusProcessing || got.CorrelationID != "outer-id" || got.UpdatedAt != "2026-01-02T03:04:06Z" {
		t.Fatalf("status = %+v", got)
	}
	if !json.Valid(got.QuoteResponse) || string(got.SwapDetails) != `{"amountIn":"1000","hashes":["a"]}` {
		t.Fatalf("raw fields = %s, %s", got.QuoteResponse, got.SwapDetails)
	}
}

func TestStatusVerifiesHistoricalQuoteWithoutCurrentFreshness(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"SUCCESS","quoteResponse":`+signedStatusQuote(t, key, now.Add(-24*time.Hour), false)+`}`)
	}))
	defer srv.Close()
	got, err := newSignedTestClient(srv.URL, key.Public().(ed25519.PublicKey), now).Status(context.Background(), "address", "")
	if err != nil || got.Status != StatusSuccess {
		t.Fatalf("historical status = %+v, err = %v", got, err)
	}
}

func TestStatusOmitsEmptyMemoAndRejectsBlankAddress(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := r.URL.Query()["depositMemo"]; ok {
			t.Errorf("empty memo was sent: %s", r.URL.RawQuery)
		}
		io.WriteString(w, `{"status":"PENDING_DEPOSIT","quoteResponse":`+signedStatusQuote(t, key, now, false)+`}`)
	}))
	defer srv.Close()
	c := newSignedTestClient(srv.URL, key.Public().(ed25519.PublicKey), now)
	if _, err := c.Status(context.Background(), "\t ", ""); err == nil || called {
		t.Fatalf("blank address: err = %v, called = %v", err, called)
	}
	if _, err := c.Status(context.Background(), "address", ""); err != nil {
		t.Fatal(err)
	}
}

func TestStatusAcceptsExactlyDocumentedStates(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	states := []StatusState{StatusPendingDeposit, StatusKnownDepositTx, StatusIncompleteDeposit, StatusProcessing, StatusSuccess, StatusRefunded, StatusFailed}
	for _, state := range append(states, StatusState("UNKNOWN"), StatusState("")) {
		t.Run(string(state), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := json.Marshal(map[string]any{"status": state, "quoteResponse": json.RawMessage(signedStatusQuote(t, key, now, false))})
				w.Write(body)
			}))
			defer srv.Close()
			got, err := newSignedTestClient(srv.URL, key.Public().(ed25519.PublicKey), now).Status(context.Background(), "address", "")
			wantErr := state == "UNKNOWN" || state == ""
			if (err != nil) != wantErr {
				t.Fatalf("state %q: status = %+v, err = %v", state, got, err)
			}
		})
	}
}

func TestStatusRejectsUnverifiedNestedQuotes(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for name, quote := range map[string]string{
		"missing":   "",
		"null":      "null",
		"malformed": `{`,
		"unsigned":  statusQuote(now, false),
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{"status":"SUCCESS","quoteResponse":`+quote+`}`)
			}))
			defer srv.Close()
			if _, err := newSignedTestClient(srv.URL, key.Public().(ed25519.PublicKey), now).Status(context.Background(), "address", ""); err == nil {
				t.Fatal("an unverified nested quote was accepted")
			}
		})
	}
}

func signedStatusQuote(t *testing.T, key ed25519.PrivateKey, now time.Time, dry bool) string {
	t.Helper()
	return signTestQuote(t, key, statusQuote(now, dry))
}

func statusQuote(now time.Time, dry bool) string {
	deposit := ""
	if !dry {
		deposit = `,"depositAddress":"deposit-address","depositMemo":"memo","deadline":"2026-01-02T04:00:00Z","timeWhenInactive":"2026-01-02T03:30:00Z","timeEstimate":812,"refundFee":"1","withdrawFee":"2"`
	}
	return `{
		"correlationId":"inner-id",
		"timestamp":` + quoteJSON(now.Format(time.RFC3339Nano)) + `,
		"quoteRequest":{
			"dry":` + map[bool]string{true: "true", false: "false"}[dry] + `,
			"swapType":"EXACT_INPUT","slippageTolerance":100,
			"originAsset":"a","depositType":"ORIGIN_CHAIN","destinationAsset":"b",
			"amount":"1000","refundTo":"r","refundType":"ORIGIN_CHAIN",
			"recipient":"p","recipientType":"DESTINATION_CHAIN","deadline":"2026-01-02T04:00:00Z",
			"referral":"partner"
		},
		"quote":{
			"amountIn":"1000","amountInFormatted":"1","amountInUsd":"2.50",
			"minAmountIn":"1000","amountOut":"2000","amountOutFormatted":"2",
			"amountOutUsd":"2.50","minAmountOut":"1900"` + deposit + `
		}
	}`
}

func quoteJSON(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}

func TestStatusUsesSharedRedirectAndResponseCap(t *testing.T) {
	called := false
	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL, http.StatusFound)
	}))
	defer srv.Close()
	if _, err := NewAt(srv.URL).Status(context.Background(), "address", ""); err == nil || called {
		t.Fatalf("redirect err = %v, followed = %v", err, called)
	}

	padding := strings.Repeat("a", maxResponseBytes)
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"`+padding+`"}`)
	}))
	defer large.Close()
	if _, err := NewAt(large.URL).Status(context.Background(), "address", ""); err == nil {
		t.Fatal("oversized status body was accepted")
	}
}
