package webauthn

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// CeremonyMode is which WebAuthn ceremony the page runs.
type CeremonyMode string

const (
	CeremonyCreate CeremonyMode = "create"
	CeremonyGet    CeremonyMode = "get"
)

// The callback the ceremony page navigates to. It is a fixed custom scheme,
// not a URL the page takes from its query, so a malicious link cannot turn
// the page into an open redirect. The scheme stays cryptopanel so an
// already-registered callback still resolves.
const (
	CallbackScheme = "cryptopanel"
	CallbackHost   = "passkey"
)

// CeremonyRequest is what the app puts in the page's URL fragment. The page
// uses its own hostname as the relying party id; it never takes one from the
// URL.
type CeremonyRequest struct {
	Mode            CeremonyMode
	Challenge       []byte
	State           string
	UserID          []byte
	UserName        string
	UserDisplayName string
	RPName          string
	CredentialID    []byte
}

// CeremonyURL builds the page URL with the request in the fragment, so the
// challenge and user id never reach the web server or its logs.
func CeremonyURL(page string, req CeremonyRequest) (string, error) {
	u, err := url.Parse(page)
	if err != nil {
		return "", fmt.Errorf("webauthn: ceremony page: %w", err)
	}
	if u.Host == "" {
		return "", errors.New("webauthn: ceremony page has no host")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "localhost") {
		return "", errors.New("webauthn: ceremony page must be https, or http on localhost")
	}
	if len(req.Challenge) < 16 || len(req.Challenge) > 64 {
		return "", errors.New("webauthn: ceremony challenge must be 16 to 64 bytes")
	}
	if req.State == "" || len(req.State) > 128 {
		return "", errors.New("webauthn: ceremony state is missing or too long")
	}
	q := url.Values{}
	q.Set("mode", string(req.Mode))
	q.Set("challenge", base64.RawURLEncoding.EncodeToString(req.Challenge))
	q.Set("state", req.State)
	switch req.Mode {
	case CeremonyCreate:
		if len(req.UserID) == 0 || len(req.UserID) > 64 {
			return "", errors.New("webauthn: ceremony user id must be 1 to 64 bytes")
		}
		if req.UserName == "" {
			return "", errors.New("webauthn: ceremony user name is missing")
		}
		q.Set("userId", base64.RawURLEncoding.EncodeToString(req.UserID))
		q.Set("userName", req.UserName)
		if req.UserDisplayName != "" {
			q.Set("userDisplayName", req.UserDisplayName)
		}
		if req.RPName != "" {
			q.Set("rpName", req.RPName)
		}
	case CeremonyGet:
		if len(req.CredentialID) == 0 || len(req.CredentialID) > 1023 {
			return "", errors.New("webauthn: ceremony credential id is missing or too long")
		}
		q.Set("credentialId", base64.RawURLEncoding.EncodeToString(req.CredentialID))
	default:
		return "", fmt.Errorf("webauthn: unknown ceremony mode %q", req.Mode)
	}
	u.Fragment = q.Encode()
	return u.String(), nil
}

// CeremonyResult is the page's answer. Only the fields for its kind are set.
type CeremonyResult struct {
	Kind              CeremonyMode
	State             string
	CredentialID      []byte
	ClientDataJSON    []byte
	AttestationObject []byte
	AuthenticatorData []byte
	Signature         []byte
	UserHandle        []byte
	Transports        []string
}

// CeremonyError is the page's error answer, or a callback that is not a
// result. It carries the state so the app can match it to its pending
// ceremony.
type CeremonyError struct {
	Code    string
	Message string
	State   string
}

func (e *CeremonyError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("webauthn: ceremony %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("webauthn: ceremony %s", e.Code)
}

// Size limits for a callback. A ceremony result is a few kilobytes; anything
// much larger is not something the page produced.
const (
	maxCallbackURL       = 1 << 20
	maxClientDataJSON    = 8 << 10
	maxAttestationObject = 64 << 10
	maxSignature         = 1 << 10
	maxUserHandle        = 64
	maxResultJSON        = 128 << 10
)

// ParseCallback reads the cryptopanel://passkey callback the page navigates
// to. It validates the shape; the caller still verifies the challenge,
// origin and relying party against the assertion.
func ParseCallback(raw string) (CeremonyResult, error) {
	if len(raw) > maxCallbackURL {
		return CeremonyResult{}, errors.New("webauthn: callback is too large")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return CeremonyResult{}, fmt.Errorf("webauthn: callback: %w", err)
	}
	if u.Scheme != CallbackScheme || u.Host != CallbackHost {
		return CeremonyResult{}, errors.New("webauthn: not a lintel passkey callback")
	}
	q := u.Query()
	if code := q.Get("error"); code != "" {
		return CeremonyResult{}, &CeremonyError{
			Code:    code,
			Message: q.Get("message"),
			State:   q.Get("state"),
		}
	}
	payload := q.Get("result")
	if payload == "" {
		return CeremonyResult{}, errors.New("webauthn: callback has no result")
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return CeremonyResult{}, fmt.Errorf("webauthn: callback result: %w", err)
	}
	var wire ceremonyResultJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return CeremonyResult{}, fmt.Errorf("webauthn: callback result: %w", err)
	}
	return parseResult(wire)
}

// ParseResultJSON reads the ceremony page's POST body, which is the same
// shape as the callback's result plus the page's error form.
func ParseResultJSON(data []byte) (CeremonyResult, error) {
	if len(data) > maxResultJSON {
		return CeremonyResult{}, errors.New("webauthn: result is too large")
	}
	var wire ceremonyResultJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return CeremonyResult{}, fmt.Errorf("webauthn: result: %w", err)
	}
	if wire.Error != "" {
		return CeremonyResult{}, &CeremonyError{
			Code:    wire.Error,
			Message: wire.Message,
			State:   wire.State,
		}
	}
	return parseResult(wire)
}

// parseResult validates one ceremony result, from either the callback URL or
// the page's POST body.
func parseResult(wire ceremonyResultJSON) (CeremonyResult, error) {
	var err error
	result := CeremonyResult{
		Kind:       CeremonyMode(wire.Kind),
		State:      wire.State,
		Transports: wire.Transports,
	}
	if result.State == "" || len(result.State) > 128 {
		return CeremonyResult{}, errors.New("webauthn: callback state is missing or too long")
	}
	if result.CredentialID, err = decodeCallbackField("credentialId", wire.CredentialID, 1023); err != nil {
		return CeremonyResult{}, err
	}
	if result.ClientDataJSON, err = decodeCallbackField("clientDataJSON", wire.ClientDataJSON, maxClientDataJSON); err != nil {
		return CeremonyResult{}, err
	}
	if len(result.ClientDataJSON) == 0 {
		return CeremonyResult{}, errors.New("webauthn: callback has no client data")
	}
	switch result.Kind {
	case CeremonyCreate:
		if result.AttestationObject, err = decodeCallbackField("attestationObject", wire.AttestationObject, maxAttestationObject); err != nil {
			return CeremonyResult{}, err
		}
		if len(result.CredentialID) == 0 || len(result.AttestationObject) == 0 {
			return CeremonyResult{}, errors.New("webauthn: create callback is incomplete")
		}
	case CeremonyGet:
		if result.AuthenticatorData, err = decodeCallbackField("authenticatorData", wire.AuthenticatorData, maxClientDataJSON); err != nil {
			return CeremonyResult{}, err
		}
		if result.Signature, err = decodeCallbackField("signature", wire.Signature, maxSignature); err != nil {
			return CeremonyResult{}, err
		}
		if result.UserHandle, err = decodeCallbackField("userHandle", wire.UserHandle, maxUserHandle); err != nil {
			return CeremonyResult{}, err
		}
		if len(result.CredentialID) == 0 || len(result.AuthenticatorData) == 0 || len(result.Signature) == 0 {
			return CeremonyResult{}, errors.New("webauthn: get callback is incomplete")
		}
	default:
		return CeremonyResult{}, fmt.Errorf("webauthn: unknown callback kind %q", wire.Kind)
	}
	if len(result.Transports) > 8 {
		return CeremonyResult{}, errors.New("webauthn: callback has too many transports")
	}
	return result, nil
}

type ceremonyResultJSON struct {
	Kind              string   `json:"kind"`
	State             string   `json:"state"`
	Error             string   `json:"error,omitempty"`
	Message           string   `json:"message,omitempty"`
	CredentialID      string   `json:"credentialId"`
	ClientDataJSON    string   `json:"clientDataJSON"`
	AttestationObject string   `json:"attestationObject,omitempty"`
	AuthenticatorData string   `json:"authenticatorData,omitempty"`
	Signature         string   `json:"signature,omitempty"`
	UserHandle        string   `json:"userHandle,omitempty"`
	Transports        []string `json:"transports,omitempty"`
}

func decodeCallbackField(name, value string, max int) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("webauthn: callback %s: %w", name, err)
	}
	if len(b) > max {
		return nil, fmt.Errorf("webauthn: callback %s is too large", name)
	}
	return b, nil
}
