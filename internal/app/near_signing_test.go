package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/base58"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/neartx"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// nearRPC answers the calls Prepare and Send make, and records what was
// broadcast.
type nearRPC struct {
	accessKey string
	broadcast []byte
	calls     []string
}

func (f *nearRPC) call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	switch method {
	case "query":
		if f.accessKey == "" {
			return nil, errors.New("access key not found")
		}
		return json.RawMessage(f.accessKey), nil
	case "block":
		return json.RawMessage(`{"header":{"hash":"F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG"}}`), nil
	case "broadcast_tx_commit":
		encoded, ok := params.([]any)[0].(string)
		if !ok {
			return nil, errors.New("broadcast without a transaction")
		}
		signed, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		f.broadcast = signed
		hash := sha256.Sum256(signed[:len(signed)-1-ed25519.SignatureSize])
		id := base58.Encode(hash[:])
		return json.RawMessage(`{"status":{"SuccessValue":""},"transaction_outcome":{"id":"` + id + `"}}`), nil
	}
	return nil, errors.New("unexpected method " + method)
}

func nearSigner(t *testing.T) (wallet.NearAccount, ed25519.PrivateKey) {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return wallet.NearAccount{
		AccountID: hex.EncodeToString(public),
		PublicKey: neartx.PublicKeyOf(key).String(),
		Label:     "Test",
	}, key
}

func TestSignNearTransactionNeedsApproval(t *testing.T) {
	acc, key := nearSigner(t)
	rpc := &nearRPC{accessKey: `{"nonce":1,"permission":"FullAccess"}`}
	var asked approvalRequest
	deps := nearSignDeps{
		sourceName: "Test",
		account:    func(string) (wallet.NearAccount, error) { return acc, nil },
		key:        func(string) (ed25519.PrivateKey, bool) { return key, true },
		approve: func(req approvalRequest) bool {
			asked = req
			return false
		},
		rpc: rpc.call,
	}
	_, err := signNearTransaction(deps, chains.NearTestnet, plugin.NearSignRequest{
		ChainID:  "near:testnet",
		Receiver: "bob.testnet",
		Actions:  []plugin.NearAction{{Type: "transfer", Amount: "1500000000000000000000000"}},
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
	if rpc.broadcast != nil {
		t.Fatal("a rejected transaction was broadcast")
	}
	if asked.near == nil || asked.near.ReceiverID != "bob.testnet" {
		t.Fatalf("approval = %+v", asked)
	}
	if asked.fromAddr != acc.AccountID || asked.fromLabel != "Test" {
		t.Fatalf("approval account = %q %q", asked.fromAddr, asked.fromLabel)
	}
	if asked.near.Actions[0].Amount.String() != "1500000000000000000000000" {
		t.Fatalf("amount = %s", asked.near.Actions[0].Amount)
	}
}

func TestSignNearTransactionSignsAfterApproval(t *testing.T) {
	acc, key := nearSigner(t)
	rpc := &nearRPC{accessKey: `{"nonce":41,"permission":"FullAccess"}`}
	deps := nearSignDeps{
		sourceName: "Test",
		account:    func(string) (wallet.NearAccount, error) { return acc, nil },
		key:        func(string) (ed25519.PrivateKey, bool) { return key, true },
		approve:    func(approvalRequest) bool { return true },
		rpc:        rpc.call,
	}
	res, err := signNearTransaction(deps, chains.NearTestnet, plugin.NearSignRequest{
		ChainID:  "near:testnet",
		Receiver: "bob.testnet",
		Actions:  []plugin.NearAction{{Type: "transfer", Amount: "1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rpc.broadcast == nil {
		t.Fatal("nothing was broadcast")
	}
	// What was broadcast is a signed transaction by this account.
	signed := rpc.broadcast
	body := signed[:len(signed)-1-ed25519.SignatureSize]
	if signed[len(body)] != neartx.KeyTypeED25519 {
		t.Fatalf("key type = %d", signed[len(body)])
	}
	hash := sha256.Sum256(body)
	sig := signed[len(body)+1:]
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), hash[:], sig) {
		t.Fatal("the broadcast transaction is not signed by the account")
	}
	if res.Hash != base58.Encode(hash[:]) {
		t.Fatalf("hash = %q", res.Hash)
	}
}

func TestSignNearTransactionStopsWhenTheAccountLocks(t *testing.T) {
	acc, _ := nearSigner(t)
	rpc := &nearRPC{accessKey: `{"nonce":1,"permission":"FullAccess"}`}
	deps := nearSignDeps{
		sourceName: "Test",
		account:    func(string) (wallet.NearAccount, error) { return acc, nil },
		key:        func(string) (ed25519.PrivateKey, bool) { return nil, false },
		approve:    func(approvalRequest) bool { return true },
		rpc:        rpc.call,
	}
	_, err := signNearTransaction(deps, chains.NearTestnet, plugin.NearSignRequest{
		Receiver: "bob.testnet",
		Actions:  []plugin.NearAction{{Type: "transfer", Amount: "1"}},
	})
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("err = %v", err)
	}
	if rpc.broadcast != nil {
		t.Fatal("a transaction was broadcast without its key")
	}
}

func TestSignNearTransactionAppliesTheKeyLimit(t *testing.T) {
	acc, key := nearSigner(t)
	rpc := &nearRPC{accessKey: `{"nonce":7,"permission":{"FunctionCall":{"allowance":"250000000000000000000000","receiver_id":"contract.testnet","method_names":["donate"]}}}`}
	approved := false
	deps := nearSignDeps{
		sourceName: "Test",
		account:    func(string) (wallet.NearAccount, error) { return acc, nil },
		key:        func(string) (ed25519.PrivateKey, bool) { return key, true },
		approve:    func(approvalRequest) bool { approved = true; return true },
		rpc:        rpc.call,
	}
	cases := map[string]plugin.NearSignRequest{
		"another method": {Receiver: "contract.testnet", Actions: []plugin.NearAction{{Type: "functionCall", Method: "withdraw"}}},
		"over allowance": {Receiver: "contract.testnet", Actions: []plugin.NearAction{{Type: "functionCall", Method: "donate", Deposit: "300000000000000000000000"}}},
		"a transfer":     {Receiver: "contract.testnet", Actions: []plugin.NearAction{{Type: "transfer", Amount: "1"}}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := signNearTransaction(deps, chains.NearTestnet, req); err == nil {
				t.Fatal("a transaction outside the key's limit was signed")
			}
		})
	}
	if approved {
		t.Fatal("the approval window was shown for a transaction the key forbids")
	}
	if rpc.broadcast != nil {
		t.Fatal("a transaction outside the key's limit was broadcast")
	}
}

func TestNearActions(t *testing.T) {
	actions, err := nearActions([]plugin.NearAction{
		{Type: "transfer", Amount: "10"},
		{Type: "functionCall", Method: "set_greeting", Args: `{"message":"hi"}`, Gas: "30000000000000", Deposit: "5"},
		{Type: "addFunctionCallKey", PublicKey: "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq", ReceiverID: "contract.testnet", MethodNames: []string{"donate"}, Allowance: "100"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 3 {
		t.Fatalf("actions = %+v", actions)
	}
	if actions[0].Kind != neartx.ActionTransfer || actions[0].Deposit.String() != "10" {
		t.Fatalf("transfer = %+v", actions[0])
	}
	if actions[1].Kind != neartx.ActionFunctionCall || string(actions[1].Args) != `{"message":"hi"}` ||
		actions[1].Gas != 30_000_000_000_000 || actions[1].Deposit.String() != "5" {
		t.Fatalf("call = %+v", actions[1])
	}
	fc := actions[2].AccessKey.Permission.FunctionCall
	if actions[2].Kind != neartx.ActionAddKey || fc == nil || fc.ReceiverID != "contract.testnet" ||
		fc.Allowance.String() != "100" || len(fc.MethodNames) != 1 {
		t.Fatalf("add key = %+v", actions[2])
	}

	// base64 args carry bytes the contract receives unchanged.
	actions, err = nearActions([]plugin.NearAction{{Type: "functionCall", Method: "f", Args: "base64:" + base64.StdEncoding.EncodeToString([]byte{0, 1, 2})}})
	if err != nil {
		t.Fatal(err)
	}
	if string(actions[0].Args) != string([]byte{0, 1, 2}) {
		t.Fatalf("args = %v", actions[0].Args)
	}

	if _, err := nearActions([]plugin.NearAction{{Type: "stake", Amount: "1"}}); err == nil {
		t.Fatal("a stake was accepted")
	}
	if _, err := nearActions([]plugin.NearAction{{Type: "transfer", Amount: "0"}}); err == nil {
		t.Fatal("a zero transfer was accepted")
	}
	if _, err := nearActions([]plugin.NearAction{{Type: "functionCall", Method: "f", Gas: "not a number"}}); err == nil {
		t.Fatal("a bad gas amount was accepted")
	}
}

func mustBig(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("%q is not a number", s)
	}
	return v
}

func nearApproval(sum *neartx.Summary) *approval {
	return &approval{
		t:          func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		pluginName: "near-tools.testnet",
		network:    chains.NearTestnet,
		near:       sum,
		fromAddr:   "alice.testnet",
		fromLabel:  "Alice",
	}
}

func TestApprovalViewShowsTheNearDecode(t *testing.T) {
	sum := &neartx.Summary{
		SignerID:   "alice.testnet",
		PublicKey:  "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq",
		ReceiverID: "bob.testnet",
		Actions: []neartx.ActionSummary{{
			Kind:   neartx.ActionTransfer,
			Amount: mustBig(t, "1500000000000000000000000"),
		}},
		KeyPermission: &neartx.KeyPermissionSummary{FullAccess: true},
	}
	tt := ui.NewTester(nearApproval(sum).view, 460, 640)
	for _, want := range []string{
		"NEAR-TOOLS.TESTNET", "Send NEAR?", "Alice", "alice.testnet",
		"Receiver", "bob.testnet", "Signing key", "Full access — no on-chain limit",
		"Amount", "1.5 NEAR", "YoctoNEAR", "1500000000000000000000000",
		"Approve and sign", "Reject",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestApprovalViewShowsAFunctionCall(t *testing.T) {
	sum := &neartx.Summary{
		SignerID:   "alice.testnet",
		ReceiverID: "contract.testnet",
		Actions: []neartx.ActionSummary{{
			Kind:       neartx.ActionFunctionCall,
			MethodName: "set_greeting",
			ArgsText:   `{"message":"hi"}`,
			Gas:        30_000_000_000_000,
			Amount:     new(big.Int),
		}},
		KeyPermission: &neartx.KeyPermissionSummary{
			Allowance:   mustBig(t, "250000000000000000000000"),
			ReceiverID:  "contract.testnet",
			MethodNames: []string{"set_greeting", "donate"},
		},
	}
	tt := ui.NewTester(nearApproval(sum).view, 460, 640)
	for _, want := range []string{
		"Call a contract?", "Function call", "set_greeting", `{"message":"hi"}`,
		"30.0 TGas", "up to 0.25 NEAR", "contract.testnet", "set_greeting, donate",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestApprovalViewShowsAnAddedKey(t *testing.T) {
	sum := &neartx.Summary{
		SignerID:   "alice.testnet",
		ReceiverID: "alice.testnet",
		Actions: []neartx.ActionSummary{{
			Kind:        neartx.ActionAddKey,
			PublicKey:   "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq",
			Allowance:   mustBig(t, "250000000000000000000000"),
			KeyReceiver: "contract.testnet",
			MethodNames: []string{"donate"},
		}},
		KeyPermission: &neartx.KeyPermissionSummary{FullAccess: true},
	}
	tt := ui.NewTester(nearApproval(sum).view, 460, 640)
	for _, want := range []string{
		"Add a key to this account?", "Key", "ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq",
		"Contract", "Methods", "donate", "Allowance", "0.25 NEAR",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}
