package app

import (
	"math/big"
	"testing"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/wallet"
)

func TestHeadline(t *testing.T) {
	a := newTestApp(t)
	if got := a.headline(); got != "Ethereum · No addresses yet" {
		t.Errorf("no accounts: %q", got)
	}
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr}}
	if got := a.headline(); got != "Ethereum · 1 account" {
		t.Errorf("one account: %q", got)
	}
	a.settings.Accounts = append(a.settings.Accounts, wallet.Address{Addr: "0x27b1fdb04752bbc536007a920d24acb045561c26"})
	if got := a.headline(); got != "Ethereum · 2 accounts" {
		t.Errorf("two accounts: %q", got)
	}
}

func TestHeadlineSolana(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.newReader()
	a.settings.Accounts = []wallet.Address{{Addr: viewSol}}
	if got := a.headline(); got != "Solana · 1 account" {
		t.Errorf("headline = %q, want Solana · 1 account", got)
	}
}

func TestRequestRefreshStartsOnce(t *testing.T) {
	a := newTestApp(t)
	calls := 0
	var gotAddrs []string
	a.startRefresh = func(gen int, reader chain.Reader, addrs []string) {
		calls++
		gotAddrs = addrs
	}
	a.settings.Accounts = []wallet.Address{
		{Addr: viewAddr},
		{Addr: "0x27b1fdb04752bbc536007a920d24acb045561c26"},
		{Addr: viewSol}, // another family: not read on Ethereum
	}
	a.requestRefresh()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if !a.loading {
		t.Fatal("loading = false")
	}
	if len(gotAddrs) != 2 {
		t.Fatalf("addresses = %v", gotAddrs)
	}
	a.requestRefresh()
	if calls != 1 {
		t.Fatalf("a second refresh started while one runs: calls = %d", calls)
	}
}

func TestRequestRefreshWithNoAccounts(t *testing.T) {
	a := newTestApp(t)
	calls := 0
	a.startRefresh = func(int, chain.Reader, []string) { calls++ }
	a.requestRefresh()
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
	if a.snap == nil {
		t.Fatal("snapshot = nil")
	}
	if !a.lastUpdated.IsZero() {
		t.Fatalf("lastUpdated = %v, want zero", a.lastUpdated)
	}
}

func TestRequestRefreshWithOnlyOtherFamily(t *testing.T) {
	a := newTestApp(t)
	calls := 0
	a.startRefresh = func(int, chain.Reader, []string) { calls++ }
	a.settings.Accounts = []wallet.Address{{Addr: viewSol}}
	a.requestRefresh()
	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestPasskeyVaultExists(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.newReader()
	vault := "Vault1111111111111111111111111111111111111"
	a.settings.Passkey = &PasskeyAccount{
		ID:            "cred",
		PublicKey:     "p256:AA",
		NEARAccount:   "0s.test",
		SolanaAddress: vault,
	}
	// Nothing learned yet: the last refresh did not answer.
	if a.passkeyVaultExists() {
		t.Fatal("no snapshot: vault reported as existing")
	}
	a.snap = &snapshot{accounts: []accountSnapshot{{addr: vault}}}
	if a.passkeyVaultExists() {
		t.Fatal("unknown state: vault reported as existing")
	}
	// The refresh learned the address already has an account.
	a.snap.vault = vaultExists
	if !a.passkeyVaultExists() {
		t.Fatal("vaultExists state not reported")
	}
	// The balance is withheld once the account may not be the user's.
	if got := a.passkeySolanaBalance(); got != "" {
		t.Fatalf("balance = %q, want empty for an existing vault", got)
	}
	// A failed check is not "the address is free": no balance, and the
	// card says the check did not succeed.
	a.snap.vault = vaultUnknown
	a.snap.accounts = []accountSnapshot{{addr: vault, native: big.NewInt(5_000_000_000)}}
	if got := a.passkeySolanaBalance(); got != "" {
		t.Fatalf("balance = %q, want empty when the vault check failed", got)
	}
	if !a.passkeyVaultUnchecked() {
		t.Fatal("a failed vault check was treated as an answer")
	}
	a.snap.vault = vaultAbsent
	if a.passkeyVaultUnchecked() {
		t.Fatal("a confirmed-absent vault was treated as unchecked")
	}
	if got := a.passkeySolanaBalance(); got == "" {
		t.Fatal("an absent vault hid the balance")
	}
	// The state belongs to the Solana page only.
	a.settings.NetworkID = chains.Ethereum.ID
	a.newReader()
	if a.passkeyVaultExists() {
		t.Fatal("Ethereum network: vault reported as existing")
	}
}
