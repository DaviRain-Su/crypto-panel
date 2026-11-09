// Package agent is the app's read-only local API: a localhost HTTP server
// with a bearer token, for a local agent to read the portfolio and the
// installed apps. It cannot sign, approve, install or reach a key, and it
// binds loopback only. The token lives in the OS keychain, not in the
// settings file.
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
type Account struct {
	Address string   `json:"address"`
	Label   string   `json:"label,omitempty"`
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
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description,omitempty"`
	Author      string      `json:"author,omitempty"`
	Enabled     bool        `json:"enabled"`
	Signed      bool        `json:"signed"`
	Official    bool        `json:"official"`
	Broken      string      `json:"broken,omitempty"`
	Folder      string      `json:"folder,omitempty"`
	Permissions Permissions `json:"permissions"`
	Pages       []string    `json:"pages,omitempty"`
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

// Close stops the server. The listener is closed here as well as by the
// server, because Serve may not have registered it yet when Close runs.
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
	err := s.srv.Close()
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
	mux.HandleFunc("POST /v0/plugins/{id}/refresh", s.guard(s.refresh))
	return mux
}

// guard is the whole access rule: a bearer token, no browser origins, and
// an answer that is never cached.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		// A page in a browser cannot read the token, but refusing an
		// Origin outright keeps a stray page from probing the API at all.
		if r.Header.Get("Origin") != "" {
			writeError(w, http.StatusForbidden, "the local API does not answer browser pages")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="crypto-panel"`)
			writeError(w, http.StatusUnauthorized, "a bearer token is required")
			return
		}
		next(w, r)
	}
}

func (s *Server) portfolio(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.data.Portfolio())
}

func (s *Server) plugins(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"plugins": s.data.Plugins()})
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	page, err := s.data.Refresh(r.PathValue("id"))
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrDisabled), errors.Is(err, ErrNoPages):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSON(w, http.StatusOK, page)
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
