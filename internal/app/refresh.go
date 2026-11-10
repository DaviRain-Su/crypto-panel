package app

import (
	"context"
	"errors"
	"fmt"
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

// accountSnapshot is one address's balances in one refresh. network is the
// preset the balances were read on: an address is watched once, but its
// balances are per chain.
type accountSnapshot struct {
	addr    string
	network chain.Network
	native  *big.Int
	tokens  []chain.TokenBalance
	err     string
}

// familyFetch is the work one network contributes to a refresh: its reader
// and the watched addresses of that network's family.
type familyFetch struct {
	network chain.Network
	reader  chain.Reader
	addrs   []string
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
// Every EVM preset reads the same watched EVM addresses; non-EVM families
// retain their selected-network behavior.
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

// fetch reads every address on every chain in the background and hands the
// snapshot back to the main thread.
func (a *App) fetch(gen int, jobs []familyFetch) {
	// Read the main-thread state before the goroutine starts.
	source := a.priceSource
	hideFiat := a.settings.HideFiat
	// The vault check is best-effort: it rides along with the refresh and
	// leaves the state unknown when the reader cannot answer. It belongs to
	// the Solana family's reader, when one is in this refresh.
	var vaultAddr string
	var exister accountExister
	if pk := a.settings.Passkey; pk != nil && pk.SolanaAddress != "" {
		vaultAddr = pk.SolanaAddress
		for _, job := range jobs {
			if job.network.Family != chain.Solana {
				continue
			}
			if ex, ok := job.reader.(accountExister); ok {
				exister = ex
				break
			}
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		snap := &snapshot{at: time.Now()}
		results := make(chan []accountSnapshot, len(jobs))
		sem := make(chan struct{}, maxRefreshConcurrency)
		var wg sync.WaitGroup
		for _, job := range jobs {
			job := job
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				var rows []accountSnapshot
				for _, addr := range job.addrs {
					s := accountSnapshot{addr: addr, network: job.network}
					acct, err := job.reader.Account(ctx, addr)
					if err != nil {
						s.err = fmt.Sprintf("%s: %v", job.network.Name, err)
					} else {
						s.native = acct.Native
						s.tokens = acct.Tokens
						if len(acct.Warnings) > 0 {
							s.err = fmt.Sprintf("%s: %s", job.network.Name, acct.Warnings[0])
						}
					}
					rows = append(rows, s)
				}
				results <- rows
			}()
		}
		wg.Wait()
		close(results)
		var firstErr error
		for rows := range results {
			for _, row := range rows {
				if row.err != "" && firstErr == nil {
					firstErr = errors.New(row.err)
				}
			}
			snap.accounts = append(snap.accounts, rows...)
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
			exists, err := exister.AccountExists(ctx, vaultAddr)
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
		if block, err := jobs[0].reader.Block(ctx); err == nil {
			snap.block = block
		}
		if source != nil && !hideFiat {
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
		})
	}
}

// totalsPartial reports that the headline total leaves some watched
// address out: its refresh failed, or it has no balance yet while another
// address does.
func (a *App) totalsPartial() bool {
	if a.snap == nil {
		return false
	}
	known := false
	missing := false
	for _, acc := range a.snap.accounts {
		if acc.err != "" || acc.native == nil {
			missing = true
			continue
		}
		known = true
	}
	return known && missing
}

// totalUSD values every asset the last snapshot holds, in USD: the native
// balances and tokens at the prices the refresh fetched. An asset no price
// names counts as zero, and ok is false when the last refresh priced
// nothing at all, so an empty total is never shown as a real number.
func (a *App) totalUSD() (float64, bool) {
	if a.snap == nil || a.settings.HideFiat || len(a.snap.usd) == 0 {
		return 0, false
	}
	total := 0.0
	for _, acc := range a.snap.accounts {
		if acc.native != nil {
			total += usdValue(acc.native, acc.network.Decimals, nativePriceKey(acc.network), a.snap.usd)
		}
		for _, tb := range acc.tokens {
			total += usdValue(tb.Amount, tb.Token.Decimals, tokenPriceKey(acc.network, tb.Token), a.snap.usd)
		}
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

// usdValue is amount's worth at the explicit asset key's price, or zero
// when the price is missing: a balance is never invented.
func usdValue(amount *big.Int, decimals int, symbol string, usd map[string]float64) float64 {
	if amount == nil || amount.Sign() == 0 {
		return 0
	}
	price := usd[symbol]
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
