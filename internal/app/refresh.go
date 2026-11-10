package app

import (
	"context"
	"errors"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
)

// familyReadTimeout is how long one family's read may take. Each family
// has its own, so a stuck endpoint cannot cancel the chains beside it.
var familyReadTimeout = 90 * time.Second

// accountSnapshot is one address's balances in one refresh. network is the
// preset the balances were read on: an address is watched once, but its
// balances are per chain.
type accountSnapshot struct {
	addr    string
	network chain.Network
	native  *big.Int
	tokens  []chain.TokenBalance
	err     string
	// block is the height this family's reader reported for this refresh.
	block uint64
}

// familyFetch is the work one chain family contributes to a refresh: the
// network it is read on, its reader, and the addresses to read.
type familyFetch struct {
	network chain.Network
	reader  chain.Reader
	addrs   []string
}

// familyResult is what one family's read produced. The goroutines write
// disjoint slots, and the refresh reads them only after every family returns.
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
	// usd prices the symbols the snapshot holds, for the approximate fiat
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

// priceSource prices assets in USD. *prices.Client implements it.
type priceSource interface {
	Prices(ctx context.Context, symbols []string) (map[string]float64, error)
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
	// Every family with watched addresses contributes its own job, so the
	// portfolio reads all chains instead of only the selected network.
	var jobs []familyFetch
	for _, family := range chains.Families() {
		accounts := a.settings.WatchedOf(family)
		if len(accounts) == 0 {
			continue
		}
		network := a.settings.NetworkFor(family)
		if network.ID == "" {
			continue
		}
		reader, err := a.settings.ReaderFor(network)
		if err != nil {
			log.Printf("lintel: %v", err)
			continue
		}
		addrs := make([]string, len(accounts))
		for i, acc := range accounts {
			addrs[i] = acc.Addr
		}
		jobs = append(jobs, familyFetch{network: network, reader: reader, addrs: addrs})
	}
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

// fetch reads every address on every chain in the background and hands the
// snapshot back to the main thread.
func (a *App) fetch(gen int, jobs []familyFetch) {
	// Read the main-thread state before the goroutine starts.
	source := a.priceSource
	// The vault check is best-effort and only applies to the selected
	// network. Another family's reader must not answer it: when Solana is
	// not selected, the state stays unknown and the balance stays hidden.
	selected := a.network()
	var vaultAddr string
	var exister accountExister
	if pk := a.settings.Passkey; pk != nil && pk.SolanaAddress != "" && selected.Family == chain.Solana {
		vaultAddr = pk.SolanaAddress
		for _, job := range jobs {
			if job.network.ID == selected.ID {
				if ex, ok := job.reader.(accountExister); ok {
					exister = ex
				}
				break
			}
		}
	}
	go func() {
		results := make([]familyResult, len(jobs))
		var wg sync.WaitGroup
		for i, job := range jobs {
			wg.Add(1)
			go func(i int, job familyFetch) {
				defer wg.Done()
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
			var symbols []string
			seen := map[string]bool{}
			add := func(symbol string) {
				if symbol != "" && !seen[symbol] {
					seen[symbol] = true
					symbols = append(symbols, symbol)
				}
			}
			for _, job := range jobs {
				add(job.network.Symbol)
			}
			for _, s := range snap.accounts {
				for _, tb := range s.tokens {
					add(tb.Token.Symbol)
				}
			}
			if usd, err := source.Prices(ctx, symbols); err == nil {
				snap.usd = usd
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
		total += usdValue(acc.native, acc.network.Decimals, acc.network.Symbol, a.snap.usd)
		for _, tb := range acc.tokens {
			total += usdValue(tb.Amount, tb.Token.Decimals, tb.Token.Symbol, a.snap.usd)
		}
	}
	if !included {
		return 0, false
	}
	return total, true
}

// usdValue is amount's worth at the symbol's price, or zero when the price
// is missing: a balance is never invented. Price keys are upper case, so
// cbETH matches CBETH.
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

func (a *App) accountSnapshot(addr string) *accountSnapshot {
	if a.snap == nil {
		return nil
	}
	for i := range a.snap.accounts {
		if a.snap.accounts[i].addr == addr {
			return &a.snap.accounts[i]
		}
	}
	return nil
}
