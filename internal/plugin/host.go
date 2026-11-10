package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MoeForge/moejs"

	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/pages"
)

// Account is one watched address, as host.accounts returns it.
type Account struct {
	Address string `json:"address"`
	Label   string `json:"label,omitempty"`
	Family  string `json:"family"`
}

// SignRequest is what host.requestSign asks for: the transaction's intent.
// The shell fills in the nonce, gas and fees, decodes the result for the
// approval window, and only signs what the user approves.
type SignRequest struct {
	ChainID              string  `json:"chainId"`
	From                 string  `json:"from,omitempty"`
	To                   string  `json:"to"`
	Value                string  `json:"value,omitempty"` // wei, decimal
	Data                 string  `json:"data,omitempty"`  // hex, 0x-prefixed
	Gas                  uint64  `json:"gas,omitempty"`
	MaxFeePerGas         string  `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string  `json:"maxPriorityFeePerGas,omitempty"`
	Nonce                *uint64 `json:"nonce,omitempty"`
	// Description is the plugin's own words for the approval window. The
	// shell shows them beside its own decode, never instead of it.
	Description string `json:"description,omitempty"`
}

// SignResult is what host.requestSign resolves to.
type SignResult struct {
	Hash    string          `json:"hash"`
	Outcome json.RawMessage `json:"outcome,omitempty"`
}

// HostDeps is what the shell gives a plugin's host functions. Every field
// is a capability the shell already has; the host functions below only
// decide which of them a manifest may reach.
type HostDeps struct {
	Manifest Manifest
	Settings func(key string) (string, error)
	KV       *KV
	RPC      func(chain, method string, params any) (json.RawMessage, error)
	Accounts func() []Account
	// Sign prepares, shows and signs an EVM transaction, blocking while the
	// approval window is open. It returns only when the user approved and
	// the transaction was broadcast, or with an error.
	Sign func(SignRequest) (SignResult, error)
	// NearSign is the same for a NEAR transaction, decoded by the shell.
	NearSign func(NearSignRequest) (SignResult, error)
	// Read-only protocol adapters. The host checks manifest permissions
	// before invoking these shell-owned capabilities.
	HyperliquidInfo func(testnet bool, operation string, params map[string]any) (json.RawMessage, error)
	IntentsTokens   func() (any, error)
	IntentsDryQuote func(request map[string]any) (any, error)
	IntentsStatus   func(depositAddress, depositMemo string) (any, error)
	Log             func(level, msg string)
	Locale          func() string
	Translate       func(key string, args []any) string
}

// Host implements the functions a plugin sees as the global `host`. A
// function not installed here does not exist for plugins (design §6).
type Host struct {
	deps HostDeps
	// pauseWatch and resumeWatch bracket a host function that waits on the
	// user, so the call's watchdog does not count the wait as the plugin's
	// time. The runner sets them.
	pauseWatch  func()
	resumeWatch func()
}

// SetWatchPause gives the host the watchdog controls of the runner that
// owns it.
func (h *Host) SetWatchPause(pause, resume func()) {
	h.pauseWatch, h.resumeWatch = pause, resume
}

// NewHost returns the host for one plugin.
func NewHost(deps HostDeps) *Host { return &Host{deps: deps} }

// Namespace is the global object SetGlobal installs as `host`.
func (h *Host) Namespace() map[string]any {
	return map[string]any{
		"setting": moejs.NativeFunc(h.setting),
		"kv": map[string]any{
			"get":    moejs.NativeFunc(h.kvGet),
			"put":    moejs.NativeFunc(h.kvPut),
			"delete": moejs.NativeFunc(h.kvDelete),
		},
		"fetch": moejs.NativeFunc(h.fetch),
		"rpc":   moejs.NativeFunc(h.rpc),
		"near": map[string]any{
			"view": moejs.NativeFunc(h.nearView),
		},
		"hyperliquid": map[string]any{
			"info": moejs.NativeFunc(h.hyperliquidInfo),
		},
		"intents": map[string]any{
			"tokens":   moejs.NativeFunc(h.intentsTokens),
			"dryQuote": moejs.NativeFunc(h.intentsDryQuote),
			"status":   moejs.NativeFunc(h.intentsStatus),
		},
		"units": map[string]any{
			"parse":         moejs.NativeFunc(h.unitsParse),
			"format":        moejs.NativeFunc(h.unitsFormat),
			"formatDisplay": moejs.NativeFunc(h.unitsFormatDisplay),
		},
		"accounts":        moejs.NativeFunc(h.accounts),
		"requestSign":     moejs.NativeFunc(h.requestSign),
		"requestNearSign": moejs.NativeFunc(h.requestNearSign),
		"log":             moejs.NativeFunc(h.log),
		"i18n":            moejs.NativeFunc(h.i18n),
		"render":          moejs.NativeFunc(h.render),
	}
}

func stringArg(r *moejs.Realm, args []moejs.Value, i int) (string, error) {
	s, err := r.ToString(moejs.Arg(args, i))
	if err != nil {
		return "", err
	}
	return s.GoString(), nil
}

func (h *Host) setting(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	if _, ok := h.deps.Manifest.setting(key); !ok {
		return moejs.Undefined(), fmt.Errorf("setting %q is not declared in plugin.json", key)
	}
	if h.deps.Settings == nil {
		return moejs.String(""), nil
	}
	value, err := h.deps.Settings(key)
	if err != nil {
		return moejs.Undefined(), err
	}
	return moejs.String(value), nil
}

func (h *Host) kvGet(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	if h.deps.KV == nil {
		return moejs.Undefined(), nil
	}
	value, ok := h.deps.KV.Get(key)
	if !ok {
		return moejs.Undefined(), nil
	}
	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return moejs.Undefined(), fmt.Errorf("stored value for %q is not JSON: %w", key, err)
	}
	return r.FromGo(decoded)
}

func (h *Host) kvPut(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	if h.deps.KV == nil {
		return moejs.Undefined(), errors.New("plugin has no storage permission")
	}
	value, err := r.ToGoStrict(moejs.Arg(args, 1))
	if err != nil {
		return moejs.Undefined(), err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return moejs.Undefined(), err
	}
	if err := h.deps.KV.Put(key, data); err != nil {
		return moejs.Undefined(), err
	}
	return moejs.Undefined(), nil
}

func (h *Host) kvDelete(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	if h.deps.KV == nil {
		return moejs.Undefined(), nil
	}
	return moejs.Undefined(), h.deps.KV.Delete(key)
}

// fetchRequest is what host.fetch(url, init) accepts.
type fetchRequest struct {
	Method  string
	Headers map[string]string
	Body    string
}

func (h *Host) fetch(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	rawURL, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	req, err := parseFetchRequest(r, moejs.Arg(args, 1))
	if err != nil {
		return moejs.Undefined(), err
	}
	res, err := h.doFetch(rawURL, req)
	if err != nil {
		h.logf("warn", "%v", err)
		return moejs.Undefined(), err
	}
	return r.FromGo(res)
}

func parseFetchRequest(r *moejs.Realm, v moejs.Value) (fetchRequest, error) {
	req := fetchRequest{Method: http.MethodGet}
	if v.IsUndefined() || v.IsNull() {
		return req, nil
	}
	raw, err := r.ToGoStrict(v)
	if err != nil {
		return req, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return req, errors.New("fetch init must be an object")
	}
	for key, value := range obj {
		switch key {
		case "method":
			m, ok := value.(string)
			if !ok {
				return req, errors.New("fetch method must be a string")
			}
			req.Method = strings.ToUpper(m)
		case "headers":
			headers, ok := value.(map[string]any)
			if !ok {
				return req, errors.New("fetch headers must be an object")
			}
			req.Headers = map[string]string{}
			for name, v := range headers {
				s, ok := v.(string)
				if !ok {
					return req, fmt.Errorf("header %q must be a string", name)
				}
				req.Headers[name] = s
			}
		case "body":
			s, ok := value.(string)
			if !ok {
				return req, errors.New("fetch body must be a string")
			}
			req.Body = s
		default:
			return req, fmt.Errorf("fetch init key %q is not supported", key)
		}
	}
	return req, nil
}

// fetchResponse is what host.fetch resolves to.
type fetchResponse struct {
	Status  int               `json:"status"`
	OK      bool              `json:"ok"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

const (
	fetchTimeout = 30 * time.Second
	fetchMaxBody = 8 << 20
)

// allowedHeaders is the safe set a plugin may send: enough for an API key
// and a content type, not enough to forge a browser request.
var allowedHeaders = map[string]bool{
	"accept":        true,
	"content-type":  true,
	"authorization": true,
}

func (h *Host) doFetch(rawURL string, req fetchRequest) (fetchResponse, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fetchResponse{}, fmt.Errorf("fetch: %w", err)
	}
	if err := checkFetchURL(u, h.deps.Manifest.Permissions.Network); err != nil {
		return fetchResponse{}, err
	}
	host := strings.ToLower(u.Hostname())
	switch req.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
	default:
		return fetchResponse{}, fmt.Errorf("fetch: method %q is not allowed", req.Method)
	}
	httpReq, err := http.NewRequest(req.Method, rawURL, strings.NewReader(req.Body))
	if err != nil {
		return fetchResponse{}, fmt.Errorf("fetch: %w", err)
	}
	for name, value := range req.Headers {
		if !allowedHeaders[strings.ToLower(name)] {
			return fetchResponse{}, fmt.Errorf("fetch: header %q is not allowed", name)
		}
		httpReq.Header.Set(name, value)
	}
	httpReq.Header.Set("User-Agent", "lintel-plugin/"+h.deps.Manifest.Version)
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	// Every hop is checked: a redirect to a host outside the allowlist is
	// refused, not followed.
	client := &http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("fetch: too many redirects")
			}
			return checkFetchURL(r.URL, h.deps.Manifest.Permissions.Network)
		},
	}
	resp, err := client.Do(httpReq.WithContext(ctx))
	if err != nil {
		return fetchResponse{}, fmt.Errorf("fetch %s: %w", host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxBody+1))
	if err != nil {
		return fetchResponse{}, fmt.Errorf("fetch %s: reading the answer: %w", host, err)
	}
	if len(body) > fetchMaxBody {
		return fetchResponse{}, fmt.Errorf("fetch %s: answer larger than %d bytes", host, fetchMaxBody)
	}
	headers := make(map[string]string, len(resp.Header))
	for name, values := range resp.Header {
		headers[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	h.logf("info", "fetch %s %s", req.Method, host)
	return fetchResponse{
		Status:  resp.StatusCode,
		OK:      resp.StatusCode >= 200 && resp.StatusCode < 300,
		Headers: headers,
		Body:    string(body),
	}, nil
}

// checkFetchURL is the allowlist rule, applied to the URL the plugin named
// and to every redirect the host answers with.
func checkFetchURL(u *url.URL, allow []string) error {
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return fmt.Errorf("fetch: %s is refused: only https URLs are allowed", u.String())
	}
	host := strings.ToLower(u.Hostname())
	if !hostAllowed(allow, host) {
		return fmt.Errorf("fetch: %s is refused: %s is not in the plugin's network allowlist", u.String(), host)
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// hostAllowed matches a host against the manifest's allowlist: an exact
// name, or "*.example.com" for every subdomain of example.com.
func hostAllowed(patterns []string, host string) bool {
	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		if pattern == host {
			return true
		}
		if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		}
	}
	return false
}

func (h *Host) rpc(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	chain, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	method, err := stringArg(r, args, 1)
	if err != nil {
		return moejs.Undefined(), err
	}
	if !slicesContains(h.deps.Manifest.Permissions.Chains, chain) {
		return moejs.Undefined(), fmt.Errorf("rpc: chain %q is not in the plugin's permissions", chain)
	}
	network, ok := chains.ByID(chain)
	if !ok {
		return moejs.Undefined(), errors.New("rpc: unknown chain")
	}
	if err := checkRPCFamily(string(network.Family), method); err != nil {
		return moejs.Undefined(), err
	}
	var params any
	if v := moejs.Arg(args, 2); !v.IsUndefined() && !v.IsNull() {
		params, err = r.ToGoStrict(v)
		if err != nil {
			return moejs.Undefined(), err
		}
	}
	if h.deps.RPC == nil {
		return moejs.Undefined(), errors.New("rpc is not available")
	}
	raw, err := h.deps.RPC(chain, method, params)
	if err != nil {
		return moejs.Undefined(), err
	}
	// The broker answers with the JSON-RPC result as JSON text; the plugin
	// gets the value, not the text, so a hex quantity is a string and a
	// block is an object.
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return moejs.Undefined(), fmt.Errorf("rpc %s: the answer is not JSON: %w", method, err)
	}
	return r.FromGo(decoded)
}

func (h *Host) accounts(r *moejs.Realm, _ moejs.Value, _ []moejs.Value) (moejs.Value, error) {
	if !slicesContains(h.deps.Manifest.Permissions.Read, "balances") {
		return moejs.Undefined(), errors.New("accounts: the plugin does not have the balances read scope")
	}
	if h.deps.Accounts == nil {
		return r.FromGo([]Account{})
	}
	return r.FromGo(h.deps.Accounts())
}

// requestSign asks the shell to sign a transaction. It blocks the plugin's
// call while the approval window is open; the runner pauses its watchdog
// for that wait, so the user may take as long as they need. A rejection is
// an error the plugin sees, never a signature.
func (h *Host) requestSign(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	if !h.deps.Manifest.Permissions.Sign {
		return moejs.Undefined(), errors.New("requestSign: the plugin does not have the sign permission")
	}
	req, err := parseSignRequest(r, moejs.Arg(args, 0))
	if err != nil {
		return moejs.Undefined(), err
	}
	if !slicesContains(h.deps.Manifest.Permissions.Chains, req.ChainID) {
		return moejs.Undefined(), fmt.Errorf("requestSign: chain %q is not in the plugin's permissions", req.ChainID)
	}
	if h.deps.Sign == nil {
		return moejs.Undefined(), errors.New("requestSign is not available")
	}
	if h.pauseWatch != nil {
		h.pauseWatch()
	}
	res, err := h.deps.Sign(req)
	if h.resumeWatch != nil {
		h.resumeWatch()
	}
	if err != nil {
		h.logf("warn", "requestSign: %v", err)
		return moejs.Undefined(), err
	}
	return r.FromGo(res)
}

// requestNearSign is the NEAR counterpart of requestSign. The manifest must
// hold the sign permission and name the NEAR chain; the chain id in the
// request is checked against that list, so a plugin permitted on Ethereum
// cannot reach a NEAR account.
func (h *Host) requestNearSign(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	if !h.deps.Manifest.Permissions.Sign {
		return moejs.Undefined(), errors.New("requestNearSign: the plugin does not have the sign permission")
	}
	req, err := parseNearSignRequest(r, moejs.Arg(args, 0))
	if err != nil {
		return moejs.Undefined(), err
	}
	if !slicesContains(h.deps.Manifest.Permissions.Chains, req.ChainID) {
		return moejs.Undefined(), fmt.Errorf("requestNearSign: chain %q is not in the plugin's permissions", req.ChainID)
	}
	if h.deps.NearSign == nil {
		return moejs.Undefined(), errors.New("requestNearSign is not available")
	}
	if h.pauseWatch != nil {
		h.pauseWatch()
	}
	res, err := h.deps.NearSign(req)
	if h.resumeWatch != nil {
		h.resumeWatch()
	}
	if err != nil {
		h.logf("warn", "requestNearSign: %v", err)
		return moejs.Undefined(), err
	}
	return r.FromGo(res)
}

// parseSignRequest reads the object host.requestSign takes. Keys are
// checked, so a typo is an error rather than a transaction with a field the
// plugin did not mean.
func parseSignRequest(r *moejs.Realm, v moejs.Value) (SignRequest, error) {
	if v.IsUndefined() || v.IsNull() {
		return SignRequest{}, errors.New("requestSign needs a transaction")
	}
	raw, err := r.ToGoStrict(v)
	if err != nil {
		return SignRequest{}, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return SignRequest{}, errors.New("requestSign needs an object")
	}
	var req SignRequest
	for key, value := range obj {
		switch key {
		case "chainId", "from", "to", "value", "data", "maxFeePerGas", "maxPriorityFeePerGas", "description":
			s, ok := value.(string)
			if !ok {
				return SignRequest{}, fmt.Errorf("requestSign: %s must be a string", key)
			}
			switch key {
			case "chainId":
				req.ChainID = s
			case "from":
				req.From = s
			case "to":
				req.To = s
			case "value":
				req.Value = s
			case "data":
				req.Data = s
			case "maxFeePerGas":
				req.MaxFeePerGas = s
			case "maxPriorityFeePerGas":
				req.MaxPriorityFeePerGas = s
			case "description":
				req.Description = pages.Trim(s, 200)
			}
		case "gas", "nonce":
			// ToGoStrict exports an integral number as int64, a fractional
			// one as float64.
			var n uint64
			switch v := value.(type) {
			case int64:
				if v < 0 {
					return SignRequest{}, fmt.Errorf("requestSign: %s must not be negative", key)
				}
				n = uint64(v)
			case float64:
				if v < 0 || v != float64(uint64(v)) {
					return SignRequest{}, fmt.Errorf("requestSign: %s must be a whole number", key)
				}
				n = uint64(v)
			default:
				return SignRequest{}, fmt.Errorf("requestSign: %s must be a whole number", key)
			}
			if key == "gas" {
				req.Gas = n
			} else {
				nonce := n
				req.Nonce = &nonce
			}
		default:
			return SignRequest{}, fmt.Errorf("requestSign: key %q is not supported", key)
		}
	}
	if req.ChainID == "" {
		return SignRequest{}, errors.New("requestSign: chainId is required")
	}
	if req.To == "" {
		return SignRequest{}, errors.New("requestSign: to is required")
	}
	return req, nil
}

func (h *Host) log(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	level, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	msg, err := stringArg(r, args, 1)
	if err != nil {
		return moejs.Undefined(), err
	}
	switch level {
	case "debug", "info", "warn", "error":
	default:
		return moejs.Undefined(), fmt.Errorf("log: level %q must be debug, info, warn or error", level)
	}
	h.logf(level, "%s", pages.Trim(msg, 2000))
	return moejs.Undefined(), nil
}

func (h *Host) i18n(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	key, err := stringArg(r, args, 0)
	if err != nil {
		return moejs.Undefined(), err
	}
	var rest []any
	for _, v := range args[1:] {
		value, err := r.ToGoStrict(v)
		if err != nil {
			return moejs.Undefined(), err
		}
		rest = append(rest, value)
	}
	if h.deps.Translate == nil {
		return moejs.String(key), nil
	}
	return moejs.String(h.deps.Translate(key, rest)), nil
}

func (h *Host) render(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
	spec, err := r.ToGoStrict(moejs.Arg(args, 0))
	if err != nil {
		return moejs.Undefined(), err
	}
	if _, err := pages.DecodeValue(spec); err != nil {
		return moejs.Undefined(), err
	}
	return moejs.Arg(args, 0), nil
}

func (h *Host) logf(level, format string, args ...any) {
	if h.deps.Log != nil {
		h.deps.Log(level, fmt.Sprintf(format, args...))
	}
}

func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
