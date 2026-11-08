package webauthn

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func TestCeremonyURL(t *testing.T) {
	page := "https://crypto-panel.app/passkey/"
	challenge := bytes.Repeat([]byte{0x42}, 32)
	createURL, err := CeremonyURL(page, CeremonyRequest{
		Mode:            CeremonyCreate,
		Challenge:       challenge,
		State:           "state-1",
		UserID:          bytes.Repeat([]byte{0x01}, 16),
		UserName:        "davirain",
		UserDisplayName: "Davirain",
		RPName:          "Crypto Panel",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(createURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "crypto-panel.app" || u.Path != "/passkey/" {
		t.Fatalf("page = %s", createURL)
	}
	if u.RawQuery != "" {
		t.Fatalf("query = %q, want the request in the fragment", u.RawQuery)
	}
	q, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("mode") != "create" || q.Get("state") != "state-1" {
		t.Fatalf("fragment = %q", u.Fragment)
	}
	if q.Get("challenge") != b64(challenge) {
		t.Fatalf("challenge = %q", q.Get("challenge"))
	}
	if q.Get("userId") != b64(bytes.Repeat([]byte{0x01}, 16)) {
		t.Fatalf("user id = %q", q.Get("userId"))
	}
	if q.Get("userName") != "davirain" || q.Get("rpName") != "Crypto Panel" {
		t.Fatalf("fragment = %q", u.Fragment)
	}

	getURL, err := CeremonyURL(page, CeremonyRequest{
		Mode:         CeremonyGet,
		Challenge:    challenge,
		State:        "state-2",
		CredentialID: bytes.Repeat([]byte{0x02}, 16),
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err = url.Parse(getURL)
	if err != nil {
		t.Fatal(err)
	}
	q, err = url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("mode") != "get" || q.Get("credentialId") != b64(bytes.Repeat([]byte{0x02}, 16)) {
		t.Fatalf("fragment = %q", u.Fragment)
	}
}

func TestCeremonyURLLocalhost(t *testing.T) {
	_, err := CeremonyURL("http://localhost:8080/passkey/", CeremonyRequest{
		Mode:         CeremonyGet,
		Challenge:    bytes.Repeat([]byte{0x42}, 32),
		State:        "state",
		CredentialID: bytes.Repeat([]byte{0x02}, 16),
	})
	if err != nil {
		t.Fatalf("localhost was refused: %v", err)
	}
}

func TestCeremonyURLRejects(t *testing.T) {
	page := "https://crypto-panel.app/passkey/"
	challenge := bytes.Repeat([]byte{0x42}, 32)
	tests := []struct {
		name string
		page string
		req  CeremonyRequest
	}{
		{"http", "http://crypto-panel.app/passkey/", CeremonyRequest{Mode: CeremonyGet, Challenge: challenge, State: "s", CredentialID: []byte{1}}},
		{"no host", "/passkey/", CeremonyRequest{Mode: CeremonyGet, Challenge: challenge, State: "s", CredentialID: []byte{1}}},
		{"short challenge", page, CeremonyRequest{Mode: CeremonyGet, Challenge: []byte{1}, State: "s", CredentialID: []byte{1}}},
		{"no state", page, CeremonyRequest{Mode: CeremonyGet, Challenge: challenge, CredentialID: []byte{1}}},
		{"create without user", page, CeremonyRequest{Mode: CeremonyCreate, Challenge: challenge, State: "s", UserName: "name"}},
		{"create without name", page, CeremonyRequest{Mode: CeremonyCreate, Challenge: challenge, State: "s", UserID: []byte{1}}},
		{"get without credential", page, CeremonyRequest{Mode: CeremonyGet, Challenge: challenge, State: "s"}},
		{"unknown mode", page, CeremonyRequest{Mode: "other", Challenge: challenge, State: "s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CeremonyURL(tt.page, tt.req); err == nil {
				t.Fatal("CeremonyURL accepted an invalid request")
			}
		})
	}
}

func TestParseCallbackCreate(t *testing.T) {
	wire := ceremonyResultJSON{
		Kind:              "create",
		State:             "state-1",
		CredentialID:      b64([]byte{1, 2, 3}),
		ClientDataJSON:    b64([]byte(`{"type":"webauthn.create"}`)),
		AttestationObject: b64([]byte{0xa1, 0x02}),
		Transports:        []string{"internal"},
	}
	raw := "cryptopanel://passkey?" + url.Values{
		"result": {b64(mustJSON(t, wire))},
	}.Encode()
	result, err := ParseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CeremonyCreate || result.State != "state-1" {
		t.Fatalf("result = %+v", result)
	}
	if !bytes.Equal(result.CredentialID, []byte{1, 2, 3}) {
		t.Fatalf("credential id = %x", result.CredentialID)
	}
	if !bytes.Equal(result.AttestationObject, []byte{0xa1, 0x02}) {
		t.Fatalf("attestation = %x", result.AttestationObject)
	}
	if len(result.Transports) != 1 || result.Transports[0] != "internal" {
		t.Fatalf("transports = %v", result.Transports)
	}
}

func TestParseCallbackGet(t *testing.T) {
	wire := ceremonyResultJSON{
		Kind:              "get",
		State:             "state-2",
		CredentialID:      b64([]byte{4, 5, 6}),
		ClientDataJSON:    b64([]byte(`{"type":"webauthn.get"}`)),
		AuthenticatorData: b64(bytes.Repeat([]byte{0x11}, 37)),
		Signature:         b64([]byte{0x30, 0x01, 0x00}),
		UserHandle:        b64([]byte{7, 8}),
	}
	raw := "cryptopanel://passkey?" + url.Values{
		"result": {b64(mustJSON(t, wire))},
	}.Encode()
	result, err := ParseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != CeremonyGet || result.State != "state-2" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.AuthenticatorData) != 37 || !bytes.Equal(result.Signature, []byte{0x30, 0x01, 0x00}) {
		t.Fatalf("result = %+v", result)
	}
	if !bytes.Equal(result.UserHandle, []byte{7, 8}) {
		t.Fatalf("user handle = %x", result.UserHandle)
	}
}

func TestParseCallbackError(t *testing.T) {
	raw := "cryptopanel://passkey?" + url.Values{
		"error":   {"NotAllowedError"},
		"message": {"The operation was canceled."},
		"state":   {"state-1"},
	}.Encode()
	_, err := ParseCallback(raw)
	var ce *CeremonyError
	if !errors.As(err, &ce) {
		t.Fatalf("error = %v, want a CeremonyError", err)
	}
	if ce.Code != "NotAllowedError" || ce.State != "state-1" {
		t.Fatalf("ceremony error = %+v", ce)
	}
}

func TestParseCallbackRejects(t *testing.T) {
	valid := ceremonyResultJSON{
		Kind:              "create",
		State:             "state-1",
		CredentialID:      b64([]byte{1}),
		ClientDataJSON:    b64([]byte(`{}`)),
		AttestationObject: b64([]byte{1}),
	}
	validPayload := b64(mustJSON(t, valid))
	oversized := valid
	oversized.ClientDataJSON = b64(bytes.Repeat([]byte{'x'}, maxClientDataJSON+1))
	noAttestation := valid
	noAttestation.AttestationObject = ""
	noState := valid
	noState.State = ""
	unknownKind := valid
	unknownKind.Kind = "other"
	get := ceremonyResultJSON{
		Kind:              "get",
		State:             "state-1",
		CredentialID:      b64([]byte{1}),
		ClientDataJSON:    b64([]byte(`{}`)),
		AuthenticatorData: b64([]byte{1}),
	}
	tests := []struct {
		name string
		raw  string
	}{
		{"wrong scheme", "https://crypto-panel.app/passkey?result=" + validPayload},
		{"wrong host", "cryptopanel://other?result=" + validPayload},
		{"no result", "cryptopanel://passkey"},
		{"bad base64", "cryptopanel://passkey?result=%%%"},
		{"bad json", "cryptopanel://passkey?result=" + b64([]byte("not json"))},
		{"unknown kind", "cryptopanel://passkey?result=" + b64(mustJSON(t, unknownKind))},
		{"no state", "cryptopanel://passkey?result=" + b64(mustJSON(t, noState))},
		{"no attestation", "cryptopanel://passkey?result=" + b64(mustJSON(t, noAttestation))},
		{"oversized client data", "cryptopanel://passkey?result=" + b64(mustJSON(t, oversized))},
		{"get without signature", "cryptopanel://passkey?result=" + b64(mustJSON(t, get))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseCallback(tt.raw); err == nil {
				t.Fatal("ParseCallback accepted an invalid callback")
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
