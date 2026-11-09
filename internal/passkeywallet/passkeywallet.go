// Package passkeywallet builds the NEAR side of a WebAuthn passkey wallet:
// the deterministic account id of NEP-616, the wallet contract's Borsh
// StateInit, the RequestMessage the passkey signs, and the proof JSON the
// wallet contract verifies.
//
// The passkey's private key never leaves the platform authenticator. This
// package holds only the public key and moves a WebAuthn assertion into the
// shapes the NEAR wallet contract expects.
package passkeywallet

import (
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"

	"github.com/davirain-su/crypto-panel/internal/base58"
)

// FactoryP256 is the NEP-591 global wallet contract for P-256 passkeys. The
// contract is deployed by account id, so the StateInit names it instead of
// carrying code.
const FactoryP256 = "p256-passkey-wallet-contract.trezu.near"

// DefaultTimeoutSecs is the wallet state's nonce timeout, as the reference
// wallet uses it.
const DefaultTimeoutSecs = 3600

// PublicKey is a P-256 public key, the only curve the wallet registers.
type PublicKey struct {
	X *big.Int
	Y *big.Int
}

// NewPublicKey checks that x and y are a point on P-256.
func NewPublicKey(x, y *big.Int) (PublicKey, error) {
	if x == nil || y == nil || x.Sign() < 0 || y.Sign() < 0 ||
		x.BitLen() > 256 || y.BitLen() > 256 {
		return PublicKey{}, errors.New("passkeywallet: invalid P-256 public key")
	}
	if !elliptic.P256().IsOnCurve(x, y) {
		return PublicKey{}, errors.New("passkeywallet: public key is not on P-256")
	}
	return PublicKey{X: x, Y: y}, nil
}

// Compressed returns the SEC-1 compressed point: one parity byte and the 32
// bytes of x. The wallet contract stores exactly these 33 bytes.
func (k PublicKey) Compressed() []byte {
	out := make([]byte, 33)
	out[0] = 0x02
	if k.Y.Bit(0) == 1 {
		out[0] = 0x03
	}
	k.X.FillBytes(out[1:])
	return out
}

// String is the wallet contract's key convention: "p256:<base58>".
func (k PublicKey) String() string {
	return "p256:" + base58.Encode(k.Compressed())
}

// ParsePublicKey reads "p256:<base58(SEC-1 compressed)>".
func ParsePublicKey(s string) (PublicKey, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(s), "p256:")
	if !ok {
		return PublicKey{}, errors.New("passkeywallet: not a p256 public key")
	}
	raw, err := base58.Decode(rest)
	if err != nil || len(raw) != 33 || (raw[0] != 0x02 && raw[0] != 0x03) {
		return PublicKey{}, errors.New("passkeywallet: malformed p256 public key")
	}
	x := new(big.Int).SetBytes(raw[1:])
	y := decompressY(x, raw[0]&1 == 1)
	if y == nil {
		return PublicKey{}, errors.New("passkeywallet: public key is not on P-256")
	}
	return PublicKey{X: x, Y: y}, nil
}

// decompressY recovers y from x and the parity bit.
func decompressY(x *big.Int, odd bool) *big.Int {
	p := elliptic.P256().Params().P
	// y² = x³ - 3x + b (mod p)
	y2 := new(big.Int).Mul(x, x)
	y2.Mul(y2, x)
	threeX := new(big.Int).Lsh(x, 1)
	threeX.Add(threeX, x)
	y2.Sub(y2, threeX)
	y2.Add(y2, elliptic.P256().Params().B)
	y2.Mod(y2, p)
	// p ≡ 3 (mod 4), so sqrt is y2^((p+1)/4).
	exp := new(big.Int).Add(p, big.NewInt(1))
	exp.Rsh(exp, 2)
	y := new(big.Int).Exp(y2, exp, p)
	if y.Bit(0) == 1 != odd {
		y.Sub(p, y)
	}
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil
	}
	return y
}

// StateInit is the Borsh StateInit::V1 for the default passkey wallet of k:
// the global contract by account id, and one data entry (the empty key) with
// the wallet state.
func StateInit(k PublicKey) []byte {
	w := &writer{}
	w.u8(0) // StateInit::V1
	w.u8(1) // GlobalContractId::AccountId
	w.str(FactoryP256)
	w.u32(1) // one data entry
	w.bytes(nil)
	w.bytes(walletState(k))
	return w.buf
}

// walletState is borsh(State) with the default configuration:
// signature_enabled=true, subwallet_id=0, the 33-byte key, a 3600s nonce
// timeout, no used nonces, and no extensions.
func walletState(k PublicKey) []byte {
	w := &writer{}
	w.u8(1)
	w.u32(0)
	w.buf = append(w.buf, k.Compressed()...)
	w.u32(DefaultTimeoutSecs)
	w.u64(0) // last_cleaned_at
	w.u32(0)
	w.u32(0)
	w.u32(0)
	return w.buf
}

// AccountID is NEP-616's deterministic account id:
// "0s" + hex(keccak256(borsh(StateInit))[12:32]).
func AccountID(k PublicKey) string {
	h := sha3.NewLegacyKeccak256()
	h.Write(StateInit(k))
	sum := h.Sum(nil)
	return "0s" + hex.EncodeToString(sum[12:32])
}

// writer is the Borsh encoder the wallet contract's layout needs.
type writer struct{ buf []byte }

func (w *writer) u8(v uint8) { w.buf = append(w.buf, v) }

func (w *writer) u32(v uint32) {
	w.buf = append(w.buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func (w *writer) u64(v uint64) {
	w.buf = append(w.buf,
		byte(v), byte(v>>8), byte(v>>16), byte(v>>24),
		byte(v>>32), byte(v>>40), byte(v>>48), byte(v>>56))
}

func (w *writer) u128(v *big.Int) error {
	if v == nil {
		return errors.New("passkeywallet: missing amount")
	}
	if v.Sign() < 0 || v.BitLen() > 128 {
		return fmt.Errorf("passkeywallet: amount %s does not fit in u128", v)
	}
	var out [16]byte
	v.FillBytes(out[:])
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	w.buf = append(w.buf, out[:]...)
	return nil
}

func (w *writer) bytes(b []byte) {
	w.u32(uint32(len(b)))
	w.buf = append(w.buf, b...)
}

func (w *writer) str(s string) {
	w.u32(uint32(len(s)))
	w.buf = append(w.buf, s...)
}
