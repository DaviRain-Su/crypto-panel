package app

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/ethereum/go-ethereum/common"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/evmtx"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/neartx"
	"github.com/davirain-su/crypto-panel/internal/plugin"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// signDeps is what signing one transaction needs. The app fills it in;
// tests replace the parts that touch the window and the keychain.
type signDeps struct {
	pluginName string
	// account picks the wallet account to sign with, or fails when none
	// can.
	account func(from string) (addr, label string, err error)
	// key reads an account's key, only while it is unlocked.
	key func(addr string) (*ecdsa.PrivateKey, bool)
	// approve shows the shell's decode and reports the user's answer.
	approve func(approvalRequest) bool
	rpc     evmtx.RPC
}

// signForPlugin is host.requestSign: it prepares the transaction the plugin
// asked for, shows the shell's own decode, and signs and broadcasts only
// what the user approved. It runs on the plugin's call goroutine, so the
// network work never touches the main thread.
func (a *App) signForPlugin(m plugin.Manifest, readers map[string]chain.Reader, req plugin.SignRequest) (plugin.SignResult, error) {
	reader, ok := readers[req.ChainID]
	if !ok {
		return plugin.SignResult{}, fmt.Errorf("chain %q is not available", req.ChainID)
	}
	network, ok := chains.ByID(req.ChainID)
	if !ok {
		return plugin.SignResult{}, fmt.Errorf("chain %q is not one this app knows", req.ChainID)
	}
	return signTransaction(signDeps{
		pluginName: m.Name,
		account:    a.signAccount,
		key:        a.signKey,
		approve:    a.askApproval,
		rpc: func(ctx context.Context, method string, params any) (json.RawMessage, error) {
			return reader.RPC(ctx, method, params)
		},
	}, network, req)
}

// signTransaction prepares, asks, and only then signs and sends. Nothing is
// signed without approve returning true.
func signTransaction(deps signDeps, network chain.Network, req plugin.SignRequest) (plugin.SignResult, error) {
	addr, label, err := deps.account(req.From)
	if err != nil {
		return plugin.SignResult{}, err
	}

	prepareCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	tx, sum, err := evmtx.Prepare(prepareCtx, deps.rpc, common.HexToAddress(addr), evmtx.Request{
		ChainID:              req.ChainID,
		To:                   req.To,
		Value:                req.Value,
		Data:                 req.Data,
		Gas:                  req.Gas,
		MaxFeePerGas:         req.MaxFeePerGas,
		MaxPriorityFeePerGas: req.MaxPriorityFeePerGas,
		Nonce:                req.Nonce,
	})
	cancel()
	if err != nil {
		return plugin.SignResult{}, err
	}

	if !deps.approve(approvalRequest{
		pluginName:  deps.pluginName,
		description: req.Description,
		network:     network,
		summary:     sum,
		fromAddr:    addr,
		fromLabel:   label,
	}) {
		return plugin.SignResult{}, errors.New("the user rejected the transaction")
	}

	// The account may have been locked while the window was open.
	key, ok := deps.key(addr)
	if !ok {
		return plugin.SignResult{}, errors.New("the account was locked before it could sign")
	}
	raw, hash, err := evmtx.Sign(key, tx)
	if err != nil {
		return plugin.SignResult{}, err
	}
	sendCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	sent, err := evmtx.Send(sendCtx, deps.rpc, raw)
	if err != nil {
		return plugin.SignResult{}, err
	}
	if sent != hash {
		// The hash is a function of the signed bytes; a node that reports
		// another one is not telling the truth about this transaction.
		return plugin.SignResult{}, fmt.Errorf("the node reported %s, but the signed transaction is %s", sent.Hex(), hash.Hex())
	}
	return plugin.SignResult{Hash: hash.Hex()}, nil
}

// signAccount picks the wallet account to sign with, on the main thread:
// the one the plugin named, or the first unlocked one. Only an unlocked
// account can sign, so the approval never promises a signature the shell
// cannot make.
func (a *App) signAccount(from string) (addr, label string, err error) {
	mygo.RunOnMain(func() {
		if a.keyring == nil {
			err = errors.New("the wallet is unavailable")
			return
		}
		if from != "" {
			normalized, nerr := wallet.Normalize(wallet.EVM, from)
			if nerr != nil {
				err = errors.New("that account is not an EVM address")
				return
			}
			if !a.settings.IsWallet(normalized) {
				err = errors.New("that account is not in the wallet")
				return
			}
			if !a.keyring.Unlocked(normalized) {
				err = errors.New("that account is locked")
				return
			}
			addr = normalized
		} else {
			for _, acc := range a.settings.Wallet {
				if a.keyring.Unlocked(acc.Addr) {
					addr = acc.Addr
					break
				}
			}
			if addr == "" {
				err = errors.New("no unlocked wallet account can sign")
				return
			}
		}
		for _, acc := range a.settings.Wallet {
			if acc.Addr == addr {
				label = acc.Label
				return
			}
		}
	})
	return addr, label, err
}

// signKey reads addr's key, on the main thread, only while it is unlocked.
func (a *App) signKey(addr string) (*ecdsa.PrivateKey, bool) {
	var (
		key *ecdsa.PrivateKey
		ok  bool
	)
	mygo.RunOnMain(func() {
		if a.keyring == nil {
			return
		}
		key, ok = a.keyring.Key(addr)
	})
	return key, ok
}

// approvalRequest is everything the approval window shows.
type approvalRequest struct {
	pluginName  string
	sourceKind  string // "" for a plugin, "dapp" for a web page
	description string
	network     chain.Network
	summary     *evmtx.Summary
	near        *neartx.Summary
	fromAddr    string
	fromLabel   string
}

// askApproval opens the approval window and waits for the user. It runs on
// the plugin's call goroutine; the window itself is created on the main
// thread. One approval shows at a time.
func (a *App) askApproval(req approvalRequest) bool {
	a.approvalMu.Lock()
	defer a.approvalMu.Unlock()
	decided := make(chan bool, 1)
	var win *mygo.Window
	mygo.RunOnMain(func() {
		if a.win == nil {
			decided <- false
			return
		}
		ap := &approval{
			t:            a.t,
			pluginName:   req.pluginName,
			sourceKind:   req.sourceKind,
			description:  req.description,
			network:      req.network,
			summary:      req.summary,
			near:         req.near,
			fromAddr:     req.fromAddr,
			fromLabel:    req.fromLabel,
			authRequired: a.presenceRequired(),
			authName:     a.authName(),
			authenticate: a.authenticate,
			onDecide:     func(ok bool) { decided <- ok },
		}
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:           a.t(i18n.ApproveWindowTitle),
			Width:           460,
			Height:          560,
			MinWidth:        420,
			MinHeight:       480,
			Parent:          a.win,
			Modal:           true,
			BackgroundColor: "light-dark(#f6f4ef, #141513)",
			Content:         ui.View(ap.view),
		})
		ap.invalidate = func() { win.Invalidate() }
		win.OnClose(func(*mygo.CloseEvent) { ap.decide(false) })
		win.Focus()
	})
	ok := <-decided
	mygo.RunOnMain(func() {
		if win != nil {
			win.Close()
		}
	})
	return ok
}

// openPasskeyApproval opens the shell's approval window for a passkey deploy
// or send. The closer rejects a late click and destroys the window, so a
// canceled operation does not leave the prompt on screen.
func (a *App) openPasskeyApproval(req approvalRequest, done func(bool)) func() {
	ap := &approval{
		t:            a.t,
		pluginName:   req.pluginName,
		sourceKind:   req.sourceKind,
		description:  req.description,
		network:      req.network,
		summary:      req.summary,
		near:         req.near,
		fromAddr:     req.fromAddr,
		fromLabel:    req.fromLabel,
		authRequired: a.presenceRequired(),
		authName:     a.authName(),
		authenticate: a.authenticate,
		onDecide:     done,
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:           a.t(i18n.ApproveWindowTitle),
		Width:           460,
		Height:          560,
		MinWidth:        420,
		MinHeight:       480,
		Parent:          a.win,
		Modal:           true,
		BackgroundColor: "light-dark(#f6f4ef, #141513)",
		Content:         ui.View(ap.view),
	})
	ap.invalidate = func() { win.Invalidate() }
	win.OnClose(func(*mygo.CloseEvent) { ap.decide(false) })
	win.Focus()
	return func() {
		ap.decide(false) // A late system confirmation cannot revive the request.
		win.Destroy()
	}
}
