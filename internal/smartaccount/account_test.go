package smartaccount

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type fakeCaller struct {
	to   common.Address
	data []byte
	out  []byte
	err  error
}

func (f *fakeCaller) CallContract(_ context.Context, call ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	if call.To != nil {
		f.to = *call.To
	}
	f.data = call.Data
	return f.out, f.err
}

func testAccount() Account {
	return Account{
		Factory:    common.HexToAddress("0x3333333333333333333333333333333333333333"),
		EntryPoint: EntryPointV07,
		Owners:     [][]byte{bytes.Repeat([]byte{0x11}, 64)},
		Nonce:      big.NewInt(0),
	}
}

func TestAccountAddress(t *testing.T) {
	account := testAccount()
	want := common.HexToAddress("0x4444444444444444444444444444444444444444")
	out, err := abi.Arguments{{Type: addressType}}.Pack(want)
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{out: out}
	got, err := account.Address(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("address = %s, want %s", got, want)
	}
	if caller.to != account.Factory {
		t.Fatalf("called %s, want the factory %s", caller.to, account.Factory)
	}
	data, err := GetAddressCallData(account.Owners, account.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(caller.data, data) {
		t.Fatal("getAddress call data does not match")
	}
}

func TestAccountAddressRejects(t *testing.T) {
	account := testAccount()
	zero := common.Address{}
	out, err := abi.Arguments{{Type: addressType}}.Pack(zero)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		caller ContractCaller
		acct   Account
	}{
		{"no caller", nil, account},
		{"no factory", &fakeCaller{out: out}, Account{}},
		{"call fails", &fakeCaller{err: errors.New("rpc down")}, account},
		{"empty result", &fakeCaller{}, account},
		{"zero address", &fakeCaller{out: out}, account},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.acct.Address(context.Background(), tt.caller); err == nil {
				t.Fatal("Address accepted an invalid result")
			}
		})
	}
}

func TestAccountInitCode(t *testing.T) {
	account := testAccount()
	initCode, err := account.InitCode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(initCode, account.Factory.Bytes()) {
		t.Fatal("init code does not start with the factory address")
	}
	call, err := CreateAccountCallData(account.Owners, account.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(initCode[common.AddressLength:], call) {
		t.Fatal("init code does not carry createAccount")
	}
	if _, err := (Account{}).InitCode(); err == nil {
		t.Fatal("InitCode accepted an account without a factory")
	}
}

func TestNewUserOperation(t *testing.T) {
	account := testAccount()
	sender := common.HexToAddress("0x5555555555555555555555555555555555555555")
	callData := []byte{0x01, 0x02}
	op, err := account.NewUserOperation(sender, callData)
	if err != nil {
		t.Fatal(err)
	}
	if op.Sender != sender || !bytes.Equal(op.CallData, callData) {
		t.Fatal("user operation does not carry the sender and call")
	}
	if len(op.InitCode) == 0 {
		t.Fatal("user operation has no init code")
	}
}
