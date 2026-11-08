package app

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/egoist/mygo"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
)

// accountSnapshot is one address's balances in one refresh.
type accountSnapshot struct {
	addr   string
	native *big.Int
	tokens []chain.TokenBalance
	err    string
}

// snapshot is everything one refresh learned.
type snapshot struct {
	accounts []accountSnapshot
	block    uint64
	at       time.Time
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
		if block, err := reader.Block(ctx); err == nil {
			snap.block = block
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
