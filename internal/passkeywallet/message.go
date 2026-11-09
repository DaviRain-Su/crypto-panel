package passkeywallet

import (
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"golang.org/x/crypto/sha3"

	"github.com/davirain-su/lintel/internal/base58"
	"github.com/davirain-su/lintel/internal/webauthn"
)

// RequestDomain is the prefix the wallet contract hashes before the Borsh
// RequestMessage. It must match the contract exactly.
const RequestDomain = "NEAR_WALLET_CONTRACT/V1"

// Action kinds the wallet contract understands.
const (
	actionFunctionCall           = 2
	actionTransfer               = 3
	actionDeterministicStateInit = 11
)

// RequestMessage is what the passkey signs: one nonce, one chain, and the
// promises the wallet contract should make.
type RequestMessage struct {
	ChainID     string    `json:"chain_id"`
	SignerID    string    `json:"signer_id"`
	Nonce       uint32    `json:"nonce"`
	CreatedAt   time.Time `json:"created_at"`
	TimeoutSecs uint32    `json:"timeout_secs"`
	Request     Request   `json:"request"`
}

// Request is the wallet contract's work: internal wallet operations and
// external promises. This build only makes external promises.
type Request struct {
	Internal []WalletOp `json:"internal,omitempty"`
	External []Promise  `json:"external,omitempty"`
}

// WalletOp is an internal wallet operation. The app does not use them, and
// the encoder refuses one rather than guessing its layout.
type WalletOp struct {
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload"`
}

// Promise is one transaction the wallet contract makes: a receiver and its
// actions.
type Promise struct {
	ReceiverID string   `json:"receiver_id"`
	RefundTo   string   `json:"refund_to,omitempty"`
	Actions    []Action `json:"actions"`
}

// Action is one NEAR action inside a promise.
type Action struct {
	Action  string `json:"action"`
	Payload any    `json:"payload"`
}

// TransferAction sends amount yoctoNEAR.
type TransferAction struct {
	Amount string `json:"amount"`
}

// FunctionCallAction calls a contract method.
type FunctionCallAction struct {
	FunctionName string `json:"function_name"`
	Args         string `json:"args,omitempty"` // base64
	Deposit      string `json:"deposit,omitempty"`
	Gas          string `json:"gas,omitempty"`
	GasWeight    string `json:"gas_weight,omitempty"`
}

// Transfer returns a transfer action for amount yoctoNEAR.
func Transfer(amount *big.Int) Action {
	return Action{Action: "transfer", Payload: TransferAction{Amount: amount.String()}}
}

// FunctionCall returns a function-call action. args are the raw contract
// arguments; deposit and gas are decimal strings, empty for zero and the
// contract's default.
func FunctionCall(method string, args []byte, deposit *big.Int, gas uint64) Action {
	p := FunctionCallAction{FunctionName: method}
	if len(args) > 0 {
		p.Args = base64.StdEncoding.EncodeToString(args)
	}
	if deposit != nil && deposit.Sign() > 0 {
		p.Deposit = deposit.String()
	}
	if gas > 0 {
		p.Gas = fmt.Sprintf("%d", gas)
	}
	return Action{Action: "function_call", Payload: p}
}

// NewTransferMessage builds the message for one transfer. created_at is a
// minute in the past so a node whose clock lags does not reject it.
func NewTransferMessage(chainID, signerID, receiverID string, amount *big.Int, now time.Time) RequestMessage {
	return RequestMessage{
		ChainID:     chainID,
		SignerID:    signerID,
		Nonce:       NewNonce(),
		CreatedAt:   now.UTC().Add(-time.Minute),
		TimeoutSecs: DefaultTimeoutSecs,
		Request: Request{External: []Promise{{
			ReceiverID: receiverID,
			Actions:    []Action{Transfer(amount)},
		}}},
	}
}

// NewNonce returns a random non-zero uint32. The wallet contract tracks used
// nonces for its timeout window, so a random value keeps two messages from
// colliding without a shared counter.
func NewNonce() uint32 {
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			// crypto/rand failing is not a condition a wallet can paper
			// over; fall back to the clock so the call still has a nonce.
			return uint32(time.Now().UnixNano()) | 1
		}
		if n := binary.LittleEndian.Uint32(b[:]); n != 0 {
			return n
		}
	}
}

// Encode returns the Borsh encoding the wallet contract hashes.
func (m RequestMessage) Encode() ([]byte, error) {
	if m.ChainID == "" || m.SignerID == "" {
		return nil, errors.New("passkeywallet: message has no chain or signer")
	}
	if m.TimeoutSecs == 0 {
		return nil, errors.New("passkeywallet: message has no timeout")
	}
	if len(m.Request.Internal) != 0 {
		return nil, errors.New("passkeywallet: internal wallet operations are not supported")
	}
	w := &writer{}
	w.str(m.ChainID)
	w.str(m.SignerID)
	w.u32(m.Nonce)
	w.u64(uint64(m.CreatedAt.UnixNano()))
	w.u32(m.TimeoutSecs)
	w.u32(0) // internal operations
	w.u32(uint32(len(m.Request.External)))
	for _, p := range m.Request.External {
		if p.ReceiverID == "" {
			return nil, errors.New("passkeywallet: promise has no receiver")
		}
		w.str(p.ReceiverID)
		if p.RefundTo == "" {
			w.u8(0)
		} else {
			w.u8(1)
			w.str(p.RefundTo)
		}
		w.u32(uint32(len(p.Actions)))
		for _, a := range p.Actions {
			if err := w.action(a); err != nil {
				return nil, err
			}
		}
	}
	return w.buf, nil
}

func (w *writer) action(a Action) error {
	switch a.Action {
	case "transfer":
		p, ok := a.Payload.(TransferAction)
		if !ok {
			return errors.New("passkeywallet: transfer payload is not a TransferAction")
		}
		amount, err := parseU128(p.Amount)
		if err != nil {
			return err
		}
		w.u8(actionTransfer)
		return w.u128(amount)
	case "function_call":
		p, ok := a.Payload.(FunctionCallAction)
		if !ok {
			return errors.New("passkeywallet: function call payload is not a FunctionCallAction")
		}
		if p.FunctionName == "" {
			return errors.New("passkeywallet: function call has no method")
		}
		var args []byte
		if p.Args != "" {
			var err error
			args, err = base64.StdEncoding.DecodeString(p.Args)
			if err != nil {
				return errors.New("passkeywallet: function call args are not base64")
			}
		}
		deposit, err := parseU128(p.Deposit)
		if err != nil {
			return err
		}
		gas, err := parseU64(p.Gas)
		if err != nil {
			return err
		}
		weight, err := parseU64(p.GasWeight)
		if err != nil {
			return err
		}
		if weight == 0 {
			weight = 1
		}
		w.u8(actionFunctionCall)
		w.str(p.FunctionName)
		w.bytes(args)
		if err := w.u128(deposit); err != nil {
			return err
		}
		w.u64(gas)
		w.u64(weight)
		return nil
	case "deterministic_state_init":
		p, ok := a.Payload.(StateInitAction)
		if !ok {
			return errors.New("passkeywallet: state init payload is not a StateInitAction")
		}
		deposit, err := parseU128(p.Deposit)
		if err != nil {
			return err
		}
		w.u8(actionDeterministicStateInit)
		w.buf = append(w.buf, p.StateInit...) // inline, not length-prefixed
		return w.u128(deposit)
	}
	return fmt.Errorf("passkeywallet: unknown action %q", a.Action)
}

// StateInitAction deploys a deterministic account as one action of a
// promise. The app uses it to create the passkey wallet account.
type StateInitAction struct {
	StateInit []byte
	Deposit   string `json:"deposit,omitempty"`
}

// Hash is SHA3-256(RequestDomain || borsh(message)): the challenge the
// passkey signs.
func (m RequestMessage) Hash() ([]byte, error) {
	raw, err := m.Encode()
	if err != nil {
		return nil, err
	}
	h := sha3.New256()
	h.Write([]byte(RequestDomain))
	h.Write(raw)
	return h.Sum(nil), nil
}

// Proof is the JSON the wallet contract's verifier reads. The signature is
// the raw low-S r||s, base58 encoded behind the curve name.
type Proof struct {
	AuthenticatorData string `json:"authenticator_data"`
	ClientDataJSON    string `json:"client_data_json"`
	Signature         string `json:"signature"`
}

// NewProof turns a WebAuthn assertion into the wallet contract's proof. The
// caller must have verified the assertion against the message hash, origin
// and relying party first.
func NewProof(assertion webauthn.Assertion) (string, error) {
	r, s, err := webauthn.ParseSignature(assertion.Signature)
	if err != nil {
		return "", err
	}
	// WebAuthn signatures may be high-S; the wallet contract's verifier
	// expects low-S, as the reference executor normalizes them.
	n := elliptic.P256().Params().N
	half := new(big.Int).Rsh(n, 1)
	if s.Cmp(half) > 0 {
		s = new(big.Int).Sub(n, s)
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	proof := Proof{
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(assertion.AuthenticatorData),
		ClientDataJSON:    string(assertion.ClientDataJSON),
		Signature:         "p256:" + base58.Encode(raw),
	}
	out, err := json.Marshal(proof)
	if err != nil {
		return "", fmt.Errorf("passkeywallet: encoding proof: %w", err)
	}
	return string(out), nil
}

// ExecuteArgs is the JSON argument of w_execute_signed(msg, proof).
func ExecuteArgs(message RequestMessage, proof string) ([]byte, error) {
	args, err := json.Marshal(struct {
		Msg   RequestMessage `json:"msg"`
		Proof string         `json:"proof"`
	}{Msg: message, Proof: proof})
	if err != nil {
		return nil, fmt.Errorf("passkeywallet: encoding execute args: %w", err)
	}
	return args, nil
}

func parseU128(s string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Sign() < 0 || v.BitLen() > 128 {
		return nil, fmt.Errorf("passkeywallet: %q is not a u128 amount", s)
	}
	return v, nil
}

func parseU64(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 64 {
		return 0, fmt.Errorf("passkeywallet: %q is not a u64", s)
	}
	return n.Uint64(), nil
}
