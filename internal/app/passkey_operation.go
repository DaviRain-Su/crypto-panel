package app

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/wallet"
)

// passkeyOperation 由 UI 线程管理。工作协程只使用 context，
// 完成回调须先核对操作身份才能更新应用状态。
type passkeyOperation struct {
	ctx     context.Context
	cancel  context.CancelFunc
	sending bool
}

func (a *App) beginPasskeyOperation(sending bool) *passkeyOperation {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	op := &passkeyOperation{ctx: ctx, cancel: cancel, sending: sending}
	a.passkeyOp = op
	if sending {
		a.passkeySendBusy = true
	} else {
		a.passkeyBusy = true
	}
	return op
}

// cancelPasskeyOperations 在锁定、关闭、移除或切换网络时由 UI 线程调用。
// 随后解锁也不能恢复已经取消的操作。批准等待观察这个 context：取消会关掉
// 已经打开的原生批准窗，也会让还在排队的等待直接返回并放开 approvalMu。
func (a *App) cancelPasskeyOperations() {
	op := a.passkeyOp
	if op == nil {
		return
	}
	op.cancel()
	a.passkeyOp = nil
	if op.sending {
		a.passkeySendBusy = false
		a.passkeySendErr = a.t(i18n.PasskeyCanceled)
	} else {
		a.passkeyBusy = false
		a.passkeyErr = a.t(i18n.PasskeyCanceled)
	}
	a.invalidate()
}

// approvePasskey 等待 shell 的批准，并在 op 被取消时结束等待。测试通过
// a.approve 代替窗口。生产环境 a.approve 为空，走和 DApp 相同的可取消等待：
// 取消后不再打开提示，已打开的提示会被销毁，approvalMu 也会放开。
// 迟到的批准返回 false，调用方因此不会签名或广播；即便返回 true，
// checkPasskeyRelay 和 passkeyOp != op 仍会拒绝过期操作。
func (a *App) approvePasskey(op *passkeyOperation, req approvalRequest) bool {
	if a.approve != nil {
		if op.ctx.Err() != nil {
			return false
		}
		return a.approve(req) && op.ctx.Err() == nil
	}
	open := a.openApproval
	if open == nil {
		open = a.openPasskeyApproval
	}
	return a.approvalDecision(op.ctx, func(done func(bool)) func() {
		return open(req, done)
	})
}

func (a *App) finishPasskeyOperation(op *passkeyOperation, err error, done func(error)) {
	a.onMain(func() {
		if a.passkeyOp != op {
			return
		}
		if err == nil {
			err = op.ctx.Err()
		}
		a.passkeyOp = nil
		op.cancel()
		if op.sending {
			a.passkeySendBusy = false
		} else {
			a.passkeyBusy = false
		}
		done(err)
		a.invalidate()
	})
}

// passkeyRelayState 在批准前只冻结公开资料，浏览器或网络等待期间不保留 relayer 私钥。
type passkeyRelayState struct {
	passkey   PasskeyAccount
	networkID string
	rpcURL    string
	relayer   wallet.NearAccount
}

// checkPasskeyRelay 在 UI 线程确认凭据身份与所选 relayer 仍与用户批准时一致。
func (a *App) checkPasskeyRelay(op *passkeyOperation, state passkeyRelayState) error {
	if err := op.ctx.Err(); err != nil {
		return err
	}
	if a.passkeyOp != op || a.quitting {
		return context.Canceled
	}
	pk := a.settings.Passkey
	if pk == nil || pk.ID != state.passkey.ID || pk.PublicKey != state.passkey.PublicKey || pk.NEARAccount != state.passkey.NEARAccount ||
		a.network().ID != state.networkID || a.settings.RPCURL != state.rpcURL {
		return errors.New(a.t(i18n.PasskeyCanceled))
	}
	if a.nearKeyring == nil || !a.settings.IsNearWallet(state.relayer.AccountID) ||
		!a.nearKeyring.Unlocked(state.relayer.AccountID) ||
		a.nearAccountOf(state.relayer.AccountID).PublicKey != state.relayer.PublicKey {
		return errors.New(a.t(i18n.PasskeyRelayerLocked))
	}
	return nil
}

func (a *App) passkeyRelayValid(op *passkeyOperation, state passkeyRelayState) error {
	var err error
	a.onMain(func() { err = a.checkPasskeyRelay(op, state) })
	return err
}

// preflightPasskeyRelay 在两种批准提示之前预检 endpoint。
func (a *App) preflightPasskeyRelay(op *passkeyOperation, state passkeyRelayState, rpc neartx.RPC) error {
	if err := a.passkeyRelayValid(op, state); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(op.ctx, 20*time.Second)
	defer cancel()
	chainID, err := neartx.Status(ctx, rpc)
	if err != nil {
		return err
	}
	if chainID != "mainnet" {
		return fmt.Errorf("passkey: the endpoint serves %q, not NEAR mainnet", chainID)
	}
	return a.passkeyRelayValid(op, state)
}

// relayPasskeyNear 先用公开资料准备交易，再在同一个 UI 线程回合重新校验并读取、使用私钥。
// RPC 守卫在广播前再次检查状态，取消信号会传递到所有正在执行的请求。
func (a *App) relayPasskeyNear(op *passkeyOperation, state passkeyRelayState, rpc neartx.RPC, actions []neartx.Action) (string, error) {
	guardedRPC := func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		if err := a.passkeyRelayValid(op, state); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return rpc(ctx, method, params)
	}
	publicKey, err := neartx.ParsePublicKey(state.relayer.PublicKey)
	if err != nil {
		return "", err
	}
	prepareCtx, cancel := context.WithTimeout(op.ctx, 60*time.Second)
	tx, _, err := neartx.Prepare(prepareCtx, guardedRPC, neartx.Signer{
		AccountID: state.relayer.AccountID,
		PublicKey: publicKey,
	}, neartx.Request{ReceiverID: state.passkey.NEARAccount, Actions: actions})
	cancel()
	if err != nil {
		return "", err
	}
	var signed []byte
	var hash [32]byte
	a.onMain(func() {
		if err = a.checkPasskeyRelay(op, state); err != nil {
			return
		}
		key, ok := a.nearKeyring.Key(state.relayer.AccountID)
		if !ok {
			err = errors.New(a.t(i18n.PasskeyRelayerLocked))
			return
		}
		defer clear(key)
		if len(key) != ed25519.PrivateKeySize || neartx.PublicKeyOf(key) != publicKey {
			err = errors.New(a.t(i18n.PasskeyRelayerLocked))
			return
		}
		signed, hash, err = neartx.Sign(key, tx)
	})
	if err != nil {
		return "", err
	}
	sendCtx, cancel := context.WithTimeout(op.ctx, 90*time.Second)
	defer cancel()
	return neartx.Send(sendCtx, guardedRPC, signed, hash)
}
