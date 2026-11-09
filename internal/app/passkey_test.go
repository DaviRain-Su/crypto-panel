package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/passkeywallet"
	"github.com/davirain-su/crypto-panel/internal/wallet"
	"github.com/davirain-su/crypto-panel/internal/webauthn"
)

// softCeremony runs the WebAuthn ceremonies in memory, over the same
// authenticator, for tests. It answers with the origin the app checks.
type softCeremony struct {
	auth       *webauthn.SoftAuthenticator
	pageOrigin string
}

func (c *softCeremony) origin() string { return c.pageOrigin }
func (c *softCeremony) close()         {}

func (c *softCeremony) run(_ context.Context, req webauthn.CeremonyRequest) (webauthn.CeremonyResult, error) {
	switch req.Mode {
	case webauthn.CeremonyCreate:
		clientData, attestation, err := c.auth.Register(req.Challenge)
		if err != nil {
			return webauthn.CeremonyResult{}, err
		}
		return webauthn.CeremonyResult{
			Kind:              webauthn.CeremonyCreate,
			State:             req.State,
			CredentialID:      c.auth.Credential().ID,
			ClientDataJSON:    clientData,
			AttestationObject: attestation,
		}, nil
	case webauthn.CeremonyGet:
		assertion, err := c.auth.Assert(req.Challenge)
		if err != nil {
			return webauthn.CeremonyResult{}, err
		}
		return webauthn.CeremonyResult{
			Kind:              webauthn.CeremonyGet,
			State:             req.State,
			CredentialID:      c.auth.Credential().ID,
			ClientDataJSON:    assertion.ClientDataJSON,
			AuthenticatorData: assertion.AuthenticatorData,
			Signature:         assertion.Signature,
		}, nil
	}
	return webauthn.CeremonyResult{}, errors.New("unknown ceremony mode")
}

func softCeremonyFactory(auth *webauthn.SoftAuthenticator) func(func(string) error) (passkeyCeremony, error) {
	return func(func(string) error) (passkeyCeremony, error) {
		return &softCeremony{auth: auth, pageOrigin: "http://localhost:1234"}, nil
	}
}

func newSoftAuthenticator(t *testing.T) *webauthn.SoftAuthenticator {
	t.Helper()
	auth, err := webauthn.NewSoftAuthenticator(ceremonyRPID, "http://localhost:1234")
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

// softPasskey is the stored public half of auth's credential.
func softPasskey(t *testing.T, auth *webauthn.SoftAuthenticator) *PasskeyAccount {
	t.Helper()
	cred := auth.Credential()
	pub, err := passkeywallet.NewPublicKey(cred.X, cred.Y)
	if err != nil {
		t.Fatal(err)
	}
	return &PasskeyAccount{
		ID:          base64.RawURLEncoding.EncodeToString(cred.ID),
		PublicKey:   pub.String(),
		NEARAccount: passkeywallet.AccountID(pub),
	}
}

// passkeyEVMReader answers the factory's getAddress view call.
type passkeyEVMReader struct {
	address common.Address
	calls   int
}

func (r *passkeyEVMReader) Account(context.Context, string) (chain.AccountBalances, error) {
	return chain.AccountBalances{}, nil
}
func (r *passkeyEVMReader) Block(context.Context) (uint64, error) { return 1, nil }
func (r *passkeyEVMReader) RPC(_ context.Context, method string, params any) (json.RawMessage, error) {
	if method != "eth_call" {
		return nil, errors.New("unexpected method " + method)
	}
	r.calls++
	return json.Marshal(hexutil.Encode(common.LeftPadBytes(r.address.Bytes(), 32)))
}

func TestCreatePasskeyStoresAndWatchesTheAccounts(t *testing.T) {
	a := newTestApp(t)
	auth := newSoftAuthenticator(t)
	a.newCeremony = softCeremonyFactory(auth)
	evm := &passkeyEVMReader{address: common.HexToAddress("0x1111111111111111111111111111111111111111")}
	a.reader = evm

	a.createPasskey()

	pk := a.settings.Passkey
	if pk == nil {
		t.Fatal("the passkey was not stored")
	}
	if !strings.HasPrefix(pk.NEARAccount, "0s") || len(pk.NEARAccount) != 42 {
		t.Fatalf("NEAR account = %q", pk.NEARAccount)
	}
	if !strings.HasPrefix(pk.PublicKey, "p256:") {
		t.Fatalf("public key = %q", pk.PublicKey)
	}
	if pk.ID == "" || pk.EVMAddress != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("id = %q, EVM = %q", pk.ID, pk.EVMAddress)
	}
	if evm.calls == 0 {
		t.Fatal("the factory was not read")
	}
	if !a.settings.Has(pk.NEARAccount) || !a.settings.Has(pk.EVMAddress) {
		t.Fatalf("accounts = %+v", a.settings.Accounts)
	}
	if a.passkeyNotice == "" || a.passkeyErr != "" {
		t.Fatalf("notice = %q, err = %q", a.passkeyNotice, a.passkeyErr)
	}
	if a.passkeyBusy {
		t.Fatal("the card is still busy")
	}
}

func TestCreatePasskeyReportsACancelledCeremony(t *testing.T) {
	a := newTestApp(t)
	a.newCeremony = func(func(string) error) (passkeyCeremony, error) {
		return nil, &webauthn.CeremonyError{Code: "Canceled", Message: "The user canceled."}
	}
	a.createPasskey()
	if a.settings.Passkey != nil {
		t.Fatal("a canceled ceremony stored a passkey")
	}
	if !strings.Contains(a.passkeyErr, "Canceled") {
		t.Fatalf("err = %q", a.passkeyErr)
	}
	if a.passkeyBusy {
		t.Fatal("the card is still busy")
	}
}

// passkeyNearReader answers the relayer's calls and reports the passkey
// account as not deployed.
type passkeyNearReader struct {
	rpc            *nearRPC
	passkeyAccount string
}

func (r *passkeyNearReader) Account(context.Context, string) (chain.AccountBalances, error) {
	return chain.AccountBalances{}, nil
}
func (r *passkeyNearReader) Block(context.Context) (uint64, error) { return 1, nil }
func (r *passkeyNearReader) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method == "query" {
		if m, ok := params.(map[string]any); ok && m["request_type"] == "view_account" && m["account_id"] == r.passkeyAccount {
			return nil, errors.New("Server error: account " + r.passkeyAccount + " does not exist while viewing")
		}
	}
	return r.rpc.call(ctx, method, params)
}

func newPasskeyApp(t *testing.T) (*App, *nearRPC, *webauthn.SoftAuthenticator, wallet.NearAccount) {
	t.Helper()
	a := newTestApp(t)
	a.settings.NetworkID = chains.NearMainnet.ID
	a.settings = a.settings.Validate()
	a.createNearAccount()
	relayer := a.settings.NearWallet[0]
	auth := newSoftAuthenticator(t)
	a.newCeremony = softCeremonyFactory(auth)
	a.settings.Passkey = softPasskey(t, auth)
	rpc := &nearRPC{accessKey: `{"nonce":7,"permission":"FullAccess"}`}
	a.reader = &passkeyNearReader{rpc: rpc, passkeyAccount: a.settings.Passkey.NEARAccount}
	return a, rpc, auth, relayer
}

func TestSendPasskeyNearSignsAfterApproval(t *testing.T) {
	a, rpc, _, _ := newPasskeyApp(t)
	var asked approvalRequest
	a.approve = func(req approvalRequest) bool { asked = req; return true }
	a.passkeySendTo = "bob.near"
	a.passkeySendAmount = "1.5"

	a.sendPasskeyNear()

	if rpc.broadcast == nil {
		t.Fatal("nothing was broadcast")
	}
	if asked.sourceKind != "wallet" || asked.fromLabel != "Passkey" {
		t.Fatalf("approval = %+v", asked)
	}
	if asked.fromAddr != a.settings.Passkey.NEARAccount {
		t.Fatalf("from = %q", asked.fromAddr)
	}
	if asked.near == nil || asked.near.ReceiverID != "bob.near" {
		t.Fatalf("summary = %+v", asked.near)
	}
	if got := asked.near.Actions[0].Amount.String(); got != "1500000000000000000000000" {
		t.Fatalf("amount = %s", got)
	}
	if !bytes.Contains(rpc.broadcast, []byte("w_execute_signed")) {
		t.Fatal("the relayed call is not w_execute_signed")
	}
	pub, err := a.settings.Passkey.publicKey()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rpc.broadcast, passkeywallet.StateInit(pub)) {
		t.Fatal("an undeployed account's transaction has no StateInit")
	}
	if a.passkeySendHash == "" || a.passkeySendNotice == "" {
		t.Fatalf("hash = %q, notice = %q", a.passkeySendHash, a.passkeySendNotice)
	}
	if a.passkeySendTo != "" || a.passkeySendAmount != "" {
		t.Fatalf("the form was not cleared: %q %q", a.passkeySendTo, a.passkeySendAmount)
	}
	if a.passkeySendBusy {
		t.Fatal("the form is still busy")
	}
}

func TestSendPasskeyNearRejected(t *testing.T) {
	a, rpc, _, _ := newPasskeyApp(t)
	a.approve = func(approvalRequest) bool { return false }
	a.passkeySendTo = "bob.near"
	a.passkeySendAmount = "1"

	a.sendPasskeyNear()

	if rpc.broadcast != nil {
		t.Fatal("a rejected transaction was broadcast")
	}
	if !strings.Contains(a.passkeySendErr, "rejected") {
		t.Fatalf("err = %q", a.passkeySendErr)
	}
}

func TestSendPasskeyNearNeedsMainnet(t *testing.T) {
	a, rpc, _, _ := newPasskeyApp(t)
	a.settings.NetworkID = chains.NearTestnet.ID
	a.settings = a.settings.Validate()
	a.passkeySendTo = "bob.testnet"
	a.passkeySendAmount = "1"

	a.sendPasskeyNear()

	if rpc.broadcast != nil {
		t.Fatal("a testnet transaction was broadcast")
	}
	if a.passkeySendErr == "" {
		t.Fatal("no error was shown")
	}
}

func TestPasskeyCardView(t *testing.T) {
	a := newTestApp(t)
	auth := newSoftAuthenticator(t)
	a.settings.Passkey = softPasskey(t, auth)
	a.settings.NetworkID = chains.NearMainnet.ID
	a.settings = a.settings.Validate()
	a.route = routeWallet

	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{
		"Passkey wallet", "NEAR + EVM", "NEAR account", "EVM account",
		"Create account", "Sign and send",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if !tt.HasText(a.settings.Passkey.NEARAccount) {
		t.Errorf("the NEAR account is not shown: %q", tt.Texts())
	}
}

func TestValidatePasskeyDropsAMismatchedAccount(t *testing.T) {
	auth := newSoftAuthenticator(t)
	pk := softPasskey(t, auth)
	pk.NEARAccount = "0s0000000000000000000000000000000000000000"
	s := DefaultSettings()
	s.Passkey = pk
	if s.Validate().Passkey != nil {
		t.Fatal("a passkey with another account id was kept")
	}
}

func TestValidatePasskeyKeepsAValidOne(t *testing.T) {
	auth := newSoftAuthenticator(t)
	pk := softPasskey(t, auth)
	s := DefaultSettings()
	s.Passkey = pk
	got := s.Validate().Passkey
	if got == nil || got.NEARAccount != pk.NEARAccount || got.PublicKey != pk.PublicKey {
		t.Fatalf("passkey = %+v", got)
	}
}

func TestPasskeyAccountIDIsWatchedAsNear(t *testing.T) {
	auth := newSoftAuthenticator(t)
	pk := softPasskey(t, auth)
	s := DefaultSettings()
	s.Accounts = []wallet.Address{{Addr: pk.NEARAccount, Label: "Passkey"}}
	got := s.Validate()
	if len(got.Accounts) != 1 || got.Accounts[0].Addr != pk.NEARAccount {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	if len(got.WatchedOf(chain.Near)) != 1 {
		t.Fatalf("watched = %+v", got.WatchedOf(chain.Near))
	}
}
