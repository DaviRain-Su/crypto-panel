package smartaccount

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/davirain-su/crypto-panel/internal/webauthn"
)

func TestWebAuthnAuthEncode(t *testing.T) {
	var r, s [32]byte
	for i := range r {
		r[i] = 0x11
		s[i] = 0x22
	}
	auth := WebAuthnAuth{
		AuthenticatorData: []byte{0x01, 0x02},
		ClientDataJSON:    []byte(`{"type":"webauthn.get"}`),
		ChallengeIndex:    big.NewInt(10),
		TypeIndex:         big.NewInt(20),
		R:                 r,
		S:                 s,
	}
	got, err := auth.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := common.FromHex("0x00000000000000000000000000000000000000000000000000000000000000c00000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000000a0000000000000000000000000000000000000000000000000000000000000014111111111111111111111111111111111111111111111111111111111111111122222222222222222222222222222222222222222222222222222222222222220000000000000000000000000000000000000000000000000000000000000002010200000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000177b2274797065223a22776562617574686e2e676574227d000000000000000000")
	if !bytes.Equal(got, want) {
		t.Fatalf("WebAuthnAuth = %x, want %x", got, want)
	}
}

func TestSignatureFromAssertion(t *testing.T) {
	auth, err := webauthn.NewSoftAuthenticator("crypto-panel.app", "https://crypto-panel.app")
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := auth.Assert(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Signature(assertion)
	if err != nil {
		t.Fatal(err)
	}
	values, err := abi.Arguments{
		{Type: bytesType},
		{Type: stringType},
		{Type: uint256Type},
		{Type: uint256Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
	}.Unpack(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(values[0].([]byte), assertion.AuthenticatorData) {
		t.Fatal("authenticator data was not preserved")
	}
	if values[1].(string) != string(assertion.ClientDataJSON) {
		t.Fatal("client data was not preserved")
	}
	challengeIndex, err := webauthn.ChallengeIndex(assertion.ClientDataJSON)
	if err != nil {
		t.Fatal(err)
	}
	if values[2].(*big.Int).Int64() != int64(challengeIndex) {
		t.Fatalf("challenge index = %s, want %d", values[2], challengeIndex)
	}
	typeIndex, err := webauthn.TypeIndex(assertion.ClientDataJSON)
	if err != nil {
		t.Fatal(err)
	}
	if values[3].(*big.Int).Int64() != int64(typeIndex) {
		t.Fatalf("type index = %s, want %d", values[3], typeIndex)
	}
	r, s, err := webauthn.ParseSignature(assertion.Signature)
	if err != nil {
		t.Fatal(err)
	}
	var wantR, wantS [32]byte
	r.FillBytes(wantR[:])
	s.FillBytes(wantS[:])
	if values[4].([32]byte) != wantR || values[5].([32]byte) != wantS {
		t.Fatal("signature components were not preserved")
	}
}

func TestSignatureRejectsBadAssertion(t *testing.T) {
	if _, err := Signature(webauthn.Assertion{}); err == nil {
		t.Fatal("Signature accepted an empty assertion")
	}
	assertion := webauthn.Assertion{
		AuthenticatorData: make([]byte, 37),
		ClientDataJSON:    []byte(`{"type":"webauthn.get","challenge":"abc"}`),
		Signature:         []byte{0x30, 0x00},
	}
	if _, err := Signature(assertion); err == nil {
		t.Fatal("Signature accepted an empty DER sequence")
	}
}

func TestWebAuthnAuthEncodeRejectsMissingIndices(t *testing.T) {
	if _, err := (WebAuthnAuth{}).Encode(); err == nil {
		t.Fatal("Encode accepted a signature without indices")
	}
}

func TestSignRejectsWrongChallenge(t *testing.T) {
	auth, err := webauthn.NewSoftAuthenticator("crypto-panel.app", "https://crypto-panel.app")
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := auth.Assert(bytes.Repeat([]byte{0x99}, 32))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Sign(
		testUserOperation(),
		EntryPointV07,
		big.NewInt(8453),
		auth.Credential(),
		assertion,
		"https://crypto-panel.app",
		"crypto-panel.app",
	)
	if err == nil {
		t.Fatal("Sign accepted an assertion for another challenge")
	}
}
