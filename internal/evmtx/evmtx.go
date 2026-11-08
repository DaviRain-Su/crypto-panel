// Package evmtx builds, decodes and signs EVM transactions for the wallet.
// A plugin states an intent — to, value, data — and the shell fills in the
// nonce, gas and fees, decodes the result for the approval window, and only
// then signs it.
package evmtx

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// RPC is one call to the chain, as chain.Reader.RPC makes it.
type RPC func(ctx context.Context, method string, params any) (json.RawMessage, error)

// Request is what a plugin asks to sign: the transaction's intent. The
// shell fills in the nonce, gas and fees itself; a plugin that names fees
// only sets the ceiling, never the amount paid.
type Request struct {
	ChainID              string // CAIP-2, e.g. "eip155:1"
	To                   string
	Value                string // wei, decimal
	Data                 string // hex, 0x-prefixed
	Gas                  uint64 // 0: estimate
	MaxFeePerGas         string // wei, decimal; empty: the shell's fee
	MaxPriorityFeePerGas string // wei, decimal
	Nonce                *uint64
}

// Summary is the shell's own decode of the transaction, for the approval
// window. A plugin never draws this.
type Summary struct {
	ChainID *big.Int
	From    common.Address
	To      common.Address
	Value   *big.Int
	Data    []byte
	Gas     uint64
	Fee     *big.Int // the most the transaction can cost in fees
	Dynamic bool     // EIP-1559 fees
	Call    string   // the calldata decoded, e.g. "Transfer 1,200 USDC"
}

// Prepare fills in what req left out and returns the transaction and the
// summary the approval window shows.
func Prepare(ctx context.Context, rpc RPC, from common.Address, req Request) (*types.Transaction, *Summary, error) {
	chainID, err := ChainID(req.ChainID)
	if err != nil {
		return nil, nil, err
	}
	if !common.IsHexAddress(req.To) {
		return nil, nil, errors.New("the recipient is not an address")
	}
	to := common.HexToAddress(req.To)
	if to == (common.Address{}) {
		return nil, nil, errors.New("the transaction has no recipient")
	}
	value := new(big.Int)
	if req.Value != "" {
		if value, err = parseBig(req.Value); err != nil {
			return nil, nil, fmt.Errorf("value: %w", err)
		}
	}
	data, err := parseData(req.Data)
	if err != nil {
		return nil, nil, err
	}
	nonce := uint64(0)
	if req.Nonce != nil {
		nonce = *req.Nonce
	} else if nonce, err = transactionCount(ctx, rpc, from); err != nil {
		return nil, nil, err
	}
	gas := req.Gas
	if gas == 0 {
		if gas, err = estimateGas(ctx, rpc, from, to, value, data); err != nil {
			return nil, nil, err
		}
	}
	fees, err := fees(ctx, rpc, req)
	if err != nil {
		return nil, nil, err
	}
	var tx *types.Transaction
	if fees.dynamic {
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: fees.tip,
			GasFeeCap: fees.cap,
			Gas:       gas,
			To:        &to,
			Value:     value,
			Data:      data,
		})
	} else {
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce,
			GasPrice: fees.cap,
			Gas:      gas,
			To:       &to,
			Value:    value,
			Data:     data,
		})
	}
	summary := &Summary{
		ChainID: chainID,
		From:    from,
		To:      to,
		Value:   value,
		Data:    data,
		Gas:     gas,
		Fee:     new(big.Int).Mul(new(big.Int).SetUint64(gas), fees.cap),
		Dynamic: fees.dynamic,
		Call:    decodeCall(ctx, rpc, to, data),
	}
	return tx, summary, nil
}

// Sign signs tx with key and returns its raw bytes and hash.
func Sign(key *ecdsa.PrivateKey, tx *types.Transaction) ([]byte, common.Hash, error) {
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(tx.ChainId()), key)
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("signing: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, common.Hash{}, fmt.Errorf("encoding the signed transaction: %w", err)
	}
	return raw, signed.Hash(), nil
}

// Send broadcasts a signed transaction and returns its hash.
func Send(ctx context.Context, rpc RPC, raw []byte) (common.Hash, error) {
	out, err := rpc(ctx, "eth_sendRawTransaction", []any{"0x" + hex.EncodeToString(raw)})
	if err != nil {
		return common.Hash{}, err
	}
	var hash string
	if err := json.Unmarshal(out, &hash); err != nil {
		return common.Hash{}, fmt.Errorf("reading the transaction hash: %w", err)
	}
	return common.HexToHash(hash), nil
}

// ChainID parses a CAIP-2 EVM chain id, e.g. "eip155:8453".
func ChainID(caip2 string) (*big.Int, error) {
	parts := strings.Split(caip2, ":")
	if len(parts) != 2 || parts[0] != "eip155" {
		return nil, fmt.Errorf("chain %q is not an EVM chain", caip2)
	}
	id, ok := new(big.Int).SetString(parts[1], 10)
	if !ok || id.Sign() <= 0 {
		return nil, fmt.Errorf("chain %q is not an EVM chain", caip2)
	}
	return id, nil
}

type feeSet struct {
	dynamic bool
	cap     *big.Int // gas price, or the max fee per gas
	tip     *big.Int
}

// fees picks the fee the shell is willing to pay: what the plugin asked for
// as a ceiling, or the chain's own suggestion.
func fees(ctx context.Context, rpc RPC, req Request) (feeSet, error) {
	if req.MaxFeePerGas != "" || req.MaxPriorityFeePerGas != "" {
		cap := new(big.Int)
		var err error
		if req.MaxFeePerGas != "" {
			if cap, err = parseBig(req.MaxFeePerGas); err != nil {
				return feeSet{}, fmt.Errorf("max fee: %w", err)
			}
		}
		tip := new(big.Int)
		if req.MaxPriorityFeePerGas != "" {
			if tip, err = parseBig(req.MaxPriorityFeePerGas); err != nil {
				return feeSet{}, fmt.Errorf("max priority fee: %w", err)
			}
		}
		if cap.Sign() == 0 {
			if cap, err = suggestedFees(ctx, rpc); err != nil {
				return feeSet{}, err
			}
		}
		if tip.Sign() == 0 {
			tip = new(big.Int).Div(cap, big.NewInt(10))
		}
		if tip.Cmp(cap) > 0 {
			tip = cap
		}
		return feeSet{dynamic: true, cap: cap, tip: tip}, nil
	}
	base, tip, err := baseAndTip(ctx, rpc)
	if err != nil {
		return feeSet{}, err
	}
	if base == nil {
		// A chain without EIP-1559 fees: the gas price is the fee.
		price, err := gasPrice(ctx, rpc)
		if err != nil {
			return feeSet{}, err
		}
		return feeSet{cap: price}, nil
	}
	return feeSet{dynamic: true, cap: new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip), tip: tip}, nil
}

// suggestedFees is a max fee per gas for a plugin that named none.
func suggestedFees(ctx context.Context, rpc RPC) (*big.Int, error) {
	base, tip, err := baseAndTip(ctx, rpc)
	if err != nil {
		return nil, err
	}
	if base == nil {
		return gasPrice(ctx, rpc)
	}
	return new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), tip), nil
}

// baseAndTip reads the latest block's base fee and the chain's suggested
// priority fee. base is nil on a chain without EIP-1559 fees.
func baseAndTip(ctx context.Context, rpc RPC) (base, tip *big.Int, err error) {
	out, err := rpc(ctx, "eth_getBlockByNumber", []any{"latest", false})
	if err != nil {
		return nil, nil, err
	}
	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := json.Unmarshal(out, &block); err != nil {
		return nil, nil, fmt.Errorf("reading the latest block: %w", err)
	}
	if block.BaseFeePerGas == "" {
		return nil, nil, nil
	}
	if base, err = parseHexBig(block.BaseFeePerGas); err != nil {
		return nil, nil, fmt.Errorf("reading the base fee: %w", err)
	}
	tip = big.NewInt(1_000_000_000) // 1 gwei
	if out, err := rpc(ctx, "eth_maxPriorityFeePerGas", []any{}); err == nil {
		var s string
		if json.Unmarshal(out, &s) == nil {
			if v, err := parseHexBig(s); err == nil && v.Sign() > 0 {
				tip = v
			}
		}
	}
	return base, tip, nil
}

func gasPrice(ctx context.Context, rpc RPC) (*big.Int, error) {
	out, err := rpc(ctx, "eth_gasPrice", []any{})
	if err != nil {
		return nil, err
	}
	var s string
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("reading the gas price: %w", err)
	}
	price, err := parseHexBig(s)
	if err != nil {
		return nil, fmt.Errorf("reading the gas price: %w", err)
	}
	return price, nil
}

func transactionCount(ctx context.Context, rpc RPC, from common.Address) (uint64, error) {
	out, err := rpc(ctx, "eth_getTransactionCount", []any{from.Hex(), "pending"})
	if err != nil {
		return 0, err
	}
	var s string
	if err := json.Unmarshal(out, &s); err != nil {
		return 0, fmt.Errorf("reading the nonce: %w", err)
	}
	n, err := parseHexBig(s)
	if err != nil {
		return 0, fmt.Errorf("reading the nonce: %w", err)
	}
	return n.Uint64(), nil
}

func estimateGas(ctx context.Context, rpc RPC, from, to common.Address, value *big.Int, data []byte) (uint64, error) {
	call := map[string]any{
		"from":  from.Hex(),
		"to":    to.Hex(),
		"value": "0x" + value.Text(16),
	}
	if len(data) > 0 {
		call["data"] = "0x" + hex.EncodeToString(data)
	}
	out, err := rpc(ctx, "eth_estimateGas", []any{call})
	if err != nil {
		return 0, err
	}
	var s string
	if err := json.Unmarshal(out, &s); err != nil {
		return 0, fmt.Errorf("reading the gas estimate: %w", err)
	}
	gas, err := parseHexBig(s)
	if err != nil {
		return 0, fmt.Errorf("reading the gas estimate: %w", err)
	}
	// A little room, as wallets give: an estimate can be a hair short.
	return gas.Uint64() * 12 / 10, nil
}

// decodeCall names what the calldata does, when it is a call the shell
// knows: an ERC-20 transfer or approval, with the token's own symbol and
// decimals when the contract answers.
func decodeCall(ctx context.Context, rpc RPC, to common.Address, data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) < 4 {
		return fmt.Sprintf("Call · %d bytes", len(data))
	}
	selector := "0x" + hex.EncodeToString(data[:4])
	switch selector {
	case "0xa9059cbb", "0x095ea7b3": // transfer(address,uint256), approve(address,uint256)
		if len(data) != 68 {
			return fmt.Sprintf("Call %s · %d bytes", selector, len(data))
		}
		spender := common.BytesToAddress(data[4:36])
		amount := new(big.Int).SetBytes(data[36:68])
		symbol, decimals := tokenInfo(ctx, rpc, to)
		value := FormatUnits(amount, decimals)
		if symbol != "" {
			value += " " + symbol
		}
		if selector == "0xa9059cbb" {
			return "Transfer " + value + " to " + shortAddr(spender)
		}
		return "Approve " + value + " for " + shortAddr(spender)
	}
	return fmt.Sprintf("Contract call %s · %d bytes", selector, len(data))
}

// tokenInfo reads a token's symbol and decimals, falling back to none and
// 18 when the contract does not answer.
func tokenInfo(ctx context.Context, rpc RPC, token common.Address) (string, int) {
	symbol := ""
	decimals := 18
	if out, err := ethCall(ctx, rpc, token, "0x95d89b41"); err == nil { // symbol()
		if s, ok := decodeABIString(out); ok {
			symbol = s
		}
	}
	if out, err := ethCall(ctx, rpc, token, "0x313ce567"); err == nil { // decimals()
		if len(out) >= 32 {
			if d := new(big.Int).SetBytes(out[len(out)-32:]); d.IsInt64() && d.Int64() <= 36 {
				decimals = int(d.Int64())
			}
		}
	}
	return symbol, decimals
}

func ethCall(ctx context.Context, rpc RPC, to common.Address, data string) ([]byte, error) {
	out, err := rpc(ctx, "eth_call", []any{map[string]any{"to": to.Hex(), "data": data}, "latest"})
	if err != nil {
		return nil, err
	}
	var s string
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

// decodeABIString reads a string return value: an offset, a length, bytes.
func decodeABIString(b []byte) (string, bool) {
	if len(b) < 64 {
		return "", false
	}
	offset := new(big.Int).SetBytes(b[:32]).Uint64()
	if offset+32 > uint64(len(b)) {
		return "", false
	}
	length := new(big.Int).SetBytes(b[offset : offset+32]).Uint64()
	if offset+32+length > uint64(len(b)) {
		return "", false
	}
	s := string(b[offset+32 : offset+32+length])
	if !isPrintable(s) {
		return "", false
	}
	return s, true
}

func isPrintable(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func shortAddr(a common.Address) string {
	h := a.Hex()
	return h[:6] + "…" + h[len(h)-4:]
}

// FormatUnits renders amount with decimals, grouping thousands and
// trimming trailing zeros, as a person reads an amount.
func FormatUnits(amount *big.Int, decimals int) string {
	if decimals < 0 {
		decimals = 0
	}
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	whole := new(big.Int)
	frac := new(big.Int)
	whole.QuoRem(amount, unit, frac)
	s := group(whole.String())
	if frac.Sign() == 0 {
		return s
	}
	digits := frac.String()
	for len(digits) < decimals {
		digits = "0" + digits
	}
	digits = strings.TrimRight(digits, "0")
	if digits == "" {
		return s
	}
	return s + "." + digits
}

// group puts a comma between every three digits of a whole number.
func group(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	first := len(s) % 3
	if first > 0 {
		b.WriteString(s[:first])
	}
	for i := first; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func parseBig(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimSpace(s), 10)
	if !ok || v.Sign() < 0 {
		return nil, fmt.Errorf("%q is not a whole number", s)
	}
	return v, nil
}

func parseHexBig(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if s == "" {
		return new(big.Int), nil
	}
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return nil, fmt.Errorf("%q is not a hex number", s)
	}
	return v, nil
}

func parseData(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	s = strings.TrimPrefix(s, "0x")
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, errors.New("data is not hex")
	}
	return data, nil
}
