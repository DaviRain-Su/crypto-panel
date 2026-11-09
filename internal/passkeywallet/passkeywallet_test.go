package passkeywallet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/base58"
	"github.com/davirain-su/lintel/internal/webauthn"
)

// testKey is the reference executor's pinned P-256 key: 0x02 followed by the
// bytes 1..32. It is a real point on the curve.
func testKey(t *testing.T) PublicKey {
	t.Helper()
	raw := make([]byte, 33)
	raw[0] = 0x02
	for i := 0; i < 32; i++ {
		raw[i+1] = byte(i + 1)
	}
	key, err := ParsePublicKey("p256:" + base58.Encode(raw))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestStateInitAndAccountIDMatchTheReferenceVector(t *testing.T) {
	key := testKey(t)
	wantState := "000127000000703235362d706173736b65792d77616c6c65742d636f6e74726163" +
		"742e7472657a752e6e6561720100000000000000" +
		"3e000000" +
		"0100000000" +
		"020102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20" +
		"100e0000" +
		"0000000000000000" +
		"000000000000000000000000"
	if got := hex.EncodeToString(StateInit(key)); got != wantState {
		t.Fatalf("StateInit:\n got %s\nwant %s", got, wantState)
	}
	if got := AccountID(key); got != "0s9dcb623ddaebc66fffc13ed50e61a3041ddae822" {
		t.Fatalf("AccountID = %q", got)
	}
}

func TestPublicKeyRoundTrips(t *testing.T) {
	key := testKey(t)
	parsed, err := ParsePublicKey(key.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.X.Cmp(key.X) != 0 || parsed.Y.Cmp(key.Y) != 0 {
		t.Fatal("the public key did not round-trip")
	}
	if !strings.HasPrefix(key.String(), "p256:") {
		t.Fatalf("key string = %q", key.String())
	}
	for _, bad := range []string{"", "p256:", "p256:111", "ed25519:abc", "p256:2"} {
		if _, err := ParsePublicKey(bad); err == nil {
			t.Errorf("ParsePublicKey(%q) accepted a malformed key", bad)
		}
	}
}

func TestNewPublicKeyRejectsOffCurve(t *testing.T) {
	if _, err := NewPublicKey(big.NewInt(1), big.NewInt(1)); err == nil {
		t.Fatal("an off-curve point was accepted")
	}
	if _, err := NewPublicKey(nil, big.NewInt(1)); err == nil {
		t.Fatal("a nil coordinate was accepted")
	}
}

func TestTransferMessageEncodingAndHash(t *testing.T) {
	key := testKey(t)
	msg := RequestMessage{
		ChainID:     "testnet",
		SignerID:    AccountID(key),
		Nonce:       7,
		CreatedAt:   time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		TimeoutSecs: 3600,
		Request: Request{External: []Promise{{
			ReceiverID: "alice.testnet",
			Actions:    []Action{Transfer(new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil))},
		}}},
	}
	wantBorsh := "07000000746573746e65742a000000307339646362363233646461656263363666666663313365643530653631613330343164646165383232070000000080e94ead5ee618100e000000000000010000000d000000616c6963652e746573746e6574000100000003000000a1edccce1bc2d3000000000000"
	raw, err := msg.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(raw); got != wantBorsh {
		t.Fatalf("Borsh:\n got %s\nwant %s", got, wantBorsh)
	}
	hash, err := msg.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(hash); got != "659f54eda9090f2d7b542fb0b680e8ae0db740e9d56d0ec6e5bc31b4e34f6ae5" {
		t.Fatalf("hash = %s", got)
	}
}

func TestMessageRefusesInternalOperations(t *testing.T) {
	msg := RequestMessage{
		ChainID:     "testnet",
		SignerID:    "0s00",
		TimeoutSecs: 3600,
		Request:     Request{Internal: []WalletOp{{Op: "set_signature_mode"}}},
	}
	if _, err := msg.Encode(); err == nil {
		t.Fatal("an internal wallet operation was encoded")
	}
}

func TestProofIsLowSAndBase58(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authData := make([]byte, 37)
	authData[32] = 0x05 // user present and verified
	clientData := []byte(`{"type":"webauthn.get","challenge":"AAAA","origin":"http://localhost:1234"}`)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte(nil), authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	proofJSON, err := NewProof(webauthn.Assertion{
		AuthenticatorData: authData,
		ClientDataJSON:    clientData,
		Signature:         sig,
	})
	if err != nil {
		t.Fatal(err)
	}
	var proof Proof
	if err := json.Unmarshal([]byte(proofJSON), &proof); err != nil {
		t.Fatal(err)
	}
	if proof.AuthenticatorData != base64.RawURLEncoding.EncodeToString(authData) {
		t.Fatal("authenticator data is not base64url without padding")
	}
	if proof.ClientDataJSON != string(clientData) {
		t.Fatal("client data is not the original JSON text")
	}
	rest, ok := strings.CutPrefix(proof.Signature, "p256:")
	if !ok {
		t.Fatalf("signature = %q", proof.Signature)
	}
	raw, err := base58.Decode(rest)
	if err != nil || len(raw) != 64 {
		t.Fatalf("signature bytes = %d, %v", len(raw), err)
	}
	s := new(big.Int).SetBytes(raw[32:])
	half := new(big.Int).Rsh(elliptic.P256().Params().N, 1)
	if s.Cmp(half) > 0 {
		t.Fatal("the signature was not normalized to low-S")
	}
}

func TestExecuteArgs(t *testing.T) {
	msg := NewTransferMessage("testnet", "0s00", "alice.testnet", big.NewInt(1), time.Now())
	args, err := ExecuteArgs(msg, `{"signature":"p256:x"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Msg   RequestMessage `json:"msg"`
		Proof string         `json:"proof"`
	}
	if err := json.Unmarshal(args, &got); err != nil {
		t.Fatal(err)
	}
	if got.Msg.SignerID != "0s00" || got.Proof == "" {
		t.Fatalf("args = %s", args)
	}
}
