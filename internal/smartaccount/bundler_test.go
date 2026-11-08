package smartaccount

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

func testUserOperation() *UserOperation {
	return &UserOperation{
		Sender:               common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:                big.NewInt(1),
		CallData:             []byte{0xde, 0xad, 0xbe, 0xef},
		VerificationGasLimit: big.NewInt(100_000),
		CallGasLimit:         big.NewInt(200_000),
		PreVerificationGas:   big.NewInt(21_000),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(2_000_000_000),
		Signature:            []byte{0xaa, 0xbb},
	}
}

func TestBundlerSendUserOperation(t *testing.T) {
	var method string
	var op rpcUserOperation
	var entryPoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			JSONRPC string            `json:"jsonrpc"`
			ID      int               `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		method = req.Method
		if len(req.Params) != 2 {
			t.Errorf("params = %d, want 2", len(req.Params))
		}
		if err := json.Unmarshal(req.Params[0], &op); err != nil {
			t.Error(err)
		}
		if err := json.Unmarshal(req.Params[1], &entryPoint); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":"0x%s"}`, strings.Repeat("ab", 32))
	}))
	defer server.Close()

	opIn := testUserOperation()
	hash, err := NewBundler(server.URL).SendUserOperation(context.Background(), opIn, EntryPointV07)
	if err != nil {
		t.Fatal(err)
	}
	if method != "eth_sendUserOperation" {
		t.Fatalf("method = %q", method)
	}
	if op.Sender != opIn.Sender.Hex() {
		t.Fatalf("sender = %q, want %q", op.Sender, opIn.Sender.Hex())
	}
	if op.CallData != hexutil.Encode(opIn.CallData) {
		t.Fatalf("callData = %q", op.CallData)
	}
	if op.Signature != hexutil.Encode(opIn.Signature) {
		t.Fatalf("signature = %q", op.Signature)
	}
	if entryPoint != EntryPointV07.Hex() {
		t.Fatalf("entry point = %q, want %q", entryPoint, EntryPointV07.Hex())
	}
	want := common.HexToHash("0x" + strings.Repeat("ab", 32))
	if hash != want {
		t.Fatalf("hash = %s, want %s", hash, want)
	}
}

func TestBundlerEstimateUserOperationGas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"callGasLimit":"0x186a0","verificationGasLimit":"0x30d40","preVerificationGas":"0x5208"}}`)
	}))
	defer server.Close()

	estimate, err := NewBundler(server.URL).EstimateUserOperationGas(context.Background(), testUserOperation(), EntryPointV07)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.CallGasLimit.Cmp(big.NewInt(100_000)) != 0 {
		t.Fatalf("call gas = %v", estimate.CallGasLimit)
	}
	if estimate.VerificationGasLimit.Cmp(big.NewInt(200_000)) != 0 {
		t.Fatalf("verification gas = %v", estimate.VerificationGasLimit)
	}
	if estimate.PreVerificationGas.Cmp(big.NewInt(21_000)) != 0 {
		t.Fatalf("pre-verification gas = %v", estimate.PreVerificationGas)
	}
}

func TestBundlerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32500,"message":"boom"}}`)
	}))
	defer server.Close()

	_, err := NewBundler(server.URL).SendUserOperation(context.Background(), testUserOperation(), EntryPointV07)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want the bundler's message", err)
	}
}

func TestBundlerHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := NewBundler(server.URL).SendUserOperation(context.Background(), testUserOperation(), EntryPointV07)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("error = %v, want an HTTP error", err)
	}
}

func TestBundlerInvalidHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":"0x1234"}`)
	}))
	defer server.Close()

	if _, err := NewBundler(server.URL).SendUserOperation(context.Background(), testUserOperation(), EntryPointV07); err == nil {
		t.Fatal("SendUserOperation accepted a short hash")
	}
}

func TestBundlerReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"userOpHash":"0x`+strings.Repeat("11", 32)+`","entryPoint":"0x0000000071727De22E5E9d8BAf0edAc6f37da032","sender":"0x2222222222222222222222222222222222222222","nonce":"0x1","success":true,"transactionHash":"0x`+strings.Repeat("33", 32)+`","blockNumber":"0x10","actualGasUsed":"0x5208"}}`)
	}))
	defer server.Close()

	receipt, err := NewBundler(server.URL).UserOperationReceipt(context.Background(), common.HexToHash("0x"+strings.Repeat("11", 32)))
	if err != nil {
		t.Fatal(err)
	}
	if receipt == nil {
		t.Fatal("receipt is nil")
	}
	if !receipt.Success {
		t.Fatal("receipt is not successful")
	}
	if receipt.TransactionHash != common.HexToHash("0x"+strings.Repeat("33", 32)) {
		t.Fatalf("transaction hash = %s", receipt.TransactionHash)
	}
	if receipt.BlockNumber.Cmp(big.NewInt(16)) != 0 {
		t.Fatalf("block number = %v", receipt.BlockNumber)
	}
	if receipt.ActualGasUsed.Cmp(big.NewInt(21_000)) != 0 {
		t.Fatalf("gas used = %v", receipt.ActualGasUsed)
	}
}

func TestBundlerReceiptPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":null}`)
	}))
	defer server.Close()

	receipt, err := NewBundler(server.URL).UserOperationReceipt(context.Background(), common.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	if receipt != nil {
		t.Fatal("pending receipt is not nil")
	}
}

func TestBundlerSupportedEntryPoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":["0x0000000071727De22E5E9d8BAf0edAc6f37da032"]}`)
	}))
	defer server.Close()

	points, err := NewBundler(server.URL).SupportedEntryPoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0] != EntryPointV07 {
		t.Fatalf("entry points = %v", points)
	}
}
