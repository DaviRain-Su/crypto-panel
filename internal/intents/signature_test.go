package intents

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/base58"
)

const stagingManagerPublicKey = "ed25519:5J5tkaxyPoR3Q9S8LXfo5bWnXK5Z2bctJ4mB9gENh7co"

const stagingDryQuote = `{
	"correlationId": "7d6d78f0-601f-4022-9735-854a22ed9dcb",
	"timestamp": "2026-06-23T17:10:55.616Z",
	"signature": "ed25519:3yVRcYGXRVj2YqrUng4Ne2yiWgh9YQfer46KW6sXiWzoyRHgsifwDp1HSZW7VLRTdKXoMgxJce22LQ9dcoihyfu5",
	"quoteRequest": {
		"dry": true,
		"depositMode": "SIMPLE",
		"swapType": "EXACT_INPUT",
		"slippageTolerance": 100,
		"originAsset": "1cs_v1:btc:native:coin",
		"depositType": "ORIGIN_CHAIN",
		"destinationAsset": "nep141:eth-0xdac17f958d2ee523a2206206994597c13d831ec7.stft.near",
		"amount": "10000",
		"refundTo": "bc1q6mte80265ghwq4vsrpm9lnaz46uvdreu9z8wly",
		"refundType": "ORIGIN_CHAIN",
		"recipient": "0xcac3C41676deF4FE375E57118f3eB83A99105577",
		"recipientType": "DESTINATION_CHAIN",
		"deadline": "2026-06-23T19:00:00.000Z",
		"confidentiality": "public",
		"quoteWaitingTimeMs": 0,
		"appFees": [{"recipient":"5880ad2b362620fadf759cbceb1cd5737ce8c6ed7fb8e9942881e6731f9247dd","fee":10}]
	},
	"quote": {
		"amountIn": "10000",
		"amountInFormatted": "0.0001",
		"amountInUsd": "6.237600000000",
		"minAmountIn": "10000",
		"amountOut": "5935024",
		"amountOutFormatted": "5.935024",
		"amountOutUsd": "5.928631979152",
		"minAmountOut": "5875673",
		"timeEstimate": 812,
		"refundFee": "1900",
		"withdrawFee": "300000"
	}
}`

func TestOfficialStagingDryQuoteVector(t *testing.T) {
	now := time.Date(2026, 6, 23, 17, 15, 55, 0, time.UTC)
	if _, err := verifyQuoteResponse([]byte(stagingDryQuote), stagingManagerPublicKey, now); err != nil {
		t.Fatal(err)
	}
}

func TestStableSignedPayloadUsesJavaScriptNumbersAndSelectsFields(t *testing.T) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{
		"timestamp":"2026-01-01T00:00:00Z",
		"quoteRequest":{
			"dry":true,"swapType":"EXACT_INPUT","slippageTolerance":100,
			"originAsset":"a\u003cb","depositType":"ORIGIN_CHAIN","destinationAsset":"b",
			"amount":"1","refundTo":"r","refundType":"ORIGIN_CHAIN",
			"recipient":"p","recipientType":"DESTINATION_CHAIN","deadline":"d",
			"quoteWaitingTimeMs":900719925474099312345,
			"referral":"","virtualChainRecipient":"v","customRecipientMsg":null,
			"sessionId":"not signed"
		},
		"quote":{
			"amountIn":"1","amountInFormatted":"1","amountInUsd":"0.000000000000000001",
			"minAmountIn":"1","amountOut":"2","amountOutFormatted":"2",
			"amountOutUsd":"3.00","minAmountOut":"2","depositAddress":"not signed in a dry quote"
		}
	}`), &response); err != nil {
		t.Fatal(err)
	}
	got, err := signedQuotePayload(response)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"amount":"1","amountIn":"1","amountInFormatted":"1","amountInUsd":"0.000000000000000001","amountOut":"2","amountOutFormatted":"2","amountOutUsd":"3.00","deadline":"d","depositType":"ORIGIN_CHAIN","destinationAsset":"b","dry":true,"minAmountIn":"1","minAmountOut":"2","originAsset":"a<b","quoteWaitingTimeMs":900719925474099300000,"recipient":"p","recipientType":"DESTINATION_CHAIN","refundTo":"r","refundType":"ORIGIN_CHAIN","slippageTolerance":100,"swapType":"EXACT_INPUT","timestamp":"2026-01-01T00:00:00Z","virtualChainRecipient":"v"}`
	if string(got) != want {
		t.Fatalf("payload =\n%s\nwant =\n%s", got, want)
	}
}

func TestStableSignedPayloadCanonicalizesNumericSpellings(t *testing.T) {
	body := []byte(`{
		"timestamp":"2026-01-01T00:00:00Z",
		"quoteRequest":{"dry":true,"swapType":"EXACT_INPUT","slippageTolerance":1e2,
			"originAsset":"a","depositType":"ORIGIN_CHAIN","destinationAsset":"b",
			"amount":"1","refundTo":"r","refundType":"ORIGIN_CHAIN",
			"recipient":"p","recipientType":"DESTINATION_CHAIN","deadline":"d"},
		"quote":{"amountIn":"1","amountInFormatted":"1","amountInUsd":"1","minAmountIn":"1",
			"amountOut":"2","amountOutFormatted":"2","amountOutUsd":"2","minAmountOut":"2"}
	}`)
	got, err := signedQuotePayload(mustRawMap(t, string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(`"slippageTolerance":100`)) || bytes.Contains(got, []byte("1e2")) {
		t.Fatalf("numeric spelling was not canonicalized: %s", got)
	}
}

func TestJavaScriptNumberNotationBoundaries(t *testing.T) {
	for input, want := range map[string]string{
		"1e-7":  "1e-7",
		"1e-6":  "0.000001",
		"1e20":  "100000000000000000000",
		"1e21":  "1e+21",
		"-1e21": "-1e+21",
		"0":     "0",
	} {
		var number float64
		if err := json.Unmarshal([]byte(input), &number); err != nil {
			t.Fatal(err)
		}
		if got := javascriptNumber(number); got != want {
			t.Errorf("%s: got %s, want %s", input, got, want)
		}
	}
	if got := javascriptNumber(math.Copysign(0, -1)); got != "0" {
		t.Errorf("negative zero: got %s", got)
	}
}

func signTestQuote(t *testing.T, key ed25519.PrivateKey, body string) string {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		t.Fatal(err)
	}
	payload, err := signedQuotePayload(top)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	hash := base58.Encode(digest[:])
	top["signature"], _ = json.Marshal("ed25519:" + base58.Encode(ed25519.Sign(key, []byte(hash))))
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestSignedQuotePayloadRequiresSignedFields(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"quoteRequest":{},"quote":{},"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"quoteRequest":null,"quote":{},"timestamp":"2026-01-01T00:00:00Z"}`,
	} {
		if _, err := signedQuotePayload(mustRawMap(t, body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func mustRawMap(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestQuoteFreshnessBoundaries(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		timestamp string
		valid     bool
	}{
		{"2026-01-01T00:00:00Z", true},
		{"2026-01-01T00:10:00Z", true},
		{"2025-12-31T23:59:59Z", false},
		{"2026-01-01T00:10:01Z", false},
		{"not a timestamp", false},
	} {
		if err := checkQuoteTimestamp(tc.timestamp, now); (err == nil) != tc.valid {
			t.Errorf("timestamp %q: err = %v, valid = %v", tc.timestamp, err, tc.valid)
		}
	}
}

func TestProductionDoesNotTrustTheStagingKey(t *testing.T) {
	if productionManagerPublicKey == stagingManagerPublicKey {
		t.Fatal("production must not trust the staging key")
	}
}
