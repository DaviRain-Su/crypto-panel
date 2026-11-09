package app

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

func (a *App) confirmDappConnect(ctx context.Context, origin string) bool {
	var confirm func(context.Context, string) bool
	a.onMain(func() {
		if a.dappBridge != nil {
			confirm = a.dappBridge.connect
		}
	})
	if ctx.Err() != nil {
		return false
	}
	if confirm != nil {
		return confirm(ctx, origin) && ctx.Err() == nil
	}
	return a.askDappConnect(ctx, origin)
}

func (a *App) approveDapp(ctx context.Context, req approvalRequest) bool {
	var approve func(context.Context, approvalRequest) bool
	a.onMain(func() {
		if a.dappBridge != nil {
			approve = a.dappBridge.approve
		}
	})
	if ctx.Err() != nil {
		return false
	}
	if approve != nil {
		return approve(ctx, req) && ctx.Err() == nil
	}
	return a.dappDecision(ctx, func(done func(bool)) func() {
		ap := &approval{
			t: a.t, pluginName: req.pluginName, sourceKind: "dapp", description: req.description,
			network: req.network, near: req.near, fromAddr: req.fromAddr, fromLabel: req.fromLabel,
			authRequired: a.presenceRequired(), authName: a.authName(), authenticate: a.authenticate,
			onDecide: done,
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: a.t(i18n.ApproveWindowTitle), Width: 460, Height: 560,
			MinWidth: 420, MinHeight: 480, Parent: a.win, Modal: true,
			BackgroundColor: "light-dark(#f6f4ef, #141513)", Content: ui.View(ap.view),
		})
		ap.invalidate = win.Invalidate
		win.OnClose(func(*mygo.CloseEvent) { ap.decide(false) })
		win.Focus()
		return func() {
			ap.decide(false) // 迟到的系统身份确认不能恢复该请求。
			win.Destroy()
		}
	})
}

func (a *App) askDappConnect(ctx context.Context, origin string) bool {
	return a.dappDecision(ctx, func(done func(bool)) func() {
		decision := &approval{onDecide: done}
		view := func(c *ui.Context) {
			p := paletteFor(c.Theme())
			c.Root().Background(p.bg)
			ui.Column(c).Fill().Padding(24).Gap(18).Children(func() {
				ui.Text(c, a.t(i18n.DappConnectTitle)).FontSize(20).FontWeight(700).MaxLines(2)
				ui.Text(c, a.t(i18n.DappConnectBody, origin)).FontSize(13).Grow(1).MaxLines(6)
				ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
					ui.Button(c, a.t(i18n.Cancel)).OnClick(func() { decision.decide(false) })
					ui.Button(c, a.t(i18n.DappConnect)).OnClick(func() { decision.decide(true) })
				})
			})
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: a.t(i18n.DappConnectTitle), Width: 440, Height: 280,
			Parent: a.win, Modal: true, BackgroundColor: "light-dark(#f6f4ef, #141513)",
			Content: ui.View(view),
		})
		win.OnClose(func(*mygo.CloseEvent) { decision.decide(false) })
		win.Focus()
		return func() { decision.decide(false); win.Destroy() }
	})
}

// dappDecision 共用 shell 的批准锁，等待期间可以取消，取消时销毁原生提示窗。
// 文档或请求失效后，不再留下等待 approvalMu 的废弃协程。
func (a *App) dappDecision(ctx context.Context, open func(func(bool)) func()) bool {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !a.approvalMu.TryLock() {
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
	defer a.approvalMu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	decided := make(chan bool, 1)
	done := func(ok bool) {
		select {
		case decided <- ok:
		default:
		}
	}
	var closePrompt func()
	a.onMain(func() {
		if ctx.Err() != nil || a.win == nil {
			done(false)
			return
		}
		closePrompt = open(done)
	})
	defer a.onMain(func() {
		if closePrompt != nil {
			closePrompt()
		}
	})
	select {
	case ok := <-decided:
		return ok && ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// dappSigningDeps 为共享 NEAR 签名器接入 DApp 的取消机制，插件和钱包仍使用原调用方式。
// 访问账户或私钥、原生批准以及每次 RPC（包括广播）前均检查取消状态，
// 已发起 RPC 的 context 也会取消，迟到的批准不能签名或发送过期请求。
func (a *App) dappSigningDeps(ctx context.Context, origin, description string, reader chain.Reader) nearSignDeps {
	return nearSignDeps{
		sourceName: origin, sourceKind: "dapp", description: description,
		account: func(from string) (wallet.NearAccount, error) {
			if err := ctx.Err(); err != nil {
				return wallet.NearAccount{}, err
			}
			return a.nearSignAccount(from)
		},
		key: func(id string) (ed25519.PrivateKey, bool) {
			if ctx.Err() != nil {
				return nil, false
			}
			key, ok := a.nearSignKey(id)
			return key, ok && ctx.Err() == nil
		},
		approve: func(req approvalRequest) bool { return a.approveDapp(ctx, req) },
		rpc: func(rpcCtx context.Context, method string, params any) (json.RawMessage, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// DApp 生命周期直接作为父 context，同步将取消状态传递到 RPC，
			// 包括广播前的最后检查边界。
			joined, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(rpcCtx, cancel)
			defer stop()
			defer cancel()
			if err := joined.Err(); err != nil {
				return nil, err
			}
			if err := rpcCtx.Err(); err != nil {
				return nil, err
			}
			return reader.RPC(joined, method, params)
		},
	}
}
