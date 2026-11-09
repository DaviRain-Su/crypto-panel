package neartx

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
)

// writer is a Borsh encoder: little-endian integers, length-prefixed strings
// and byte arrays, and one tag byte per enum.
type writer struct {
	buf []byte
}

func (w *writer) u8(v uint8) { w.buf = append(w.buf, v) }

func (w *writer) u32(v uint32) {
	w.buf = binary.LittleEndian.AppendUint32(w.buf, v)
}

func (w *writer) u64(v uint64) {
	w.buf = binary.LittleEndian.AppendUint64(w.buf, v)
}

func (w *writer) u128(v *big.Int) error {
	if v == nil {
		return fmt.Errorf("%w: missing amount", ErrInvalidTransaction)
	}
	if v.Sign() < 0 || v.BitLen() > 128 {
		return fmt.Errorf("%w: amount %s does not fit in u128", ErrInvalidTransaction, v)
	}
	var out [16]byte
	v.FillBytes(out[:]) // big-endian
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	w.buf = append(w.buf, out[:]...)
	return nil
}

func (w *writer) optionU128(v *big.Int) error {
	if v == nil {
		w.u8(0)
		return nil
	}
	w.u8(1)
	return w.u128(v)
}

func (w *writer) bytes(b []byte) {
	w.u32(uint32(len(b)))
	w.buf = append(w.buf, b...)
}

func (w *writer) str(s string) {
	w.u32(uint32(len(s)))
	w.buf = append(w.buf, s...)
}

func (w *writer) publicKey(k PublicKey) {
	w.u8(KeyTypeED25519)
	w.buf = append(w.buf, k.Bytes[:]...)
}

func (w *writer) accessKey(k *AccessKey) error {
	if k == nil {
		return fmt.Errorf("%w: add key without an access key", ErrInvalidTransaction)
	}
	w.u64(k.Nonce)
	if k.Permission.FunctionCall == nil {
		w.u8(1) // full access
		return nil
	}
	w.u8(0) // function call
	fc := k.Permission.FunctionCall
	if err := w.optionU128(fc.Allowance); err != nil {
		return err
	}
	w.str(fc.ReceiverID)
	w.u32(uint32(len(fc.MethodNames)))
	for _, method := range fc.MethodNames {
		w.str(method)
	}
	return nil
}

func (w *writer) action(a Action) error {
	w.u8(uint8(a.Kind))
	switch a.Kind {
	case ActionCreateAccount:
		return nil
	case ActionDeployContract:
		w.bytes(a.Code)
		return nil
	case ActionFunctionCall:
		w.str(a.MethodName)
		w.bytes(a.Args)
		w.u64(a.Gas)
		return w.u128(a.Deposit)
	case ActionTransfer:
		return w.u128(a.Deposit)
	case ActionStake:
		if err := w.u128(a.Deposit); err != nil {
			return err
		}
		w.publicKey(a.PublicKey)
		return nil
	case ActionAddKey:
		w.publicKey(a.PublicKey)
		return w.accessKey(a.AccessKey)
	case ActionDeleteKey:
		w.publicKey(a.PublicKey)
		return nil
	case ActionDeleteAccount:
		w.str(a.BeneficiaryID)
		return nil
	case ActionDeterministicStateInit:
		// The StateInit struct is inline, not a length-prefixed byte vector.
		w.buf = append(w.buf, a.StateInit...)
		return w.u128(a.Deposit)
	}
	return fmt.Errorf("%w: unknown action %d", ErrInvalidTransaction, uint8(a.Kind))
}

func (w *writer) transaction(tx *Transaction) error {
	if tx == nil {
		return fmt.Errorf("%w: nothing to encode", ErrInvalidTransaction)
	}
	if tx.SignerID == "" || tx.ReceiverID == "" {
		return fmt.Errorf("%w: signer and receiver are required", ErrInvalidTransaction)
	}
	w.str(tx.SignerID)
	w.publicKey(tx.PublicKey)
	w.u64(tx.Nonce)
	w.str(tx.ReceiverID)
	w.buf = append(w.buf, tx.BlockHash[:]...)
	w.u32(uint32(len(tx.Actions)))
	for _, a := range tx.Actions {
		if err := w.action(a); err != nil {
			return err
		}
	}
	return nil
}

func sha256Sum(b []byte) [32]byte { return sha256.Sum256(b) }
