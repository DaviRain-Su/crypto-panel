package neartx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/davirain-su/lintel/internal/base58"
)

// RPC is one call to the chain, as chain.Reader.RPC makes it.
type RPC func(ctx context.Context, method string, params any) (json.RawMessage, error)

// Prepare fills in the access key nonce and the recent block hash, and
// returns the transaction and the summary the approval window shows. It
// refuses an action the shell does not sign, so a plugin cannot smuggle a
// staking or contract-deploy transaction through a transfer request.
func Prepare(ctx context.Context, rpc RPC, signer Signer, req Request) (*Transaction, *Summary, error) {
	signerID, err := ParseAccountID(signer.AccountID)
	if err != nil {
		return nil, nil, fmt.Errorf("signer: %w", err)
	}
	receiverID, err := ParseAccountID(req.ReceiverID)
	if err != nil {
		return nil, nil, fmt.Errorf("receiver: %w", err)
	}
	if len(req.Actions) == 0 {
		return nil, nil, errors.New("the transaction has no actions")
	}
	for i := range req.Actions {
		if err := prepareAction(&req.Actions[i]); err != nil {
			return nil, nil, fmt.Errorf("action %d: %w", i+1, err)
		}
	}
	key, err := AccessKeyOf(ctx, rpc, signer)
	if err != nil {
		return nil, nil, err
	}
	// A function-call key is the shell's limit on what a plugin or DApp can
	// do. Refuse here, before the user is asked, what the chain would refuse
	// anyway, so the failure is a sentence rather than a failed transaction.
	if err := checkPermission(key, signerID, receiverID, req.Actions); err != nil {
		return nil, nil, err
	}
	blockHash, err := RecentBlockHash(ctx, rpc)
	if err != nil {
		return nil, nil, err
	}
	tx := &Transaction{
		SignerID:   signerID,
		PublicKey:  signer.PublicKey,
		Nonce:      key.Nonce + 1,
		ReceiverID: receiverID,
		BlockHash:  blockHash,
		Actions:    req.Actions,
	}
	summary := Summarize(tx)
	summary.KeyPermission = keySummary(key)
	return tx, summary, nil
}

// checkPermission applies a function-call key's on-chain limits to the
// transaction: one contract, the named methods, and a deposit within the
// allowance. A full-access key has no limits here.
func checkPermission(key AccessKey, signerID, receiverID string, actions []Action) error {
	fc := key.Permission.FunctionCall
	if fc == nil {
		return nil
	}
	if receiverID != fc.ReceiverID {
		return fmt.Errorf("the signing key may only call %s, not %s", fc.ReceiverID, receiverID)
	}
	total := new(big.Int)
	for _, a := range actions {
		switch a.Kind {
		case ActionFunctionCall:
			if len(fc.MethodNames) > 0 && !contains(fc.MethodNames, a.MethodName) {
				return fmt.Errorf("the signing key may not call %q on %s", a.MethodName, receiverID)
			}
			if a.Deposit != nil {
				total.Add(total, a.Deposit)
			}
		default:
			return fmt.Errorf("a function-call key cannot %s", a.Kind)
		}
	}
	if fc.Allowance != nil && total.Cmp(fc.Allowance) > 0 {
		return fmt.Errorf("the deposit of %s yoctoNEAR is above the key's allowance of %s", total, fc.Allowance)
	}
	return nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// prepareAction validates one action and fills its defaults.
func prepareAction(a *Action) error {
	switch a.Kind {
	case ActionTransfer:
		if a.Deposit == nil || a.Deposit.Sign() <= 0 {
			return errors.New("a transfer needs an amount")
		}
		if a.Deposit.BitLen() > 128 {
			return errors.New("the amount does not fit in u128")
		}
	case ActionFunctionCall:
		if a.MethodName == "" || len(a.MethodName) > 256 {
			return errors.New("a function call needs a method name")
		}
		if a.Gas == 0 {
			a.Gas = DefaultGas
		}
		if a.Gas > MaxGas {
			return fmt.Errorf("gas %d is above the %d TGas limit", a.Gas, MaxGas/1_000_000_000_000)
		}
		if a.Deposit == nil {
			a.Deposit = new(big.Int)
		}
		if a.Deposit.Sign() < 0 || a.Deposit.BitLen() > 128 {
			return errors.New("the deposit is not a u128 amount")
		}
	case ActionAddKey:
		if a.PublicKey == (PublicKey{}) {
			return errors.New("add key needs a public key")
		}
		if a.AccessKey == nil {
			return errors.New("add key needs an access key")
		}
		if fc := a.AccessKey.Permission.FunctionCall; fc != nil {
			receiver, err := ParseAccountID(fc.ReceiverID)
			if err != nil {
				return fmt.Errorf("the key's contract: %w", err)
			}
			fc.ReceiverID = receiver
			if fc.Allowance != nil && (fc.Allowance.Sign() < 0 || fc.Allowance.BitLen() > 128) {
				return errors.New("the allowance is not a u128 amount")
			}
			for _, method := range fc.MethodNames {
				if method == "" || len(method) > 256 {
					return errors.New("the key's method list has an empty or too long name")
				}
			}
		}
	case ActionDeleteKey:
		if a.PublicKey == (PublicKey{}) {
			return errors.New("delete key needs a public key")
		}
	case ActionCreateAccount:
	case ActionDeployContract:
		if len(a.Code) == 0 {
			return errors.New("deploy contract needs code")
		}
	case ActionDeleteAccount:
		beneficiary, err := ParseAccountID(a.BeneficiaryID)
		if err != nil {
			return fmt.Errorf("the beneficiary: %w", err)
		}
		a.BeneficiaryID = beneficiary
	case ActionDeterministicStateInit:
		if len(a.StateInit) == 0 {
			return errors.New("a deterministic state init needs a StateInit")
		}
		if a.Deposit == nil {
			a.Deposit = new(big.Int)
		}
		if a.Deposit.Sign() < 0 || a.Deposit.BitLen() > 128 {
			return errors.New("the deposit is not a u128 amount")
		}
	default:
		return fmt.Errorf("%s is not an action this wallet signs", a.Kind)
	}
	return nil
}

// AccessKeyOf reads the signer's access key: its nonce and what it may do.
func AccessKeyOf(ctx context.Context, rpc RPC, signer Signer) (AccessKey, error) {
	raw, err := rpc(ctx, "query", map[string]any{
		"request_type": "view_access_key",
		"finality":     "final",
		"account_id":   signer.AccountID,
		"public_key":   signer.PublicKey.String(),
	})
	if err != nil {
		return AccessKey{}, fmt.Errorf("reading the access key: %w", err)
	}
	var out struct {
		Nonce      uint64          `json:"nonce"`
		Permission json.RawMessage `json:"permission"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return AccessKey{}, fmt.Errorf("reading the access key: %w", err)
	}
	key := AccessKey{Nonce: out.Nonce}
	if string(out.Permission) == `"FullAccess"` {
		return key, nil
	}
	var fc struct {
		FunctionCall struct {
			Allowance   string   `json:"allowance"`
			ReceiverID  string   `json:"receiver_id"`
			MethodNames []string `json:"method_names"`
		} `json:"FunctionCall"`
	}
	if err := json.Unmarshal(out.Permission, &fc); err != nil {
		return AccessKey{}, fmt.Errorf("reading the access key's permission: %w", err)
	}
	allowance := new(big.Int)
	if fc.FunctionCall.Allowance != "" {
		if _, ok := allowance.SetString(fc.FunctionCall.Allowance, 10); !ok {
			return AccessKey{}, errors.New("reading the access key's allowance")
		}
	}
	key.Permission.FunctionCall = &FunctionCallPermission{
		Allowance:   allowance,
		ReceiverID:  fc.FunctionCall.ReceiverID,
		MethodNames: fc.FunctionCall.MethodNames,
	}
	return key, nil
}

// RecentBlockHash reads the latest final block's hash, which a transaction
// names to prove it is recent.
func RecentBlockHash(ctx context.Context, rpc RPC) ([32]byte, error) {
	raw, err := rpc(ctx, "block", map[string]any{"finality": "final"})
	if err != nil {
		return [32]byte{}, fmt.Errorf("reading the latest block: %w", err)
	}
	var out struct {
		Header struct {
			Hash string `json:"hash"`
		} `json:"header"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return [32]byte{}, fmt.Errorf("reading the latest block: %w", err)
	}
	hash, err := base58.Decode(out.Header.Hash)
	if err != nil || len(hash) != 32 {
		return [32]byte{}, errors.New("reading the latest block: the hash is not 32 bytes")
	}
	var out32 [32]byte
	copy(out32[:], hash)
	return out32, nil
}

// Send broadcasts a signed transaction and waits for its final outcome. It
// refuses a node that reports another transaction id than the one signed.
func Send(ctx context.Context, rpc RPC, signed []byte, expected [32]byte) (string, error) {
	raw, err := rpc(ctx, "broadcast_tx_commit", []any{base64.StdEncoding.EncodeToString(signed)})
	if err != nil {
		return "", err
	}
	var out struct {
		Status             json.RawMessage `json:"status"`
		TransactionOutcome struct {
			ID string `json:"id"`
		} `json:"transaction_outcome"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("reading the transaction outcome: %w", err)
	}
	want := base58.Encode(expected[:])
	if out.TransactionOutcome.ID != "" && out.TransactionOutcome.ID != want {
		return "", fmt.Errorf("the node reported %s, but the signed transaction is %s", out.TransactionOutcome.ID, want)
	}
	var status map[string]json.RawMessage
	if err := json.Unmarshal(out.Status, &status); err != nil {
		return "", fmt.Errorf("reading the transaction status: %w", err)
	}
	if failure, ok := status["Failure"]; ok {
		return "", fmt.Errorf("the transaction failed: %s", failure)
	}
	return want, nil
}

// Status reads the node's chain id, for checking that an endpoint serves the
// network the settings name.
func Status(ctx context.Context, rpc RPC) (string, error) {
	raw, err := rpc(ctx, "status", []any{})
	if err != nil {
		return "", err
	}
	var out struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("reading the node status: %w", err)
	}
	return out.ChainID, nil
}

func keySummary(key AccessKey) *KeyPermissionSummary {
	if key.Permission.FunctionCall == nil {
		return &KeyPermissionSummary{FullAccess: true}
	}
	fc := key.Permission.FunctionCall
	return &KeyPermissionSummary{
		Allowance:   fc.Allowance,
		ReceiverID:  fc.ReceiverID,
		MethodNames: append([]string(nil), fc.MethodNames...),
	}
}
