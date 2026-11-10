package intents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
			{"assetId":"nep141:wrap.near","decimals":24,"blockchain":"near","symbol":"wNEAR","price":2.5,"priceUpdatedAt":"2026-11-09T00:00:00Z"},
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
}

func TestTokensDropsUnusableAssets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[
			{"assetId":"nep141:wrap.near","decimals":24,"blockchain":"near","symbol":"wNEAR"},
			{"assetId":"nep141:missing-decimals.near","blockchain":"near","symbol":"MISS"},
			{"assetId":"nep141:null-decimals.near","decimals":null,"blockchain":"near","symbol":"NULL"},
			{"assetId":"nep141:neg.near","decimals":-3,"blockchain":"near","symbol":"NEG"},
			{"assetId":"nep141:frac.near","decimals":24.5,"blockchain":"near","symbol":"FRAC"},
			{"assetId":"nep141:huge.near","decimals":4000000000,"blockchain":"near","symbol":"HUGE"},
			{"assetId":"nep141:exp.near","decimals":1e300,"blockchain":"near","symbol":"EXP"},
			{"assetId":"nep141:wide.near","decimals":256,"blockchain":"near","symbol":"WIDE"},
			{"assetId":"nep141:noblock.near","decimals":18,"blockchain":"","symbol":"NOB"},
			{"assetId":"","decimals":6,"blockchain":"eth","symbol":"EMPTY"}
		]`)
	}))
	defer srv.Close()
	tokens, err := NewAt(srv.URL).Tokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0].Symbol != "wNEAR" || tokens[0].Decimals != 24 {
		t.Fatalf("tokens = %+v", tokens)
	}
}

func TestQuoteFailsClosed(t *testing.T) {
	good := QuoteRequest{
		OriginAsset:      "nep141:wrap.near",
		DestinationAsset: "nep141:eth.omft.near",
		Amount:           "1000",
		Recipient:        "0xabc",
		RefundTo:         "alice.testnet",
	}
	echo := func(change string) string {
		req := `{
			"dry": true,
			"swapType": "EXACT_INPUT",
			"slippageTolerance": 100,
			"originAsset": "nep141:wrap.near",
			"depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near",
			"amount": "1000",
			"refundTo": "alice.testnet",
			"refundType": "ORIGIN_CHAIN",
			"recipient": "0xabc",
			"recipientType": "DESTINATION_CHAIN"
		}`
		if change != "" {
			req = change
		}
		return `{
			"correlationId": "c1",
			"quoteRequest": ` + req + `,
			"quote": {
				"amountIn": "1000",
				"amountOut": "2000",
				"minAmountOut": "1900"
			}
		}`
	}
	bodies := []string{
		`{}`,
		`null`,
		`{"quote":null,"correlationId":"c1"}`,
		`{"correlationId":"c1","quote":{"amountInFormatted":"1"}}`,
		`{"correlationId":"","quoteRequest":` + `{
			"dry": true, "swapType": "EXACT_INPUT", "slippageTolerance": 100,
			"originAsset": "nep141:wrap.near", "depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near", "amount": "1000",
			"refundTo": "alice.testnet", "refundType": "ORIGIN_CHAIN",
			"recipient": "0xabc", "recipientType": "DESTINATION_CHAIN"
		}` + `,"quote":{"amountIn":"1000","amountOut":"2000","minAmountOut":"1900"}}`,
		`{"correlationId":"c1","quoteRequest":{
			"dry": false, "swapType": "EXACT_INPUT", "slippageTolerance": 100,
			"originAsset": "nep141:wrap.near", "depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near", "amount": "1000",
			"refundTo": "alice.testnet", "refundType": "ORIGIN_CHAIN",
			"recipient": "0xabc", "recipientType": "DESTINATION_CHAIN"
		},"quote":{"amountIn":"1000","amountOut":"2000","minAmountOut":"1900"}}`,
		`{"correlationId":"c1","quoteRequest":{
			"dry": true, "swapType": "EXACT_INPUT", "slippageTolerance": 100,
			"originAsset": "nep141:wrap.near", "depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near", "amount": "1000",
			"refundTo": "alice.testnet", "refundType": "ORIGIN_CHAIN",
			"recipient": "0xchanged", "recipientType": "DESTINATION_CHAIN"
		},"quote":{"amountIn":"1000","amountOut":"2000","minAmountOut":"1900"}}`,
		`{"correlationId":"c1","quoteRequest":{
			"dry": true, "swapType": "EXACT_INPUT", "slippageTolerance": 100,
			"originAsset": "nep141:wrap.near", "depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near", "amount": "1000",
			"refundTo": "alice.testnet", "refundType": "ORIGIN_CHAIN",
			"recipient": "0xabc", "recipientType": "DESTINATION_CHAIN"
		},"quote":{"depositAddress":"0xATTACKERDEPOSIT","amountIn":"1000","amountOut":"2000","minAmountOut":"1900"}}`,
		`{"correlationId":"c1","quoteRequest":{
			"dry": true, "swapType": "EXACT_INPUT", "slippageTolerance": 100,
			"originAsset": "nep141:wrap.near", "depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near", "amount": "1000",
			"refundTo": "alice.testnet", "refundType": "ORIGIN_CHAIN",
			"recipient": "0xabc", "recipientType": "DESTINATION_CHAIN"
		},"quote":{"amountIn":"0","amountOut":"2000","minAmountOut":"1900"}}`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, body)
			}))
			defer srv.Close()
			if _, err := NewAt(srv.URL).Quote(context.Background(), good); err == nil {
				t.Fatal("a bad quote was accepted")
			}
		})
	}
	// The matching dry echo is accepted and carries no deposit address.
	okBody := echo("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, okBody)
	}))
	defer srv.Close()
	q, err := NewAt(srv.URL).Quote(context.Background(), good)
	if err != nil {
		t.Fatal(err)
	}
	if q.DepositAddress != "" || q.CorrelationID != "c1" || q.MinAmountOut != "1900" {
		t.Fatalf("quote = %+v", q)
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	evilCalled := false
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilCalled = true
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"correlationId":"evil","quote":{"amountIn":"1","amountOut":"1","minAmountOut":"1"}}`)
	}))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err := NewAt(srv.URL).Quote(context.Background(), QuoteRequest{
		OriginAsset: "nep141:wrap.near", DestinationAsset: "nep141:eth.omft.near",
		Amount: "1", Recipient: "0xabc", RefundTo: "alice.testnet",
	})
	if err == nil || evilCalled {
		t.Fatalf("err = %v, evil called = %v", err, evilCalled)
	}
}

func TestOnlyOKIsSuccess(t *testing.T) {
	body := `{
		"correlationId": "c1",
		"quoteRequest": {
			"dry": true, "swapType": "EXACT_INPUT", "slippageTolerance": 100,
			"originAsset": "nep141:wrap.near", "depositType": "ORIGIN_CHAIN",
			"destinationAsset": "nep141:eth.omft.near", "amount": "1",
			"refundTo": "alice.testnet", "refundType": "ORIGIN_CHAIN",
			"recipient": "0xabc", "recipientType": "DESTINATION_CHAIN"
		},
		"quote": {"amountIn": "1", "amountOut": "1", "minAmountOut": "1"}
	}`
	for _, status := range []int{http.StatusCreated, 299, http.StatusTemporaryRedirect} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, body)
		}))
		_, err := NewAt(srv.URL).Quote(context.Background(), QuoteRequest{
			OriginAsset: "nep141:wrap.near", DestinationAsset: "nep141:eth.omft.near",
			Amount: "1", Recipient: "0xabc", RefundTo: "alice.testnet",
		})
		srv.Close()
		if err == nil {
			t.Fatalf("status %d was treated as success", status)
		}
	}
}

func TestJSONDecodeErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{`)
	}))
	defer srv.Close()
	if _, err := NewAt(srv.URL).Tokens(context.Background()); err == nil {
		t.Fatal("broken JSON was accepted")
	}
}

func TestResponseIsCappedAndTimedOut(t *testing.T) {
	c := New()
	if c.http.Timeout != clientTimeout {
		t.Fatalf("timeout = %s", c.http.Timeout)
	}
	if err := c.http.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect = %v", err)
	}
	if maxResponseBytes != 4<<20 {
		t.Fatalf("cap = %d", maxResponseBytes)
	}
	padding := strings.Repeat("a", maxResponseBytes)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"assetId":"`+padding+`","symbol":"X","blockchain":"near","decimals":8}]`)
	}))
	defer srv.Close()
	if _, err := NewAt(srv.URL).Tokens(context.Background()); err == nil {
		t.Fatal("a body over 4MiB was decoded")
	}

	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hung.Close()
	short := NewAt(hung.URL)
	short.http.Timeout = 30 * time.Millisecond
	if _, err := short.Tokens(context.Background()); err == nil {
		t.Fatal("a request past the client timeout succeeded")
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
