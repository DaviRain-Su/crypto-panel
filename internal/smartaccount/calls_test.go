package smartaccount

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestExecuteCallData(t *testing.T) {
	got, err := ExecuteCallData(
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		big.NewInt(12345),
		[]byte{0xaa, 0xbb, 0xcc},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := common.FromHex("0xb61d27f60000000000000000000000002222222222222222222222222222222222222222000000000000000000000000000000000000000000000000000000000000303900000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000000000003aabbcc0000000000000000000000000000000000000000000000000000000000")
	if !bytes.Equal(got, want) {
		t.Fatalf("execute call data = %x, want %x", got, want)
	}
}

func TestFactoryCallData(t *testing.T) {
	owners := [][]byte{
		bytes.Repeat([]byte{0x11}, 64),
		bytes.Repeat([]byte{0x22}, 20),
	}
	nonce := big.NewInt(0)
	create, err := CreateAccountCallData(owners, nonce)
	if err != nil {
		t.Fatal(err)
	}
	wantCreate := common.FromHex("0x3ffba36f000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000a000000000000000000000000000000000000000000000000000000000000000401111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111100000000000000000000000000000000000000000000000000000000000000142222222222222222222222222222222222222222000000000000000000000000")
	if !bytes.Equal(create, wantCreate) {
		t.Fatalf("createAccount call data = %x, want %x", create, wantCreate)
	}

	get, err := GetAddressCallData(owners, nonce)
	if err != nil {
		t.Fatal(err)
	}
	wantGet := append(selector("getAddress(bytes[],uint256)"), wantCreate[4:]...)
	if !bytes.Equal(get, wantGet) {
		t.Fatalf("getAddress call data = %x, want %x (len %d vs %d)", get, wantGet, len(get), len(wantGet))
	}
}

func TestPasskeyOwner(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := PasskeyOwner(key.X, key.Y)
	if err != nil {
		t.Fatal(err)
	}
	if len(owner) != 64 {
		t.Fatalf("owner length = %d, want 64", len(owner))
	}
	if !bytes.Equal(owner[:32], key.X.FillBytes(make([]byte, 32))) {
		t.Fatal("owner x coordinate does not match")
	}
	if !bytes.Equal(owner[32:], key.Y.FillBytes(make([]byte, 32))) {
		t.Fatal("owner y coordinate does not match")
	}
	if _, err := PasskeyOwner(big.NewInt(1), big.NewInt(1)); err == nil {
		t.Fatal("PasskeyOwner accepted a point off the curve")
	}
}

func TestOwnersReject(t *testing.T) {
	tests := [][][]byte{
		nil,
		{},
		{make([]byte, 19)},
		{make([]byte, 21)},
		{make([]byte, 63)},
		{make([]byte, 65)},
	}
	for _, owners := range tests {
		if _, err := CreateAccountCallData(owners, big.NewInt(0)); err == nil {
			t.Fatalf("CreateAccountCallData accepted owners of %d", len(owners))
		}
	}
}
