package app

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/evmtx"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/plugin"
)

const signTo = "0x27b1fdb04752bbc536007a920d24acb045561c26"

// signRPC answers what Prepare asks and records the raw transaction Send
// broadcasts.
func signRPC(t *testing.T, sent *[]byte) evmtx.RPC {
	t.Helper()
	return func(_ context.Context, method string, params any) (json.RawMessage, error) {
		switch method {
		case "eth_getTransactionCount":
			return json.Marshal("0x0")
		case "eth_estimateGas":
			return json.Marshal("0x5208")
		case "eth_getBlockByNumber":
			return json.Marshal(map[string]any{"baseFeePerGas": "0x3b9aca00"})
		case "eth_maxPriorityFeePerGas":
			return json.Marshal("0x3b9aca00")
		case "eth_sendRawTransaction":
			rawHex := params.([]any)[0].(string)
			raw, err := hex.DecodeString(strings.TrimPrefix(rawHex, "0x"))
			if err != nil {
				return nil, err
			}
			*sent = raw
			var tx types.Transaction
			if err := tx.UnmarshalBinary(raw); err != nil {
				return nil, err
			}
			return json.Marshal(tx.Hash().Hex())
		}
		return nil, fmt.Errorf("unexpected call %s", method)
	}
}

func TestSignTransactionNeedsApproval(t *testing.T) {
	key, err := ethcrypto.HexToECDSA(walletTestKey)
	if err != nil {
		t.Fatal(err)
	}
	addr := strings.ToLower(ethcrypto.PubkeyToAddress(key.PublicKey).Hex())
	var sent []byte
	deps := signDeps{
		pluginName: "Test",
		account:    func(string) (string, string, error) { return addr, "Main", nil },
		key:        func(string) (*ecdsa.PrivateKey, bool) { return key, true },
		approve:    func(approvalRequest) bool { return false },
		rpc:        signRPC(t, &sent),
	}
	_, err = signTransaction(deps, chains.Ethereum, plugin.SignRequest{
		ChainID: "eip155:1",
		To:      signTo,
		Value:   "1000000000000000",
	})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("err = %v", err)
	}
	if sent != nil {
		t.Fatal("a rejected transaction was broadcast")
	}
}

func TestSignTransactionSignsAfterApproval(t *testing.T) {
	key, err := ethcrypto.HexToECDSA(walletTestKey)
	if err != nil {
		t.Fatal(err)
	}
	from := ethcrypto.PubkeyToAddress(key.PublicKey)
	var (
		sent []byte
		ask  approvalRequest
	)
	deps := signDeps{
		pluginName: "Portfolio",
		account:    func(string) (string, string, error) { return strings.ToLower(from.Hex()), "Main", nil },
		key:        func(string) (*ecdsa.PrivateKey, bool) { return key, true },
		approve: func(req approvalRequest) bool {
			ask = req
			return true
		},
		rpc: signRPC(t, &sent),
	}
	res, err := signTransaction(deps, chains.Ethereum, plugin.SignRequest{
		ChainID:     "eip155:1",
		To:          signTo,
		Value:       "1000000000000000",
		Description: "A test payment",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The approval window showed the shell's decode, not the plugin's.
	if ask.pluginName != "Portfolio" || ask.description != "A test payment" {
		t.Fatalf("approval = %+v", ask)
	}
	if ask.summary.From != from || ask.summary.To != common.HexToAddress(signTo) {
		t.Fatalf("summary = %+v", ask.summary)
	}
	if ask.summary.Value.String() != "1000000000000000" {
		t.Fatalf("value = %v", ask.summary.Value)
	}
	if ask.fromLabel != "Main" {
		t.Fatalf("label = %q", ask.fromLabel)
	}
	// What was broadcast is what was approved, signed by the account.
	var tx types.Transaction
	if err := tx.UnmarshalBinary(sent); err != nil {
		t.Fatal(err)
	}
	sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(1)), &tx)
	if err != nil {
		t.Fatal(err)
	}
	if sender != from {
		t.Fatalf("sender = %v, want %v", sender, from)
	}
	if res.Hash != tx.Hash().Hex() {
		t.Fatalf("hash = %q, want %q", res.Hash, tx.Hash().Hex())
	}
}

func TestSignTransactionStopsWhenTheAccountLocks(t *testing.T) {
	key, err := ethcrypto.HexToECDSA(walletTestKey)
	if err != nil {
		t.Fatal(err)
	}
	addr := strings.ToLower(ethcrypto.PubkeyToAddress(key.PublicKey).Hex())
	var sent []byte
	deps := signDeps{
		pluginName: "Test",
		account:    func(string) (string, string, error) { return addr, "", nil },
		key:        func(string) (*ecdsa.PrivateKey, bool) { return nil, false },
		approve:    func(approvalRequest) bool { return true },
		rpc:        signRPC(t, &sent),
	}
	_, err = signTransaction(deps, chains.Ethereum, plugin.SignRequest{ChainID: "eip155:1", To: signTo})
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("err = %v", err)
	}
	if sent != nil {
		t.Fatal("a transaction was broadcast without its key")
	}
}

func approvalFor(sum *evmtx.Summary) *approval {
	return &approval{
		t:           func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		pluginName:  "Portfolio",
		description: "Swap on Uniswap",
		network:     chains.Ethereum,
		summary:     sum,
		fromAddr:    walletTestAddr,
		fromLabel:   "Main",
	}
}

func TestApprovalViewShowsTheShellDecode(t *testing.T) {
	sum := &evmtx.Summary{
		From:  common.HexToAddress(walletTestAddr),
		To:    common.HexToAddress(signTo),
		Value: big.NewInt(0),
		Data:  []byte{1, 2, 3, 4},
		Gas:   21000,
		Fee:   big.NewInt(63_000_000_000_000),
		Call:  "Transfer 1,200 USDC to 0x27b1…1c26",
	}
	ap := approvalFor(sum)
	var decisions []bool
	ap.onDecide = func(ok bool) { decisions = append(decisions, ok) }
	tt := ui.NewTester(ap.view, 460, 560)
	for _, want := range []string{
		"PORTFOLIO", "Transfer tokens?", "Transfer 1,200 USDC to 0x27b1…1c26",
		"Main", "0.000063 ETH", "The plugin says", "Swap on Uniswap",
		"Approve and sign", "Reject",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Approve and sign"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Reject"); err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !decisions[0] {
		t.Fatalf("decisions = %v", decisions)
	}
}

func TestApprovalViewNamesASend(t *testing.T) {
	sum := &evmtx.Summary{
		From:  common.HexToAddress(walletTestAddr),
		To:    common.HexToAddress(signTo),
		Value: big.NewInt(1_500_000_000_000_000_000),
		Gas:   21000,
		Fee:   big.NewInt(63_000_000_000_000),
	}
	tt := ui.NewTester(approvalFor(sum).view, 460, 560)
	if !tt.HasText("Send ETH?") || !tt.HasText("1.5 ETH") {
		t.Fatalf("texts = %q", tt.Texts())
	}
}
