package app

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/i18n"
)

// accountSnapshot is one address's balances in one refresh.
type accountSnapshot struct {
	addr   string
	native *big.Int
	tokens []chain.TokenBalance
	err    string
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
	network := a.network()
	accounts := a.settings.WatchedOf(network.Family)
	if len(accounts) == 0 {
		a.snap = &snapshot{at: time.Now()}
		a.lastErr = ""
		a.lastUpdated = time.Time{}
		a.invalidate()
		return
	}
	if a.reader == nil {
		a.lastErr = a.t(i18n.RefreshFailed)
		a.invalidate()
		return
	}
	a.loading = true
	a.lastErr = ""
	gen := a.gen
	addrs := make([]string, len(accounts))
	for i, acc := range accounts {
		addrs[i] = acc.Addr
	}
	a.startRefresh(gen, a.reader, addrs)
	a.invalidate()
}

// fetch reads every address in the background and hands the snapshot back
// to the main thread.
func (a *App) fetch(gen int, reader chain.Reader, addrs []string) {
	// Read the main-thread state before the goroutine starts.
	symbol := a.network().Symbol
	source := a.priceSource
	hideFiat := a.settings.HideFiat
	// The vault check is best-effort: it rides along with the refresh and
	// leaves the state unknown when the reader cannot answer.
	var vaultAddr string
	var exister accountExister
	if pk := a.settings.Passkey; pk != nil && pk.SolanaAddress != "" {
		vaultAddr = pk.SolanaAddress
		if ex, ok := reader.(accountExister); ok {
			exister = ex
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		snap := &snapshot{at: time.Now()}
		var firstErr error
		for _, addr := range addrs {
			acct, err := reader.Account(ctx, addr)
			s := accountSnapshot{addr: addr}
			if err != nil {
				s.err = err.Error()
				if firstErr == nil {
					firstErr = err
				}
			} else {
				s.native = acct.Native
				s.tokens = acct.Tokens
				if len(acct.Warnings) > 0 && firstErr == nil {
					firstErr = errors.New(acct.Warnings[0])
				}
			}
			snap.accounts = append(snap.accounts, s)
		}
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
		if block, err := reader.Block(ctx); err == nil {
			snap.block = block
		}
		if source != nil && !hideFiat {
			symbols := []string{symbol}
			seen := map[string]bool{symbol: true}
			for _, s := range snap.accounts {
				for _, tb := range s.tokens {
					if !seen[tb.Token.Symbol] {
						seen[tb.Token.Symbol] = true
						symbols = append(symbols, tb.Token.Symbol)
					}
				}
			}
			if usd, err := source.Prices(ctx, symbols); err == nil {
				snap.usd = usd
			} else {
				snap.fiatErr = err.Error()
			}
		}
		mygo.RunOnMain(func() {
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

// totalNative sums the native balances the last snapshot holds. ok is false
// when no account's balance is known, so an all-failed refresh is not shown
// as zero.
// totalsPartial reports that the headline total leaves some watched
// address out: its refresh failed, or it has no balance yet while another
// address does.
func (a *App) totalsPartial() bool {
	if a.snap == nil {
		return false
	}
	known := false
	missing := false
	for _, acc := range a.settings.WatchedOf(a.network().Family) {
		snap := a.accountSnapshot(acc.Addr)
		if snap == nil || snap.err != "" || snap.native == nil {
			missing = true
			continue
		}
		known = true
	}
	return known && missing
}

func (a *App) totalNative() (*big.Int, bool) {
	if a.snap == nil || len(a.snap.accounts) == 0 {
		return nil, false
	}
	total := new(big.Int)
	known := false
	for _, acc := range a.snap.accounts {
		if acc.native != nil {
			total.Add(total, acc.native)
			known = true
		}
	}
	return total, known
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
