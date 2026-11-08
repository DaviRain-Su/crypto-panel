package webauthn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
)

// SoftAuthenticator is an in-memory P-256 authenticator for tests and local
// development. It produces the same structures a platform passkey does, but
// its private key is in memory: never use it for a real account.
type SoftAuthenticator struct {
	key          *ecdsa.PrivateKey
	rpID         string
	origin       string
	credentialID []byte
	signCount    uint32
}

// NewSoftAuthenticator makes a software authenticator bound to one relying
// party and origin.
func NewSoftAuthenticator(rpID, origin string) (*SoftAuthenticator, error) {
	if rpID == "" || origin == "" {
		return nil, errors.New("webauthn: rp id and origin are required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	return &SoftAuthenticator{
		key:          key,
		rpID:         rpID,
		origin:       origin,
		credentialID: id,
	}, nil
}

// Credential returns the public half of the authenticator.
func (a *SoftAuthenticator) Credential() Credential {
	return Credential{ID: append([]byte(nil), a.credentialID...), X: a.key.X, Y: a.key.Y}
}

// Register performs a create() ceremony over challenge and returns the
// clientDataJSON and attestationObject a browser would hand back.
func (a *SoftAuthenticator) Register(challenge []byte) ([]byte, []byte, error) {
	clientData := fmt.Sprintf(
		`{"type":"webauthn.create","challenge":"%s","origin":"%s","crossOrigin":false}`,
		base64.RawURLEncoding.EncodeToString(challenge), a.origin,
	)
	authData := a.authData(flagUserPresent|flagUserVerified|flagAttested, 0)
	authData = append(authData, make([]byte, 16)...) // AAGUID
	idLen := len(a.credentialID)
	authData = append(authData, byte(idLen>>8), byte(idLen))
	authData = append(authData, a.credentialID...)
	cose := cborValue{kind: cborMap, m: map[any]cborValue{
		int64(1):  {kind: cborUint, u: 2},    // kty: EC2
		int64(3):  {kind: cborNegInt, i: -7}, // alg: ES256
		int64(-1): {kind: cborUint, u: 1},    // crv: P-256
		int64(-2): {kind: cborBytes, b: pad32(a.key.X)},
		int64(-3): {kind: cborBytes, b: pad32(a.key.Y)},
	}}
	authData = append(authData, encodeCBOR(cose)...)
	attestation := cborValue{kind: cborMap, m: map[any]cborValue{
		"fmt":      {kind: cborText, s: "none"},
		"attStmt":  {kind: cborMap, m: map[any]cborValue{}},
		"authData": {kind: cborBytes, b: authData},
	}}
	return []byte(clientData), encodeCBOR(attestation), nil
}

// Assert performs a get() ceremony over challenge and returns the assertion
// a browser would hand back.
func (a *SoftAuthenticator) Assert(challenge []byte) (Assertion, error) {
	a.signCount++
	authData := a.authData(flagUserPresent|flagUserVerified, a.signCount)
	clientData := fmt.Sprintf(
		`{"type":"webauthn.get","challenge":"%s","origin":"%s","crossOrigin":false}`,
		base64.RawURLEncoding.EncodeToString(challenge), a.origin,
	)
	clientHash := sha256.Sum256([]byte(clientData))
	msg := make([]byte, 0, len(authData)+len(clientHash))
	msg = append(msg, authData...)
	msg = append(msg, clientHash[:]...)
	digest := sha256.Sum256(msg)
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		return Assertion{}, err
	}
	return Assertion{
		AuthenticatorData: authData,
		ClientDataJSON:    []byte(clientData),
		Signature:         signature,
	}, nil
}

func (a *SoftAuthenticator) authData(flags byte, signCount uint32) []byte {
	rpHash := sha256.Sum256([]byte(a.rpID))
	out := make([]byte, 0, 37)
	out = append(out, rpHash[:]...)
	out = append(out, flags)
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], signCount)
	out = append(out, count[:]...)
	return out
}

func pad32(n *big.Int) []byte {
	return n.FillBytes(make([]byte, 32))
}
