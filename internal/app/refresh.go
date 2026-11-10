package app

import (
	"context"
	"errors"
	"log"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/wallet"
)

const maxRefreshConcurrency = 4

// familyReadTimeout is how long one network's read may take. Each job has
// its own, so a stuck endpoint cannot cancel the networks beside it.
var familyReadTimeout = 90 * time.Second

// accountSnapshot is one address's balances in one refresh. network is the
// preset the balances were read on. The same address is watched once per
// family and read on every network of that family that this refresh covers.
type accountSnapshot struct {
	addr    string
	network chain.Network
	native  *big.Int
	tokens  []chain.TokenBalance
	err     string
	// block is the height this network's reader reported for this refresh.
	block uint64
}

// familyFetch is the work one network contributes to a refresh.
type familyFetch struct {
	network chain.Network
	reader  chain.Reader
	addrs   []string
}

// familyResult is what one network's read produced. The goroutines write
// disjoint slots, and the refresh reads them only after every job returns.
type familyResult struct {
	accounts []accountSnapshot
	block    uint64
	err      error
}

// vaultState says whether the passkey's Solana vault address already has an
// account on the selected network. The zero value is unknown: no answer yet,
// or the selected network is not Solana.
type vaultState int

const (
	vaultUnknown vaultState = iota
	vaultAbsent
	vaultExists
)

// accountExister is a reader that can say whether an account exists. The
// Solana client implements it; the interface keeps fetch honest about what
// it may ask.
type accountExister interface {
	AccountExists(ctx context.Context, addr string) (bool, error)
}

// snapshot is everything one refresh learned.
type snapshot struct {
	accounts []accountSnapshot
	block    uint64
	at       time.Time
	// usd prices explicit network/asset identities for the approximate fiat
	// values beside balances. fiatErr says why it is empty.
	usd     map[string]float64
	fiatErr string
	// vault is the passkey's Solana vault: whether the derived address
	// already has an account on this network. Someone else may have created
	// that account first, so an existing vault must not be shown as the
	// user's.
	vault vaultState
}

// runOnUI delivers a finished refresh to the main thread. Tests replace it.
var runOnUI = mygo.RunOnMain

// priceSource prices assets in USD by their curated feed identity.
// *prices.Client implements it.
type priceSource interface {
	PricesByID(ctx context.Context, assetIDs []string) (map[string]float64, error)
}

// requestRefresh starts a refresh unless one is running. It runs on the
// main thread.
func (a *App) requestRefresh() {
	if a.route.kind == "plugin" {
		a.loadPluginPage()
	}
	if a.loading {
		return
	}
	jobs := a.refreshJobs()
	if len(jobs) == 0 {
		a.snap = &snapshot{at: time.Now()}
		a.lastErr = ""
		a.lastUpdated = time.Time{}
		a.invalidate()
		return
	}
	a.loading = true
	a.lastErr = ""
	gen := a.gen
	a.startRefresh(gen, jobs)
	a.invalidate()
}

// refreshJobs builds one independent job per network with watched addresses.
// Every EVM preset reads the same watched EVM addresses. A non-EVM family
// uses NetworkFor, which is the selected network or that family's mainnet.
func (a *App) refreshJobs() []familyFetch {
	var jobs []familyFetch
	for _, family := range chains.Families() {
		accounts := a.settings.WatchedOf(family)
		if len(accounts) == 0 {
			continue
		}
		networks := []chain.Network{a.settings.NetworkFor(family)}
		if family == chain.EVM {
			networks = chains.NetworksOf(chain.EVM)
		}
		addrs := make([]string, len(accounts))
		for i, acc := range accounts {
			addrs[i] = acc.Addr
		}
		for _, network := range networks {
			if network.ID == "" {
				continue
			}
			reader, err := a.settings.ReaderFor(network)
			if err != nil {
				log.Printf("lintel: %v", err)
				continue
			}
			jobs = append(jobs, familyFetch{network: network, reader: reader, addrs: addrs})
		}
	}
	return jobs
}

// fetch reads every address on every network in the background and hands the
// snapshot back to the main thread. At most maxRefreshConcurrency reads run
// at once, and each one has its own timeout.
func (a *App) fetch(gen int, jobs []familyFetch) {
	source := a.priceSource
	// The vault check is best-effort and only applies to the selected
	// network. Another network's reader must not answer it: when Solana is
	// not selected, the state stays unknown and the balance stays hidden.
	selected := a.network()
	var vaultAddr string
	var exister accountExister
	if pk := a.settings.Passkey; pk != nil && pk.SolanaAddress != "" && selected.Family == chain.Solana {
		vaultAddr = pk.SolanaAddress
		for _, job := range jobs {
			if job.network.ID != selected.ID {
				continue
			}
			if ex, ok := job.reader.(accountExister); ok {
				exister = ex
			}
			break
		}
	}
	go func() {
		results := make([]familyResult, len(jobs))
		var wg sync.WaitGroup
		sem := make(chan struct{}, maxRefreshConcurrency)
		for i, job := range jobs {
			wg.Add(1)
			go func(i int, job familyFetch) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ctx, cancel := context.WithTimeout(context.Background(), familyReadTimeout)
				defer cancel()
				var res familyResult
				for _, addr := range job.addrs {
					acct, err := job.reader.Account(ctx, addr)
					s := accountSnapshot{addr: addr, network: job.network}
					if err != nil {
						s.err = err.Error()
						if res.err == nil {
							res.err = err
						}
					} else {
						s.native = acct.Native
						s.tokens = acct.Tokens
						if len(acct.Warnings) > 0 && res.err == nil {
							res.err = errors.New(acct.Warnings[0])
						}
					}
					res.accounts = append(res.accounts, s)
				}
				if block, err := job.reader.Block(ctx); err == nil {
					res.block = block
					for j := range res.accounts {
						res.accounts[j].block = block
					}
				}
				results[i] = res
			}(i, job)
		}
		wg.Wait()
		snap := &snapshot{at: time.Now()}
		var firstErr error
		for i, res := range results {
			snap.accounts = append(snap.accounts, res.accounts...)
			if firstErr == nil && res.err != nil {
				firstErr = res.err
			}
			if jobs[i].network.ID == selected.ID {
				snap.block = res.block
			}
		}
		order := make(map[string]int, len(jobs))
		for i, job := range jobs {
			order[job.network.ID] = i
		}
		sort.Slice(snap.accounts, func(i, j int) bool {
			left, right := snap.accounts[i], snap.accounts[j]
			if order[left.network.ID] != order[right.network.ID] {
				return order[left.network.ID] < order[right.network.ID]
			}
			return left.addr < right.addr
		})
		if exister != nil && vaultAddr != "" {
			ctx, cancel := context.WithTimeout(context.Background(), familyReadTimeout)
			exists, err := exister.AccountExists(ctx, vaultAddr)
			cancel()
			switch {
			case err != nil:
				// Leave the check unknown. The card must not treat a failed
				// lookup as "this address is free" and show a balance.
				snap.vault = vaultUnknown
			case exists:
				snap.vault = vaultExists
			default:
				snap.vault = vaultAbsent
			}
		}
		if source != nil {
			ctx, cancel := context.WithTimeout(context.Background(), familyReadTimeout)
			var ids []string
			seen := map[string]bool{}
			add := func(id string) {
				if id != "" && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			for _, job := range jobs {
				add(job.network.PriceID)
				for _, token := range job.network.Tokens {
					add(token.PriceID)
				}
			}
			if usd, err := source.PricesByID(ctx, ids); err == nil {
				snap.usd = make(map[string]float64)
				for _, job := range jobs {
					putPrice(snap.usd, nativePriceKey(job.network), usd, job.network.PriceID)
					for _, token := range job.network.Tokens {
						putPrice(snap.usd, tokenPriceKey(job.network, token), usd, token.PriceID)
					}
				}
			} else {
				snap.fiatErr = err.Error()
			}
			cancel()
		}
		runOnUI(func() {
			a.loading = false
			if gen != a.gen {
				// The user changed what is watched while this ran; its
				// answer is about the old set.
				a.requestRefresh()
				return
			}
			a.snap = snap
			a.lastUpdated = snap.at
			if firstErr != nil {
				a.lastErr = firstErr.Error()
			} else {
				a.lastErr = ""
			}
			a.invalidate()
		})
	}()
}

// refreshLoop asks for a refresh every RefreshSeconds, re-reading the
// interval each time so a change in the settings applies at once.
func (a *App) refreshLoop() {
	for {
		var d time.Duration
		mygo.RunOnMain(func() {
			d = time.Duration(a.settings.RefreshSeconds) * time.Second
			if d <= 0 {
				d = DefaultRefreshSeconds * time.Second
			}
		})
		time.Sleep(d)
		mygo.RunOnMain(func() {
			a.lockIfIdle()
			a.requestRefresh()
			a.updateTray()
		})
	}
}

// countsInTotal reports that this account belongs in the fiat total.
// A testnet balance is never priced in, even when that testnet is selected.
func countsInTotal(acc accountSnapshot) bool {
	return !testnetNetwork(acc.network)
}

// totalsPartial reports that the headline total leaves some mainnet address
// out: its refresh failed, or it has no balance yet while another does.
// Every chain failing is not partial — there is no total to qualify.
func (a *App) totalsPartial() bool {
	if a.snap == nil {
		return false
	}
	known := false
	missing := false
	for _, acc := range a.snap.accounts {
		if !countsInTotal(acc) {
			continue
		}
		if acc.err != "" || acc.native == nil {
			missing = true
			continue
		}
		known = true
	}
	return known && missing
}

// totalUSD values every mainnet asset the last snapshot holds, in USD.
// A chain whose read failed is left out. When every counted chain failed,
// or nothing was priced, ok is false so the headline stays an em dash
// instead of $0.00. HideFiat does not change this sum; the view hides it.
// A balance is priced by its network identity first, then by its symbol, so
// a curated feed and an upper-case symbol such as CBETH both count.
func (a *App) totalUSD() (float64, bool) {
	if a.snap == nil || len(a.snap.usd) == 0 {
		return 0, false
	}
	included := false
	total := 0.0
	for _, acc := range a.snap.accounts {
		if !countsInTotal(acc) || acc.err != "" || acc.native == nil {
			continue
		}
		included = true
		total += usdAmount(acc.native, acc.network.Decimals, a.snap.usd, nativePriceKey(acc.network), acc.network.Symbol)
		for _, tb := range acc.tokens {
			total += usdAmount(tb.Amount, tb.Token.Decimals, a.snap.usd, tokenPriceKey(acc.network, tb.Token), tb.Token.Symbol)
		}
	}
	if !included {
		return 0, false
	}
	return total, true
}

func nativePriceKey(network chain.Network) string { return network.ID + ":native" }

func tokenPriceKey(network chain.Network, token chain.Token) string {
	return network.ID + ":token:" + strings.ToLower(strings.TrimSpace(token.Address))
}

func putPrice(out map[string]float64, key string, prices map[string]float64, id string) {
	if id != "" && prices[id] > 0 {
		out[key] = prices[id]
	}
}

// usdAmount is amount's worth at the first key that has a price. The symbol
// is the fallback, matched in either case.
func usdAmount(amount *big.Int, decimals int, usd map[string]float64, keys ...string) float64 {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if usd[key] > 0 {
			return usdValue(amount, decimals, key, usd)
		}
		if upper := strings.ToUpper(key); usd[upper] > 0 {
			return usdValue(amount, decimals, upper, usd)
		}
	}
	return 0
}

// usdValue is amount's worth at the key's price, or zero when the price is
// missing: a balance is never invented. Price keys are also tried in upper
// case, so cbETH matches CBETH.
func usdValue(amount *big.Int, decimals int, symbol string, usd map[string]float64) float64 {
	if amount == nil || amount.Sign() == 0 {
		return 0
	}
	price := usd[symbol]
	if price <= 0 {
		price = usd[strings.ToUpper(symbol)]
	}
	if price <= 0 {
		return 0
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	value := new(big.Float).SetInt(amount)
	value.Quo(value, new(big.Float).SetInt(scale))
	value.Mul(value, big.NewFloat(price))
	out, _ := value.Float64()
	return out
}

// accountSnapshot returns the balance for one network/address identity.
// The optional network keeps legacy selected-network callers working while
// the portfolio supplies it for every row.
func (a *App) accountSnapshot(addr string, networkID ...string) *accountSnapshot {
	if a.snap == nil {
		return nil
	}
	wantNetwork := ""
	if len(networkID) > 0 {
		wantNetwork = networkID[0]
	} else {
		wantNetwork = a.network().ID
	}
	wantAddr := addr
	if family, ok := addressFamily(addr); ok {
		if normalized, err := wallet.Normalize(wallet.Kind(family), addr); err == nil {
			wantAddr = normalized
		}
	}
	for i := range a.snap.accounts {
		snap := &a.snap.accounts[i]
		gotAddr := snap.addr
		if family, ok := addressFamily(gotAddr); ok {
			if normalized, err := wallet.Normalize(wallet.Kind(family), gotAddr); err == nil {
				gotAddr = normalized
			}
		}
		if gotAddr == wantAddr && (snap.network.ID == wantNetwork || snap.network.ID == "") {
			return snap
		}
	}
	return nil
}
