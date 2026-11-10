package plugin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHostUnitsPreserveExactDecimalStrings(t *testing.T) {
	m := testManifest(t, Permissions{})
	r := testRunner(t, m, `
export function page() {
  const parsed = host.units.parse("1.2345", 6);
  return { title: parsed + "|" + host.units.format(parsed, 6) + "|" + host.units.format("-1234500", 6), sections: [] };
}`)
	page, err := r.Page("overview")
	if err != nil || page.Title != "1234500|1.2345|-1.2345" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestHostUnitsRejectPrecisionLossAndInvalidBounds(t *testing.T) {
	m := testManifest(t, Permissions{})
	r := testRunner(t, m, `
export function page() {
  const errors = [];
  for (const call of [
    () => host.units.parse("1.234", 2),
    () => host.units.parse("-1", 6),
    () => host.units.parse("1e6", 6),
    () => host.units.parse("1", 256),
    () => host.units.format(1234500, 6),
    () => host.units.parse(1.5, 6)
  ]) {
    try { call(); } catch (err) { errors.push(String(err.message)); }
  }
  return { title: String(errors.length), sections: [{ text: errors.join("|") }] };
}`)
	page, err := r.Page("overview")
	if err != nil || page.Title != "6" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestHostNearViewUsesPinnedQueryAndDecodesResult(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}})
	var gotChain, gotMethod string
	var gotParams map[string]any
	host := NewHost(HostDeps{Manifest: m, RPC: func(chain, method string, params any) (json.RawMessage, error) {
		gotChain, gotMethod = chain, method
		gotParams = params.(map[string]any)
		return json.Marshal(map[string]any{"result": []byte(`{"balance":"1000000000000000000000000"}`), "logs": []string{}, "block_height": 123, "block_hash": "hash"})
	}})
	r, err := NewRunner(m, []byte(`export function page() {
  const value = host.near.view({chainId:"near:testnet",contract:"pool.testnet",method:"get_account",args:{account_id:"alice.testnet"}});
  return {title:value.balance,sections:[]};
}`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil || page.Title != "1000000000000000000000000" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if gotChain != "near:testnet" || gotMethod != "query" || gotParams["request_type"] != "call_function" || gotParams["account_id"] != "pool.testnet" || gotParams["method_name"] != "get_account" || gotParams["finality"] != "final" {
		t.Fatalf("RPC = %s %s %+v", gotChain, gotMethod, gotParams)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotParams["args_base64"].(string))
	if err != nil || string(decoded) != `{"account_id":"alice.testnet"}` {
		t.Fatalf("args = %q, %v", decoded, err)
	}
}

func TestHostNearViewReturnsNullForEmptyResultAndBoundsArguments(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"near:testnet"}})
	host := NewHost(HostDeps{Manifest: m, RPC: func(string, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{"result":[],"logs":[]}`), nil
	}})
	r, err := NewRunner(m, []byte(`export function page() {
  const empty = host.near.view({chainId:"near:testnet",contract:"pool.testnet",method:"get_account"});
  let bounded = false;
  try { host.near.view({chainId:"near:testnet",contract:"pool.testnet",method:"get_account",args:{padding:"x".repeat(65536)}}); } catch (_) { bounded = true; }
  return {title:String(empty === null)+"|"+String(bounded),sections:[]};
}`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil || page.Title != "true|true" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestHostNearViewRejectsPermissionsAndMalformedResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    Manifest
		raw  json.RawMessage
		rpc  bool
	}{
		{name: "chain permission", m: testManifest(t, Permissions{}), raw: json.RawMessage(`{"result":""}`), rpc: true},
		{name: "evm chain", m: testManifest(t, Permissions{Chains: []string{"eip155:1"}}), raw: json.RawMessage(`{"result":""}`), rpc: true},
		{name: "missing broker", m: testManifest(t, Permissions{Chains: []string{"near:testnet"}})},
		{name: "missing result", m: testManifest(t, Permissions{Chains: []string{"near:testnet"}}), raw: json.RawMessage(`{}`), rpc: true},
		{name: "bad base64", m: testManifest(t, Permissions{Chains: []string{"near:testnet"}}), raw: json.RawMessage(`{"result":"%%%"}`), rpc: true},
		{name: "bad json bytes", m: testManifest(t, Permissions{Chains: []string{"near:testnet"}}), raw: json.RawMessage(`{"result":[110,111,116,32,106,115,111,110]}`), rpc: true},
		{name: "bad json base64", m: testManifest(t, Permissions{Chains: []string{"near:testnet"}}), raw: json.RawMessage(`{"result":"bm90IGpzb24="}`), rpc: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := HostDeps{Manifest: tc.m}
			if tc.rpc {
				deps.RPC = func(string, string, any) (json.RawMessage, error) { return tc.raw, nil }
			}
			host := NewHost(deps)
			r, err := NewRunner(tc.m, []byte(`export function page() { host.near.view({chainId:"near:testnet",contract:"pool.testnet",method:"get_account"}); return {}; }`), host, "en-US")
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err := r.Page("overview"); err == nil {
				t.Fatal("invalid NEAR view was accepted")
			}
		})
	}
}

func TestHostProtocolCapabilitiesRequireDeclaredNetwork(t *testing.T) {
	for _, call := range []string{
		`host.hyperliquid.info({type:"allMids"})`,
		`host.intents.tokens()`,
		`host.intents.status({depositAddress:"deposit.near"})`,
	} {
		m := testManifest(t, Permissions{})
		host := NewHost(HostDeps{Manifest: m,
			HyperliquidInfo: func(bool, string, map[string]any) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
			IntentsTokens:   func() (any, error) { return []any{}, nil },
			IntentsStatus:   func(string, string) (any, error) { return map[string]any{}, nil },
		})
		r, err := NewRunner(m, []byte(`export function page() { `+call+`; return {}; }`), host, "en-US")
		if err != nil {
			t.Fatal(err)
		}
		_, err = r.Page("overview")
		r.Close()
		if err == nil || !strings.Contains(err.Error(), "allowlist") {
			t.Fatalf("%s: err = %v", call, err)
		}
	}
}

func TestHostHyperliquidForwardsOnlyReadRequest(t *testing.T) {
	m := testManifest(t, Permissions{Network: []string{"api.hyperliquid.xyz"}})
	var gotType string
	var gotParams map[string]any
	host := NewHost(HostDeps{Manifest: m, HyperliquidInfo: func(testnet bool, kind string, params map[string]any) (json.RawMessage, error) {
		if testnet {
			t.Error("mainnet request selected testnet")
		}
		gotType, gotParams = kind, params
		return json.RawMessage(`{"BTC":"50000"}`), nil
	}})
	r, err := NewRunner(m, []byte(`export function page() { const value=host.hyperliquid.info({type:"allMids"}); return {title:value.BTC,sections:[]}; }`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil || page.Title != "50000" || gotType != "allMids" || len(gotParams) != 0 {
		t.Fatalf("page=%+v type=%q params=%+v err=%v", page, gotType, gotParams, err)
	}
}

func TestHostHyperliquidSelectsTestnetWithoutForwardingFlag(t *testing.T) {
	m := testManifest(t, Permissions{Network: []string{"api.hyperliquid-testnet.xyz"}})
	called := false
	host := NewHost(HostDeps{Manifest: m, HyperliquidInfo: func(testnet bool, kind string, params map[string]any) (json.RawMessage, error) {
		called = true
		if !testnet || kind != "meta" || len(params) != 0 {
			t.Fatalf("testnet=%v kind=%q params=%+v", testnet, kind, params)
		}
		return json.RawMessage(`{"universe":[]}`), nil
	}})
	r, err := NewRunner(m, []byte(`export function page() { host.hyperliquid.info({type:"meta",testnet:true}); return {}; }`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Page("overview"); err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestHostIntentsDryQuotePreservesAmountStrings(t *testing.T) {
	m := testManifest(t, Permissions{Network: []string{"1click.chaindefuser.com"}})
	host := NewHost(HostDeps{Manifest: m, IntentsDryQuote: func(request map[string]any) (any, error) {
		if request["amount"] != "1000000000000000000000000" {
			t.Fatalf("amount = %#v", request["amount"])
		}
		return map[string]any{"amountIn": request["amount"], "verified": true}, nil
	}})
	r, err := NewRunner(m, []byte(`export function page() { const q=host.intents.dryQuote({originAsset:"near",destinationAsset:"usdc",amount:"1000000000000000000000000",recipient:"alice",refundTo:"bob"}); return {title:q.amountIn+"|"+q.verified,sections:[]}; }`), host, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil || page.Title != "1000000000000000000000000|true" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestHostIntentsStatusRequiresTypedBoundedStrings(t *testing.T) {
	m := testManifest(t, Permissions{Network: []string{"1click.chaindefuser.com"}})
	r, err := NewRunner(m, []byte(`export function page() {
  let rejected = 0;
  for (const request of [
    {depositAddress:"deposit.near",depositMemo:42},
    {depositAddress:"x".repeat(513)},
    {depositAddress:"deposit.near",depositMemo:"x".repeat(513)}
  ]) {
    try { host.intents.status(request); } catch (_) { rejected++; }
  }
  return {title:String(rejected),sections:[]};
}`), NewHost(HostDeps{Manifest: m}), "en-US")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil || page.Title != "3" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
}

func TestHostIntentsRejectsWetQuoteInputAndUnavailableAdapter(t *testing.T) {
	m := testManifest(t, Permissions{Network: []string{"1click.chaindefuser.com"}})
	for name, deps := range map[string]HostDeps{
		"wet input":   {Manifest: m, IntentsDryQuote: func(map[string]any) (any, error) { return nil, errors.New("must not forward dry") }},
		"unavailable": {Manifest: m},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := NewRunner(m, []byte(`export function page() { host.intents.dryQuote({originAsset:"near",destinationAsset:"usdc",amount:"1",recipient:"a",refundTo:"b",dry:false}); return {}; }`), NewHost(deps), "en-US")
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if _, err := r.Page("overview"); err == nil {
				t.Fatal("invalid dry quote call was accepted")
			}
		})
	}
}
