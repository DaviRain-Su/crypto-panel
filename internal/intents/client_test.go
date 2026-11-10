package intents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQuoteAsksForADryQuote(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v0/quote" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"correlationId": "c1",
			"timestamp": "2026-11-09T00:00:00Z",
			"signature": "sig",
			"quoteRequest": {
				"dry": true,
				"swapType": "EXACT_INPUT",
				"slippageTolerance": 100,
				"originAsset": "nep141:wrap.near",
				"depositType": "ORIGIN_CHAIN",
				"destinationAsset": "nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near",
				"amount": "1000000000000000000000000",
				"refundTo": "alice.testnet",
				"refundType": "ORIGIN_CHAIN",
				"recipient": "0x2527D02599Ba641c19FEa793cD0F167589a0f10D",
				"recipientType": "DESTINATION_CHAIN",
				"deadline": "2026-11-09T12:30:00Z"
			},
			"quote": {
				"amountIn": "1000000000000000000000000",
				"amountInFormatted": "1",
				"amountInUsd": "2.5",
				"minAmountIn": "1000000000000000000000000",
				"amountOut": "10000000",
				"amountOutFormatted": "10",
				"amountOutUsd": "10",
				"minAmountOut": "9900000",
				"deadline": "2026-11-09T12:30:00Z",
				"timeEstimate": 120
			}
		}`)
	}))
	defer srv.Close()

	q, err := NewAt(srv.URL).Quote(context.Background(), QuoteRequest{
		OriginAsset:      "nep141:wrap.near",
		DestinationAsset: "nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near",
		Amount:           "1000000000000000000000000",
		Recipient:        "0x2527D02599Ba641c19FEa793cD0F167589a0f10D",
		RefundTo:         "alice.testnet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["dry"] != true {
		t.Fatalf("dry = %v, want true", body["dry"])
	}
	if body["swapType"] != "EXACT_INPUT" || body["depositType"] != "ORIGIN_CHAIN" ||
		body["recipientType"] != "DESTINATION_CHAIN" || body["refundType"] != "ORIGIN_CHAIN" {
		t.Fatalf("request = %+v", body)
	}
	if body["originAsset"] != "nep141:wrap.near" || body["amount"] != "1000000000000000000000000" {
		t.Fatalf("request = %+v", body)
	}
	if q.AmountInText != "1" || q.MinAmountOut != "9900000" || q.AmountOutText != "10" {
		t.Fatalf("quote = %+v", q)
	}
	if q.Deadline.IsZero() {
		t.Fatal("the deadline was dropped")
	}
	if q.CorrelationID != "c1" || q.Signature != "sig" {
		t.Fatalf("quote = %+v", q)
	}
}

func TestQuoteRefusesBadRequestsBeforeTheNetwork(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer srv.Close()
	c := NewAt(srv.URL)
	base := QuoteRequest{
		OriginAsset:      "nep141:wrap.near",
		DestinationAsset: "nep141:eth.omft.near",
		Amount:           "1",
		Recipient:        "0xabc",
		RefundTo:         "alice.testnet",
	}
	cases := map[string]func(*QuoteRequest){
		"no origin":      func(r *QuoteRequest) { r.OriginAsset = "" },
		"no destination": func(r *QuoteRequest) { r.DestinationAsset = "" },
		"same assets":    func(r *QuoteRequest) { r.DestinationAsset = r.OriginAsset },
		"fraction":       func(r *QuoteRequest) { r.Amount = "1.5" },
		"zero":           func(r *QuoteRequest) { r.Amount = "0" },
		"signed":         func(r *QuoteRequest) { r.Amount = "-1" },
		"no recipient":   func(r *QuoteRequest) { r.Recipient = "" },
		"no refund":      func(r *QuoteRequest) { r.RefundTo = "" },
		"too much slip":  func(r *QuoteRequest) { r.SlippageBps = 1001 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			req := base
			change(&req)
			if _, err := c.Quote(context.Background(), req); err == nil {
				t.Fatal("a bad request was sent")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("%d requests reached the server", calls)
	}
}

func TestQuoteShowsTheAPIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"message":"the origin asset is not supported"}`)
	}))
	defer srv.Close()
	_, err := NewAt(srv.URL).Quote(context.Background(), QuoteRequest{
		OriginAsset:      "nep141:nope.near",
		DestinationAsset: "nep141:eth.omft.near",
		Amount:           "1",
		Recipient:        "0xabc",
		RefundTo:         "alice.testnet",
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/tokens" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[
			{"assetId":"nep141:wrap.near","decimals":24,"blockchain":"near","symbol":"wNEAR","price":2.5,"icon":"https://assets.example/near.png","priceUpdatedAt":"2026-11-09T00:00:00Z"},
			{"assetId":"nep141:eth.omft.near","decimals":18,"blockchain":"eth","symbol":"ETH","price":3000,"priceUpdatedAt":"2026-11-09T00:00:00Z"},
			{"assetId":"","decimals":6,"blockchain":"eth","symbol":"","price":1,"priceUpdatedAt":"2026-11-09T00:00:00Z"}
		]`)
	}))
	defer srv.Close()
	tokens, err := NewAt(srv.URL).Tokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("tokens = %+v", tokens)
	}
	if tokens[0].Symbol != "ETH" || tokens[1].Symbol != "wNEAR" {
		t.Fatalf("tokens are not sorted: %+v", tokens)
	}
	if tokens[1].Decimals != 24 || tokens[1].Blockchain != "near" {
		t.Fatalf("token = %+v", tokens[1])
	}
	if tokens[1].IconURL != "https://assets.example/near.png" {
		t.Fatalf("icon = %q", tokens[1].IconURL)
	}
}

func TestPositiveInteger(t *testing.T) {
	for s, want := range map[string]bool{
		"1": true, "1000000000000000000000000": true, "0": false, "": false,
		"1.5": false, "-1": false, "+1": false, "1e6": false, " 1": false,
	} {
		if got := positiveInteger(s); got != want {
			t.Errorf("positiveInteger(%q) = %v", s, got)
		}
	}
}
