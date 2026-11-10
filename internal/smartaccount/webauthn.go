package smartaccount

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/davirain-su/lintel/internal/webauthn"
)

// WebAuthnAuth is the signature payload CoinbaseSmartWallet decodes from
// userOp.signature and from an EIP-1271 signature. The indices point into
// clientDataJSON so the contract does not have to parse JSON on chain.
type WebAuthnAuth struct {
	AuthenticatorData []byte
	ClientDataJSON    []byte
	ChallengeIndex    *big.Int
	TypeIndex         *big.Int
	R                 [32]byte
	S                 [32]byte
}

// Encode returns the ABI-encoded WebAuthnAuth the account expects.
func (a WebAuthnAuth) Encode() ([]byte, error) {
	if a.ChallengeIndex == nil || a.TypeIndex == nil {
		return nil, errors.New("smartaccount: WebAuthn signature has no indices")
	}
	out, err := abi.Arguments{
		{Type: bytesType},
		{Type: stringType},
		{Type: uint256Type},
		{Type: uint256Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
	}.Pack(
		a.AuthenticatorData,
		string(a.ClientDataJSON),
		a.ChallengeIndex,
		a.TypeIndex,
		a.R,
		a.S,
	)
	if err != nil {
		return nil, fmt.Errorf("smartaccount: packing WebAuthn signature: %w", err)
	}
	return out, nil
}

// Sign checks a WebAuthn assertion against the user operation's hash and
// returns the signature payload the account verifies. The passkey signs in
// the platform authenticator; this function is the app-side check that the
// assertion is for this operation and this origin.
func Sign(op *UserOperation, entryPoint common.Address, chainID *big.Int, cred webauthn.Credential, assertion webauthn.Assertion, origin, rpID string) ([]byte, error) {
	if op == nil {
		return nil, errors.New("smartaccount: nil user operation")
	}
	hash, err := op.Hash(entryPoint, chainID)
	if err != nil {
		return nil, err
	}
	if err := assertion.Verify(cred, hash.Bytes(), origin, rpID); err != nil {
		return nil, err
	}
	return Signature(assertion)
}

// Signature turns a WebAuthn assertion into the account's signature payload.
// The assertion must already be verified against the user operation hash;
// this function only rearranges it.
func Signature(assertion webauthn.Assertion) ([]byte, error) {
	challengeIndex, err := webauthn.ChallengeIndex(assertion.ClientDataJSON)
	if err != nil {
		return nil, err
	}
	typeIndex, err := webauthn.TypeIndex(assertion.ClientDataJSON)
	if err != nil {
		return nil, err
	}
	r, s, err := webauthn.ParseSignature(assertion.Signature)
	if err != nil {
		return nil, err
	}
	if r.BitLen() > 256 || s.BitLen() > 256 {
		return nil, errors.New("smartaccount: signature component is too large")
	}
	// WebAuthn signatures may be high-S; the account's verifier expects
	// low-S, as the reference executor normalizes them.
	n := elliptic.P256().Params().N
	half := new(big.Int).Rsh(n, 1)
	if s.Cmp(half) > 0 {
		s = new(big.Int).Sub(n, s)
	}
	auth := WebAuthnAuth{
		AuthenticatorData: assertion.AuthenticatorData,
		ClientDataJSON:    assertion.ClientDataJSON,
		ChallengeIndex:    big.NewInt(int64(challengeIndex)),
		TypeIndex:         big.NewInt(int64(typeIndex)),
	}
	r.FillBytes(auth.R[:])
	s.FillBytes(auth.S[:])
	return auth.Encode()
}
