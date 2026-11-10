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
)

func TestQuoteRejectsMissingTamperedAndStaleSignatures(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	valid := signTestQuote(t, key, statusQuote(now, true))
	for name, mutate := range map[string]func(map[string]json.RawMessage){
		"missing signature": func(top map[string]json.RawMessage) { delete(top, "signature") },
		"empty signature":   func(top map[string]json.RawMessage) { top["signature"] = json.RawMessage(`""`) },
		"malformed":         func(top map[string]json.RawMessage) { top["signature"] = json.RawMessage(`"not base58!"`) },
		"wrong key": func(top map[string]json.RawMessage) {
			top["signature"] = json.RawMessage(`"ed25519:3yVRcYGXRVj2YqrUng4Ne2yiWgh9YQfer46KW6sXiWzoyRHgsifwDp1HSZW7VLRTdKXoMgxJce22LQ9dcoihyfu5"`)
		},
		"tampered amount": func(top map[string]json.RawMessage) {
			quote := mustRawMap(t, string(top["quote"]))
			quote["amountOut"] = json.RawMessage(`"999999999999"`)
			top["quote"], _ = json.Marshal(quote)
		},
		"tampered routing": func(top map[string]json.RawMessage) {
			request := mustRawMap(t, string(top["quoteRequest"]))
			request["referral"] = json.RawMessage(`"attacker"`)
			top["quoteRequest"], _ = json.Marshal(request)
		},
		"missing timestamp": func(top map[string]json.RawMessage) { delete(top, "timestamp") },
		"stale": func(top map[string]json.RawMessage) {
			top["timestamp"], _ = json.Marshal(now.Add(-5*time.Minute - time.Second).Format(time.RFC3339Nano))
		},
		"future": func(top map[string]json.RawMessage) {
			top["timestamp"], _ = json.Marshal(now.Add(5*time.Minute + time.Second).Format(time.RFC3339Nano))
		},
	} {
		t.Run(name, func(t *testing.T) {
			top := mustRawMap(t, valid)
			mutate(top)
			body, _ := json.Marshal(top)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(body)
			}))
			defer srv.Close()
			_, err := newSignedTestClient(srv.URL, key.Public().(ed25519.PublicKey), now).Quote(context.Background(), QuoteRequest{
				OriginAsset: "a", DestinationAsset: "b", Amount: "1000", Recipient: "p", RefundTo: "r", Deadline: now.Add(time.Hour),
			})
			if err == nil {
				t.Fatal("invalid quote was accepted")
			}
		})
	}
}

func TestProductionConstructorsCannotUseTestSignatures(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	body := signTestQuote(t, key, statusQuote(now, true))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, body)
	}))
	defer srv.Close()
	for name, client := range map[string]*Client{"NewAt": NewAt(srv.URL), "newClient production": newClient(srv.URL, time.Now, productionManagerPublicKey)} {
		t.Run(name, func(t *testing.T) {
			_, err := client.Quote(context.Background(), QuoteRequest{
				OriginAsset: "a", DestinationAsset: "b", Amount: "1000", Recipient: "p", RefundTo: "r",
			})
			if err == nil || !strings.Contains(err.Error(), "signature") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
