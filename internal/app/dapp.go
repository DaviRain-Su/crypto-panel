package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

// The DApp mode: a web page of someone else's, in an untrusted web view,
// with a small NEAR wallet bridge injected. The page can ask the shell to
// connect and to sign; it cannot read a key, call a Go method, open a
// window, download a file or grant itself a permission. Every signature
// still opens the shell's own approval window.
//
// The page's requests reach the shell through the cryptopanel:// scheme.
// The scheme name is unchanged so pages and tests that already call it
// keep working.
// An untrusted page cannot read a cross-origin response, so the answer goes
// back as an event to the page that asked, which the preload script turns
// into the promise's result.

const (
	dappScheme     = "cryptopanel"
	dappRequestURL = dappScheme + "://near/request"
)

// dappEvent carries a bridge answer back to the page that asked.
var dappEvent = mygo.NewEvent[dappResponse]("cryptopanel:near")

// dappRequest is one bridge call from a page.
type dappRequest struct {
	ID         string          `json:"id"`
	Method     string          `json:"method"`
	Params     json.RawMessage `json:"params"`
	Capability string          `json:"capability"`
}

// dappResponse is the answer, matched to the request by ID and origin: an
// event mis-delivered to the next document must not be readable there.
type dappResponse struct {
	ID         string `json:"id"`
	Capability string `json:"capability"`
	Origin     string `json:"origin"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
}

// dappAccount is one account the page may see.
type dappAccount struct {
	AccountID string `json:"accountId"`
	PublicKey string `json:"publicKey"`
}

// registerDappScheme installs the bridge's scheme. It must run before the
// window is created.
func (a *App) registerDappScheme() {
	if err := mygo.Protocol.HandleFunc(dappScheme, a.serveDappRequest); err != nil {
		log.Printf("lintel: the DApp bridge: %v", err)
	}
}

// serveDappRequest 在原生主线程读取 SDK 提供的调用页面。页面只能标识 WebView，
// 不能识别发起请求的 frame；使用钱包的权限由 serveDappRequestFrom 校验 capability。
func (a *App) serveDappRequest(w http.ResponseWriter, r *http.Request) {
	var page *mygo.Page
	var origin string
	var bridge *dappBridgeState
	a.onMain(func() {
		page = mygo.CallerPage(r.Context())
		bridge = a.dappBridge
		if page != nil && bridge != nil {
			origin = dappOrigin(page.URL())
		}
	})
	a.serveDappRequestFrom(w, r, bridge, page, origin, func(out dappResponse) {
		if err := dappEvent.EmitPage(page, out); err != nil {
			log.Printf("lintel: answering %s: %v", origin, err)
		}
	})
}

// 来源参数是原生状态快照，单独传入以便在不打开钱包窗口时测试 HTTP 处理与 capability 校验。
func (a *App) serveDappRequestFrom(w http.ResponseWriter, r *http.Request, bridge *dappBridgeState, page *mygo.Page, origin string, emit func(dappResponse)) {
	// cryptopanel://near/request 对应 Host="near"、Path="/request"。
	if r.Method != http.MethodPost || r.URL.Scheme != dappScheme || r.URL.Host != "near" ||
		r.URL.EscapedPath() != "/request" || r.URL.User != nil || r.URL.RawQuery != "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if bridge == nil || page == nil || origin == "" {
		http.Error(w, "no wallet document", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(body) > 64<<10 {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var req dappRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.ID == "" || len(req.ID) > 64 || req.Method == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	call, err := bridge.begin(r.Context(), page, origin, req)
	if err != nil {
		http.Error(w, "no wallet capability or request unavailable", http.StatusForbidden)
		return
	}
	defer bridge.finish(call)
	// scheme 任务持续到处理结束，让导航、关闭或 fetch.abort() 取消 context。
	// 在批准前返回 204 会丢失这条取消链。
	result, err := a.handleDappRequest(call.ctx, origin, req)
	out := dappResponse{ID: req.ID, Capability: call.binding.capability, Origin: call.binding.origin, Result: result}
	if err != nil {
		out.Result, out.Error = nil, err.Error()
	}
	if call.ctx.Err() == nil && bridge.current(call.binding) {
		emit(out)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleDappRequest(ctx context.Context, origin string, req dappRequest) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch req.Method {
	case "connect":
		return a.dappConnect(ctx, origin, req.Params)
	case "disconnect":
		a.onMain(func() {
			if ctx.Err() == nil {
				delete(a.dappConnected, origin)
				if a.dappBridge != nil {
					a.dappBridge.disconnect(req.ID)
				}
			}
		})
		return nil, ctx.Err()
	case "getAccounts":
		return a.dappGetAccounts(origin, dappNetworkParam(req.Params))
	case "getNetwork":
		return a.dappGetNetwork()
	case "signAndSendTransaction":
		return a.dappSign(ctx, origin, req.Params)
	case "signAndSendTransactions":
		return a.dappSignTransactions(ctx, origin, req.Params)
	case "addFunctionCallKey":
		return a.dappAddFunctionCallKey(ctx, origin, req.Params)
	case "signMessage", "verifyOwner", "signTransaction", "signTransactions", "signDelegateActions", "signInAndSignMessage":
		return nil, fmt.Errorf("%s is unsupported: Lintel does not implement offline or NEP-413 signing", req.Method)
	default:
		return nil, fmt.Errorf("the wallet has no method %q", req.Method)
	}
}

// dappConnect asks the user, once per origin, to let the page see the NEAR
// accounts. A refusal leaves the page unconnected.
func (a *App) dappConnect(ctx context.Context, origin string, params json.RawMessage) (any, error) {
	var p struct {
		ContractID  string   `json:"contractId,omitempty"`
		MethodNames []string `json:"methodNames,omitempty"`
		Network     string   `json:"network,omitempty"`
	}
	if len(params) > 0 && string(params) != "null" {
		dec := json.NewDecoder(bytes.NewReader(params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return nil, errors.New("connect: the parameters are not understood")
		}
	}
	network, _, connected := a.dappState(origin)
	if network.Family != chain.Near {
		return nil, errors.New("switch to a NEAR network before connecting")
	}
	if err := checkDappNetwork(network, p.Network); err != nil {
		return nil, err
	}
	accounts, err := a.dappAccounts()
	if err != nil {
		return nil, err
	}
	// An origin that is already connected must not open the question again.
	if !connected && !a.confirmDappConnect(ctx, origin) {
		return nil, errors.New("the user rejected the connection")
	}
	a.onMain(func() {
		if ctx.Err() == nil {
			if a.dappConnected == nil {
				a.dappConnected = map[string]bool{}
			}
			a.dappConnected[origin] = true
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"accounts": accounts}, nil
}

func (a *App) dappGetAccounts(origin, requested string) (any, error) {
	network, _, connected := a.dappState(origin)
	if !connected {
		return nil, errors.New("connect first")
	}
	if network.Family != chain.Near {
		return nil, errors.New("switch to a NEAR network before reading the accounts")
	}
	if err := checkDappNetwork(network, requested); err != nil {
		return nil, err
	}
	return a.dappAccounts()
}

func dappNetworkParam(raw json.RawMessage) string {
	var p struct {
		Network string `json:"network"`
	}
	_ = json.Unmarshal(raw, &p)
	return p.Network
}

func checkDappNetwork(network chain.Network, requested string) error {
	if requested == "" || requested == network.ChainName {
		return nil
	}
	return fmt.Errorf("network %s does not match selected %s", requested, network.ChainName)
}

func (a *App) dappGetNetwork() (any, error) {
	var out map[string]string
	a.onMain(func() {
		n := a.network()
		out = map[string]string{"networkId": n.ChainName, "chainId": n.ID}
	})
	return out, nil
}

// dappSign signs and sends the actions a connected page asked for. The
// approval window names the page's origin, and the shell's own decode is
// what the user reads.
func (a *App) dappSign(ctx context.Context, origin string, params json.RawMessage) (any, error) {
	if !a.dappIsConnected(origin) {
		return nil, errors.New("connect first")
	}
	var p dappTransaction
	if err := decodeDappParams(params, &p); err != nil {
		return nil, fmt.Errorf("signAndSendTransaction: %w", err)
	}
	if len(p.Actions) == 0 {
		return nil, errors.New("signAndSendTransaction: actions are required")
	}
	network, reader, connected := a.dappState(origin)
	if !connected {
		return nil, errors.New("connect first")
	}
	if network.Family != chain.Near {
		return nil, errors.New("switch to a NEAR network before signing")
	}
	if err := checkDappNetwork(network, p.Network); err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("the NEAR reader is unavailable")
	}
	res, err := signNearTransaction(a.dappSigningDeps(ctx, origin, p.Description, reader), network, plugin.NearSignRequest{
		ChainID:     network.ID,
		From:        p.From,
		Receiver:    p.ReceiverID,
		Actions:     p.Actions,
		Description: p.Description,
	})
	if err != nil {
		return nil, err
	}
	return withTopLevelHash(res.Outcome, res.Hash), nil
}

// withTopLevelHash keeps the full NEAR outcome and also the old {hash}
// field callers already read.
func withTopLevelHash(outcome json.RawMessage, hash string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(outcome, &fields); err != nil || fields == nil {
		fields = map[string]json.RawMessage{}
	}
	encoded, err := json.Marshal(hash)
	if err != nil {
		return outcome
	}
	fields["hash"] = encoded
	raw, err := json.Marshal(fields)
	if err != nil {
		return outcome
	}
	return raw
}

type dappTransaction struct {
	Network     string              `json:"network,omitempty"`
	ReceiverID  string              `json:"receiverId"`
	From        string              `json:"from,omitempty"`
	SignerID    string              `json:"signerId,omitempty"`
	Actions     []plugin.NearAction `json:"actions"`
	Description string              `json:"description,omitempty"`
}

func (a *App) dappSignTransactions(ctx context.Context, origin string, params json.RawMessage) (any, error) {
	var p struct {
		Network      string            `json:"network,omitempty"`
		SignerID     string            `json:"signerId,omitempty"`
		Transactions []dappTransaction `json:"transactions"`
	}
	if err := decodeDappParams(params, &p); err != nil {
		return nil, fmt.Errorf("signAndSendTransactions: %w", err)
	}
	if len(p.Transactions) == 0 {
		return nil, errors.New("signAndSendTransactions: transactions are required")
	}
	outcomes := make([]json.RawMessage, 0, len(p.Transactions))
	for i, transaction := range p.Transactions {
		if p.Network != "" {
			transaction.Network = p.Network
		}
		if p.SignerID != "" {
			transaction.SignerID = p.SignerID
		}
		if transaction.SignerID != "" {
			transaction.From = transaction.SignerID
		}
		transaction.SignerID = ""
		raw, err := json.Marshal(transaction)
		if err != nil {
			return nil, err
		}
		result, err := a.dappSign(ctx, origin, raw)
		if err != nil {
			return outcomes, fmt.Errorf("signAndSendTransactions: transaction %d: %w", i+1, err)
		}
		outcome, ok := result.(json.RawMessage)
		if !ok {
			return nil, errors.New("signAndSendTransactions: native signing returned no outcome")
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// dappAddFunctionCallKey adds a key the page holds itself, limited to one
// contract, a method list and an allowance. The approval window shows the
// limit before the user signs it.
func (a *App) dappAddFunctionCallKey(ctx context.Context, origin string, params json.RawMessage) (any, error) {
	if !a.dappIsConnected(origin) {
		return nil, errors.New("connect first")
	}
	var p struct {
		PublicKey   string   `json:"publicKey"`
		ContractID  string   `json:"contractId"`
		MethodNames []string `json:"methodNames,omitempty"`
		Allowance   string   `json:"allowance,omitempty"`
		From        string   `json:"from,omitempty"`
	}
	if err := decodeDappParams(params, &p); err != nil {
		return nil, fmt.Errorf("addFunctionCallKey: %w", err)
	}
	if strings.TrimSpace(p.PublicKey) == "" || strings.TrimSpace(p.ContractID) == "" {
		return nil, errors.New("addFunctionCallKey: publicKey and contractId are required")
	}
	network, reader, _ := a.dappState(origin)
	if network.Family != chain.Near {
		return nil, errors.New("switch to a NEAR network before signing")
	}
	if reader == nil {
		return nil, errors.New("the NEAR reader is unavailable")
	}
	res, err := signNearTransaction(a.dappSigningDeps(ctx, origin, "", reader), network, plugin.NearSignRequest{
		ChainID: network.ID,
		From:    p.From,
		Actions: []plugin.NearAction{{
			Type:        "addFunctionCallKey",
			PublicKey:   p.PublicKey,
			ReceiverID:  p.ContractID,
			MethodNames: p.MethodNames,
			Allowance:   p.Allowance,
		}},
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"hash": res.Hash}, nil
}

func decodeDappParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("parameters are required")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

// dappState snapshots what a request needs, on the main thread.
func (a *App) dappState(origin string) (chain.Network, chain.Reader, bool) {
	var (
		network   chain.Network
		reader    chain.Reader
		connected bool
	)
	a.onMain(func() {
		network = a.network()
		reader = a.reader
		connected = a.dappConnected[origin]
	})
	return network, reader, connected
}

func (a *App) dappIsConnected(origin string) bool {
	_, _, connected := a.dappState(origin)
	return connected
}

func (a *App) dappAccounts() ([]dappAccount, error) {
	var accounts []dappAccount
	a.onMain(func() {
		for _, acc := range a.settings.NearWallet {
			accounts = append(accounts, dappAccount{AccountID: acc.AccountID, PublicKey: acc.PublicKey})
		}
	})
	if len(accounts) == 0 {
		return nil, errors.New("the wallet has no NEAR account")
	}
	return accounts, nil
}

// askConnect shows the shell's own question. The page cannot answer it.
func (a *App) askConnect(origin string) bool {
	return a.askDappConnect(context.Background(), origin)
}

// approver is the approval window, or the test's stand-in.
func (a *App) approver() func(approvalRequest) bool {
	if a.approve != nil {
		return a.approve
	}
	return a.askApproval
}

// onMain runs fn on the UI thread, through the app's hook so tests can run
// it directly.
func (a *App) onMain(fn func()) {
	if a.runOnMain != nil {
		a.runOnMain(fn)
		return
	}
	mygo.RunOnMain(fn)
}

// dappOrigin is the origin of a page URL: scheme and host, nothing else. A
// page that is not https (or http on loopback) has none.
func dappOrigin(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(host)) {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// openDapp loads the URL in the DApp web view, creating it on first use. An
// existing page keeps its state while the user visits another page of the
// shell.
func (a *App) openDapp() {
	raw := strings.TrimSpace(a.dappURLDraft)
	if dappOrigin(raw) == "" {
		a.dappErr = a.t(i18n.DappInvalidURL)
		a.invalidate()
		return
	}
	if a.win == nil {
		a.dappErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	if a.dappView == nil {
		view, err := a.win.NewWebView(mygo.WebViewOptions{
			Page:            mygo.PageOptions{Untrusted: true, PreloadScript: nearBridgeScript},
			BackgroundColor: "light-dark(#f6f4ef, #141513)",
		})
		if err != nil {
			a.dappErr = err.Error()
			a.invalidate()
			return
		}
		a.dappView = view
		page := view.Page()
		bridge := newDappBridge(page)
		a.dappBridge = bridge
		page.SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
		page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
		page.OnWillDownload(func(e *mygo.DownloadEvent) { e.PreventDefault() })
		page.OnWillNavigate(func(e *mygo.NavigateEvent) {
			// 保留不受信任页面原有的同源导航限制。
			if dappOrigin(e.URL) == "" || dappOrigin(e.URL) != dappOrigin(page.URL()) {
				e.PreventDefault()
				return
			}
			bridge.revoke(false)
		})
		page.OnDidNavigate(func(url string) {
			bridge.committed(dappOrigin(url))
			a.dappURLDraft = url
			a.invalidate()
		})
		// 以原生加载完成作为文档就绪边界。页面可以伪造 JavaScript DOM-ready 通知，
		// commit 时旧 JavaScript realm 也可能尚未替换，二者均不能触发 capability 发放。
		page.OnDidFinishLoad(func() {
			if binding, ok := bridge.binding(); ok {
				// 在原生回调中捕获代数，避免延迟协程绑定后续尚未加载完成的文档。
				go a.bindDappDocument(bridge, binding)
			}
		})
		page.OnDidFailLoad(func(*mygo.LoadError) { bridge.revoke(false) })
		page.OnRenderProcessGone(func(string) { bridge.revoke(false) })
		a.win.OnClosed(func() { bridge.revoke(true) })
	}
	a.dappErr = ""
	if a.dappBridge != nil {
		a.dappBridge.revoke(false)
	}
	if err := a.dappView.Page().LoadURL(raw); err != nil {
		a.dappErr = err.Error()
	}
	a.invalidate()
}

// closeDapp destroys the web view and forgets which origins were connected.
func (a *App) closeDapp() {
	if a.dappBridge != nil {
		a.dappBridge.revoke(true)
		a.dappBridge = nil
	}
	if a.dappView != nil {
		a.dappView.Destroy()
		a.dappView = nil
	}
	a.dappConnected = map[string]bool{}
	a.dappErr = ""
	a.invalidate()
}

// dappSubtitle heads the Apps page: the open page's origin, or what the
// page is for.
func (a *App) dappSubtitle() string {
	if origin := dappOrigin(a.dappURLDraft); origin != "" {
		return origin
	}
	return a.t(i18n.AppsSubtitle)
}

// dappBody is the Apps page: a URL bar, the network note, and the web view
// filling the rest.
func (a *App) dappBody(c *ui.Context) {
	p := a.pal
	ui.Column(c).Grow(1).FillHeight().Padding(0, 44, 20, 44).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("dapp-url"), &a.dappURLDraft).
				Placeholder(a.t(i18n.DappURLPlaceholder)).
				Label(a.t(i18n.DappURLLabel)).
				Grow(1).
				OnSubmit(a.openDapp)
			a.primaryButton(c, a.t(i18n.DappOpen), nil,
				strings.TrimSpace(a.dappURLDraft) == "", a.openDapp)
		})
		if a.dappErr != "" {
			ui.Text(c, a.dappErr).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
		wrongNetwork := a.network().Family != chain.Near
		if wrongNetwork {
			ui.Row(c).Gap(10).Padding(10, 12).Radius(9).Background(p.inset).
				AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.t(i18n.DappNearOnly)).FontSize(12).TextColor(p.muted).
					Grow(1).MaxLines(2)
				a.softButton(c, a.t(i18n.DappSwitchNear), nil, p.accent, func() {
					a.setNetwork(chains.NearTestnet.ID)
				})
			})
		}
		if a.dappView != nil {
			ui.WebView(c, a.dappView).Grow(1).Radius(12).Border(1, p.hairline)
			return
		}
		// The banner or the error already says why nothing is open.
		if a.dappErr != "" || wrongNetwork {
			return
		}
		ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Radius(14).
			Background(p.card).Border(1, p.hairline).Children(func() {
			ui.Box(c).Size(46, 46).Radius(13).Background(p.accentSoft).Center().Children(func() {
				iconGlobe.draw(c, 22, p.accent)
			})
			ui.Text(c, a.t(i18n.DappEmpty)).FontSize(15).FontWeight(700)
			ui.Text(c, a.t(i18n.DappEmptyWhy)).FontSize(12.5).TextColor(p.muted).
				TextAlign(ui.Center).MaxLines(4)
		})
	})
}
