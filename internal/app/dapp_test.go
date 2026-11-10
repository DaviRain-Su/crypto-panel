package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/wallet"
)

// dappReader is a chain.Reader whose RPC is the test's fake.
type dappReader struct{ rpc *nearRPC }

func (d *dappReader) Account(context.Context, string) (chain.AccountBalances, error) {
	return chain.AccountBalances{}, nil
}
func (d *dappReader) Block(context.Context) (uint64, error) { return 0, nil }
func (d *dappReader) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return d.rpc.call(ctx, method, params)
}

// newDappApp is an app on NEAR testnet with one unlocked NEAR account and a
// fake reader.
func newDappApp(t *testing.T) (*App, *nearRPC, wallet.NearAccount) {
	t.Helper()
	a := newTestApp(t)
	a.settings.NetworkID = chains.NearTestnet.ID
	a.settings = a.settings.Validate()
	a.createNearAccount()
	acc := a.settings.NearWallet[0]
	rpc := &nearRPC{accessKey: `{"nonce":1,"permission":"FullAccess"}`}
	a.reader = &dappReader{rpc: rpc}
	a.dappBridge = newDappBridge(new(mygo.Page))
	a.dappBridge.approve = func(_ context.Context, req approvalRequest) bool { return a.approve(req) }
	a.dappBridge.connect = func(_ context.Context, origin string) bool { return a.confirmConnect(origin) }
	return a, rpc, acc
}

func TestDappOrigin(t *testing.T) {
	cases := map[string]string{
		"https://app.testnet.near.org/swap?x=1": "https://app.testnet.near.org",
		"https://example.com":                   "https://example.com",
		"HTTPS://EXAMPLE.COM:443/app":           "https://example.com",
		"http://LOCALHOST:80/app":               "http://localhost",
		"http://[::1]:8080/app":                 "http://[::1]:8080",
		"http://localhost:3000/app":             "http://localhost:3000",
		"http://127.0.0.1:8080":                 "http://127.0.0.1:8080",
		"http://example.com":                    "",
		"file:///etc/passwd":                    "",
		"javascript:alert(1)":                   "",
		"":                                      "",
	}
	for raw, want := range cases {
		if got := dappOrigin(raw); got != want {
			t.Errorf("dappOrigin(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDappConnect(t *testing.T) {
	a, _, acc := newDappApp(t)
	var asked string
	a.confirmConnect = func(origin string) bool { asked = origin; return true }
	res, err := a.dappConnect(context.Background(), "https://dapp.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "https://dapp.test" {
		t.Fatalf("asked about %q", asked)
	}
	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result = %T", res)
	}
	accounts, ok := out["accounts"].([]dappAccount)
	if !ok || len(accounts) != 1 || accounts[0].AccountID != acc.AccountID {
		t.Fatalf("accounts = %+v", out["accounts"])
	}
	if !a.dappConnected["https://dapp.test"] {
		t.Fatal("the origin is not connected")
	}
}

func TestDappConnectRejected(t *testing.T) {
	a, _, _ := newDappApp(t)
	a.confirmConnect = func(string) bool { return false }
	if _, err := a.dappConnect(context.Background(), "https://dapp.test", nil); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
	if a.dappConnected["https://dapp.test"] {
		t.Fatal("a rejected connection was remembered")
	}
}

func TestDisconnectRequiresANewConnection(t *testing.T) {
	a, _, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	if _, err := a.handleDappRequest(context.Background(), "https://dapp.test", dappRequest{ID: "bye", Method: "disconnect"}); err != nil {
		t.Fatal(err)
	}
	if a.dappConnected["https://dapp.test"] {
		t.Fatal("disconnect left the origin connected")
	}
	asked := false
	a.confirmConnect = func(string) bool { asked = true; return true }
	if _, err := a.dappConnect(context.Background(), "https://dapp.test", nil); err != nil {
		t.Fatal(err)
	}
	if !asked {
		t.Fatal("connect after disconnect did not ask again")
	}
}

func TestSwitchingNearNetworkDropsTheConnection(t *testing.T) {
	a, _, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	a.setNetwork(chains.NearMainnet.ID)
	if a.dappConnected["https://dapp.test"] {
		t.Fatal("a testnet connection survived the switch to mainnet")
	}
}

func TestDappGetAccountsNeedsANearNetwork(t *testing.T) {
	a := newTestApp(t)
	a.dappConnected["https://dapp.test"] = true
	if _, err := a.dappGetAccounts("https://dapp.test", "testnet"); err == nil || !strings.Contains(err.Error(), "NEAR network") {
		t.Fatalf("err = %v", err)
	}
}

func TestDappConnectNeedsANearNetwork(t *testing.T) {
	a := newTestApp(t)
	a.createNearAccount()
	if _, err := a.dappConnect(context.Background(), "https://dapp.test", nil); err == nil || !strings.Contains(err.Error(), "NEAR network") {
		t.Fatalf("err = %v", err)
	}
}

func TestDappConnectNeedsAnAccount(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.NearTestnet.ID
	a.settings = a.settings.Validate()
	if _, err := a.dappConnect(context.Background(), "https://dapp.test", nil); err == nil || !strings.Contains(err.Error(), "no NEAR account") {
		t.Fatalf("err = %v", err)
	}
}

func TestDappSignNeedsAConnection(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	params := json.RawMessage(`{"receiverId":"bob.testnet","actions":[{"type":"transfer","amount":"1"}]}`)
	if _, err := a.dappSign(context.Background(), "https://dapp.test", params); err == nil || !strings.Contains(err.Error(), "connect first") {
		t.Fatalf("err = %v", err)
	}
	if rpc.broadcast != nil {
		t.Fatal("an unconnected page signed")
	}
}

func TestDappSign(t *testing.T) {
	a, rpc, acc := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	var asked approvalRequest
	a.approve = func(req approvalRequest) bool { asked = req; return true }
	params := json.RawMessage(`{"receiverId":"bob.testnet","actions":[{"type":"transfer","amount":"1500000000000000000000000"}],"description":"Buy a coffee"}`)
	res, err := a.dappSign(context.Background(), "https://dapp.test", params)
	if err != nil {
		t.Fatal(err)
	}
	if rpc.broadcast == nil {
		t.Fatal("nothing was broadcast")
	}
	if asked.sourceKind != "dapp" || asked.pluginName != "https://dapp.test" {
		t.Fatalf("approval source = %q %q", asked.sourceKind, asked.pluginName)
	}
	if asked.fromAddr != acc.AccountID {
		t.Fatalf("approval from = %q", asked.fromAddr)
	}
	if asked.near == nil || asked.near.ReceiverID != "bob.testnet" {
		t.Fatalf("approval summary = %+v", asked.near)
	}
	out, ok := res.(json.RawMessage)
	if !ok {
		t.Fatalf("result = %T", res)
	}
	var outcome struct {
		Status      map[string]json.RawMessage `json:"status"`
		Transaction struct {
			Hash string `json:"hash"`
		} `json:"transaction"`
		TransactionOutcome struct {
			ID string `json:"id"`
		} `json:"transaction_outcome"`
	}
	if err := json.Unmarshal(out, &outcome); err != nil || outcome.Transaction.Hash == "" || outcome.Transaction.Hash != outcome.TransactionOutcome.ID || outcome.Status["SuccessValue"] == nil {
		t.Fatalf("full outcome = %s, err = %v", out, err)
	}
}

func TestDappNetworkMismatch(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	for name, call := range map[string]func() error{
		"connect": func() error {
			_, err := a.dappConnect(context.Background(), "https://dapp.test", json.RawMessage(`{"network":"mainnet"}`))
			return err
		},
		"accounts": func() error {
			_, err := a.dappGetAccounts("https://dapp.test", "mainnet")
			return err
		},
		"sign": func() error {
			_, err := a.dappSign(context.Background(), "https://dapp.test", json.RawMessage(`{"network":"mainnet","receiverId":"bob.near","actions":[{"type":"transfer","amount":"1"}]}`))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil || !strings.Contains(err.Error(), "network mainnet does not match selected testnet") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	if rpc.calls != nil {
		t.Fatal("a network mismatch reached the RPC")
	}
}

func TestDappConnectSkipsTheQuestionWhenConnected(t *testing.T) {
	a, _, acc := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	a.confirmConnect = func(string) bool {
		t.Error("a connected origin reopened the connect question")
		return false
	}
	res, err := a.dappConnect(context.Background(), "https://dapp.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]any)
	accounts := out["accounts"].([]dappAccount)
	if len(accounts) != 1 || accounts[0].AccountID != acc.AccountID {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestDappGetAccounts(t *testing.T) {
	a, _, acc := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	res, err := a.dappGetAccounts("https://dapp.test", "testnet")
	if err != nil {
		t.Fatal(err)
	}
	accounts := res.([]dappAccount)
	if len(accounts) != 1 || accounts[0].AccountID != acc.AccountID {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestDappSignRejected(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	a.approve = func(approvalRequest) bool { return false }
	params := json.RawMessage(`{"receiverId":"bob.testnet","actions":[{"type":"transfer","amount":"1"}]}`)
	if _, err := a.dappSign(context.Background(), "https://dapp.test", params); err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
	if rpc.broadcast != nil {
		t.Fatal("a rejected transaction was broadcast")
	}
}

func TestDappAddFunctionCallKey(t *testing.T) {
	a, rpc, acc := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	var asked approvalRequest
	a.approve = func(req approvalRequest) bool { asked = req; return true }
	pageKey, _ := nearSigner(t)
	params, _ := json.Marshal(map[string]any{
		"publicKey":   pageKey.PublicKey,
		"contractId":  "contract.testnet",
		"methodNames": []string{"donate"},
		"allowance":   "250000000000000000000000",
	})
	if _, err := a.dappAddFunctionCallKey(context.Background(), "https://dapp.test", params); err != nil {
		t.Fatal(err)
	}
	if rpc.broadcast == nil {
		t.Fatal("nothing was broadcast")
	}
	if asked.near == nil || len(asked.near.Actions) != 1 {
		t.Fatalf("approval = %+v", asked.near)
	}
	action := asked.near.Actions[0]
	if action.Kind != neartx.ActionAddKey || action.KeyReceiver != "contract.testnet" || len(action.MethodNames) != 1 {
		t.Fatalf("action = %+v", action)
	}
	// The transaction goes to the signing account itself.
	if asked.near.ReceiverID != acc.AccountID {
		t.Fatalf("receiver = %q, want %q", asked.near.ReceiverID, acc.AccountID)
	}
}

func TestDappSignsTransactionsSequentially(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	approvals := 0
	a.approve = func(approvalRequest) bool { approvals++; return true }
	res, err := a.dappSignTransactions(context.Background(), "https://dapp.test", json.RawMessage(`{
		"signerId":"", "transactions":[
			{"receiverId":"one.testnet","actions":[{"type":"transfer","amount":"1"}]},
			{"receiverId":"two.testnet","actions":[{"type":"transfer","amount":"2"}]}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if approvals != 2 || len(rpc.broadcasts) != 2 {
		t.Fatalf("approvals = %d, broadcasts = %d", approvals, len(rpc.broadcasts))
	}
	outcomes := res.([]json.RawMessage)
	for i, raw := range outcomes {
		var outcome map[string]any
		if json.Unmarshal(raw, &outcome) != nil || outcome["transaction"] == nil {
			t.Fatalf("outcome %d = %s", i, raw)
		}
	}
}

func TestDappBatchStopsAfterRejection(t *testing.T) {
	a, rpc, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	a.approve = func(approvalRequest) bool { return false }
	_, err := a.dappSignTransactions(context.Background(), "https://dapp.test", json.RawMessage(`{"transactions":[
		{"receiverId":"one.testnet","actions":[{"type":"transfer","amount":"1"}]},
		{"receiverId":"two.testnet","actions":[{"type":"transfer","amount":"2"}]}
	]}`))
	if err == nil || !strings.Contains(err.Error(), "transaction 1") || len(rpc.broadcasts) != 0 {
		t.Fatalf("err = %v, broadcasts = %d", err, len(rpc.broadcasts))
	}
}

func TestDappRejectsUnknownParams(t *testing.T) {
	a, _, _ := newDappApp(t)
	a.dappConnected["https://dapp.test"] = true
	params := json.RawMessage(`{"receiverId":"bob.testnet","actions":[{"type":"transfer","amount":"1"}],"contractId":"x"}`)
	if _, err := a.dappSign(context.Background(), "https://dapp.test", params); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err = %v", err)
	}
}

func TestDappBridgeScript(t *testing.T) {
	for _, want := range []string{
		"cryptopanel://near/request", "signAndSendTransaction", "signAndSendTransactions", "addFunctionCallKey",
		"mygo.on", "window.nearWallet", "normalizeActions", "near-wallet-injected", "near-selector-ready",
	} {
		if !strings.Contains(nearBridgeScript, want) {
			t.Errorf("the bridge script is missing %q", want)
		}
	}
}

func TestDappPageView(t *testing.T) {
	a := newTestApp(t)
	a.route = routeApps
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Apps", "WEB APPS, SIGNED BY THE LOCAL WALLET", "DApp URL",
		"DApp signing works on a NEAR network", "Open",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}
