package smartaccount

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestUserOperationHash(t *testing.T) {
	op := &UserOperation{
		Sender:               common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:                big.NewInt(0),
		CallData:             []byte{0xde, 0xad, 0xbe, 0xef},
		VerificationGasLimit: big.NewInt(100_000),
		CallGasLimit:         big.NewInt(200_000),
		PreVerificationGas:   big.NewInt(21_000),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000),
		MaxFeePerGas:         big.NewInt(2_000_000_000),
	}
	got, err := op.Hash(EntryPointV07, big.NewInt(8453))
	if err != nil {
		t.Fatal(err)
	}
	want := common.HexToHash("0xcbbf3003fc87b578d9e18922fd4fff92038c57e68723876e4d8215d931dff8ce")
	if got != want {
		t.Fatalf("userOpHash = %s, want %s", got, want)
	}
}

func TestPackGas(t *testing.T) {
	got, err := PackGas(big.NewInt(100_000), big.NewInt(200_000))
	if err != nil {
		t.Fatal(err)
	}
	want := common.HexToHash("0x000000000000000000000000000186a000000000000000000000000000030d40")
	if common.BytesToHash(got[:]) != want {
		t.Fatalf("packed gas = %x, want %s", got, want)
	}
	tooLarge := new(big.Int).Lsh(big.NewInt(1), 128)
	if _, err := PackGas(tooLarge, big.NewInt(0)); err == nil {
		t.Fatal("PackGas accepted a value above uint128")
	}
	if _, err := PackGas(nil, big.NewInt(0)); err == nil {
		t.Fatal("PackGas accepted a nil value")
	}
	if _, err := PackGas(big.NewInt(-1), big.NewInt(0)); err == nil {
		t.Fatal("PackGas accepted a negative value")
	}
}

func TestUserOperationHashRejects(t *testing.T) {
	valid := &UserOperation{
		Sender:               common.HexToAddress("0x1111111111111111111111111111111111111111"),
		VerificationGasLimit: big.NewInt(1),
		CallGasLimit:         big.NewInt(1),
		PreVerificationGas:   big.NewInt(1),
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(1),
	}
	tests := []struct {
		name       string
		op         *UserOperation
		entryPoint common.Address
		chainID    *big.Int
	}{
		{"nil op", nil, EntryPointV07, big.NewInt(1)},
		{"no sender", &UserOperation{}, EntryPointV07, big.NewInt(1)},
		{"no entry point", valid, common.Address{}, big.NewInt(1)},
		{"no chain id", valid, EntryPointV07, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.op.Hash(tt.entryPoint, tt.chainID); err == nil {
				t.Fatal("Hash accepted an invalid operation")
			}
		})
	}
}
