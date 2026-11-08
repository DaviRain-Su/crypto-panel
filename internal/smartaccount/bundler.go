package smartaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Bundler is an ERC-4337 bundler's JSON-RPC endpoint. Pimlico, Alchemy and a
// self-hosted bundler all speak the same methods.
type Bundler struct {
	URL  string
	HTTP *http.Client
}

// NewBundler returns a client for one bundler URL.
func NewBundler(url string) *Bundler {
	return &Bundler{URL: url, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// GasEstimate is what a bundler expects a user operation to need.
type GasEstimate struct {
	CallGasLimit                  *big.Int
	VerificationGasLimit          *big.Int
	PreVerificationGas            *big.Int
	PaymasterVerificationGasLimit *big.Int
	PaymasterPostOpGasLimit       *big.Int
}

// UserOperationReceipt is the part of a bundler receipt the app shows and
// records. A nil receipt means the operation is not mined yet.
type UserOperationReceipt struct {
	UserOpHash      common.Hash
	EntryPoint      common.Address
	Sender          common.Address
	Nonce           *big.Int
	Success         bool
	Reason          string
	TransactionHash common.Hash
	BlockNumber     *big.Int
	ActualGasUsed   *big.Int
}

// EstimateUserOperationGas asks the bundler for gas limits.
func (b *Bundler) EstimateUserOperationGas(ctx context.Context, op *UserOperation, entryPoint common.Address) (GasEstimate, error) {
	if op == nil {
		return GasEstimate{}, errors.New("smartaccount: nil user operation")
	}
	var raw gasEstimateJSON
	if err := b.call(ctx, "eth_estimateUserOperationGas", []any{op.rpc(), entryPoint.Hex()}, &raw); err != nil {
		return GasEstimate{}, err
	}
	return GasEstimate{
		CallGasLimit:                  bigOrNil(raw.CallGasLimit),
		VerificationGasLimit:          bigOrNil(raw.VerificationGasLimit),
		PreVerificationGas:            bigOrNil(raw.PreVerificationGas),
		PaymasterVerificationGasLimit: bigOrNil(raw.PaymasterVerificationGasLimit),
		PaymasterPostOpGasLimit:       bigOrNil(raw.PaymasterPostOpGasLimit),
	}, nil
}

// SendUserOperation submits a signed operation and returns its hash.
func (b *Bundler) SendUserOperation(ctx context.Context, op *UserOperation, entryPoint common.Address) (common.Hash, error) {
	if op == nil {
		return common.Hash{}, errors.New("smartaccount: nil user operation")
	}
	var out string
	if err := b.call(ctx, "eth_sendUserOperation", []any{op.rpc(), entryPoint.Hex()}, &out); err != nil {
		return common.Hash{}, err
	}
	return parseHash(out)
}

// UserOperationReceipt returns the receipt, or nil while the operation is
// still pending.
func (b *Bundler) UserOperationReceipt(ctx context.Context, hash common.Hash) (*UserOperationReceipt, error) {
	var raw json.RawMessage
	if err := b.call(ctx, "eth_getUserOperationReceipt", []any{hash.Hex()}, &raw); err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var r receiptJSON
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("smartaccount: decoding receipt: %w", err)
	}
	return &UserOperationReceipt{
		UserOpHash:      common.HexToHash(r.UserOpHash),
		EntryPoint:      common.HexToAddress(r.EntryPoint),
		Sender:          common.HexToAddress(r.Sender),
		Nonce:           bigOrNil(r.Nonce),
		Success:         r.Success,
		Reason:          r.Reason,
		TransactionHash: common.HexToHash(r.TransactionHash),
		BlockNumber:     bigOrNil(r.BlockNumber),
		ActualGasUsed:   bigOrNil(r.ActualGasUsed),
	}, nil
}

// SupportedEntryPoints lists the EntryPoint contracts the bundler serves.
func (b *Bundler) SupportedEntryPoints(ctx context.Context) ([]common.Address, error) {
	var out []string
	if err := b.call(ctx, "eth_supportedEntryPoints", []any{}, &out); err != nil {
		return nil, err
	}
	addrs := make([]common.Address, 0, len(out))
	for _, s := range out {
		if !common.IsHexAddress(s) {
			return nil, fmt.Errorf("smartaccount: bundler returned an invalid entry point %q", s)
		}
		addrs = append(addrs, common.HexToAddress(s))
	}
	return addrs, nil
}

// rpcUserOperation is the JSON shape the bundler methods take.
type rpcUserOperation struct {
	Sender               string `json:"sender"`
	Nonce                string `json:"nonce"`
	InitCode             string `json:"initCode"`
	CallData             string `json:"callData"`
	CallGasLimit         string `json:"callGasLimit"`
	VerificationGasLimit string `json:"verificationGasLimit"`
	PreVerificationGas   string `json:"preVerificationGas"`
	MaxFeePerGas         string `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
	PaymasterAndData     string `json:"paymasterAndData"`
	Signature            string `json:"signature"`
}

func (op *UserOperation) rpc() rpcUserOperation {
	return rpcUserOperation{
		Sender:               op.Sender.Hex(),
		Nonce:                hexutil.EncodeBig(zero(op.Nonce)),
		InitCode:             hexutil.Encode(op.InitCode),
		CallData:             hexutil.Encode(op.CallData),
		CallGasLimit:         hexutil.EncodeBig(zero(op.CallGasLimit)),
		VerificationGasLimit: hexutil.EncodeBig(zero(op.VerificationGasLimit)),
		PreVerificationGas:   hexutil.EncodeBig(zero(op.PreVerificationGas)),
		MaxFeePerGas:         hexutil.EncodeBig(zero(op.MaxFeePerGas)),
		MaxPriorityFeePerGas: hexutil.EncodeBig(zero(op.MaxPriorityFeePerGas)),
		PaymasterAndData:     hexutil.Encode(op.PaymasterAndData),
		Signature:            hexutil.Encode(op.Signature),
	}
}

type gasEstimateJSON struct {
	CallGasLimit                  *hexutil.Big `json:"callGasLimit"`
	VerificationGasLimit          *hexutil.Big `json:"verificationGasLimit"`
	PreVerificationGas            *hexutil.Big `json:"preVerificationGas"`
	PaymasterVerificationGasLimit *hexutil.Big `json:"paymasterVerificationGasLimit"`
	PaymasterPostOpGasLimit       *hexutil.Big `json:"paymasterPostOpGasLimit"`
}

type receiptJSON struct {
	UserOpHash      string       `json:"userOpHash"`
	EntryPoint      string       `json:"entryPoint"`
	Sender          string       `json:"sender"`
	Nonce           *hexutil.Big `json:"nonce"`
	Success         bool         `json:"success"`
	Reason          string       `json:"reason"`
	TransactionHash string       `json:"transactionHash"`
	BlockNumber     *hexutil.Big `json:"blockNumber"`
	ActualGasUsed   *hexutil.Big `json:"actualGasUsed"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("smartaccount: bundler error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("smartaccount: bundler error %d: %s", e.Code, e.Message)
}

func (b *Bundler) call(ctx context.Context, method string, params any, out any) error {
	if b == nil || b.URL == "" {
		return errors.New("smartaccount: bundler has no URL")
	}
	client := b.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("smartaccount: encoding %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("smartaccount: %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("smartaccount: %s: %w", method, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("smartaccount: %s: reading response: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("smartaccount: %s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		return fmt.Errorf("smartaccount: %s: decoding response: %w", method, err)
	}
	if rpcResp.Error != nil {
		return rpcResp.Error
	}
	if len(rpcResp.Result) == 0 {
		return fmt.Errorf("smartaccount: %s: empty result", method)
	}
	if out != nil {
		if err := json.Unmarshal(rpcResp.Result, out); err != nil {
			return fmt.Errorf("smartaccount: %s: decoding result: %w", method, err)
		}
	}
	return nil
}

func bigOrNil(v *hexutil.Big) *big.Int {
	if v == nil {
		return nil
	}
	return (*big.Int)(v)
}

func parseHash(s string) (common.Hash, error) {
	raw, err := hexutil.Decode(s)
	if err != nil || len(raw) != 32 {
		return common.Hash{}, fmt.Errorf("smartaccount: bundler returned an invalid hash %q", s)
	}
	return common.BytesToHash(raw), nil
}
