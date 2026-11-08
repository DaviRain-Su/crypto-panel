package smartaccount

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// ContractCaller is the read-only chain access Account needs. An
// ethclient.Client implements it.
type ContractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// Account is a counterfactual Coinbase Smart Wallet account. Owners are the
// 20-byte addresses or 64-byte P-256 public keys the account accepts; a
// passkey owner is made with PasskeyOwner. The account holds no key.
type Account struct {
	Factory    common.Address
	EntryPoint common.Address
	Owners     [][]byte
	Nonce      *big.Int
}

// InitCode is the factory address followed by createAccount(owners, nonce).
// A user operation carries it until the account is deployed.
func (a Account) InitCode() ([]byte, error) {
	if a.Factory == (common.Address{}) {
		return nil, errors.New("smartaccount: account has no factory")
	}
	call, err := CreateAccountCallData(a.Owners, a.Nonce)
	if err != nil {
		return nil, err
	}
	return append(a.Factory.Bytes(), call...), nil
}

// Address asks the factory for the account's counterfactual address. It is a
// view call, so it works before the account is deployed.
func (a Account) Address(ctx context.Context, caller ContractCaller) (common.Address, error) {
	if caller == nil {
		return common.Address{}, errors.New("smartaccount: no chain caller")
	}
	if a.Factory == (common.Address{}) {
		return common.Address{}, errors.New("smartaccount: account has no factory")
	}
	data, err := GetAddressCallData(a.Owners, a.Nonce)
	if err != nil {
		return common.Address{}, err
	}
	out, err := caller.CallContract(ctx, ethereum.CallMsg{To: &a.Factory, Data: data}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("smartaccount: reading account address: %w", err)
	}
	values, err := abi.Arguments{{Type: addressType}}.Unpack(out)
	if err != nil {
		return common.Address{}, fmt.Errorf("smartaccount: decoding account address: %w", err)
	}
	addr, ok := values[0].(common.Address)
	if !ok || addr == (common.Address{}) {
		return common.Address{}, errors.New("smartaccount: factory returned no address")
	}
	return addr, nil
}

// NewUserOperation starts a user operation for callData. InitCode is set so
// the first operation deploys the account; once it is deployed, clear
// InitCode before estimating gas.
func (a Account) NewUserOperation(sender common.Address, callData []byte) (*UserOperation, error) {
	initCode, err := a.InitCode()
	if err != nil {
		return nil, err
	}
	return &UserOperation{
		Sender:   sender,
		InitCode: initCode,
		CallData: callData,
	}, nil
}
