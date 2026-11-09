package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/ethereum/go-ethereum/common"

	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/webauthn"
)

func recoveryData(t *testing.T, pk *PasskeyAccount) []byte {
	t.Helper()
	data, err := passkeyRecoveryJSON(pk)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPasskeyPublicRecoveryExportForgetRestoreSameIdentity(t *testing.T) {
	a := newTestApp(t)
	auth := newSoftAuthenticator(t)
	a.newCeremony = softCeremonyFactory(auth)
	evm := &passkeyEVMReader{address: common.HexToAddress("0x1111111111111111111111111111111111111111")}
	a.reader = evm
	a.createPasskey()
	original := *a.settings.Passkey
	data := recoveryData(t, &original)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 6 || fields["privateKey"] != nil || fields["evmAddress"] != nil || fields["createdAt"] != nil {
		t.Fatalf("unexpected recovery fields: %s", data)
	}
	path := filepath.Join(t.TempDir(), "public-recovery.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	a.removePasskey()
	if a.settings.Passkey != nil || a.settings.Has(original.NEARAccount) || a.settings.Has(original.EVMAddress) {
		t.Fatal("forget did not remove the app's mapping")
	}
	beforeCalls := evm.calls
	data, err := readPasskeyRecoveryFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a.newCeremony = hookedPasskeyFactory(auth, func(_ context.Context, req webauthn.CeremonyRequest) error {
		if a.settings.Passkey != nil || a.settings.Has(original.NEARAccount) {
			t.Fatal("public identity was saved before proof of control")
		}
		stored, err := LoadSettings(a.settingsPath)
		if err != nil || stored.Passkey != nil {
			t.Fatalf("identity persisted before proof: %v, %+v", err, stored.Passkey)
		}
		if req.Mode != webauthn.CeremonyGet || !bytes.Equal(req.CredentialID, auth.Credential().ID) || len(req.Challenge) != 32 {
			t.Fatalf("recovery ceremony = %+v", req)
		}
		return nil
	}, nil)
	a.restorePasskey(data)
	got := a.settings.Passkey
	if got == nil || got.ID != original.ID || got.PublicKey != original.PublicKey || got.NEARAccount != original.NEARAccount || got.EVMAddress != original.EVMAddress {
		t.Fatalf("original=%+v, restored=%+v, error=%q", original, got, a.passkeyErr)
	}
	if !a.settings.Has(got.NEARAccount) || !a.settings.Has(got.EVMAddress) || evm.calls <= beforeCalls {
		t.Fatal("restored accounts were not watched or EVM address was not re-derived")
	}
	stored, err := LoadSettings(a.settingsPath)
	if err != nil || stored.Passkey == nil || stored.Passkey.ID != original.ID {
		t.Fatalf("restored identity not persisted: %v, %+v", err, stored.Passkey)
	}
	if a.passkeyBusy || a.passkeyOp != nil || a.passkeyErr != "" || a.passkeyNotice == "" {
		t.Fatalf("busy=%v, error=%q, notice=%q", a.passkeyBusy, a.passkeyErr, a.passkeyNotice)
	}
	// 恢复的身份必须能够用同一凭据授权转账；
	// 此处所有 RPC 和签名均使用内存中的测试替身。
	a.settings.NetworkID = chains.NearMainnet.ID
	a.settings = a.settings.Validate()
	a.createNearAccount()
	rpc := &nearRPC{accessKey: `{"nonce":7,"permission":"FullAccess"}`}
	a.reader = &passkeyNearReader{rpc: rpc, passkeyAccount: original.NEARAccount}
	a.newCeremony = softCeremonyFactory(auth)
	a.passkeySendTo, a.passkeySendAmount = "bob.near", "1"
	a.sendPasskeyNear()
	if rpc.broadcast == nil || a.passkeySendErr != "" {
		t.Fatalf("restored credential could not send: %q", a.passkeySendErr)
	}
}

func TestPasskeyRecoveryRejectsAReplayedProof(t *testing.T) {
	a := newTestApp(t)
	auth := newSoftAuthenticator(t)
	a.reader = &passkeyEVMReader{address: common.HexToAddress("0x1111111111111111111111111111111111111111")}
	data := recoveryData(t, softPasskey(t, auth))
	var previous webauthn.CeremonyResult
	a.newCeremony = hookedPasskeyFactory(auth, nil, func(res *webauthn.CeremonyResult) { previous = *res })
	a.restorePasskey(data)
	if a.settings.Passkey == nil {
		t.Fatalf("first recovery: %s", a.passkeyErr)
	}
	a.removePasskey()
	a.newCeremony = hookedPasskeyFactory(auth, nil, func(res *webauthn.CeremonyResult) { *res = previous })
	a.restorePasskey(data)
	if a.settings.Passkey != nil || !strings.Contains(a.passkeyErr, "challenge does not match") {
		t.Fatalf("replayed proof accepted: %+v, error=%q", a.settings.Passkey, a.passkeyErr)
	}
}

func TestPasskeyRecoveryRequiresOriginalCredentialProof(t *testing.T) {
	for _, failure := range []string{"other key", "other credential id", "bad signature", "wrong challenge", "wrong origin", "wrong RP", "missing UV", "canceled", "revoked"} {
		t.Run(failure, func(t *testing.T) {
			a := newTestApp(t)
			auth := newSoftAuthenticator(t)
			pk := softPasskey(t, auth)
			before := func(context.Context, webauthn.CeremonyRequest) error { return nil }
			after := func(res *webauthn.CeremonyResult) {
				switch failure {
				case "other credential id":
					res.CredentialID = []byte("another id")
				case "bad signature":
					res.Signature[len(res.Signature)-1] ^= 1
				case "wrong challenge":
					res.ClientDataJSON = []byte(`{"type":"webauthn.get","challenge":"different","origin":"http://localhost:1234"}`)
				case "missing UV":
					res.AuthenticatorData[32] &^= 0x04
				}
			}
			signingAuth := auth
			switch failure {
			case "other key":
				signingAuth = newSoftAuthenticator(t)
				// 仅返回预期 ID 不能绕过签名校验。
				after = func(res *webauthn.CeremonyResult) { res.CredentialID = auth.Credential().ID }
			case "wrong origin":
				var err error
				signingAuth, err = webauthn.NewSoftAuthenticator(ceremonyRPID, "http://localhost:9999")
				if err != nil {
					t.Fatal(err)
				}
				pk = softPasskey(t, signingAuth)
			case "wrong RP":
				var err error
				signingAuth, err = webauthn.NewSoftAuthenticator("other.example", "http://localhost:1234")
				if err != nil {
					t.Fatal(err)
				}
				pk = softPasskey(t, signingAuth)
			case "canceled":
				before = func(context.Context, webauthn.CeremonyRequest) error {
					return &webauthn.CeremonyError{Code: "Canceled"}
				}
			case "revoked":
				before = func(ctx context.Context, _ webauthn.CeremonyRequest) error {
					a.lockAll()
					if !errors.Is(ctx.Err(), context.Canceled) {
						t.Fatalf("context = %v", ctx.Err())
					}
					return nil // Even an authenticator ignoring cancellation cannot restore.
				}
			}
			a.newCeremony = hookedPasskeyFactory(signingAuth, before, after)
			a.restorePasskey(recoveryData(t, pk))
			if a.settings.Passkey != nil || a.settings.Has(pk.NEARAccount) || a.passkeyBusy || a.passkeyOp != nil {
				t.Fatalf("invalid proof stored an identity: %+v", a.settings.Passkey)
			}
			if a.passkeyErr == "" {
				t.Fatal("no recovery error was shown")
			}
			if _, err := os.Stat(a.settingsPath); !os.IsNotExist(err) {
				t.Fatalf("unverified identity wrote settings: %v", err)
			}
		})
	}
}

func TestParsePasskeyRecoveryRejectsInvalidPublicData(t *testing.T) {
	pk := softPasskey(t, newSoftAuthenticator(t))
	valid := recoveryData(t, pk)
	mutate := func(key string, value any) []byte {
		var record map[string]any
		if err := json.Unmarshal(valid, &record); err != nil {
			t.Fatal(err)
		}
		record[key] = value
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cases := map[string][]byte{
		"empty":                   nil,
		"too large":               bytes.Repeat([]byte(" "), maxPasskeyRecoverySize+1),
		"not object":              []byte("[]"),
		"truncated":               valid[:len(valid)/2],
		"trailing object":         append(append([]byte(nil), valid...), []byte("{}")...),
		"trailing junk":           append(append([]byte(nil), valid...), []byte("junk")...),
		"unknown field":           mutate("privateKey", "secret"),
		"duplicate field":         []byte(strings.Replace(string(valid), `"version": 1`, `"version": 1, "version": 1`, 1)),
		"missing field":           []byte(strings.Replace(string(valid), `"version": 1,`, "", 1)),
		"wrong format":            mutate("format", "another-wallet"),
		"wrong version":           mutate("version", 2),
		"null version":            mutate("version", nil),
		"wrong RP":                mutate("rpId", "other.example"),
		"empty id":                mutate("credentialId", ""),
		"oversized id":            mutate("credentialId", base64.RawURLEncoding.EncodeToString(make([]byte, 1024))),
		"padded id":               mutate("credentialId", pk.ID+"="),
		"id with newline":         mutate("credentialId", pk.ID+"\n"),
		"invalid id":              mutate("credentialId", "!not-base64"),
		"invalid public key":      mutate("publicKey", "p256:invalid"),
		"oversized public key":    mutate("publicKey", "p256:"+strings.Repeat("1", 1000)),
		"noncanonical public key": mutate("publicKey", " "+pk.PublicKey),
		"wrong account":           mutate("nearAccount", "0s0000000000000000000000000000000000000000"),
		"missing account":         mutate("nearAccount", ""),
		"EVM mapping supplied":    mutate("evmAddress", "0x1111111111111111111111111111111111111111"),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePasskeyRecovery(data); err == nil {
				t.Fatalf("invalid data accepted: %s", data)
			}
			a := newTestApp(t)
			called := false
			a.newCeremony = func(func(string) error) (passkeyCeremony, error) {
				called = true
				return nil, errors.New("unexpected ceremony")
			}
			a.restorePasskey(data)
			if called || a.settings.Passkey != nil || a.passkeyBusy || a.passkeyErr == "" {
				t.Fatal("malformed data reached a ceremony or was stored")
			}
		})
	}
}

func TestReadPasskeyRecoveryFileRejectsOversizedInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "too-large.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte(" "), maxPasskeyRecoverySize+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPasskeyRecoveryFile(path); err == nil {
		t.Fatal("an oversized file was read")
	}
}

func TestPasskeyRecoveryControlsAndConditionsAreVisible(t *testing.T) {
	a := newTestApp(t)
	a.route = routeWallet
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Restore passkey") {
		t.Fatalf("missing restore control: %q", tt.Texts())
	}
	a.settings.Passkey = softPasskey(t, newSoftAuthenticator(t))
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Export recovery file") {
		t.Fatalf("missing export control: %q", tt.Texts())
	}
	text := strings.Join(tt.Texts(), " ")
	if !strings.Contains(text, "Neither works alone") || !strings.Contains(text, "cannot reconstruct") {
		t.Fatalf("recovery conditions are missing: %s", text)
	}
}
