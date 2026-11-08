package app

import (
	"bytes"
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

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/plugin"
)

// The DApp mode: a web page of someone else's, in an untrusted web view,
// with a small NEAR wallet bridge injected. The page can ask the shell to
// connect and to sign; it cannot read a key, call a Go method, open a
// window, download a file or grant itself a permission. Every signature
// still opens the shell's own approval window.
//
// The page's requests reach the shell through the cryptopanel:// scheme.
// An untrusted page cannot read a cross-origin response, so the answer goes
// back as an event to the page that asked, which the preload script turns
// into the promise's result.

const dappScheme = "cryptopanel"

// dappEvent carries a bridge answer back to the page that asked.
var dappEvent = mygo.NewEvent[dappResponse]("cryptopanel:near")

// dappRequest is one bridge call from a page.
type dappRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// dappResponse is the answer, matched to the request by ID.
type dappResponse struct {
	ID     string `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
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
		log.Printf("crypto-panel: the DApp bridge: %v", err)
	}
}

// serveDappRequest accepts one bridge call. The origin is the page's actual
// URL, never a field of the request: a page cannot claim to be another.
func (a *App) serveDappRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/near/request" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	page := mygo.CallerPage(r.Context())
	if page == nil {
		http.Error(w, "no page", http.StatusForbidden)
		return
	}
	origin := dappOrigin(page.URL())
	if origin == "" {
		http.Error(w, "no origin", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var req dappRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.ID == "" || len(req.ID) > 64 || req.Method == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// The page cannot read this response; the answer comes as an event.
	w.WriteHeader(http.StatusNoContent)
	go a.answerDapp(page, origin, req)
}

// answerDapp runs one bridge call and sends its answer to the page.
func (a *App) answerDapp(page *mygo.Page, origin string, req dappRequest) {
	result, err := a.handleDappRequest(origin, req)
	out := dappResponse{ID: req.ID, Result: result}
	if err != nil {
		out.Result = nil
		out.Error = err.Error()
	}
	if err := dappEvent.EmitPage(page, out); err != nil {
		log.Printf("crypto-panel: answering %s: %v", origin, err)
	}
}

func (a *App) handleDappRequest(origin string, req dappRequest) (any, error) {
	switch req.Method {
	case "connect":
		return a.dappConnect(origin, req.Params)
	case "getAccounts":
		return a.dappGetAccounts(origin)
	case "getNetwork":
		return a.dappGetNetwork()
	case "signAndSendTransaction":
		return a.dappSign(origin, req.Params)
	case "addFunctionCallKey":
		return a.dappAddFunctionCallKey(origin, req.Params)
	default:
		return nil, fmt.Errorf("the wallet has no method %q", req.Method)
	}
}

// dappConnect asks the user, once per origin, to let the page see the NEAR
// accounts. A refusal leaves the page unconnected.
func (a *App) dappConnect(origin string, params json.RawMessage) (any, error) {
	var p struct {
		ContractID  string   `json:"contractId,omitempty"`
		MethodNames []string `json:"methodNames,omitempty"`
	}
	if len(params) > 0 && string(params) != "null" {
		dec := json.NewDecoder(bytes.NewReader(params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			return nil, errors.New("connect: the parameters are not understood")
		}
	}
	network, _, _ := a.dappState(origin)
	if network.Family != chain.Near {
		return nil, errors.New("switch to a NEAR network before connecting")
	}
	accounts, err := a.dappAccounts()
	if err != nil {
		return nil, err
	}
	confirm := a.confirmConnect
	if confirm == nil {
		confirm = a.askConnect
	}
	if !confirm(origin) {
		return nil, errors.New("the user rejected the connection")
	}
	a.onMain(func() { a.dappConnected[origin] = true })
	return map[string]any{"accounts": accounts}, nil
}

func (a *App) dappGetAccounts(origin string) (any, error) {
	if !a.dappIsConnected(origin) {
		return nil, errors.New("connect first")
	}
	return a.dappAccounts()
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
func (a *App) dappSign(origin string, params json.RawMessage) (any, error) {
	if !a.dappIsConnected(origin) {
		return nil, errors.New("connect first")
	}
	var p struct {
		ReceiverID  string              `json:"receiverId"`
		From        string              `json:"from,omitempty"`
		Actions     []plugin.NearAction `json:"actions"`
		Description string              `json:"description,omitempty"`
	}
	if err := decodeDappParams(params, &p); err != nil {
		return nil, fmt.Errorf("signAndSendTransaction: %w", err)
	}
	if len(p.Actions) == 0 {
		return nil, errors.New("signAndSendTransaction: actions are required")
	}
	network, reader, _ := a.dappState(origin)
	if network.Family != chain.Near {
		return nil, errors.New("switch to a NEAR network before signing")
	}
	if reader == nil {
		return nil, errors.New("the NEAR reader is unavailable")
	}
	res, err := signNearTransaction(nearSignDeps{
		sourceName:  origin,
		sourceKind:  "dapp",
		description: p.Description,
		account:     a.nearSignAccount,
		key:         a.nearSignKey,
		approve:     a.approver(),
		rpc:         reader.RPC,
	}, network, plugin.NearSignRequest{
		ChainID:     network.ID,
		From:        p.From,
		Receiver:    p.ReceiverID,
		Actions:     p.Actions,
		Description: p.Description,
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"hash": res.Hash}, nil
}

// dappAddFunctionCallKey adds a key the page holds itself, limited to one
// contract, a method list and an allowance. The approval window shows the
// limit before the user signs it.
func (a *App) dappAddFunctionCallKey(origin string, params json.RawMessage) (any, error) {
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
	res, err := signNearTransaction(nearSignDeps{
		sourceName: origin,
		sourceKind: "dapp",
		account:    a.nearSignAccount,
		key:        a.nearSignKey,
		approve:    a.approver(),
		rpc:        reader.RPC,
	}, network, plugin.NearSignRequest{
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
	decided := make(chan bool, 1)
	a.onMain(func() {
		if a.win == nil {
			decided <- false
			return
		}
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:        a.win,
			Type:          mygo.MessageQuestion,
			Title:         a.t(i18n.DappConnectTitle),
			Message:       a.t(i18n.DappConnectBody, origin),
			Buttons:       []string{a.t(i18n.DappConnect), a.t(i18n.Cancel)},
			DefaultButton: 0,
			CancelButton:  1,
		})
		decided <- err == nil && res.Button == 0
	})
	return <-decided
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
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return ""
	}
	return u.Scheme + "://" + u.Host
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
		page.SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
		page.SetWindowOpenHandler(func(mygo.WindowOpenRequest) *mygo.WindowOptions { return nil })
		page.OnWillDownload(func(e *mygo.DownloadEvent) { e.PreventDefault() })
		page.OnDidNavigate(func(url string) {
			a.dappURLDraft = url
			a.invalidate()
		})
	}
	a.dappErr = ""
	if err := a.dappView.Page().LoadURL(raw); err != nil {
		a.dappErr = err.Error()
	}
	a.invalidate()
}

// closeDapp destroys the web view and forgets which origins were connected.
func (a *App) closeDapp() {
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
		if a.network().Family != chain.Near {
			ui.Row(c).Gap(10).Padding(10, 12).Radius(9).Background(p.inset).
				AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.t(i18n.DappNearOnly)).FontSize(11.5).TextColor(p.muted).
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
		ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Radius(14).
			Background(p.card).Border(1, p.hairline).Children(func() {
			ui.Box(c).Size(46, 46).Radius(13).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, iconGlobe).Size(22, 22).TextColor(p.accent)
			})
			ui.Text(c, a.t(i18n.DappEmpty)).FontSize(15).FontWeight(700)
			ui.Text(c, a.t(i18n.DappEmptyWhy)).FontSize(12.5).TextColor(p.muted).
				TextAlign(ui.Center).MaxLines(4)
		})
	})
}
