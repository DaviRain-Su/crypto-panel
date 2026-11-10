// Package webauthn parses and verifies the two WebAuthn ceremonies a passkey
// wallet needs: registration (an attestationObject that carries a P-256
// public key) and assertion (authenticatorData, clientDataJSON and a DER
// signature). It does not run a ceremony; a browser or the platform
// authenticator does that. This package is the trust boundary between that
// result and the smart account that verifies it on chain.
package webauthn

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

// Authenticator data flags.
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

// Credential is a registered passkey: its credential id and the P-256 public
// key the authenticator signs with. The private key never leaves the
// authenticator.
type Credential struct {
	ID []byte
	X  *big.Int
	Y  *big.Int
}

// PublicKey returns the credential's public key.
func (c Credential) PublicKey() *ecdsa.PublicKey {
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: c.X, Y: c.Y}
}

// Registration is what an attestationObject proves: a credential id and the
// public key the authenticator will sign with.
type Registration struct {
	CredentialID []byte
	X            *big.Int
	Y            *big.Int
	AAGUID       []byte
	SignCount    uint32
	// RPIDHash is the relying party the authenticator bound the credential
	// to. ParseRegistration checks it against the app's rp id.
	RPIDHash []byte
}

// ParseRegistration reads a create() ceremony: it checks the client data's
// type, challenge and origin, then parses the attestationObject. Both halves
// come from the browser; checking them together is what binds the new
// credential to this registration.
func ParseRegistration(clientDataJSON, attestationObject, challenge []byte, origin, rpID string) (Registration, error) {
	cd, err := ParseClientData(clientDataJSON)
	if err != nil {
		return Registration{}, err
	}
	if cd.Type != "webauthn.create" {
		return Registration{}, fmt.Errorf("webauthn: client data type is %q, not webauthn.create", cd.Type)
	}
	want := base64.RawURLEncoding.EncodeToString(challenge)
	if cd.Challenge != want {
		return Registration{}, errors.New("webauthn: registration challenge does not match")
	}
	if origin != "" && cd.Origin != origin {
		return Registration{}, fmt.Errorf("webauthn: origin is %q, not %q", cd.Origin, origin)
	}
	if cd.CrossOrigin {
		return Registration{}, errors.New("webauthn: the ceremony ran in a cross-origin frame")
	}
	if rpID == "" {
		return Registration{}, errors.New("webauthn: empty relying party id")
	}
	reg, err := ParseAttestation(attestationObject)
	if err != nil {
		return Registration{}, err
	}
	rpHash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(reg.RPIDHash, rpHash[:]) {
		return Registration{}, errors.New("webauthn: registration is for another relying party")
	}
	return reg, nil
}

// ParseAttestation reads a WebAuthn attestationObject and returns the
// credential it registered. It requires the user-presence, user-verification
// and attested-credential flags, and a P-256 ES256 COSE key: the wallet asks
// for exactly that ceremony.
func ParseAttestation(attestationObject []byte) (Registration, error) {
	v, rest, err := decodeCBOR(attestationObject)
	if err != nil {
		return Registration{}, fmt.Errorf("webauthn: attestation: %w", err)
	}
	if len(rest) != 0 {
		return Registration{}, errors.New("webauthn: attestation: trailing data")
	}
	if v.kind != cborMap {
		return Registration{}, errors.New("webauthn: attestation: not a map")
	}
	authData, ok := v.m["authData"]
	if !ok || authData.kind != cborBytes {
		return Registration{}, errors.New("webauthn: attestation: no authData")
	}
	return parseAuthData(authData.b)
}

func parseAuthData(authData []byte) (Registration, error) {
	// rpIdHash (32) + flags (1) + signCount (4).
	if len(authData) < 37 {
		return Registration{}, errors.New("webauthn: authenticator data is too short")
	}
	flags := authData[32]
	switch {
	case flags&flagUserPresent == 0:
		return Registration{}, errors.New("webauthn: user presence was not asserted")
	case flags&flagUserVerified == 0:
		return Registration{}, errors.New("webauthn: user verification was not performed")
	case flags&flagAttested == 0:
		return Registration{}, errors.New("webauthn: no attested credential")
	}
	signCount := binary.BigEndian.Uint32(authData[33:37])
	p := 37
	// AAGUID (16) + credentialIdLength (2).
	if len(authData) < p+18 {
		return Registration{}, errors.New("webauthn: attested credential is truncated")
	}
	aaguid := append([]byte(nil), authData[p:p+16]...)
	p += 16
	idLen := int(binary.BigEndian.Uint16(authData[p : p+2]))
	p += 2
	// WebAuthn caps credential ids at 1023 bytes.
	if idLen == 0 || idLen > 1023 || len(authData) < p+idLen {
		return Registration{}, errors.New("webauthn: invalid credential id")
	}
	credentialID := append([]byte(nil), authData[p:p+idLen]...)
	p += idLen
	cose, _, err := decodeCBOR(authData[p:])
	if err != nil {
		return Registration{}, fmt.Errorf("webauthn: credential public key: %w", err)
	}
	x, y, err := coseP256(cose)
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		CredentialID: credentialID,
		X:            x,
		Y:            y,
		AAGUID:       aaguid,
		SignCount:    signCount,
		RPIDHash:     append([]byte(nil), authData[:32]...),
	}, nil
}

// coseP256 reads a COSE_Key map and returns its P-256 coordinates. Only
// EC2/ES256/P-256 is accepted; the wallet never registers another algorithm.
func coseP256(v cborValue) (*big.Int, *big.Int, error) {
	if v.kind != cborMap {
		return nil, nil, errors.New("webauthn: credential public key is not a map")
	}
	get := func(key int64) (cborValue, bool) {
		val, ok := v.m[key]
		return val, ok
	}
	if kty, ok := get(1); !ok || kty.kind != cborUint || kty.u != 2 {
		return nil, nil, errors.New("webauthn: credential key is not EC2")
	}
	if alg, ok := get(3); !ok || alg.kind != cborNegInt || alg.i != -7 {
		return nil, nil, errors.New("webauthn: credential key is not ES256")
	}
	if crv, ok := get(-1); !ok || crv.kind != cborUint || crv.u != 1 {
		return nil, nil, errors.New("webauthn: credential key is not P-256")
	}
	xv, ok := get(-2)
	if !ok || xv.kind != cborBytes || len(xv.b) != 32 {
		return nil, nil, errors.New("webauthn: credential key has no x coordinate")
	}
	yv, ok := get(-3)
	if !ok || yv.kind != cborBytes || len(yv.b) != 32 {
		return nil, nil, errors.New("webauthn: credential key has no y coordinate")
	}
	x := new(big.Int).SetBytes(xv.b)
	y := new(big.Int).SetBytes(yv.b)
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, nil, errors.New("webauthn: credential key is not on P-256")
	}
	return x, y, nil
}

// ClientData is the JSON a browser hands the authenticator. The challenge is
// base64url without padding; the origin is the page's origin.
type ClientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin,omitempty"`
}

// ParseClientData parses clientDataJSON and requires the fields every
// ceremony must carry.
func ParseClientData(raw []byte) (ClientData, error) {
	var cd ClientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return ClientData{}, fmt.Errorf("webauthn: client data: %w", err)
	}
	if cd.Type == "" || cd.Challenge == "" {
		return ClientData{}, errors.New("webauthn: client data is incomplete")
	}
	return cd, nil
}

// Assertion is the result of a WebAuthn get() ceremony.
type Assertion struct {
	AuthenticatorData []byte
	ClientDataJSON    []byte
	Signature         []byte // ASN.1 DER
}

// Verify checks an assertion against the credential, the challenge the app
// issued, the expected origin and the relying party id. The challenge binds
// the assertion to one user operation; the origin and rpId bind it to the
// site that asked for it.
func (a Assertion) Verify(cred Credential, challenge []byte, origin, rpID string) error {
	cd, err := ParseClientData(a.ClientDataJSON)
	if err != nil {
		return err
	}
	if cd.Type != "webauthn.get" {
		return fmt.Errorf("webauthn: client data type is %q, not webauthn.get", cd.Type)
	}
	want := base64.RawURLEncoding.EncodeToString(challenge)
	if cd.Challenge != want {
		return errors.New("webauthn: challenge does not match")
	}
	if origin != "" && cd.Origin != origin {
		return fmt.Errorf("webauthn: origin is %q, not %q", cd.Origin, origin)
	}
	if cd.CrossOrigin {
		return errors.New("webauthn: the ceremony ran in a cross-origin frame")
	}
	if rpID == "" {
		return errors.New("webauthn: empty relying party id")
	}
	if len(a.AuthenticatorData) < 37 {
		return errors.New("webauthn: authenticator data is too short")
	}
	rpHash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(a.AuthenticatorData[:32], rpHash[:]) {
		return errors.New("webauthn: relying party id does not match")
	}
	flags := a.AuthenticatorData[32]
	if flags&flagUserPresent == 0 {
		return errors.New("webauthn: user presence was not asserted")
	}
	if flags&flagUserVerified == 0 {
		return errors.New("webauthn: user verification was not performed")
	}
	return VerifySignature(cred.PublicKey(), a.AuthenticatorData, a.ClientDataJSON, a.Signature)
}

// VerifySignature verifies a WebAuthn assertion signature: ECDSA P-256 over
// SHA-256(authenticatorData || SHA-256(clientDataJSON)).
func VerifySignature(pub *ecdsa.PublicKey, authData, clientDataJSON, signature []byte) error {
	if pub == nil || pub.Curve != elliptic.P256() || pub.X == nil || pub.Y == nil {
		return errors.New("webauthn: not a P-256 public key")
	}
	clientHash := sha256.Sum256(clientDataJSON)
	msg := make([]byte, 0, len(authData)+len(clientHash))
	msg = append(msg, authData...)
	msg = append(msg, clientHash[:]...)
	digest := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(pub, digest[:], signature) {
		return errors.New("webauthn: signature does not verify")
	}
	return nil
}

// ParseSignature splits a DER signature into r and s, as the smart account's
// WebAuthn verifier expects them.
func ParseSignature(der []byte) (r, s *big.Int, err error) {
	var sig struct{ R, S *big.Int }
	rest, err := asn1.Unmarshal(der, &sig)
	if err != nil {
		return nil, nil, fmt.Errorf("webauthn: signature: %w", err)
	}
	if len(rest) != 0 {
		return nil, nil, errors.New("webauthn: signature has trailing data")
	}
	if sig.R == nil || sig.S == nil || sig.R.Sign() <= 0 || sig.S.Sign() <= 0 {
		return nil, nil, errors.New("webauthn: signature is incomplete")
	}
	return sig.R, sig.S, nil
}

// ChallengeIndex returns the byte offset of the base64url challenge inside
// clientDataJSON. The smart account's WebAuthn verifier takes the index
// instead of parsing JSON on chain.
func ChallengeIndex(clientDataJSON []byte) (int, error) {
	const prefix = `"challenge":"`
	i := bytes.Index(clientDataJSON, []byte(prefix))
	if i < 0 {
		return 0, errors.New("webauthn: client data has no challenge")
	}
	return i + len(prefix), nil
}

// TypeIndex returns the byte offset of the "webauthn.get" value inside
// clientDataJSON, for the same reason as ChallengeIndex.
func TypeIndex(clientDataJSON []byte) (int, error) {
	const prefix = `"type":"`
	i := bytes.Index(clientDataJSON, []byte(prefix))
	if i < 0 {
		return 0, errors.New("webauthn: client data has no type")
	}
	return i + len(prefix), nil
}
