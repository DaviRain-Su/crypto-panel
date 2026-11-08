// Package neartx builds, signs and sends NEAR transactions for the wallet.
// A plugin or DApp states an intent; the shell fills in the access key nonce
// and the recent block hash, decodes the result for the approval window, and
// only then signs it.
//
// NEAR serializes transactions with Borsh. The encoder here covers every
// action the shell exposes (transfer, function call, add and delete keys,
// create account, deploy contract, delete account) and refuses anything else
// rather than writing a transaction with a field it does not understand. The
// encoding is checked against a real testnet transaction in the tests.
package neartx

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"math/big"
)

// KeyTypeED25519 is the only key type the wallet holds.
const KeyTypeED25519 = 0

// Gas limits. A function call defaults to 30 TGas, the amount a simple call
// needs; the shell refuses more than 300 TGas, the protocol's per-transaction
// ceiling.
const (
	DefaultGas = 30_000_000_000_000
	MaxGas     = 300_000_000_000_000
)

// PublicKey is an ed25519 public key.
type PublicKey struct {
	Bytes [32]byte
}

// Signature is an ed25519 signature.
type Signature struct {
	Bytes [64]byte
}

// AccessKey is a key's on-chain state: its nonce and what it may do.
type AccessKey struct {
	Nonce      uint64
	Permission Permission
}

// Permission is either a function-call key or a full-access key. A nil
// FunctionCall is full access.
type Permission struct {
	FunctionCall *FunctionCallPermission
}

// FunctionCallPermission limits a key to calling named methods of one
// contract, spending at most Allowance yoctoNEAR. A nil Allowance spends the
// account's balance on gas and deposits without a cap.
type FunctionCallPermission struct {
	Allowance   *big.Int
	ReceiverID  string
	MethodNames []string
}

// ActionKind is a Borsh action variant.
type ActionKind uint8

const (
	ActionCreateAccount  ActionKind = 0
	ActionDeployContract ActionKind = 1
	ActionFunctionCall   ActionKind = 2
	ActionTransfer       ActionKind = 3
	ActionStake          ActionKind = 4
	ActionAddKey         ActionKind = 5
	ActionDeleteKey      ActionKind = 6
	ActionDeleteAccount  ActionKind = 7
)

// String names the action the way the approval window says it.
func (k ActionKind) String() string {
	switch k {
	case ActionCreateAccount:
		return "create account"
	case ActionDeployContract:
		return "deploy contract"
	case ActionFunctionCall:
		return "function call"
	case ActionTransfer:
		return "transfer"
	case ActionStake:
		return "stake"
	case ActionAddKey:
		return "add key"
	case ActionDeleteKey:
		return "delete key"
	case ActionDeleteAccount:
		return "delete account"
	}
	return fmt.Sprintf("action %d", uint8(k))
}

// Action is one action of a transaction. Only the fields of its Kind are
// read.
type Action struct {
	Kind ActionKind
	// Deposit is the transfer amount, the function call's attached deposit,
	// or the stake, in yoctoNEAR.
	Deposit *big.Int
	// FunctionCall.
	MethodName string
	Args       []byte
	Gas        uint64
	// AddKey and DeleteKey.
	PublicKey PublicKey
	AccessKey *AccessKey
	// DeployContract.
	Code []byte
	// DeleteAccount.
	BeneficiaryID string
}

// Transfer returns an action sending amount yoctoNEAR.
func Transfer(amount *big.Int) Action {
	return Action{Kind: ActionTransfer, Deposit: amount}
}

// FunctionCall returns an action calling method with args, gas and deposit.
func FunctionCall(method string, args []byte, gas uint64, deposit *big.Int) Action {
	return Action{
		Kind:       ActionFunctionCall,
		MethodName: method,
		Args:       args,
		Gas:        gas,
		Deposit:    deposit,
	}
}

// AddFunctionCallKey returns an action adding a key limited to calling
// methods of receiverID, spending at most allowance yoctoNEAR.
func AddFunctionCallKey(publicKey PublicKey, allowance *big.Int, receiverID string, methods []string) Action {
	return Action{
		Kind:      ActionAddKey,
		PublicKey: publicKey,
		AccessKey: &AccessKey{
			Permission: Permission{FunctionCall: &FunctionCallPermission{
				Allowance:   allowance,
				ReceiverID:  receiverID,
				MethodNames: append([]string(nil), methods...),
			}},
		},
	}
}

// AddFullAccessKey returns an action adding a key with no limits.
func AddFullAccessKey(publicKey PublicKey) Action {
	return Action{
		Kind:      ActionAddKey,
		PublicKey: publicKey,
		AccessKey: &AccessKey{},
	}
}

// DeleteKey returns an action deleting publicKey from the receiver account.
func DeleteKey(publicKey PublicKey) Action {
	return Action{Kind: ActionDeleteKey, PublicKey: publicKey}
}

// Transaction is a NEAR transaction before its signature.
type Transaction struct {
	SignerID   string
	PublicKey  PublicKey
	Nonce      uint64
	ReceiverID string
	BlockHash  [32]byte
	Actions    []Action
}

// Signer is the account and key a transaction is signed with.
type Signer struct {
	AccountID string
	PublicKey PublicKey
}

// Request is what a plugin or DApp asks the shell to sign: the receiver and
// the actions. The shell fills in the nonce and block hash.
type Request struct {
	ReceiverID string
	Actions    []Action
}

// ErrInvalidTransaction reports a transaction the encoder refuses.
var ErrInvalidTransaction = errors.New("invalid NEAR transaction")

// Encode returns the Borsh encoding of tx.
func (tx *Transaction) Encode() ([]byte, error) {
	w := &writer{}
	if err := w.transaction(tx); err != nil {
		return nil, err
	}
	return w.buf, nil
}

// Hash returns the transaction's hash, sha256 of its Borsh encoding. It is
// what the signer signs and what the chain reports as the transaction id.
func (tx *Transaction) Hash() ([32]byte, error) {
	raw, err := tx.Encode()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256Sum(raw), nil
}

// Sign signs tx with key and returns the Borsh encoding of the signed
// transaction and its hash.
func Sign(key ed25519.PrivateKey, tx *Transaction) ([]byte, [32]byte, error) {
	raw, err := tx.Encode()
	if err != nil {
		return nil, [32]byte{}, err
	}
	hash := sha256Sum(raw)
	sig := ed25519.Sign(key, hash[:])
	if len(sig) != ed25519.SignatureSize {
		return nil, [32]byte{}, errors.New("signing: unexpected signature size")
	}
	signed := make([]byte, 0, len(raw)+1+len(sig))
	signed = append(signed, raw...)
	signed = append(signed, KeyTypeED25519)
	signed = append(signed, sig...)
	return signed, hash, nil
}
