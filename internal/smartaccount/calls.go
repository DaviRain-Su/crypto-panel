package smartaccount

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ExecuteCallData encodes CoinbaseSmartWallet.execute(target, value, data),
// the call a user operation carries to move funds or call a contract.
func ExecuteCallData(target common.Address, value *big.Int, data []byte) ([]byte, error) {
	if target == (common.Address{}) {
		return nil, errors.New("smartaccount: no target")
	}
	if value != nil && value.Sign() < 0 {
		return nil, errors.New("smartaccount: negative value")
	}
	args, err := abi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
		{Type: bytesType},
	}.Pack(target, zero(value), data)
	if err != nil {
		return nil, fmt.Errorf("smartaccount: packing execute: %w", err)
	}
	return append(selector("execute(address,uint256,bytes)"), args...), nil
}

// CreateAccountCallData encodes
// CoinbaseSmartWalletFactory.createAccount(owners, nonce). The account is
// deployed by the EntryPoint on the first user operation that carries it as
// initCode.
func CreateAccountCallData(owners [][]byte, nonce *big.Int) ([]byte, error) {
	return factoryCallData("createAccount(bytes[],uint256)", owners, nonce)
}

// GetAddressCallData encodes
// CoinbaseSmartWalletFactory.getAddress(owners, nonce), the view call that
// returns the counterfactual account address.
func GetAddressCallData(owners [][]byte, nonce *big.Int) ([]byte, error) {
	return factoryCallData("getAddress(bytes[],uint256)", owners, nonce)
}

func factoryCallData(signature string, owners [][]byte, nonce *big.Int) ([]byte, error) {
	if err := validateOwners(owners); err != nil {
		return nil, err
	}
	if nonce != nil && nonce.Sign() < 0 {
		return nil, errors.New("smartaccount: negative nonce")
	}
	args, err := abi.Arguments{
		{Type: bytesArrayType},
		{Type: uint256Type},
	}.Pack(owners, zero(nonce))
	if err != nil {
		return nil, fmt.Errorf("smartaccount: packing %s: %w", signature, err)
	}
	return append(selector(signature), args...), nil
}

// PasskeyOwner returns the 64-byte owner a Coinbase Smart Wallet stores for a
// passkey: the P-256 public key's x and y coordinates.
func PasskeyOwner(x, y *big.Int) ([]byte, error) {
	if x == nil || y == nil || x.BitLen() > 256 || y.BitLen() > 256 {
		return nil, errors.New("smartaccount: invalid passkey public key")
	}
	if !elliptic.P256().IsOnCurve(x, y) {
		return nil, errors.New("smartaccount: passkey public key is not on P-256")
	}
	owner := make([]byte, 64)
	x.FillBytes(owner[:32])
	y.FillBytes(owner[32:])
	return owner, nil
}

// validateOwners checks the two owner forms Coinbase Smart Wallet accepts: a
// 20-byte address or a 64-byte P-256 public key.
func validateOwners(owners [][]byte) error {
	if len(owners) == 0 {
		return errors.New("smartaccount: no owners")
	}
	for i, owner := range owners {
		switch len(owner) {
		case 20, 64:
		default:
			return fmt.Errorf("smartaccount: owner %d is %d bytes, want 20 or 64", i, len(owner))
		}
	}
	return nil
}
