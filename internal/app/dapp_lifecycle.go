package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const (
	dappRequestTimeout = 120 * time.Second
	dappMaxPending     = 32
	dappDocumentKey    = "__cryptoPanelNearDocument"
	dappBindKey        = "__cryptoPanelBindNear"
)

// dappBridgeState 只向一个 WebView 的当前文档授权。CallerPage 仅能识别 WebView，
// 包括其中异源 frame 发起的请求，因此不能单独证明调用权限。原生加载完成后，
// 主 frame 的 preload 闭包才会收到新的 256-bit capability；页面的 DOM-ready 消息不能触发发放。
// 文档 marker 防止延迟执行的 Eval 将 capability 装入另一个文档，即使两个文档 URL 相同。
// HTTP Origin 和 Eval 返回值均不能作为证明；请求必须持有当前原生 capability。
//
// SDK 0.7.5 没有公开 frame/document ID，也没有覆盖 reload 的导航开始事件。
// 请求必须保留 scheme context 直到处理完成，让 WebKit 的 stopURLSchemeTask
// 在导航、关闭或 AbortController 取消时同时撤销批准。
// 以下方法可以跨主线程和请求协程调用；App.dappBridge 本身只在主线程访问。
type dappBridgeState struct {
	mu         sync.Mutex
	page       *mygo.Page
	origin     string
	epoch      uint64
	capability string
	document   context.Context
	cancel     context.CancelFunc
	closed     bool
	pending    map[string]*dappCall

	// 测试仅替换原生批准结果，仍须执行来源校验。
	approve func(context.Context, approvalRequest) bool
	connect func(context.Context, string) bool
}

type dappBinding struct {
	page       *mygo.Page
	origin     string
	epoch      uint64
	capability string
	ctx        context.Context
}

type dappCall struct {
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	binding dappBinding
	id      string
}

func newDappBridge(page *mygo.Page) *dappBridgeState {
	return &dappBridgeState{page: page, pending: make(map[string]*dappCall)}
}

// revoke 在应用发起加载、可观测导航以及加载失败或关闭时撤销权限。
// 导航失败后保持禁用，直到下一次文档 commit。
func (b *dappBridgeState) revoke(close bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.revokeLocked()
	b.closed = b.closed || close
}

func (b *dappBridgeState) revokeLocked() {
	if b.cancel != nil {
		b.cancel()
	}
	for _, call := range b.pending {
		call.cancel()
	}
	b.pending = make(map[string]*dappCall)
	b.capability, b.origin = "", ""
	b.epoch++
}

func (b *dappBridgeState) committed(origin string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.revokeLocked()
	if b.closed || origin == "" {
		return
	}
	b.origin = origin
	b.capability = hex.EncodeToString(randomDappCapability())
	b.document, b.cancel = context.WithCancel(context.Background())
}

func randomDappCapability() []byte {
	// 系统 RNG 失败时，crypto/rand.Read 会终止进程。
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return buf
}

func (b *dappBridgeState) binding() (dappBinding, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.capability == "" || b.document.Err() != nil {
		return dappBinding{}, false
	}
	return dappBinding{b.page, b.origin, b.epoch, b.capability, b.document}, true
}

func (b *dappBridgeState) currentLocked(binding dappBinding) bool {
	return !b.closed && b.page == binding.page && b.epoch == binding.epoch &&
		b.origin == binding.origin && b.capability != "" &&
		subtle.ConstantTimeCompare([]byte(b.capability), []byte(binding.capability)) == 1
}

func (b *dappBridgeState) current(binding dappBinding) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.currentLocked(binding)
}

func (b *dappBridgeState) begin(ctx context.Context, page *mygo.Page, origin string, req dappRequest) (*dappCall, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if page == nil || b.closed || page != b.page || origin == "" || origin != b.origin ||
		b.capability == "" || subtle.ConstantTimeCompare([]byte(req.Capability), []byte(b.capability)) != 1 {
		return nil, errors.New("the document has no wallet capability")
	}
	if ctx.Err() != nil || b.document.Err() != nil {
		return nil, errors.New("the document request was canceled")
	}
	if _, exists := b.pending[req.ID]; exists {
		return nil, errors.New("the request is already pending")
	}
	if len(b.pending) >= dappMaxPending {
		return nil, errors.New("too many pending wallet requests")
	}
	callCtx, cancel := context.WithTimeout(ctx, dappRequestTimeout)
	call := &dappCall{
		ctx: callCtx, cancel: cancel, stop: context.AfterFunc(b.document, cancel), id: req.ID,
		binding: dappBinding{b.page, b.origin, b.epoch, b.capability, b.document},
	}
	b.pending[req.ID] = call
	return call, nil
}

func (b *dappBridgeState) finish(call *dappCall) {
	call.stop()
	call.cancel()
	b.mu.Lock()
	defer b.mu.Unlock()
	// 旧请求完成时，不能删除新文档中复用相同 ID 的请求。
	if b.pending[call.id] == call {
		delete(b.pending, call.id)
	}
}

func (b *dappBridgeState) disconnect(except string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, call := range b.pending {
		if id != except {
			call.cancel()
		}
	}
}

// bindDappDocument 通过原生主 frame Eval 单向投递 capability。SDK Eval 使用页面的
// JSON.stringify 序列化返回值，页面可以篡改结果，因此不能把返回值作为来源证明。
// 读出的 marker 仅用于选择目标文档；不可替换的 preload binder 须核对其捕获的
// marker 和 origin，匹配后才能接收 capability。
func (a *App) bindDappDocument(b *dappBridgeState, binding dappBinding) {
	current := false
	a.onMain(func() {
		current = a.dappBridge == b && b.current(binding) && !binding.page.IsLoading() &&
			dappOrigin(binding.page.URL()) == binding.origin
	})
	if !current {
		return
	}
	ctx, cancel := context.WithTimeout(binding.ctx, 5*time.Second)
	defer cancel()
	marker, err := binding.page.EvalContext(ctx, "window."+dappDocumentKey)
	if err != nil {
		return // 无法确认主文档已加载，保持拒绝授权。
	}
	document, ok := marker.(string)
	if !ok || len(document) != 64 {
		return
	}
	if _, err := hex.DecodeString(document); err != nil {
		return
	}
	current = false
	a.onMain(func() {
		current = a.dappBridge == b && b.current(binding) && !binding.page.IsLoading() &&
			dappOrigin(binding.page.URL()) == binding.origin
	})
	if !current || ctx.Err() != nil {
		return
	}
	// 脚本在实际执行时再次检查文档 marker，不能只依赖异步 Eval 入队前观察到的 URL。
	_, _ = binding.page.EvalContext(ctx, fmt.Sprintf("window.%s(%q, %d, %q, %q)",
		dappBindKey, binding.capability, binding.epoch, document, binding.origin))
}
