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

// TestRealTestnetTransaction checks the encoder against a transaction the
// testnet actually accepted: its fields, its block hash and its signature
// reproduce the on-chain hash and verify.
func TestRealTestnetTransaction(t *testing.T) {
	pub, err := ParsePublicKey("ed25519:CeFWmbswZmVnDVGo2QMkNRUggmZQ2coZJ1X9R5t7UXZq")
	if err != nil {
		t.Fatal(err)
	}
	blockHash, err := base58.Decode("F3bucPK7JgJPCPf8yuBB28DaGqQ6jHfUuxJ68Tnc9PbG")
	if err != nil {
		t.Fatal(err)
	}
	var bh [32]byte
	copy(bh[:], blockHash)
	tx := &Transaction{
		SignerID:   "acfb74d1570268f22df01eb6c3f38d57c1915d3be92afb30210eb4bc2d004220",
		PublicKey:  pub,
		Nonce:      276791609000002,
		ReceiverID: "reg-d14.sr-tom-n1a.testnet",
		BlockHash:  bh,
		Actions: []Action{
			FunctionCall("confirm_details", []byte("{}"), 30_000_000_000_000, new(big.Int)),
		},
	}
	hash, err := tx.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := base58.Encode(hash[:]), "CbSQfoTGYCie6fwFyvZe2upMFbHvvCnfKZGeKKru3MMu"; got != want {
		t.Fatalf("transaction hash = %s, want %s", got, want)
	}
	sig, err := base58.Decode("5PZS9Htvy3vf1VSBRKbbhAwwPWfPLR5nGmTdyVe4CidKBHczwfZ1k6JPnGx2BbTquFBr5MUAQnNvKXnPmPPC3qTZ")
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub.Bytes[:]), hash[:], sig) {
		t.Fatal("the on-chain signature does not verify")
	}
}

func TestEncodeVectors(t *testing.T) {
	pub := PublicKey{Bytes: [32]byte{}}
	for i := range pub.Bytes {
		pub.Bytes[i] = 0x22
	}
	var blockHash [32]byte
	for i := range blockHash {
		blockHash[i] = 0x11
	}
	key33 := PublicKey{Bytes: [32]byte{}}
	for i := range key33.Bytes {
		key33.Bytes[i] = 0x33
	}
	key44 := PublicKey{Bytes: [32]byte{}}
	for i := range key44.Bytes {
		key44.Bytes[i] = 0x44
	}
	oneNEAR := new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)
	quarterNEAR := new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil)
	quarterNEAR.Quo(quarterNEAR, big.NewInt(4))

	cases := []struct {
		name string
		tx   *Transaction
		hex  string
		hash string
	}{
		{
			name: "transfer",
			tx: &Transaction{
				SignerID: "alice.testnet", PublicKey: pub, Nonce: 1,
				ReceiverID: "bob.testnet", BlockHash: blockHash,
				Actions: []Action{Transfer(oneNEAR)},
			},
			hex:  "0d000000616c6963652e746573746e657400222222222222222222222222222222222222222222222222222222222222222201000000000000000b000000626f622e746573746e657411111111111111111111111111111111111111111111111111111111111111110100000003000000a1edccce1bc2d3000000000000",
			hash: "3f516b1632c638032e03d08317bb3e9bc2a992a7d40cc305ce125a4e1e1438f4",
		},
		{
			name: "function call",
			tx: &Transaction{
				SignerID: "alice.testnet", PublicKey: pub, Nonce: 2,
				ReceiverID: "contract.testnet", BlockHash: blockHash,
				Actions: []Action{FunctionCall("set_greeting", []byte(`{"greeting":"hi"}`), 30_000_000_000_000, new(big.Int))},
			},
			hex:  "0d000000616c6963652e746573746e6574002222222222222222222222222222222222222222222222222222222222222222020000000000000010000000636f6e74726163742e746573746e6574111111111111111111111111111111111111111111111111111111111111111101000000020c0000007365745f6772656574696e67110000007b226772656574696e67223a226869227d00e057eb481b000000000000000000000000000000000000",
			hash: "094efaa87b2afef3afbf2ad71947cb738a343b814c7700937f4592a41be2d1d6",
		},
		{
			name: "add function-call key",
			tx: &Transaction{
				SignerID: "alice.testnet", PublicKey: pub, Nonce: 3,
				ReceiverID: "alice.testnet", BlockHash: blockHash,
				Actions: []Action{AddFunctionCallKey(key33, quarterNEAR, "contract.testnet", []string{"set_greeting", "donate"})},
			},
			hex:  "0d000000616c6963652e746573746e657400222222222222222222222222222222222222222222222222222222222222222203000000000000000d000000616c6963652e746573746e65741111111111111111111111111111111111111111111111111111111111111111010000000500333333333333333333333333333333333333333333333333333333333333333300000000000000000001000040683bb3f386f03400000000000010000000636f6e74726163742e746573746e6574020000000c0000007365745f6772656574696e6706000000646f6e617465",
			hash: "6049563fa8d2b41d0ce3a4a4aed89a820051880cd378492b0c36c028f0c8a124",
		},
		{
			name: "add full-access key",
			tx: &Transaction{
				SignerID: "alice.testnet", PublicKey: pub, Nonce: 4,
				ReceiverID: "alice.testnet", BlockHash: blockHash,
				Actions: []Action{AddFullAccessKey(key44)},
			},
			hex:  "0d000000616c6963652e746573746e657400222222222222222222222222222222222222222222222222222222222222222204000000000000000d000000616c6963652e746573746e657411111111111111111111111111111111111111111111111111111111111111110100000005004444444444444444444444444444444444444444444444444444444444444444000000000000000001",
			hash: "7d3f2b9c1fcaebd2e8291569e59e2f03775818ccb72013c8e20e518874eb15f0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := tc.tx.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(raw); got != tc.hex {
				t.Fatalf("encoding:\n got %s\nwant %s", got, tc.hex)
			}
			hash, err := tc.tx.Hash()
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(hash[:]); got != tc.hash {
				t.Fatalf("hash = %s, want %s", got, tc.hash)
			}
		})
	}
}

func TestSign(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tx := &Transaction{
		SignerID:   "alice.testnet",
		PublicKey:  PublicKey{Bytes: [32]byte(pub)},
		Nonce:      1,
		ReceiverID: "bob.testnet",
		Actions:    []Action{Transfer(big.NewInt(1))},
	}
	signed, hash, err := Sign(key, tx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tx.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(signed) != len(raw)+1+ed25519.SignatureSize {
		t.Fatalf("signed length %d", len(signed))
	}
	if signed[len(raw)] != KeyTypeED25519 {
		t.Fatalf("key type = %d", signed[len(raw)])
	}
	if !ed25519.Verify(pub, hash[:], signed[len(raw)+1:]) {
		t.Fatal("the signature does not verify")
	}
}

func TestEncodeRefuses(t *testing.T) {
	valid := &Transaction{SignerID: "a.testnet", ReceiverID: "b.testnet", Actions: []Action{Transfer(big.NewInt(1))}}
	cases := map[string]*Transaction{
		"no signer":   {ReceiverID: "b.testnet", Actions: []Action{Transfer(big.NewInt(1))}},
		"no receiver": {SignerID: "a.testnet", Actions: []Action{Transfer(big.NewInt(1))}},
		"no amount":   {SignerID: "a.testnet", ReceiverID: "b.testnet", Actions: []Action{{Kind: ActionTransfer}}},
		"negative":    {SignerID: "a.testnet", ReceiverID: "b.testnet", Actions: []Action{Transfer(big.NewInt(-1))}},
		"too large":   {SignerID: "a.testnet", ReceiverID: "b.testnet", Actions: []Action{Transfer(new(big.Int).Lsh(big.NewInt(1), 128))}},
		"unknown":     {SignerID: "a.testnet", ReceiverID: "b.testnet", Actions: []Action{{Kind: 99}}},
	}
	for name, tx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tx.Encode(); err == nil {
				t.Fatal("encoded an invalid transaction")
			}
		})
	}
	if _, err := valid.Encode(); err != nil {
		t.Fatalf("a valid transaction was refused: %v", err)
	}
}

func TestActionKindString(t *testing.T) {
	for kind, want := range map[ActionKind]string{
		ActionCreateAccount: "create account",
		ActionFunctionCall:  "function call",
		ActionTransfer:      "transfer",
		ActionAddKey:        "add key",
	} {
		if got := kind.String(); got != want {
			t.Errorf("%d: %q, want %q", kind, got, want)
		}
	}
	if got := ActionKind(99).String(); !strings.Contains(got, "99") {
		t.Errorf("unknown action: %q", got)
	}
}
