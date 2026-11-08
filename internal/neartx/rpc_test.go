package neartx

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/davirain-su/crypto-panel/internal/base58"
)

// fakeRPC answers the calls Prepare and Send make.
type fakeRPC struct {
	accessKey string
	block     string
	send      string
	sendErr   error
	calls     []string
}

func (f *fakeRPC) call(_ context.Context, method string, params any) (json.RawMessage, error) {
	f.calls = append(f.calls, method)
	switch method {
	case "query":
		if f.accessKey == "" {
			return nil, errors.New("access key not found")
		}
		return json.RawMessage(f.accessKey), nil
	case "block":
		return json.RawMessage(f.block), nil
	case "broadcast_tx_commit":
		if f.sendErr != nil {
			return nil, f.sendErr
		}
		return json.RawMessage(f.send), nil
	case "status":
		return json.RawMessage(`{"chain_id":"testnet"}`), nil
	}
	return nil, errors.New("unexpected method " + method)
}

func testSigner(t *testing.T) Signer {
	t.Helper()
	pub, err := ParsePublicKey("ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq")
	if err != nil {
		t.Fatal(err)
	}
	return Signer{AccountID: "alice.testnet", PublicKey: pub}
}

func TestPrepare(t *testing.T) {
	rpc := &fakeRPC{
		accessKey: `{"nonce":41,"permission":"FullAccess"}`,
		block:     `{"header":{"hash":"F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG"}}`,
	}
	tx, sum, err := Prepare(context.Background(), rpc.call, testSigner(t), Request{
		ReceiverID: "bob.testnet",
		Actions:    []Action{Transfer(big.NewInt(1))},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Nonce != 42 {
		t.Errorf("nonce = %d, want 42", tx.Nonce)
	}
	if tx.SignerID != "alice.testnet" || tx.ReceiverID != "bob.testnet" {
		t.Errorf("tx = %+v", tx)
	}
	if sum.KeyPermission == nil || !sum.KeyPermission.FullAccess {
		t.Errorf("key permission = %+v", sum.KeyPermission)
	}
	if len(sum.Actions) != 1 || sum.Actions[0].Amount.String() != "1" {
		t.Errorf("summary = %+v", sum)
	}
}

func TestPrepareDefaultsGas(t *testing.T) {
	rpc := &fakeRPC{
		accessKey: `{"nonce":1,"permission":"FullAccess"}`,
		block:     `{"header":{"hash":"F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG"}}`,
	}
	tx, _, err := Prepare(context.Background(), rpc.call, testSigner(t), Request{
		ReceiverID: "contract.testnet",
		Actions:    []Action{FunctionCall("set_greeting", nil, 0, nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Actions[0].Gas != DefaultGas {
		t.Errorf("gas = %d, want %d", tx.Actions[0].Gas, DefaultGas)
	}
	if tx.Actions[0].Deposit.Sign() != 0 {
		t.Errorf("deposit = %s", tx.Actions[0].Deposit)
	}
}

func TestPrepareRefuses(t *testing.T) {
	rpc := &fakeRPC{
		accessKey: `{"nonce":1,"permission":"FullAccess"}`,
		block:     `{"header":{"hash":"F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG"}}`,
	}
	cases := map[string]Request{
		"no actions":      {ReceiverID: "bob.testnet"},
		"bad receiver":    {ReceiverID: "Bob", Actions: []Action{Transfer(big.NewInt(1))}},
		"stake":           {ReceiverID: "bob.testnet", Actions: []Action{{Kind: ActionStake, Deposit: big.NewInt(1)}}},
		"zero transfer":   {ReceiverID: "bob.testnet", Actions: []Action{Transfer(new(big.Int))}},
		"too much gas":    {ReceiverID: "bob.testnet", Actions: []Action{FunctionCall("f", nil, MaxGas+1, nil)}},
		"empty method":    {ReceiverID: "bob.testnet", Actions: []Action{FunctionCall("", nil, 1, nil)}},
		"key no receiver": {ReceiverID: "bob.testnet", Actions: []Action{AddFunctionCallKey(PublicKey{Bytes: [32]byte{1}}, nil, "", nil)}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Prepare(context.Background(), rpc.call, testSigner(t), req); err == nil {
				t.Fatal("prepared an invalid transaction")
			}
		})
	}
}

func TestAccessKeyOfFunctionCall(t *testing.T) {
	rpc := &fakeRPC{accessKey: `{"nonce":7,"permission":{"FunctionCall":{"allowance":"250000000000000000000000","receiver_id":"contract.testnet","method_names":["set_greeting","donate"]}}}`}
	key, err := AccessKeyOf(context.Background(), rpc.call, testSigner(t))
	if err != nil {
		t.Fatal(err)
	}
	if key.Nonce != 7 || key.Permission.FunctionCall == nil {
		t.Fatalf("key = %+v", key)
	}
	fc := key.Permission.FunctionCall
	if fc.Allowance.String() != "250000000000000000000000" || fc.ReceiverID != "contract.testnet" || len(fc.MethodNames) != 2 {
		t.Fatalf("permission = %+v", fc)
	}
}

func TestPrepareAppliesFunctionCallKeyLimits(t *testing.T) {
	const allowance = "250000000000000000000000" // 0.25 NEAR
	rpc := &fakeRPC{
		accessKey: `{"nonce":7,"permission":{"FunctionCall":{"allowance":"` + allowance + `","receiver_id":"contract.testnet","method_names":["set_greeting","donate"]}}}`,
		block:     `{"header":{"hash":"F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG"}}`,
	}
	signer := testSigner(t)
	within, _ := new(big.Int).SetString("100000000000000000000000", 10) // 0.1 NEAR
	if _, _, err := Prepare(context.Background(), rpc.call, signer, Request{
		ReceiverID: "contract.testnet",
		Actions:    []Action{FunctionCall("set_greeting", nil, 0, within)},
	}); err != nil {
		t.Fatalf("a call within the key's limits was refused: %v", err)
	}
	over, _ := new(big.Int).SetString("300000000000000000000000", 10) // 0.3 NEAR
	cases := map[string]Request{
		"another contract": {ReceiverID: "other.testnet", Actions: []Action{FunctionCall("set_greeting", nil, 0, nil)}},
		"another method":   {ReceiverID: "contract.testnet", Actions: []Action{FunctionCall("withdraw", nil, 0, nil)}},
		"over allowance":   {ReceiverID: "contract.testnet", Actions: []Action{FunctionCall("donate", nil, 0, over)}},
		"a transfer":       {ReceiverID: "contract.testnet", Actions: []Action{Transfer(big.NewInt(1))}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Prepare(context.Background(), rpc.call, signer, req); err == nil {
				t.Fatal("a transaction outside the key's permission was prepared")
			}
		})
	}
}

func TestRecentBlockHash(t *testing.T) {
	rpc := &fakeRPC{block: `{"header":{"hash":"F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG"}}`}
	hash, err := RecentBlockHash(context.Background(), rpc.call)
	if err != nil {
		t.Fatal(err)
	}
	if got := base58.Encode(hash[:]); got != "F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG" {
		t.Fatalf("hash = %s", got)
	}
}

func TestSend(t *testing.T) {
	var want [32]byte
	want[0] = 1
	id := base58.Encode(want[:])
	rpc := &fakeRPC{send: `{"status":{"SuccessValue":""},"transaction_outcome":{"id":"` + id + `"}}`}
	got, err := Send(context.Background(), rpc.call, []byte{1, 2, 3}, want)
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("hash = %s, want %s", got, id)
	}
}

func TestSendFailure(t *testing.T) {
	var want [32]byte
	id := base58.Encode(want[:])
	rpc := &fakeRPC{send: `{"status":{"Failure":{"ActionError":{"kind":{"FunctionCallError":{"ExecutionError":"boom"}}}}},"transaction_outcome":{"id":"` + id + `"}}`}
	_, err := Send(context.Background(), rpc.call, []byte{1}, want)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestSendRefusesAnotherHash(t *testing.T) {
	var want [32]byte
	var other [32]byte
	other[0] = 9
	rpc := &fakeRPC{send: `{"status":{"SuccessValue":""},"transaction_outcome":{"id":"` + base58.Encode(other[:]) + `"}}`}
	_, err := Send(context.Background(), rpc.call, []byte{1}, want)
	if err == nil || !strings.Contains(err.Error(), "reported") {
		t.Fatalf("err = %v", err)
	}
}

func TestSendRPCError(t *testing.T) {
	rpc := &fakeRPC{sendErr: errors.New("nonce too low")}
	if _, err := Send(context.Background(), rpc.call, []byte{1}, [32]byte{}); err == nil {
		t.Fatal("an RPC error was ignored")
	}
}

func TestStatus(t *testing.T) {
	rpc := &fakeRPC{}
	got, err := Status(context.Background(), rpc.call)
	if err != nil || got != "testnet" {
		t.Fatalf("status = %q, %v", got, err)
	}
}
