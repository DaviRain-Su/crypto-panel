package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/davirain-su/crypto-panel/internal/webauthn"
	passkeypage "github.com/davirain-su/crypto-panel/web/passkey"
)

// ceremonyTimeout is how long the app waits for the browser to finish one
// WebAuthn ceremony.
const ceremonyTimeout = 2 * time.Minute

// ceremonyOutcome is one answer from the page: a result or the page's error.
type ceremonyOutcome struct {
	result webauthn.CeremonyResult
	err    error
}

// ceremonyServer serves the passkey page on loopback and waits for its
// result. The page runs in the system browser because that is where the
// platform authenticator (Touch ID, Windows Hello, a password manager) is.
//
// The server binds 127.0.0.1 on a random port and is reached as
// http://localhost:<port>, a secure context WebAuthn accepts. The challenge
// and user id travel in the URL fragment, which the browser does not send to
// the server; the result comes back in a POST body.
type ceremonyServer struct {
	ln      net.Listener
	srv     *http.Server
	open    func(string) error
	results chan ceremonyOutcome

	mu    sync.Mutex
	state string
}

// newCeremonyServer starts the page server. open opens the page in the
// system browser; tests replace it.
func newCeremonyServer(open func(string) error) (*ceremonyServer, error) {
	if open == nil {
		return nil, errors.New("passkey: no browser to open")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("passkey: starting the ceremony page: %w", err)
	}
	c := &ceremonyServer{
		ln:      ln,
		open:    open,
		results: make(chan ceremonyOutcome, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", c.servePage)
	mux.HandleFunc("/ceremony.js", c.serveAsset("ceremony.js", "text/javascript; charset=utf-8"))
	mux.HandleFunc("/style.css", c.serveAsset("style.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/result", c.serveResult)
	c.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = c.srv.Serve(ln) }()
	return c, nil
}

// origin is the page's origin. localhost, not 127.0.0.1: WebAuthn's relying
// party id is a domain name, and the page uses its own hostname.
func (c *ceremonyServer) origin() string {
	return fmt.Sprintf("http://localhost:%d", c.ln.Addr().(*net.TCPAddr).Port)
}

// run opens the page for req and waits for its answer.
func (c *ceremonyServer) run(ctx context.Context, req webauthn.CeremonyRequest) (webauthn.CeremonyResult, error) {
	if err := ctx.Err(); err != nil {
		return webauthn.CeremonyResult{}, err
	}
	state, err := randomState()
	if err != nil {
		return webauthn.CeremonyResult{}, err
	}
	req.State = state
	page, err := webauthn.CeremonyURL(c.origin()+"/", req)
	if err != nil {
		return webauthn.CeremonyResult{}, err
	}
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.state == state {
			c.state = ""
		}
		c.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return webauthn.CeremonyResult{}, err
	}
	if err := c.open(page); err != nil {
		return webauthn.CeremonyResult{}, fmt.Errorf("passkey: opening the browser: %w", err)
	}
	timer := time.NewTimer(ceremonyTimeout)
	defer timer.Stop()
	select {
	case out := <-c.results:
		if err := ctx.Err(); err != nil {
			return webauthn.CeremonyResult{}, err
		}
		if out.result.State != state {
			return webauthn.CeremonyResult{}, errors.New("passkey: the result is for another ceremony")
		}
		if out.err != nil {
			return webauthn.CeremonyResult{}, out.err
		}
		return out.result, nil
	case <-ctx.Done():
		return webauthn.CeremonyResult{}, ctx.Err()
	case <-timer.C:
		return webauthn.CeremonyResult{}, errors.New("passkey: the browser did not answer in time")
	}
}

func (c *ceremonyServer) close() {
	if c == nil || c.srv == nil {
		return
	}
	c.mu.Lock()
	c.state = ""
	c.mu.Unlock()
	_ = c.srv.Close()
}

func (c *ceremonyServer) servePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := passkeypage.Files.ReadFile("index.html")
	if err != nil {
		http.Error(w, "the passkey page is missing", http.StatusInternalServerError)
		return
	}
	noStore(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (c *ceremonyServer) serveAsset(name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, err := passkeypage.Files.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		noStore(w)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}
}

// serveResult accepts the page's POST. The body is at most a ceremony
// result; the state must match the ceremony the app started. A forged result
// still has to pass the WebAuthn checks the app runs next.
func (c *ceremonyServer) serveResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	noStore(w)
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<10))
	if err != nil {
		http.Error(w, "the result is too large", http.StatusRequestEntityTooLarge)
		return
	}
	result, err := webauthn.ParseResultJSON(body)
	out := ceremonyOutcome{result: result}
	if err != nil {
		var ceremonyErr *webauthn.CeremonyError
		if !errors.As(err, &ceremonyErr) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out.result.State = ceremonyErr.State
		out.err = ceremonyErr
	}
	if !c.deliver(out) {
		http.Error(w, "the app is not waiting for this ceremony", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *ceremonyServer) deliver(out ceremonyOutcome) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == "" || out.result.State != c.state {
		return false
	}
	select {
	case c.results <- out:
		c.state = "" // One result or error consumes the pending ceremony.
		return true
	default:
		return false
	}
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func randomState() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("passkey: generating a ceremony state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
