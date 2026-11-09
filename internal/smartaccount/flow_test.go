package smartaccount

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/davirain-su/lintel/internal/webauthn"
)

// TestPasskeyUserOperationFlow walks the whole local path: register a
// credential, turn its public key into an account owner, build a user
// operation, sign its hash with the passkey, and encode the signature the
// account verifies on chain.
func TestPasskeyUserOperationFlow(t *testing.T) {
	const (
		rpID   = "lintel.example"
		origin = "https://lintel.example"
	)
	auth, err := webauthn.NewSoftAuthenticator(rpID, origin)
	if err != nil {
		t.Fatal(err)
	}
	registrationChallenge := bytes.Repeat([]byte{0x42}, 32)
	clientData, attestation, err := auth.Register(registrationChallenge)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := webauthn.ParseRegistration(clientData, attestation, registrationChallenge, origin, rpID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := PasskeyOwner(reg.X, reg.Y)
	if err != nil {
		t.Fatal(err)
	}
	account := Account{
		Factory:    common.HexToAddress("0x3333333333333333333333333333333333333333"),
		EntryPoint: EntryPointV07,
		Owners:     [][]byte{owner},
		Nonce:      big.NewInt(0),
	}
	callData, err := ExecuteCallData(
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		big.NewInt(0),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	op, err := account.NewUserOperation(
		common.HexToAddress("0x4444444444444444444444444444444444444444"),
		callData,
	)
	if err != nil {
		t.Fatal(err)
	}
	op.VerificationGasLimit = big.NewInt(100_000)
	op.CallGasLimit = big.NewInt(200_000)
	op.PreVerificationGas = big.NewInt(21_000)
	op.MaxPriorityFeePerGas = big.NewInt(1_000_000_000)
	op.MaxFeePerGas = big.NewInt(2_000_000_000)

	hash, err := op.Hash(EntryPointV07, big.NewInt(8453))
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := auth.Assert(hash.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	credential := webauthn.Credential{ID: reg.CredentialID, X: reg.X, Y: reg.Y}
	signature, err := Sign(op, EntryPointV07, big.NewInt(8453), credential, assertion, origin, rpID)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	op.Signature = signature

	// The signature is not part of the hash, so signing does not change what
	// the account checks.
	again, err := op.Hash(EntryPointV07, big.NewInt(8453))
	if err != nil {
		t.Fatal(err)
	}
	if again != hash {
		t.Fatalf("hash changed after signing: %s, want %s", again, hash)
	}
}
