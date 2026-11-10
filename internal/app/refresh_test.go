package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chain/evm"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/wallet"
)

func TestHeadline(t *testing.T) {
	a := newTestApp(t)
	if got := a.headline(); got != "No addresses yet" {
		t.Errorf("no accounts: %q", got)
	}
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr}}
	if got := a.headline(); got != "1 account" {
		t.Errorf("one account: %q", got)
	}
	a.settings.Accounts = append(a.settings.Accounts, wallet.Address{Addr: "0x27b1fdb04752bbc536007a920d24acb045561c26"})
	if got := a.headline(); got != "2 accounts" {
		t.Errorf("two accounts: %q", got)
	}
}

func TestHeadlineSolana(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.newReader()
	a.settings.Accounts = []wallet.Address{{Addr: viewSol}}
	if got := a.headline(); got != "1 account" {
		t.Errorf("headline = %q, want the count alone", got)
	}
}

func TestRequestRefreshStartsOnce(t *testing.T) {
	a := newTestApp(t)
	calls := 0
	var gotJobs []familyFetch
	a.startRefresh = func(gen int, jobs []familyFetch) {
		calls++
		gotJobs = jobs
	}
	a.settings.Accounts = []wallet.Address{
		{Addr: viewAddr},
		{Addr: "0x27b1fdb04752bbc536007a920d24acb045561c26"},
		{Addr: viewSol}, // another family: its own job, on its own chain
	}
	a.requestRefresh()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if !a.loading {
		t.Fatal("loading = false")
	}
	evmNetworks := chains.NetworksOf(chain.EVM)
	if len(gotJobs) != len(evmNetworks)+1 {
		t.Fatalf("jobs = %d, want one per EVM network plus Solana", len(gotJobs))
	}
	for i, network := range evmNetworks {
		if gotJobs[i].network.ID != network.ID || len(gotJobs[i].addrs) != 2 {
			t.Errorf("EVM job %d = network %q, addresses %v", i, gotJobs[i].network.ID, gotJobs[i].addrs)
		}
	}
	if got := gotJobs[len(gotJobs)-1].network.ID; got != chains.SolanaMainnet.ID {
		t.Fatalf("Solana job reads %q, want the preset", got)
	}
	a.requestRefresh()
	if calls != 1 {
		t.Fatalf("a second refresh started while one runs: calls = %d", calls)
	}
}

func TestRequestRefreshWithNoAccounts(t *testing.T) {
	a := newTestApp(t)
	calls := 0
	a.startRefresh = func(int, []familyFetch) { calls++ }
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
	var gotJobs []familyFetch
	a.startRefresh = func(gen int, jobs []familyFetch) {
		calls++
		gotJobs = jobs
	}
	a.settings.Accounts = []wallet.Address{{Addr: viewSol}}
	a.requestRefresh()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1: every family is read, not only the selected network", calls)
	}
	if got := gotJobs[0].network.ID; got != chains.SolanaMainnet.ID {
		t.Fatalf("job network = %q, want SolanaMainnet", got)
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

type refreshReader struct {
	account func(context.Context, string) (chain.AccountBalances, error)
}

func (r refreshReader) Account(ctx context.Context, addr string) (chain.AccountBalances, error) {
	return r.account(ctx, addr)
}
func (refreshReader) Block(context.Context) (uint64, error) { return 42, nil }
func (refreshReader) RPC(context.Context, string, any) (json.RawMessage, error) {
	return nil, errors.New("not implemented")
}

func TestFetchKeepsEVMNetworksSeparate(t *testing.T) {
	a := newTestApp(t)
	done := make(chan struct{})
	a.runOnMain = func(fn func()) { fn(); close(done) }
	jobs := []familyFetch{
		{network: chains.Ethereum, addrs: []string{strings.ToUpper(viewAddr)}, reader: refreshReader{account: func(context.Context, string) (chain.AccountBalances, error) {
			return chain.AccountBalances{Native: big.NewInt(1)}, nil
		}}},
		{network: chains.BSC, addrs: []string{viewAddr}, reader: refreshReader{account: func(context.Context, string) (chain.AccountBalances, error) {
			return chain.AccountBalances{Native: big.NewInt(2)}, errors.New("BSC RPC unavailable")
		}}},
	}
	a.fetch(0, jobs)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if len(a.snap.accounts) != 2 || a.snap.accounts[0].network.ID != chains.Ethereum.ID || a.snap.accounts[1].network.ID != chains.BSC.ID {
		t.Fatalf("snapshots = %+v, want stable Ethereum then BSC rows", a.snap.accounts)
	}
	if got := a.accountSnapshot(viewAddr, chains.Ethereum.ID); got == nil || got.native.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("Ethereum snapshot = %+v", got)
	}
	if got := a.accountSnapshot(viewAddr, chains.BSC.ID); got == nil || got.err == "" {
		t.Fatalf("BSC error snapshot = %+v", got)
	}
	if a.lastErr == "" || !a.totalsPartial() {
		t.Fatalf("partial error not preserved: error %q, partial %v", a.lastErr, a.totalsPartial())
	}
}

func TestFetchBoundsConcurrentJobs(t *testing.T) {
	a := newTestApp(t)
	done := make(chan struct{})
	a.runOnMain = func(fn func()) { fn(); close(done) }
	var active atomic.Int32
	var maximum atomic.Int32
	var jobs []familyFetch
	for _, network := range chains.NetworksOf(chain.EVM) {
		jobs = append(jobs, familyFetch{network: network, addrs: []string{viewAddr}, reader: refreshReader{account: func(context.Context, string) (chain.AccountBalances, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
			}
			time.Sleep(15 * time.Millisecond)
			return chain.AccountBalances{Native: big.NewInt(1)}, nil
		}}})
	}
	a.fetch(0, jobs)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if got := maximum.Load(); got < 2 || got > maxRefreshConcurrency {
		t.Fatalf("maximum concurrent jobs = %d, want 2..%d", got, maxRefreshConcurrency)
	}
}

func TestTotalUSDUsesPerNetworkAssetIdentity(t *testing.T) {
	a := newTestApp(t)
	a.snap = &snapshot{
		accounts: []accountSnapshot{
			{addr: viewAddr, network: chains.Ethereum, native: new(big.Int).Mul(big.NewInt(2), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))},
			{addr: viewAddr, network: chains.BSC, native: new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)},
		},
		usd: map[string]float64{
			"eip155:1:native":  3000,
			"eip155:56:native": 600,
		},
	}
	total, ok := a.totalUSD()
	if !ok || total != 6600 {
		t.Fatalf("total USD = %v, %v; want 6600", total, ok)
	}
	a.snap.accounts[1].native = nil
	a.snap.accounts[1].err = "BSC RPC unavailable"
	total, ok = a.totalUSD()
	if !ok || total != 6000 || !a.totalsPartial() {
		t.Fatalf("partial total USD = %v, %v, partial %v; want 6000 and incomplete", total, ok, a.totalsPartial())
	}
}

func TestCustomRPCStaysOnSelectedEVMNetwork(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.Base.ID
	a.settings.RPCURL = "http://127.0.0.1:1/custom"
	jobs := a.refreshJobs()
	for _, job := range jobs {
		client, ok := job.reader.(interface{ Endpoint() evm.Endpoint })
		if !ok {
			t.Fatal("EVM reader does not expose its endpoint")
		}
		got := client.Endpoint().URL
		if job.network.ID == chains.Base.ID && got != a.settings.RPCURL {
			t.Errorf("selected network endpoint = %q", got)
		}
		if job.network.ID != chains.Base.ID && got != job.network.RPCURL {
			t.Errorf("%s inherited custom endpoint %q", job.network.Name, got)
		}
	}
}

func Example_accountSnapshotNetworkIdentity() {
	first := accountSnapshot{addr: viewAddr, network: chains.Ethereum}
	second := accountSnapshot{addr: viewAddr, network: chains.Base}
	fmt.Println(first.network.Symbol, second.network.Symbol, first.network.ID != second.network.ID)
	// Output: ETH ETH true
}
