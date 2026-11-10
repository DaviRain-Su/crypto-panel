package app

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chain/evm"
	"github.com/davirain-su/lintel/internal/chain/solana"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/wallet"
)

// gateReader is a chain reader whose Account call is the test's.
type gateReader struct {
	native   *big.Int
	tokens   []chain.TokenBalance
	block    uint64
	err      error
	exists   bool
	existErr error
	account  func(context.Context) error
}

func (g gateReader) Account(ctx context.Context, _ string) (chain.AccountBalances, error) {
	if g.account != nil {
		if err := g.account(ctx); err != nil {
			return chain.AccountBalances{}, err
		}
	}
	if g.err != nil {
		return chain.AccountBalances{}, g.err
	}
	return chain.AccountBalances{Native: g.native, Tokens: g.tokens}, nil
}

func (g gateReader) Block(context.Context) (uint64, error) { return g.block, nil }

func (g gateReader) RPC(context.Context, string, any) (json.RawMessage, error) {
	return nil, errors.New("no rpc")
}

func (g gateReader) AccountExists(context.Context, string) (bool, error) {
	return g.exists, g.existErr
}

type fixedPrices map[string]float64

func (m fixedPrices) Prices(context.Context, []string) (map[string]float64, error) {
	return m, nil
}

func finishFetch(t *testing.T, a *App, jobs []familyFetch) {
	t.Helper()
	done := make(chan struct{})
	previous := runOnUI
	runOnUI = func(fn func()) {
		fn()
		select {
		case <-done:
		default:
			close(done)
		}
	}
	t.Cleanup(func() { runOnUI = previous })
	a.fetch(a.gen, jobs)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not finish")
	}
}

func TestNetworkForStaysOnMainnet(t *testing.T) {
	s := DefaultSettings()
	s.NetworkID = chains.Ethereum.ID
	if got := s.NetworkFor(chain.Near); got.ID != chains.NearMainnet.ID {
		t.Fatalf("unselected NEAR = %q, want mainnet", got.ID)
	}
	if got := s.NetworkFor(chain.Solana); got.ID != chains.SolanaMainnet.ID {
		t.Fatalf("unselected Solana = %q", got.ID)
	}
	if got := s.NetworkFor(chain.EVM); got.ID != chains.Ethereum.ID {
		t.Fatalf("selected EVM = %q", got.ID)
	}
	s.NetworkID = chains.Base.ID
	if got := s.NetworkFor(chain.EVM); got.ID != chains.Base.ID {
		t.Fatalf("selected Base fell through to %q", got.ID)
	}
	s.NetworkID = chains.NearTestnet.ID
	if got := s.NetworkFor(chain.Near); got.ID != chains.NearTestnet.ID {
		t.Fatalf("selected testnet = %q", got.ID)
	}
	if got := s.NetworkFor(chain.EVM); got.ID != chains.Ethereum.ID {
		t.Fatalf("unselected EVM = %q", got.ID)
	}
}

func TestCustomRPCStaysOnItsNetwork(t *testing.T) {
	a := newTestApp(t)
	a.settings.RPCURL = "https://custom.example/eth"
	a.settings.Accounts = []wallet.Address{
		{Addr: viewAddr},
		{Addr: viewSol},
		{Addr: "alice.testnet"},
	}
	var jobs []familyFetch
	a.startRefresh = func(_ int, got []familyFetch) { jobs = got }
	a.requestRefresh()
	evmNetworks := chains.NetworksOf(chain.EVM)
	if len(jobs) != len(evmNetworks)+2 {
		t.Fatalf("jobs = %d, want one per EVM network plus NEAR and Solana", len(jobs))
	}
	for _, job := range jobs {
		switch job.network.Family {
		case chain.EVM:
			got := job.reader.(*evm.Client).Endpoint().URL
			if job.network.ID == a.settings.NetworkID {
				if got != a.settings.RPCURL {
					t.Fatalf("selected endpoint = %q", got)
				}
				continue
			}
			if got != job.network.RPCURL {
				t.Fatalf("%s endpoint = %q, the custom Ethereum URL leaked", job.network.Name, got)
			}
		case chain.Solana:
			if got := job.reader.(*solana.Client).Endpoint().URL; got != chains.SolanaMainnet.RPCURL {
				t.Fatalf("Solana endpoint = %q, the custom Ethereum URL leaked", got)
			}
		case chain.Near:
			if job.network.ID != chains.NearMainnet.ID {
				t.Fatalf("NEAR job = %q, want mainnet", job.network.ID)
			}
		}
	}
	// Base is the same family as Ethereum and still keeps its own endpoint.
	a.settings.NetworkID = chains.Base.ID
	a.settings.RPCURL = "https://custom.example/base"
	base, err := a.settings.ReaderFor(chains.Ethereum)
	if err != nil {
		t.Fatal(err)
	}
	if got := base.(*evm.Client).Endpoint().URL; got != chains.Ethereum.RPCURL {
		t.Fatalf("Ethereum endpoint = %q while Base is selected", got)
	}
}

func TestFetchKeepsEachChainsBalance(t *testing.T) {
	a := newTestApp(t)
	evmAmt := big.NewInt(5)
	solAmt := big.NewInt(9)
	finishFetch(t, a, []familyFetch{
		{network: chains.Ethereum, reader: gateReader{native: evmAmt, block: 111}, addrs: []string{viewAddr}},
		{network: chains.SolanaMainnet, reader: gateReader{native: solAmt, block: 333}, addrs: []string{viewSol}},
	})
	if len(a.snap.accounts) != 2 {
		t.Fatalf("accounts = %+v", a.snap.accounts)
	}
	byAddr := map[string]accountSnapshot{}
	for _, acc := range a.snap.accounts {
		byAddr[acc.addr] = acc
	}
	eth, sol := byAddr[viewAddr], byAddr[viewSol]
	if eth.network.ID != chains.Ethereum.ID || eth.native.Cmp(evmAmt) != 0 || eth.block != 111 {
		t.Fatalf("ethereum account = %+v", eth)
	}
	if sol.network.ID != chains.SolanaMainnet.ID || sol.native.Cmp(solAmt) != 0 || sol.block != 333 {
		t.Fatalf("solana account = %+v", sol)
	}
	if a.snap.block != 111 {
		t.Fatalf("selected block = %d, want the Ethereum height", a.snap.block)
	}
}

func TestTotalOmitsFailuresTestnetsAndCase(t *testing.T) {
	a := newTestApp(t)
	one := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) // 1.0 of an 18-decimal asset
	tenNear := new(big.Int).Exp(big.NewInt(10), big.NewInt(25), nil)
	prices := map[string]float64{"ETH": 2000, "NEAR": 5, "CBETH": 2000}
	a.snap = &snapshot{
		usd: prices,
		accounts: []accountSnapshot{
			{addr: viewAddr, network: chains.Ethereum, native: one, tokens: []chain.TokenBalance{{
				Token:  chain.Token{Symbol: "cbETH", Decimals: 18},
				Amount: one,
			}}},
			{addr: viewSol, network: chains.SolanaMainnet, err: "rpc down", native: one},
			{addr: "alice.testnet", network: chains.NearTestnet, native: tenNear},
		},
	}
	total, ok := a.totalUSD()
	if !ok || total != 4000 {
		t.Fatalf("total = %v ok=%v, want 4000 (ETH + cbETH); a failed chain and a testnet must be left out", total, ok)
	}
	if !a.totalsPartial() {
		t.Fatal("a failed chain did not mark the total incomplete")
	}
	// Every counted chain failed: the headline is an em dash, not $0.00,
	// and there is no partial total to call incomplete.
	a.snap.accounts = []accountSnapshot{
		{addr: viewAddr, network: chains.Ethereum, err: "down"},
		{addr: viewSol, network: chains.SolanaMainnet, err: "down"},
		{addr: "alice.near", network: chains.NearMainnet, err: "down"},
	}
	if total, ok = a.totalUSD(); ok || total != 0 {
		t.Fatalf("all failed: total = %v ok=%v, want no total", total, ok)
	}
	if a.totalsPartial() {
		t.Fatal("every chain failing was called incomplete")
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if tt.HasText("$0.00") || tt.HasText("Incomplete") || !tt.HasText("—") {
		t.Fatalf("all-fail headline = %q", tt.Texts())
	}
	// Decimals are applied: 1 wei at $2000 is not $2000.
	if got := usdValue(big.NewInt(1), 18, "ETH", prices); got >= 0.01 {
		t.Fatalf("1 wei = %v, decimals were ignored", got)
	}
	a.settings.HideFiat = true
	a.snap.accounts = []accountSnapshot{{addr: viewAddr, network: chains.Ethereum, native: one}}
	if total, ok = a.totalUSD(); !ok || total != 2000 {
		t.Fatalf("HideFiat changed the total to %v ok=%v", total, ok)
	}
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	if tt.HasText("$2,000.00") {
		t.Fatal("HideFiat still shows the total")
	}
}

func TestPartialTotalShowsIncomplete(t *testing.T) {
	a := newTestApp(t)
	one := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr, Label: "Main"}, {Addr: viewSol, Label: "Sol"}}
	a.snap = &snapshot{
		usd: map[string]float64{"ETH": 2000, "SOL": 100},
		accounts: []accountSnapshot{
			{addr: viewAddr, network: chains.Ethereum, native: one},
			{addr: viewSol, network: chains.SolanaMainnet, err: "timeout"},
		},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("$2,000.00") || !tt.HasText("Incomplete") {
		t.Fatalf("partial headline = %q", tt.Texts())
	}
	if tt.HasText("0 SOL") || tt.HasText("0.0000 SOL") {
		t.Fatalf("a failed chain was shown as a zero balance: %q", tt.Texts())
	}
}

func TestCardAndAPIUseTheAccountsChain(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr, Label: "Main"}, {Addr: viewSol, Label: "Sol"}}
	a.snap = &snapshot{
		block: 111,
		usd:   map[string]float64{"ETH": 2000, "SOL": 100},
		accounts: []accountSnapshot{
			{addr: viewAddr, network: chains.Ethereum, native: big.NewInt(1_000_000_000_000_000_000), block: 111},
			{addr: viewSol, network: chains.SolanaMainnet, native: big.NewInt(2_500_000_000), block: 333},
		},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("2.5 SOL") || !tt.HasText("Solana") {
		t.Fatalf("solana card = %q", tt.Texts())
	}
	p := a.Portfolio()
	if p.ChainID != chains.Ethereum.ID || p.Block != 111 {
		t.Fatalf("selected header = %+v", p)
	}
	if len(p.Accounts) != 2 {
		t.Fatalf("accounts = %+v", p.Accounts)
	}
	sol := p.Accounts[1]
	if sol.ChainID != chains.SolanaMainnet.ID || sol.Network != "Solana" ||
		sol.Native.Symbol != "SOL" || sol.Native.Decimals != 9 || sol.Block != 333 {
		t.Fatalf("solana API account = %+v", sol)
	}
	if p.Accounts[0].ChainID != chains.Ethereum.ID || p.Accounts[0].Block != 111 || p.Accounts[0].Native.Decimals != 18 {
		t.Fatalf("ethereum API account = %+v", p.Accounts[0])
	}
}

func TestBlockFollowsTheSelectedFamily(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.NearMainnet.ID
	a.settings.Passkey = &PasskeyAccount{ID: "cred", PublicKey: "p256:AA", NEARAccount: "alice.near", SolanaAddress: "vault"}
	finishFetch(t, a, []familyFetch{
		{network: chains.Ethereum, reader: gateReader{native: big.NewInt(1), block: 111}, addrs: []string{viewAddr}},
		{network: chains.NearMainnet, reader: gateReader{native: big.NewInt(2), block: 222}, addrs: []string{"alice.near"}},
		{network: chains.SolanaMainnet, reader: gateReader{native: big.NewInt(3), block: 333, exists: true}, addrs: []string{viewSol}},
	})
	if a.snap.block != 222 {
		t.Fatalf("block = %d, want the selected NEAR height, not the first job", a.snap.block)
	}
	p := a.Portfolio()
	if p.ChainID != chains.NearMainnet.ID || p.Block != 222 {
		t.Fatalf("API header = %+v", p)
	}
	blocks := map[string]uint64{}
	for _, acc := range p.Accounts {
		blocks[acc.ChainID] = acc.Block
		if acc.ChainID == "" || acc.Network == "" {
			t.Fatalf("account has no chain: %+v", acc)
		}
	}
	if blocks[chains.Ethereum.ID] != 111 || blocks[chains.NearMainnet.ID] != 222 || blocks[chains.SolanaMainnet.ID] != 333 {
		t.Fatalf("per-account blocks = %v", blocks)
	}
	// Solana's reader would say the vault exists, but Solana is not selected.
	if a.snap.vault != vaultUnknown {
		t.Fatalf("vault = %v, a non-Solana selection must leave it unknown", a.snap.vault)
	}
}

func TestVaultCheckUsesTheSelectedSolanaReader(t *testing.T) {
	a := newTestApp(t)
	a.settings.NetworkID = chains.SolanaMainnet.ID
	a.settings.Passkey = &PasskeyAccount{ID: "cred", PublicKey: "p256:AA", NEARAccount: "0s.test", SolanaAddress: "vault"}
	finishFetch(t, a, []familyFetch{
		{network: chains.SolanaMainnet, reader: gateReader{native: big.NewInt(1), block: 7, exists: true}, addrs: []string{viewSol}},
	})
	if a.snap.vault != vaultExists {
		t.Fatalf("vault = %v, want exists", a.snap.vault)
	}
}

func TestQueuedNetworkDoesNotWaitASecondTimeout(t *testing.T) {
	if familyReadTimeout != 90*time.Second {
		t.Fatalf("family timeout = %s, want 90s", familyReadTimeout)
	}
	previous := familyReadTimeout
	familyReadTimeout = 250 * time.Millisecond
	t.Cleanup(func() { familyReadTimeout = previous })
	a := newTestApp(t)
	jobs := make([]familyFetch, maxRefreshConcurrency+1)
	for i := range jobs {
		jobs[i] = familyFetch{
			network: chain.Network{ID: "job-" + string(rune('a'+i)), Family: chain.EVM},
			addrs:   []string{viewAddr},
			reader: gateReader{account: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			}},
		}
	}
	start := time.Now()
	finishFetch(t, a, jobs)
	if elapsed := time.Since(start); elapsed > 450*time.Millisecond {
		t.Fatalf("refresh took %s; a job queued on the semaphore started another full timeout", elapsed)
	}
}

func TestOtherFamiliesAreNotStarvedByHungEVM(t *testing.T) {
	if familyReadTimeout != 90*time.Second {
		t.Fatalf("family timeout = %s, want 90s", familyReadTimeout)
	}
	previous := familyReadTimeout
	familyReadTimeout = 2 * time.Second
	t.Cleanup(func() { familyReadTimeout = previous })
	var entered sync.WaitGroup
	entered.Add(maxRefreshConcurrency + 2)
	hold := make(chan struct{})
	var release sync.Once
	closeHold := func() { release.Do(func() { close(hold) }) }
	t.Cleanup(closeHold)
	block := func(ctx context.Context) error {
		entered.Done()
		select {
		case <-hold:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	start := func(context.Context) error {
		entered.Done()
		return nil
	}
	jobs := make([]familyFetch, 0, maxRefreshConcurrency+2)
	for i := 0; i < maxRefreshConcurrency; i++ {
		jobs = append(jobs, familyFetch{
			network: chain.Network{ID: "eip155:" + string(rune('1'+i)), Family: chain.EVM},
			addrs:   []string{viewAddr},
			reader:  gateReader{account: block},
		})
	}
	jobs = append(jobs,
		familyFetch{network: chain.Network{ID: "near:mainnet", Family: chain.Near}, addrs: []string{"alice.near"}, reader: gateReader{account: start}},
		familyFetch{network: chain.Network{ID: "solana:mainnet", Family: chain.Solana}, addrs: []string{"sol"}, reader: gateReader{account: start}},
	)
	a := newTestApp(t)
	done := make(chan struct{})
	previousUI := runOnUI
	runOnUI = func(fn func()) {
		fn()
		select {
		case <-done:
		default:
			close(done)
		}
	}
	t.Cleanup(func() { runOnUI = previousUI })
	a.fetch(a.gen, jobs)
	allEntered := make(chan struct{})
	go func() {
		entered.Wait()
		close(allEntered)
	}()
	select {
	case <-allEntered:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("NEAR or Solana did not start while the EVM slots were full")
	}
	closeHold()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not finish")
	}
}

func TestFamiliesReadConcurrently(t *testing.T) {
	if familyReadTimeout != 90*time.Second {
		t.Fatalf("family timeout = %s, want 90s", familyReadTimeout)
	}
	previous := familyReadTimeout
	familyReadTimeout = 400 * time.Millisecond
	t.Cleanup(func() { familyReadTimeout = previous })

	var evmAt, nearAt atomic.Int64
	a := newTestApp(t)
	finishFetch(t, a, []familyFetch{
		{network: chains.Ethereum, addrs: []string{viewAddr}, reader: gateReader{
			block: 111,
			account: func(ctx context.Context) error {
				evmAt.Store(time.Now().UnixNano())
				<-ctx.Done()
				return ctx.Err()
			},
		}},
		{network: chains.NearMainnet, addrs: []string{"alice.near"}, reader: gateReader{
			native: big.NewInt(4),
			block:  222,
			account: func(ctx context.Context) error {
				nearAt.Store(time.Now().UnixNano())
				return ctx.Err()
			},
		}},
	})
	if evmAt.Load() == 0 || nearAt.Load() == 0 {
		t.Fatal("a family was not read")
	}
	if nearAt.Load() > evmAt.Load()+150*int64(time.Millisecond) {
		t.Fatalf("NEAR started %s after the stuck EVM read", time.Duration(nearAt.Load()-evmAt.Load()))
	}
	var near accountSnapshot
	for _, acc := range a.snap.accounts {
		if acc.addr == "alice.near" {
			near = acc
		}
	}
	if near.err != "" || near.native == nil || near.native.Cmp(big.NewInt(4)) != 0 {
		t.Fatalf("NEAR was stalled by the EVM read: %+v", near)
	}
	var eth accountSnapshot
	for _, acc := range a.snap.accounts {
		if acc.addr == viewAddr {
			eth = acc
		}
	}
	if eth.err == "" {
		t.Fatal("the stuck EVM read did not time out")
	}
}
