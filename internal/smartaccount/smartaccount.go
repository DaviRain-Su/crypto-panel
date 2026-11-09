// Package smartaccount builds the pieces of an ERC-4337 account whose owner
// is a WebAuthn passkey: the counterfactual address, the calls that create
// and execute, the v0.7 user operation hash, the WebAuthn signature the
// account verifies, and a bundler client. It holds no key: the passkey signs
// in the platform authenticator, and this package only moves the result into
// the shapes the contracts expect.
package smartaccount

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// EntryPointV07 is the ERC-4337 v0.7 EntryPoint. It is deployed at the same
// address on the chains the app supports.
var EntryPointV07 = common.HexToAddress("0x0000000071727De22E5E9d8BAf0edAc6f37da032")

// DefaultFactory is the Coinbase Smart Wallet factory v1.1. It is deployed at
// the same address on the EVM chains the panel reads, so one passkey owns the
// same smart account address everywhere.
var DefaultFactory = common.HexToAddress("0xBA5ED110eFDBa3D005bfC882d75358ACBbB85842")

// ABI types used by the encoders. They are constants; a failure here is a
// programming error, not something a caller can fix.
var (
	addressType    = mustType("address")
	uint256Type    = mustType("uint256")
	bytesType      = mustType("bytes")
	bytes32Type    = mustType("bytes32")
	stringType     = mustType("string")
	bytesArrayType = mustType("bytes[]")
)

func mustType(name string) abi.Type {
	typ, err := abi.NewType(name, "", nil)
	if err != nil {
		panic("smartaccount: " + err.Error())
	}
	return typ
}

// selector returns the four-byte selector of a Solidity function signature.
func selector(signature string) []byte {
	return crypto.Keccak256([]byte(signature))[:4]
}

// zero returns v, or zero when v is nil, so an encoder never sees a nil
// integer.
func zero(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return v
}
