package neartx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/davirain-su/crypto-panel/internal/base58"
)

func TestParseAccountID(t *testing.T) {
	valid := []string{
		"alice.testnet",
		"alice.near",
		"sub.alice.testnet",
		"a-b.testnet",
		"a_b.testnet",
		"acfb74d1570268f22df01eb6c3f38d57c1915d3be92afb30210eb4bc2d004220",
	}
	for _, s := range valid {
		if got, err := ParseAccountID(s); err != nil || got != s {
			t.Errorf("%q: %q, %v", s, got, err)
		}
	}
	invalid := []string{
		"", "a", "Alice.testnet", ".alice.testnet", "alice.testnet.",
		"alice..testnet", "-alice.testnet", "alice-.testnet",
		"_alice.testnet", "alice_.testnet", "alice testnet",
		strings.Repeat("a", 65),
	}
	for _, s := range invalid {
		if _, err := ParseAccountID(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

func TestParsePublicKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	text := "ed25519:" + base58.Encode(pub)
	key, err := ParsePublicKey(text)
	if err != nil {
		t.Fatal(err)
	}
	if string(key.Bytes[:]) != string(pub) {
		t.Fatal("the key changed")
	}
	if key.String() != text {
		t.Fatalf("String() = %q, want %q", key.String(), text)
	}
	if _, err := ParsePublicKey(base58.Encode(pub)); err != nil {
		t.Fatalf("bare base58: %v", err)
	}
	for _, s := range []string{
		"", "ed25519:not-base58", "secp256k1:" + base58.Encode(pub),
		"ed25519:" + base58.Encode(pub[:31]),
	} {
		if _, err := ParsePublicKey(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

func TestParseSecretKey(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := key.Public().(ed25519.PublicKey)

	forms := map[string]string{
		"near-cli": SecretKeyString(key),
		"seed hex": hex.EncodeToString(key.Seed()),
		"seed b58": base58.Encode(key.Seed()),
		"key b58":  base58.Encode(key),
	}
	for name, text := range forms {
		t.Run(name, func(t *testing.T) {
			got, err := ParseSecretKey(text)
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Public().(ed25519.PublicKey)) != string(pub) {
				t.Fatal("the key changed")
			}
			derived := PublicKeyOf(got)
			if string(derived.Bytes[:]) != string(pub) {
				t.Fatal("PublicKeyOf disagrees")
			}
		})
	}
	// A 64-byte key whose public half does not match its seed is refused.
	bad := append([]byte(nil), key...)
	bad[63] ^= 0xff
	if _, err := ParseSecretKey(base58.Encode(bad)); err == nil {
		t.Fatal("a mismatched key was accepted")
	}
	for _, s := range []string{"", "ed25519:not-base58", "secp256k1:" + base58.Encode(key), base58.Encode(key[:16])} {
		if _, err := ParseSecretKey(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

func TestParseAmount(t *testing.T) {
	for in, want := range map[string]string{"": "0", "0": "0", "1": "1", "1000000000000000000000000": "1000000000000000000000000"} {
		got, err := ParseAmount(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got.String() != want {
			t.Errorf("%q = %s, want %s", in, got, want)
		}
	}
	for _, s := range []string{"-1", "1.5", "abc"} {
		if _, err := ParseAmount(s); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}

func TestSummarize(t *testing.T) {
	pub := PublicKey{Bytes: [32]byte{1, 2, 3}}
	tx := &Transaction{
		SignerID: "alice.testnet", PublicKey: pub, ReceiverID: "contract.testnet",
		Actions: []Action{
			FunctionCall("set_greeting", []byte(`{"greeting":"hi"}`), 30_000_000_000_000, big.NewInt(5)),
			AddFunctionCallKey(pub, big.NewInt(10), "contract.testnet", []string{"set_greeting"}),
			DeleteKey(pub),
			{Kind: ActionDeployContract, Code: []byte{1, 2}},
			{Kind: ActionDeleteAccount, BeneficiaryID: "bob.testnet"},
		},
	}
	sum := Summarize(tx)
	if sum.SignerID != "alice.testnet" || sum.ReceiverID != "contract.testnet" {
		t.Fatalf("summary = %+v", sum)
	}
	if len(sum.Actions) != 5 {
		t.Fatalf("actions = %d", len(sum.Actions))
	}
	if sum.Actions[0].ArgsText != `{"greeting":"hi"}` {
		t.Errorf("args = %q", sum.Actions[0].ArgsText)
	}
	if sum.Actions[1].Allowance.String() != "10" || sum.Actions[1].KeyReceiver != "contract.testnet" {
		t.Errorf("key = %+v", sum.Actions[1])
	}
	if sum.Actions[3].CodeSize != 2 {
		t.Errorf("code size = %d", sum.Actions[3].CodeSize)
	}
	if sum.Actions[4].BeneficiaryID != "bob.testnet" {
		t.Errorf("beneficiary = %q", sum.Actions[4].BeneficiaryID)
	}
}

func TestDecodeArgs(t *testing.T) {
	if got := DecodeArgs(nil); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := DecodeArgs([]byte(`{ "a" : 1 }`)); got != `{"a":1}` {
		t.Errorf("json = %q", got)
	}
	if got := DecodeArgs([]byte{0xff, 0x00}); !strings.HasPrefix(got, "base64:") {
		t.Errorf("binary = %q", got)
	}
	long := strings.Repeat("a", 600)
	if got := DecodeArgs([]byte(long)); len(got) > 512+len("…") {
		t.Errorf("long args were not truncated: %d", len(got))
	}
}
