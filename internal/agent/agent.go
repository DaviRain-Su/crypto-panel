// Package agent 提供仅监听回环地址的本机 API。Bearer token 授权读取、
// 校验与安装提议，安装批准仍由宿主原生界面决定。API 不提供钱包签名或密钥，
// token 保存在系统凭据服务中。
package agent

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/davirain-su/lintel/internal/plugin"
)

// Data is what the server answers with. The app implements it; every method
// is called from an HTTP goroutine and must be safe to call there.
type Data interface {
	// Portfolio is what the app last read: the network, the block and one
	// entry per watched account, with amounts in their smallest unit.
	Portfolio() Portfolio
	// Plugins is the installed apps and what each may do.
	Plugins() []Plugin
	// Refresh asks one app for its first page and returns the page data.
	Refresh(id string) (any, error)
}

// Errors a Data implementation returns from Refresh, mapped to statuses.
var (
	ErrNotFound = errors.New("no such app")
	ErrDisabled = errors.New("the app is turned off")
	ErrNoPages  = errors.New("the app has no pages")
)

// Portfolio is the answer of GET /v0/portfolio.
type Portfolio struct {
	Network  string    `json:"network"`
	ChainID  string    `json:"chainId"`
	Updated  time.Time `json:"updated,omitempty"`
	Block    uint64    `json:"block,omitempty"`
	Error    string    `json:"error,omitempty"`
	Accounts []Account `json:"accounts"`
}

// Account is one watched address. Native and Tokens carry amounts in their
// smallest unit as decimal strings, so no precision is lost to JSON numbers.
// Network, ChainID and Block are this account's own chain, not the network
// selected in the app: one answer lists every family.
type Account struct {
	Address string   `json:"address"`
	Label   string   `json:"label,omitempty"`
	Network string   `json:"network,omitempty"`
	ChainID string   `json:"chainId,omitempty"`
	Block   uint64   `json:"block,omitempty"`
	Native  Amount   `json:"native"`
	Tokens  []Amount `json:"tokens,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Amount is one balance.
type Amount struct {
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Amount   string `json:"amount"`
	Contract string `json:"contract,omitempty"`
}

// Plugin is one installed app, as GET /v0/plugins lists it. Signed is a
// registry install whose files still match. Official is a first-party
// example shipped with the shell. The two are independent.
type Plugin struct {
	ID                    string                 `json:"id"`
	Name                  string                 `json:"name"`
	Version               string                 `json:"version"`
	Description           string                 `json:"description,omitempty"`
	Author                string                 `json:"author,omitempty"`
	Enabled               bool                   `json:"enabled"`
	Signed                bool                   `json:"signed"`
	Official              bool                   `json:"official"`
	Broken                string                 `json:"broken,omitempty"`
	Folder                string                 `json:"folder,omitempty"`
	Permissions           Permissions            `json:"permissions"`
	Pages                 []string               `json:"pages,omitempty"`
	Source                string                 `json:"source,omitempty"`
	InstalledVia          string                 `json:"installedVia,omitempty"`
	PackageHash           string                 `json:"packageHash,omitempty"`
	Settings              []plugin.SettingStatus `json:"settings,omitempty"`
	PendingInstallationID string                 `json:"pendingInstallationId,omitempty"`
	Verification          *Verification          `json:"verification,omitempty"`
}

// Verification 仅描述当前已批准包的首页检查，不含页面、凭据或异常原文。
type Verification struct {
	RequestID   string     `json:"requestId"`
	PackageHash string     `json:"packageHash"`
	Page        string     `json:"page"`
	Status      string     `json:"status"`
	Code        string     `json:"code,omitempty"`
	CheckedAt   *time.Time `json:"checkedAt,omitempty"`
}

// Permissions is what an app may do, as its manifest declares.
type Permissions struct {
	Network   []string `json:"network,omitempty"`
	Chains    []string `json:"chains,omitempty"`
	Read      []string `json:"read,omitempty"`
	Sign      bool     `json:"sign"`
	StorageKb int      `json:"storageKb,omitempty"`
}

// Server is the local API. One server runs while the setting is on.
type Server struct {
	data  Data
	token string
	// port is captured once in Start. Host checks read it and never the
	// listener, which Close sets to nil.
	port string

	srv *http.Server
	ln  net.Listener
}

// New returns a server that answers for data and checks token.
func New(data Data, token string) *Server {
	return &Server{data: data, token: token}
}

// Start binds 127.0.0.1:port and serves in the background. Only loopback is
// bound, so the API is not reachable from the network whatever the firewall
// says.
func (s *Server) Start(port int) error {
	if s.ln != nil {
		return errors.New("the local API is already running")
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("the local API cannot listen on 127.0.0.1:%d: %w", port, err)
	}
	s.ln = ln
	if _, port, err := net.SplitHostPort(ln.Addr().String()); err == nil {
		s.port = port
	}
	s.srv = &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go s.srv.Serve(ln)
	return nil
}

// Addr is the address the server listens on, once started.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close 先停止 HTTP 服务，再关闭可能尚未被 Serve 登记的 listener。
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	err := s.srv.Close()
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.srv = nil
	s.ln = nil
	return err
}

// NewToken returns a fresh bearer token: 32 random bytes, base64url.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v0/portfolio", s.guard(s.portfolio))
	mux.HandleFunc("GET /v0/plugins", s.guard(s.plugins))
	mux.HandleFunc("GET /v0/catalog", s.guard(s.catalog))
	mux.HandleFunc("GET /v0/examples/{id}", s.guard(s.example))
	mux.HandleFunc("POST /v0/plugins/validate", s.guard(s.validate))
	mux.HandleFunc("POST /v0/plugins", s.guard(s.propose))
	mux.HandleFunc("PUT /v0/plugins/{id}", s.guard(s.propose))
	mux.HandleFunc("GET /v0/installations/{requestId}", s.guard(s.installation))
	mux.HandleFunc("DELETE /v0/installations/{requestId}", s.guard(s.installation))
	mux.HandleFunc("PUT /v0/plugins/{id}/settings", s.guard(s.settings))
	mux.HandleFunc("DELETE /v0/plugins/{id}", s.guard(s.remove))
	mux.HandleFunc("POST /v0/plugins/{id}/refresh", s.guard(s.refresh))
	// ServeMux 的兜底 404/405 是纯文本；换成文档规定的 JSON 错误信封。
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallback := &jsonFallback{ResponseWriter: w}
		mux.ServeHTTP(fallback, r)
		if !fallback.intercept {
			return
		}
		message, code := "no such endpoint", "NOT_FOUND"
		if fallback.status == http.StatusMethodNotAllowed {
			message, code = "method not allowed", "METHOD_NOT_ALLOWED"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
	})
}

// jsonFallback 只接管 ServeMux 自己产生的纯文本 404/405；已经按 JSON
// 应答的处理器（guard 先设置了 JSON Content-Type）保持原样。
type jsonFallback struct {
	http.ResponseWriter
	intercept bool
	status    int
}

func (w *jsonFallback) WriteHeader(status int) {
	if (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) &&
		!strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.intercept = true
		w.status = status
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *jsonFallback) Write(data []byte) (int, error) {
	if w.intercept {
		// 丢弃 http.Error 的纯文本正文，由外层写 JSON 信封。
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}

// guard is the whole access rule: a loopback host, no browser origins, a
// bearer token, and an answer that is never cached.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if reason := s.forbiddenRequest(r); reason != "" {
			writeError(w, http.StatusForbidden, reason)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="lintel"`)
			writeError(w, http.StatusUnauthorized, "a bearer token is required")
			return
		}
		next(w, r)
	}
}

// forbiddenRequest rejects browser pages and any request that is not aimed at
// this loopback listener. A single empty Origin is ignored; any real Origin,
// more than one Origin, and a cross-site fetch are refused.
func (s *Server) forbiddenRequest(r *http.Request) string {
	origins := r.Header.Values("Origin")
	if len(origins) > 1 || (len(origins) == 1 && origins[0] != "") {
		return "the local API does not answer browser pages"
	}
	for _, site := range r.Header.Values("Sec-Fetch-Site") {
		if strings.EqualFold(strings.TrimSpace(site), "cross-site") {
			return "the local API does not answer cross-site requests"
		}
	}
	if !s.loopbackHost(r.Host) {
		return "the local API only answers 127.0.0.1 or localhost"
	}
	return ""
}

func (s *Server) loopbackHost(host string) bool {
	name, port, err := net.SplitHostPort(host)
	if err != nil || port == "" {
		return false
	}
	if name != "127.0.0.1" && !strings.EqualFold(name, "localhost") {
		return false
	}
	if s.port == "" {
		return true
	}
	return port == s.port
}

func (s *Server) portfolio(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.data.Portfolio())
}

func (s *Server) plugins(w http.ResponseWriter, _ *http.Request) {
	items := s.data.Plugins()
	if items == nil {
		items = []Plugin{}
	}
	jobs := []plugin.Installation{}
	if data, ok := s.data.(Management); ok {
		if manager := data.ApplicationManager(); manager != nil {
			jobs = localAPIInstallations(manager.Installations())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": items, "installations": jobs})
}

func localAPIInstallations(jobs []plugin.Installation) []plugin.Installation {
	out := []plugin.Installation{}
	for _, job := range jobs {
		if job.Via == "local-api" {
			out = append(out, job)
		}
	}
	return out
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	if data, ok := s.data.(Management); ok {
		result, err := data.RefreshPage(r.PathValue("id"), r.URL.Query().Get("page"))
		if err != nil {
			writeManagementError(w, err)
			return
		}
		setRefreshMeta(w, result)
		writeJSON(w, http.StatusOK, result.Page)
		return
	}
	page, err := s.data.Refresh(r.PathValue("id"))
	if err != nil {
		writeManagementError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// setRefreshMeta keeps the original header names and also sends the Lintel
// names, so older clients and newer ones both see the hash and the time.
func setRefreshMeta(w http.ResponseWriter, result PageResult) {
	if result.PackageHash != "" {
		w.Header().Set("X-Crypto-Panel-Package-Hash", result.PackageHash)
		w.Header().Set("X-Lintel-Package-Hash", result.PackageHash)
	}
	if !result.EvaluatedAt.IsZero() {
		formatted := result.EvaluatedAt.UTC().Format(time.RFC3339Nano)
		w.Header().Set("X-Crypto-Panel-Evaluated-At", formatted)
		w.Header().Set("X-Lintel-Evaluated-At", formatted)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
