package neartx

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math/big"
)

// Summary is the shell's own decode of a transaction, for the approval
// window. A plugin or DApp never draws this.
type Summary struct {
	SignerID   string
	PublicKey  string
	ReceiverID string
	Actions    []ActionSummary
	// KeyPermission is what the signing key may do. A function-call key is
	// the shell's limit on a plugin or DApp, shown before the user approves.
	KeyPermission *KeyPermissionSummary
}

// KeyPermissionSummary is the signing key's on-chain permission.
type KeyPermissionSummary struct {
	FullAccess  bool
	Allowance   *big.Int
	ReceiverID  string
	MethodNames []string
}

// ActionSummary is one action as the approval window reads it.
type ActionSummary struct {
	Kind ActionKind
	// Amount is the transfer, deposit or stake, in yoctoNEAR.
	Amount *big.Int
	// FunctionCall.
	MethodName string
	Args       []byte
	ArgsText   string
	Gas        uint64
	// AddKey and DeleteKey.
	PublicKey string
	// AddKey with a function-call permission.
	Allowance   *big.Int
	KeyReceiver string
	MethodNames []string
	// DeployContract.
	CodeSize int
	// DeleteAccount.
	BeneficiaryID string
	// DeterministicStateInit.
	StateInitSize int
}

// Summarize decodes tx for the approval window.
func Summarize(tx *Transaction) *Summary {
	if tx == nil {
		return nil
	}
	out := &Summary{
		SignerID:   tx.SignerID,
		PublicKey:  tx.PublicKey.String(),
		ReceiverID: tx.ReceiverID,
		Actions:    make([]ActionSummary, 0, len(tx.Actions)),
	}
	for _, a := range tx.Actions {
		s := ActionSummary{Kind: a.Kind, Amount: a.Deposit}
		switch a.Kind {
		case ActionFunctionCall:
			s.MethodName = a.MethodName
			s.Args = a.Args
			s.ArgsText = DecodeArgs(a.Args)
			s.Gas = a.Gas
		case ActionAddKey:
			s.PublicKey = a.PublicKey.String()
			if a.AccessKey != nil && a.AccessKey.Permission.FunctionCall != nil {
				fc := a.AccessKey.Permission.FunctionCall
				s.Allowance = fc.Allowance
				s.KeyReceiver = fc.ReceiverID
				s.MethodNames = append([]string(nil), fc.MethodNames...)
			}
		case ActionDeleteKey:
			s.PublicKey = a.PublicKey.String()
		case ActionDeployContract:
			s.CodeSize = len(a.Code)
		case ActionDeleteAccount:
			s.BeneficiaryID = a.BeneficiaryID
		case ActionDeterministicStateInit:
			s.StateInitSize = len(a.StateInit)
		}
		out.Actions = append(out.Actions, s)
	}
	return out
}

// DecodeArgs renders function-call arguments for a person: compact JSON when
// they are JSON, base64 otherwise. It never pretends to have decoded what it
// has not.
func DecodeArgs(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	if json.Valid(args) {
		var buf bytes.Buffer
		if err := json.Compact(&buf, args); err == nil {
			return truncate(buf.String(), 512)
		}
	}
	return truncate("base64:"+base64.StdEncoding.EncodeToString(args), 512)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
