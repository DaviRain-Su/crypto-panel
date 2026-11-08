package evmtx

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// fakeRPC answers the calls Prepare makes, so tests never touch a chain.
func fakeRPC(t *testing.T, answers map[string]any) RPC {
	t.Helper()
	return func(_ context.Context, method string, _ any) (json.RawMessage, error) {
		v, ok := answers[method]
		if !ok {
			return nil, fmt.Errorf("unexpected call %s", method)
		}
		if err, ok := v.(error); ok {
			return nil, err
		}
		out, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func TestChainID(t *testing.T) {
	id, err := ChainID("eip155:8453")
	if err != nil || id.Int64() != 8453 {
		t.Fatalf("id = %v, err = %v", id, err)
	}
	for _, bad := range []string{"", "eip155", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp", "eip155:0", "eip155:x"} {
		if _, err := ChainID(bad); err == nil {
			t.Errorf("ChainID(%q) = nil error", bad)
		}
	}
}

func TestFormatUnits(t *testing.T) {
	cases := []struct {
		amount   string
		decimals int
		want     string
	}{
		{"1200000000", 6, "1,200"},
		{"1", 6, "0.000001"},
		{"1500000000000000000", 18, "1.5"},
		{"0", 18, "0"},
		{"42", 0, "42"},
	}
	for _, c := range cases {
		amount, _ := new(big.Int).SetString(c.amount, 10)
		if got := FormatUnits(amount, c.decimals); got != c.want {
			t.Errorf("FormatUnits(%s, %d) = %q, want %q", c.amount, c.decimals, got, c.want)
		}
	}
}

func TestPrepareFillsInTheTransaction(t *testing.T) {
	from := common.HexToAddress("0x2c7536E3605D9C16a7a3D7b1898e529396a65c23")
	to := common.HexToAddress("0x27b1fdb04752bbc536007a920d24acb045561c26")
	rpc := fakeRPC(t, map[string]any{
		"eth_getTransactionCount":  "0x5",
		"eth_estimateGas":          "0x5208",
		"eth_getBlockByNumber":     map[string]any{"baseFeePerGas": "0x3b9aca00"},
		"eth_maxPriorityFeePerGas": "0x3b9aca00",
	})
	tx, sum, err := Prepare(context.Background(), rpc, from, Request{
		ChainID: "eip155:1",
		To:      to.Hex(),
		Value:   "1500000000000000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Nonce() != 5 {
		t.Fatalf("nonce = %d", tx.Nonce())
	}
	if tx.Gas() != 0x5208*12/10 {
		t.Fatalf("gas = %d", tx.Gas())
	}
	if tx.Type() != types.DynamicFeeTxType {
		t.Fatalf("type = %d, want a dynamic fee transaction", tx.Type())
	}
	if tx.ChainId().Int64() != 1 {
		t.Fatalf("chain = %v", tx.ChainId())
	}
	if sum.From != from || sum.To != to {
		t.Fatalf("summary = %+v", sum)
	}
	if sum.Value.String() != "1500000000000000000" {
		t.Fatalf("value = %v", sum.Value)
	}
	// max fee = 2 * base + tip = 3 gwei; the most it can cost is gas * max fee.
	wantFee := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), big.NewInt(3_000_000_000))
	if sum.Fee.Cmp(wantFee) != 0 {
		t.Fatalf("fee = %v, want %v", sum.Fee, wantFee)
	}
}

func TestPrepareUsesALegacyFeeWithoutEIP1559(t *testing.T) {
	from := common.HexToAddress("0x2c7536E3605D9C16a7a3D7b1898e529396a65c23")
	to := common.HexToAddress("0x27b1fdb04752bbc536007a920d24acb045561c26")
	rpc := fakeRPC(t, map[string]any{
		"eth_getTransactionCount": "0x0",
		"eth_estimateGas":         "0x5208",
		"eth_getBlockByNumber":    map[string]any{},
		"eth_gasPrice":            "0x3b9aca00",
	})
	tx, _, err := Prepare(context.Background(), rpc, from, Request{ChainID: "eip155:1", To: to.Hex()})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Type() != types.LegacyTxType {
		t.Fatalf("type = %d, want a legacy transaction", tx.Type())
	}
	if tx.GasPrice().Int64() != 1_000_000_000 {
		t.Fatalf("gas price = %v", tx.GasPrice())
	}
}

func TestPrepareRefusesAMissingRecipient(t *testing.T) {
	rpc := fakeRPC(t, map[string]any{})
	if _, _, err := Prepare(context.Background(), rpc, common.Address{}, Request{ChainID: "eip155:1"}); err == nil {
		t.Fatal("a transaction without a recipient was prepared")
	}
}

func TestPrepareDecodesAnERC20Transfer(t *testing.T) {
	from := common.HexToAddress("0x2c7536E3605D9C16a7a3D7b1898e529396a65c23")
	token := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48") // USDC
	to := common.HexToAddress("0x27b1fdb04752bbc536007a920d24acb045561c26")
	amount, _ := new(big.Int).SetString("1200000000", 10)
	data := append(selector("a9059cbb"), common.LeftPadBytes(to.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	decimals := "0x" + hex.EncodeToString(common.LeftPadBytes([]byte{6}, 32))
	rpc := func(_ context.Context, method string, params any) (json.RawMessage, error) {
		switch method {
		case "eth_call":
			call := params.([]any)[0].(map[string]any)
			switch call["data"] {
			case "0x95d89b41":
				return json.Marshal(abiString("USDC"))
			case "0x313ce567":
				return json.Marshal(decimals)
			}
			return nil, fmt.Errorf("unexpected call %v", call["data"])
		case "eth_getTransactionCount":
			return json.Marshal("0x0")
		case "eth_estimateGas":
			return json.Marshal("0x9c40")
		case "eth_getBlockByNumber":
			return json.Marshal(map[string]any{"baseFeePerGas": "0x3b9aca00"})
		case "eth_maxPriorityFeePerGas":
			return json.Marshal("0x3b9aca00")
		}
		return nil, fmt.Errorf("unexpected call %s", method)
	}
	_, sum, err := Prepare(context.Background(), rpc, from, Request{
		ChainID: "eip155:1",
		To:      token.Hex(),
		Data:    "0x" + hex.EncodeToString(data),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Call != "Transfer 1,200 USDC to 0x27b1…1c26" {
		t.Fatalf("call = %q", sum.Call)
	}
}

func TestSignRecoversTheSender(t *testing.T) {
	key, err := ethcrypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318")
	if err != nil {
		t.Fatal(err)
	}
	from := ethcrypto.PubkeyToAddress(key.PublicKey)
	to := common.HexToAddress("0x27b1fdb04752bbc536007a920d24acb045561c26")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     5,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(3_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1_500_000_000_000_000_000),
	})
	raw, hash, err := Sign(key, tx)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.Transaction
	if err := decoded.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(1)), &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if sender != from {
		t.Fatalf("sender = %v, want %v", sender, from)
	}
	if decoded.Hash() != hash {
		t.Fatalf("hash = %v, want %v", hash, decoded.Hash())
	}
}

func TestSendReturnsTheHash(t *testing.T) {
	want := "0x" + strings.Repeat("ab", 32)
	rpc := fakeRPC(t, map[string]any{"eth_sendRawTransaction": want})
	hash, err := Send(context.Background(), rpc, []byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if hash.Hex() != want {
		t.Fatalf("hash = %v, want %v", hash, want)
	}
}

func selector(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// abiString returns a string as an ABI-encoded return value.
func abiString(s string) string {
	body := []byte(s)
	out := make([]byte, 32+32+((len(body)+31)/32)*32)
	copy(out[31:], []byte{32})
	copy(out[63:], []byte{byte(len(body))})
	copy(out[64:], body)
	return "0x" + hex.EncodeToString(out)
}
