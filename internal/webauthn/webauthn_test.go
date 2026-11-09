package webauthn

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

const (
	testRPID   = "lintel.example"
	testOrigin = "https://lintel.example"
)

func newTestAuthenticator(t *testing.T) *SoftAuthenticator {
	t.Helper()
	auth, err := NewSoftAuthenticator(testRPID, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func TestRegistrationAndAssertion(t *testing.T) {
	auth := newTestAuthenticator(t)
	challenge := bytes.Repeat([]byte{0x42}, 32)
	clientData, attestation, err := auth.Register(challenge)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := ParseRegistration(clientData, attestation, challenge, testOrigin, testRPID)
	if err != nil {
		t.Fatal(err)
	}
	cred := auth.Credential()
	if !bytes.Equal(reg.CredentialID, cred.ID) {
		t.Fatalf("credential id = %x, want %x", reg.CredentialID, cred.ID)
	}
	if reg.X.Cmp(cred.X) != 0 || reg.Y.Cmp(cred.Y) != 0 {
		t.Fatal("registration returned a different public key")
	}
	if len(reg.AAGUID) != 16 {
		t.Fatalf("aaguid length = %d, want 16", len(reg.AAGUID))
	}

	assertion, err := auth.Assert(challenge)
	if err != nil {
		t.Fatal(err)
	}
	credential := Credential{ID: reg.CredentialID, X: reg.X, Y: reg.Y}
	if err := assertion.Verify(credential, challenge, testOrigin, testRPID); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestRegistrationRejects(t *testing.T) {
	auth := newTestAuthenticator(t)
	challenge := bytes.Repeat([]byte{0x42}, 32)
	clientData, attestation, err := auth.Register(challenge)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		clientData []byte
		challenge  []byte
		origin     string
		rpID       string
	}{
		{"wrong challenge", clientData, bytes.Repeat([]byte{0x43}, 32), testOrigin, testRPID},
		{"wrong origin", clientData, challenge, "https://evil.example", testRPID},
		{"wrong rp id", clientData, challenge, testOrigin, "evil.example"},
		{"not client data", []byte("nope"), challenge, testOrigin, testRPID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseRegistration(tt.clientData, attestation, tt.challenge, tt.origin, tt.rpID); err == nil {
				t.Fatal("ParseRegistration accepted invalid client data")
			}
		})
	}
}

func TestAssertionRejects(t *testing.T) {
	auth := newTestAuthenticator(t)
	challenge := bytes.Repeat([]byte{0x42}, 32)
	assertion, err := auth.Assert(challenge)
	if err != nil {
		t.Fatal(err)
	}
	cred := auth.Credential()

	otherAuth := newTestAuthenticator(t)
	otherCred := otherAuth.Credential()

	tampered := assertion
	tampered.Signature = append([]byte(nil), assertion.Signature...)
	tampered.Signature[len(tampered.Signature)-1] ^= 0xff

	noUV := assertion
	noUV.AuthenticatorData = append([]byte(nil), assertion.AuthenticatorData...)
	noUV.AuthenticatorData[32] &^= flagUserVerified

	tests := []struct {
		name      string
		assertion Assertion
		cred      Credential
		challenge []byte
		origin    string
		rpID      string
	}{
		{"wrong challenge", assertion, cred, bytes.Repeat([]byte{0x43}, 32), testOrigin, testRPID},
		{"wrong origin", assertion, cred, challenge, "https://evil.example", testRPID},
		{"wrong rp id", assertion, cred, challenge, testOrigin, "evil.example"},
		{"tampered signature", tampered, cred, challenge, testOrigin, testRPID},
		{"wrong credential", assertion, otherCred, challenge, testOrigin, testRPID},
		{"no user verification", noUV, cred, challenge, testOrigin, testRPID},
		{"empty rp id", assertion, cred, challenge, testOrigin, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.assertion.Verify(tt.cred, tt.challenge, tt.origin, tt.rpID); err == nil {
				t.Fatal("verify accepted an invalid assertion")
			}
		})
	}
}

func TestChallengeAndTypeIndex(t *testing.T) {
	auth := newTestAuthenticator(t)
	challenge := bytes.Repeat([]byte{0x7f}, 32)
	assertion, err := auth.Assert(challenge)
	if err != nil {
		t.Fatal(err)
	}
	wantChallenge := base64.RawURLEncoding.EncodeToString(challenge)
	ci, err := ChallengeIndex(assertion.ClientDataJSON)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(assertion.ClientDataJSON[ci : ci+len(wantChallenge)]); got != wantChallenge {
		t.Fatalf("challenge at index = %q, want %q", got, wantChallenge)
	}
	ti, err := TypeIndex(assertion.ClientDataJSON)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(assertion.ClientDataJSON[ti : ti+len("webauthn.get")]); got != "webauthn.get" {
		t.Fatalf("type at index = %q, want webauthn.get", got)
	}
}

func TestParseSignature(t *testing.T) {
	auth := newTestAuthenticator(t)
	assertion, err := auth.Assert(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	r, s, err := ParseSignature(assertion.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		t.Fatal("signature has a non-positive component")
	}
	if _, _, err := ParseSignature([]byte{0x30, 0x00}); err == nil {
		t.Fatal("ParseSignature accepted an empty sequence")
	}
	if _, _, err := ParseSignature(append(assertion.Signature, 0)); err == nil {
		t.Fatal("ParseSignature accepted trailing data")
	}
}

func TestAttestationRejects(t *testing.T) {
	auth := newTestAuthenticator(t)
	noAttested := cborValue{kind: cborMap, m: map[any]cborValue{
		"authData": {kind: cborBytes, b: auth.authData(flagUserPresent|flagUserVerified, 0)},
	}}
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"truncated", []byte{0xa1}},
		{"not a map", encodeCBOR(cborValue{kind: cborText, s: "nope"})},
		{"no auth data", encodeCBOR(cborValue{kind: cborMap, m: map[any]cborValue{}})},
		{"no attested credential", encodeCBOR(noAttested)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseAttestation(tt.data); err == nil {
				t.Fatal("ParseAttestation accepted invalid data")
			}
		})
	}
}

func TestClientDataRejects(t *testing.T) {
	tests := []string{
		`not json`,
		`{"type":"webauthn.get"}`,
		`{"challenge":"abc"}`,
	}
	for _, raw := range tests {
		if _, err := ParseClientData([]byte(raw)); err == nil {
			t.Fatalf("ParseClientData accepted %q", raw)
		}
	}
}

func TestCBORRejects(t *testing.T) {
	tests := [][]byte{
		nil,
		{0x9f},                         // indefinite array
		{0x5a, 0xff, 0xff, 0xff, 0xff}, // byte string longer than the input
		{0x81},                         // array with a missing item
		{0xa1, 0x01},                   // map with a missing value
		{0xa1, 0xf7, 0x01},             // map with a simple-value key
	}
	for _, data := range tests {
		if _, _, err := decodeCBOR(data); err == nil {
			t.Fatalf("decodeCBOR accepted %x", data)
		}
	}
}

func TestVerifySignatureRejectsNonP256(t *testing.T) {
	auth := newTestAuthenticator(t)
	assertion, err := auth.Assert(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(nil, assertion.AuthenticatorData, assertion.ClientDataJSON, assertion.Signature); err == nil {
		t.Fatal("VerifySignature accepted a nil key")
	}
}

func TestClientDataOriginIsChecked(t *testing.T) {
	auth := newTestAuthenticator(t)
	assertion, err := auth.Assert(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cd, err := ParseClientData(assertion.ClientDataJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cd.Origin, "https://") {
		t.Fatalf("origin = %q", cd.Origin)
	}
	if cd.CrossOrigin {
		t.Fatal("crossOrigin is true")
	}
}

func TestSoftAuthenticatorSignCountAdvances(t *testing.T) {
	auth := newTestAuthenticator(t)
	if _, err := auth.Assert(bytes.Repeat([]byte{4}, 32)); err != nil {
		t.Fatal(err)
	}
	first := auth.signCount
	if _, err := auth.Assert(bytes.Repeat([]byte{5}, 32)); err != nil {
		t.Fatal(err)
	}
	if auth.signCount != first+1 {
		t.Fatalf("sign count = %d, want %d", auth.signCount, first+1)
	}
}

func TestRPIDHash(t *testing.T) {
	auth := newTestAuthenticator(t)
	assertion, err := auth.Assert(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(testRPID))
	if !bytes.Equal(assertion.AuthenticatorData[:32], want[:]) {
		t.Fatal("authenticator data does not carry the rpId hash")
	}
}
