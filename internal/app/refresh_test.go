package app

import (
	"testing"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/wallet"
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
