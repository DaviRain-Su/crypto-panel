package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/base58"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/intents"
	"github.com/davirain-su/lintel/internal/wallet"
)

const (
	testOriginAsset = "nep141:wrap.near"
	testDestAsset   = "nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near"
	testRecipient   = "0x2527D02599Ba641c19FEa793cD0F167589a0f10D"
)

// intentsTestApp is an app on the Intents page with the asset list already
// loaded and the form filled.
func intentsTestApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.intentsTokens = []intents.Token{
		{AssetID: testOriginAsset, Symbol: "wNEAR", Blockchain: "near", Decimals: 24},
		{AssetID: testDestAsset, Symbol: "USDC", Blockchain: "eth", Decimals: 6},
	}
	a.intentsTokensLoaded = true
	a.intentsOrigin = testOriginAsset
	a.intentsDestination = testDestAsset
	a.intentsAmount = "1"
	a.intentsRecipient = testRecipient
	a.intentsRefund = "alice.testnet"
	return a
}

var intentsTestKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, ed25519.SeedSize))
var intentsTestPublicKey = "ed25519:" + base58.Encode(intentsTestKey.Public().(ed25519.PublicKey))

// quoteServer answers /v0/quote with a signed dry quote and records the request.
func quoteServer(t *testing.T, amount *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if amount != nil {
			*amount, _ = body["amount"].(string)
		}
		requestJSON, _ := json.Marshal(body)
		quoteJSON := []byte(`{"amountIn":"1000000000000000000000000","amountInFormatted":"1","amountInUsd":"2.5","minAmountIn":"1000000000000000000000000","amountOut":"10000000","amountOutFormatted":"10","amountOutUsd":"10","minAmountOut":"9900000","deadline":"2026-11-09T12:30:00Z","timeEstimate":120}`)
		var requestFields map[string]json.RawMessage
		var quoteFields map[string]json.RawMessage
		_ = json.Unmarshal(requestJSON, &requestFields)
		_ = json.Unmarshal(quoteJSON, &quoteFields)
		signed := map[string]json.RawMessage{"timestamp": json.RawMessage(`"2026-11-09T00:00:00Z"`)}
		for _, key := range []string{"dry", "swapType", "slippageTolerance", "originAsset", "depositType", "destinationAsset", "amount", "refundTo", "refundType", "recipient", "recipientType", "deadline"} {
			signed[key] = requestFields[key]
		}
		for _, key := range []string{"amountIn", "amountInFormatted", "amountInUsd", "minAmountIn", "amountOut", "amountOutFormatted", "amountOutUsd", "minAmountOut"} {
			signed[key] = quoteFields[key]
		}
		payload, _ := json.Marshal(signed)
		digest := sha256.Sum256(payload)
		hash := base58.Encode(digest[:])
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"correlationId": "quote-1",
			"timestamp":     "2026-11-09T00:00:00Z",
			"signature":     "ed25519:" + base58.Encode(ed25519.Sign(intentsTestKey, []byte(hash))),
			"quoteRequest":  body,
			"quote":         json.RawMessage(quoteJSON),
		})
	}))
}

func intentsTestClient(url string) *intents.Client {
	return intents.NewAtWithOptions(url,
		intents.WithVerifierKey(intentsTestPublicKey),
		intents.WithClock(func() time.Time { return time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC) }),
	)
}

func TestIntentsPageView(t *testing.T) {
	a := intentsTestApp(t)
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Intents", "CROSS-CHAIN SWAPS, QUOTED BY 1CLICK", "Pay with", "Receive",
		"Amount", "Recipient", "Refund to", "Get a dry quote",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestIntentsDefaults(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	a.intentsTokens = []intents.Token{
		{AssetID: "nep141:eth.omft.near", Symbol: "ETH", Blockchain: "eth", Decimals: 18},
		{AssetID: testOriginAsset, Symbol: "wNEAR", Blockchain: "near", Decimals: 24},
		{AssetID: testDestAsset, Symbol: "USDC", Blockchain: "eth", Decimals: 6},
	}
	a.applyIntentsDefaults()
	if a.intentsOrigin != testOriginAsset {
		t.Fatalf("origin = %q", a.intentsOrigin)
	}
	if a.intentsDestination != testDestAsset {
		t.Fatalf("destination = %q", a.intentsDestination)
	}
	if a.intentsRefund != a.settings.NearWallet[0].AccountID {
		t.Fatalf("refund = %q", a.intentsRefund)
	}
}

func TestIntentsQuoteShowsResultLanguage(t *testing.T) {
	a := intentsTestApp(t)
	var amount string
	srv := quoteServer(t, &amount)
	defer srv.Close()
	a.intentsClient = intentsTestClient(srv.URL)
	var captured intentsApprovalRequest
	a.approveIntents = func(req intentsApprovalRequest) bool {
		captured = req
		return true
	}
	a.requestIntentsQuote()
	if amount != "1000000000000000000000000" {
		t.Fatalf("the quote asked for %q base units", amount)
	}
	if a.intentsBusy || a.intentsErr != "" {
		t.Fatalf("busy = %v, err = %q", a.intentsBusy, a.intentsErr)
	}
	if a.intentsQuote == nil || !a.intentsAccepted {
		t.Fatalf("quote = %+v, accepted = %v", a.intentsQuote, a.intentsAccepted)
	}
	if a.intentsNotice != i18n.T(i18n.English, i18n.IntentsAccepted) {
		t.Fatalf("notice = %q", a.intentsNotice)
	}
	if captured.origin.Symbol != "wNEAR" || captured.destination.Symbol != "USDC" {
		t.Fatalf("approval assets = %+v %+v", captured.origin, captured.destination)
	}

	ap := &intentsApproval{
		t:   func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		req: captured,
	}
	tt := ui.NewTester(ap.view, 480, 620)
	for _, want := range []string{
		"NEAR INTENTS · DRY RUN", "Swap via NEAR Intents?",
		"You pay", "1 wNEAR", "You receive at least", "9.9 USDC",
		"Deadline", "Deposit", "None — a dry quote issues no deposit address",
		"Refund to", "alice.testnet", "Recipient", wallet.Short(testRecipient),
		"Intent", "Swap 1 wNEAR on NEAR for at least 9.9 USDC on Ethereum; refund to alice.testnet.",
		"Requested by", "Lintel · dry run",
		"no funds move and nothing is signed", "Accept quote", "Reject",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestBadQuoteDoesNotOpenApproval(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`null`,
		`{"quote":null,"correlationId":"c"}`,
		`{"correlationId":"","quote":{"amountIn":"1","amountOut":"1","minAmountOut":"1"}}`,
		`{"correlationId":"c","quote":{"amountInFormatted":"1"}}`,
	} {
		t.Run(body, func(t *testing.T) {
			a := intentsTestApp(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, body)
			}))
			defer srv.Close()
			a.intentsClient = intentsTestClient(srv.URL)
			opened := false
			a.approveIntents = func(intentsApprovalRequest) bool {
				opened = true
				return true
			}
			a.requestIntentsQuote()
			if opened || a.intentsQuote != nil || a.intentsAccepted {
				t.Fatalf("approval opened for %s (err %q)", body, a.intentsErr)
			}
			if a.intentsErr == "" {
				t.Fatal("a bad quote was treated as success")
			}
		})
	}
}

func TestIntentsRejected(t *testing.T) {
	a := intentsTestApp(t)
	srv := quoteServer(t, nil)
	defer srv.Close()
	a.intentsClient = intentsTestClient(srv.URL)
	a.approveIntents = func(intentsApprovalRequest) bool { return false }
	a.requestIntentsQuote()
	if a.intentsAccepted {
		t.Fatal("a rejected quote was accepted")
	}
	if a.intentsQuote == nil {
		t.Fatal("the quote is not shown")
	}
	if a.intentsNotice != i18n.T(i18n.English, i18n.IntentsRejected) {
		t.Fatalf("notice = %q", a.intentsNotice)
	}
}

func TestIntentsQuoteNeedsARecipient(t *testing.T) {
	a := intentsTestApp(t)
	a.intentsRecipient = ""
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	a.intentsClient = intentsTestClient(srv.URL)
	a.requestIntentsQuote()
	if calls != 0 {
		t.Fatal("a quote was asked for without a recipient")
	}
	if a.intentsErr != i18n.T(i18n.English, i18n.IntentsRecipientRequired) {
		t.Fatalf("err = %q", a.intentsErr)
	}
}

func TestIntentsQuoteNeedsTheRightDecimals(t *testing.T) {
	a := intentsTestApp(t)
	a.intentsAmount = "1.0000001" // wNEAR has 24 decimals, so this is fine
	srv := quoteServer(t, nil)
	defer srv.Close()
	a.intentsClient = intentsTestClient(srv.URL)
	a.approveIntents = func(intentsApprovalRequest) bool { return true }
	a.requestIntentsQuote()
	if a.intentsErr != "" {
		t.Fatalf("err = %q", a.intentsErr)
	}

	// USDC has 6 decimals; seven is too many.
	a.intentsOrigin = testDestAsset
	a.intentsAmount = "1.0000001"
	a.intentsQuote = nil
	a.requestIntentsQuote()
	if !strings.Contains(a.intentsErr, "6") {
		t.Fatalf("err = %q", a.intentsErr)
	}
}

func TestIntentsApprovalRejectButton(t *testing.T) {
	a := intentsTestApp(t)
	srv := quoteServer(t, nil)
	defer srv.Close()
	a.intentsClient = intentsTestClient(srv.URL)
	var captured intentsApprovalRequest
	a.approveIntents = func(req intentsApprovalRequest) bool {
		captured = req
		return true
	}
	a.requestIntentsQuote()
	ap := &intentsApproval{
		t:   func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		req: captured,
	}
	decided := false
	ap.onDecide = func(ok bool) { decided = ok }
	tt := ui.NewTester(ap.view, 480, 620)
	if err := tt.Click("Reject"); err != nil {
		t.Fatal(err)
	}
	if decided {
		t.Fatal("Reject approved the quote")
	}
}

func TestIntentsAssetSearchFilters(t *testing.T) {
	a := intentsTestApp(t)
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Pay with"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown) // Down opens the whole list
	if !tt.HasText("wNEAR · NEAR") || !tt.HasText("USDC · Ethereum") {
		t.Fatalf("the picker did not open: %q", tt.Texts())
	}
	tt.Key(ui.Cmd, ui.KeyA) // replace the choice, rather than append to it
	tt.Type("usd")
	if !tt.HasText("USDC · Ethereum") {
		t.Fatalf("the search did not find USDC: %q", tt.Texts())
	}
	if tt.HasText("wNEAR · NEAR") {
		t.Fatalf("the search did not filter: %q", tt.Texts())
	}
}

func TestIntentsAssetSearchChooses(t *testing.T) {
	a := intentsTestApp(t)
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Pay with"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("usdc")
	tt.Key(0, ui.KeyEnter)
	if a.intentsOrigin != testDestAsset {
		t.Fatalf("origin = %q", a.intentsOrigin)
	}
}

func TestIntentsAssetSearchArrows(t *testing.T) {
	a := intentsTestApp(t)
	a.intentsTokens = append(a.intentsTokens, intents.Token{
		AssetID: "nep141:eth.omft.near", Symbol: "ETH", Blockchain: "eth", Decimals: 18,
	})
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Pay with"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("eth") // ETH · Ethereum first, then USDC · Ethereum
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if a.intentsOrigin != testDestAsset {
		t.Fatalf("origin = %q", a.intentsOrigin)
	}
}

func TestIntentsAssetPickerKeepsATypedRefund(t *testing.T) {
	a := intentsTestApp(t)
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Pay with"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("usdc")
	tt.Key(0, ui.KeyEnter)
	if a.intentsRefund != "alice.testnet" {
		t.Fatalf("a typed refund changed to %q", a.intentsRefund)
	}
}

func TestIntentsAssetPickerFollowsTheDefaultRefund(t *testing.T) {
	a := intentsTestApp(t)
	a.createNearAccount()
	a.settings.Wallet = []wallet.Account{{Addr: walletTestAddr}}
	a.intentsRefund = a.settings.NearWallet[0].AccountID
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if err := tt.Click("Pay with"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("usdc")
	tt.Key(0, ui.KeyEnter)
	if a.intentsRefund != walletTestAddr {
		t.Fatalf("the default refund did not follow the chain: %q", a.intentsRefund)
	}
}

func TestIntentsPageShowsTheQuote(t *testing.T) {
	a := intentsTestApp(t)
	srv := quoteServer(t, nil)
	defer srv.Close()
	a.intentsClient = intentsTestClient(srv.URL)
	a.approveIntents = func(intentsApprovalRequest) bool { return true }
	a.requestIntentsQuote()
	a.route = routeIntents
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Your dry quote", "DRY RUN", "You pay", "1 wNEAR",
		"You receive at least", "9.9 USDC", "Review in the approval window",
		"Quoted by 1Click · quote-1",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestIntentsErrorText(t *testing.T) {
	a := newTestApp(t)
	cases := map[string]struct {
		raw   string
		says  string
		keeps string
	}{
		"recipient":  {"1Click: recipient is not valid", "recipient address", "1Click: recipient is not valid"},
		"asset pair": {"1Click: tokenIn is not valid", "asset pair", "1Click: tokenIn is not valid"},
		"server":     {"1Click: Internal server error", "internal error", "1Click: Internal server error"},
		"unknown":    {"1Click: the amount is too small", "the amount is too small", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := a.intentsErrorText(errors.New(tc.raw))
			if !strings.Contains(got, tc.says) {
				t.Errorf("intentsErrorText(%v) = %q, want it to say %q", tc.raw, got, tc.says)
			}
			if tc.keeps != "" && !strings.Contains(got, "("+tc.keeps+")") {
				t.Errorf("intentsErrorText(%v) = %q, want the original kept in parentheses", tc.raw, got)
			}
		})
	}
}
