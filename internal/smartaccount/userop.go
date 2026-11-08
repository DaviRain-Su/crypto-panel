package smartaccount

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// UserOperation is an ERC-4337 v0.7 PackedUserOperation. The gas limits and
// fees are kept as big.Ints; the EntryPoint packs each pair into two uint128
// halves.
type UserOperation struct {
	Sender               common.Address
	Nonce                *big.Int
	InitCode             []byte
	CallData             []byte
	VerificationGasLimit *big.Int
	CallGasLimit         *big.Int
	PreVerificationGas   *big.Int
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
	PaymasterAndData     []byte
	Signature            []byte
}

// Hash returns the v0.7 userOpHash:
//
//	keccak256(abi.encode(
//	  keccak256(abi.encode(sender, nonce, keccak256(initCode), keccak256(callData),
//	    accountGasLimits, preVerificationGas, gasFees, keccak256(paymasterAndData))),
//	  entryPoint, chainID))
//
// The signature is not part of it; the passkey signs this hash, and the
// account checks the signature against the same value on chain.
func (op *UserOperation) Hash(entryPoint common.Address, chainID *big.Int) (common.Hash, error) {
	if op == nil {
		return common.Hash{}, errors.New("smartaccount: nil user operation")
	}
	if op.Sender == (common.Address{}) {
		return common.Hash{}, errors.New("smartaccount: user operation has no sender")
	}
	if entryPoint == (common.Address{}) {
		return common.Hash{}, errors.New("smartaccount: no entry point")
	}
	if chainID == nil || chainID.Sign() <= 0 {
		return common.Hash{}, errors.New("smartaccount: no chain id")
	}
	accountGasLimits, err := PackGas(op.VerificationGasLimit, op.CallGasLimit)
	if err != nil {
		return common.Hash{}, fmt.Errorf("smartaccount: verification and call gas: %w", err)
	}
	gasFees, err := PackGas(op.MaxPriorityFeePerGas, op.MaxFeePerGas)
	if err != nil {
		return common.Hash{}, fmt.Errorf("smartaccount: fees: %w", err)
	}
	inner, err := abi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
		{Type: uint256Type},
		{Type: bytes32Type},
		{Type: bytes32Type},
	}.Pack(
		op.Sender,
		zero(op.Nonce),
		crypto.Keccak256Hash(op.InitCode),
		crypto.Keccak256Hash(op.CallData),
		accountGasLimits,
		zero(op.PreVerificationGas),
		gasFees,
		crypto.Keccak256Hash(op.PaymasterAndData),
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("smartaccount: packing user operation: %w", err)
	}
	outer, err := abi.Arguments{
		{Type: bytes32Type},
		{Type: addressType},
		{Type: uint256Type},
	}.Pack(crypto.Keccak256Hash(inner), entryPoint, chainID)
	if err != nil {
		return common.Hash{}, fmt.Errorf("smartaccount: packing user operation hash: %w", err)
	}
	return crypto.Keccak256Hash(outer), nil
}

// PackGas packs two uint128 values into the bytes32 the EntryPoint reads:
// high in the first 16 bytes, low in the last 16.
func PackGas(high, low *big.Int) ([32]byte, error) {
	var out [32]byte
	if high == nil || low == nil {
		return out, errors.New("a gas value is missing")
	}
	if high.Sign() < 0 || low.Sign() < 0 {
		return out, errors.New("a gas value is negative")
	}
	if high.BitLen() > 128 || low.BitLen() > 128 {
		return out, errors.New("a gas value does not fit in uint128")
	}
	high.FillBytes(out[:16])
	low.FillBytes(out[16:])
	return out, nil
}
