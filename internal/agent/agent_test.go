package agent

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeData struct {
	portfolio Portfolio
	plugins   []Plugin
	pages     map[string]any
	errs      map[string]error
}

func (f *fakeData) Portfolio() Portfolio { return f.portfolio }
func (f *fakeData) Plugins() []Plugin    { return f.plugins }
func (f *fakeData) Refresh(id string) (any, error) {
	if err := f.errs[id]; err != nil {
		return nil, err
	}
	if page, ok := f.pages[id]; ok {
		return page, nil
	}
	return nil, ErrNotFound
}

func newTestServer(t *testing.T) (*Server, *fakeData) {
	t.Helper()
	data := &fakeData{
		portfolio: Portfolio{
			Network: "Ethereum",
			ChainID: "eip155:1",
			Updated: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
			Block:   26370027,
			Accounts: []Account{{
				Address: "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23",
				Label:   "Main",
				Native:  Amount{Symbol: "ETH", Decimals: 18, Amount: "1500000000000000000"},
			}},
		},
		plugins: []Plugin{{
			ID: "com.example.gas", Name: "Gas", Version: "1.0.0", Enabled: true,
			Permissions: Permissions{Chains: []string{"eip155:1"}},
			Pages:       []string{"overview"},
		}},
		pages: map[string]any{"com.example.gas": map[string]any{
			"title": "Gas", "sections": []any{map[string]any{
				"stats": []any{map[string]any{"label": "Gas", "value": "1.00 gwei"}},
			}},
		}},
		errs: map[string]error{"com.example.off": ErrDisabled, "com.example.empty": ErrNoPages},
	}
	return New(data, "test-token"), data
}

func withLocalHost(s *Server, req *http.Request) *http.Request {
	req.Host = "127.0.0.1:9"
	if s != nil && s.ln != nil {
		if host := s.ln.Addr().String(); host != "" {
			req.Host = host
		}
	}
	return req
}

func do(t *testing.T, s *Server, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := withLocalHost(s, httptest.NewRequest(method, path, nil))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	return rec
}

func TestRefusesWithoutTheToken(t *testing.T) {
	s, _ := newTestServer(t)
	for _, token := range []string{"", "wrong", "test-token "} {
		rec := do(t, s, http.MethodGet, "/v0/portfolio", token)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: status %d", token, rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatal("no WWW-Authenticate header")
		}
	}
}

func TestRefusesBrowserOrigins(t *testing.T) {
	s, _ := newTestServer(t)
	req := withLocalHost(s, httptest.NewRequest(http.MethodGet, "/v0/portfolio", nil))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestRejectsMultipleOriginsCrossSiteAndForeignHost(t *testing.T) {
	s, _ := newTestServer(t)
	forbid := func(t *testing.T, mutate func(*http.Request)) {
		t.Helper()
		req := withLocalHost(s, httptest.NewRequest(http.MethodGet, "/v0/portfolio", nil))
		req.Header.Set("Authorization", "Bearer test-token")
		mutate(req)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status %d, body %s", rec.Code, rec.Body.Bytes())
		}
	}
	t.Run("multiple origins", func(t *testing.T) {
		forbid(t, func(req *http.Request) {
			req.Header.Add("Origin", "https://a.example")
			req.Header.Add("Origin", "https://b.example")
		})
	})
	t.Run("multiple origins with empty first", func(t *testing.T) {
		forbid(t, func(req *http.Request) {
			req.Header["Origin"] = []string{"", "https://evil.example"}
		})
	})
	t.Run("cross-site", func(t *testing.T) {
		forbid(t, func(req *http.Request) {
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		})
	})
	t.Run("cross-site case and padding", func(t *testing.T) {
		forbid(t, func(req *http.Request) {
			req.Header.Set("Sec-Fetch-Site", " Cross-Site ")
		})
	})
	t.Run("foreign host", func(t *testing.T) {
		forbid(t, func(req *http.Request) { req.Host = "evil.example" })
	})
	t.Run("host without port", func(t *testing.T) {
		forbid(t, func(req *http.Request) { req.Host = "127.0.0.1" })
	})
	for _, site := range []string{"none", "same-origin", "same-site"} {
		t.Run(site, func(t *testing.T) {
			req := withLocalHost(s, httptest.NewRequest(http.MethodGet, "/v0/portfolio", nil))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Sec-Fetch-Site", site)
			rec := httptest.NewRecorder()
			s.handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s status %d", site, rec.Code)
			}
		})
	}
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	forbid(t, func(req *http.Request) { req.Host = "127.0.0.1:1" })
	forbid(t, func(req *http.Request) { req.Host = "localhost:1" })
	req := withLocalHost(s, httptest.NewRequest(http.MethodGet, "/v0/portfolio", nil))
	req.Header.Set("Authorization", "Bearer test-token")
	port := s.Addr()[strings.LastIndex(s.Addr(), ":"):]
	req.Host = "localhost" + port
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("localhost status %d", rec.Code)
	}
	req = withLocalHost(s, httptest.NewRequest(http.MethodGet, "/v0/portfolio", nil))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Host = "LOCALHOST" + port
	rec = httptest.NewRecorder()
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("LOCALHOST status %d", rec.Code)
	}
}

func TestLoopbackHostDoesNotRaceWithClose(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	host := s.Addr()
	done := make(chan struct{})
	go func() {
		_ = s.Close()
		close(done)
	}()
	for {
		if !s.loopbackHost(host) {
			t.Fatal("bound host rejected")
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

func TestRefreshOmitsEmptyMetadataHeaders(t *testing.T) {
	_, base := newTestServer(t)
	s := New(headerManagedEmpty{base}, "test-token")
	rec := do(t, s, http.MethodPost, "/v0/plugins/com.example.gas/refresh", "test-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	for _, name := range []string{
		"X-Crypto-Panel-Package-Hash", "X-Lintel-Package-Hash",
		"X-Crypto-Panel-Evaluated-At", "X-Lintel-Evaluated-At",
	} {
		if rec.Header().Get(name) != "" {
			t.Fatalf("%s = %q", name, rec.Header().Get(name))
		}
	}
}

func TestPortfolio(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/v0/portfolio", "test-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got Portfolio
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Network != "Ethereum" || got.Block != 26370027 {
		t.Fatalf("portfolio = %+v", got)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Native.Amount != "1500000000000000000" {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the answer may be cached")
	}
}

func TestPlugins(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/v0/plugins", "test-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var got struct {
		Plugins []Plugin `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].ID != "com.example.gas" || !got.Plugins[0].Enabled {
		t.Fatalf("plugins = %+v", got.Plugins)
	}
}

func TestRefreshReturnsThePage(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodPost, "/v0/plugins/com.example.gas/refresh", "test-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "1.00 gwei") {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestRefreshMapsErrors(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct {
		id     string
		status int
	}{
		{"com.example.missing", http.StatusNotFound},
		{"com.example.off", http.StatusConflict},
		{"com.example.empty", http.StatusConflict},
	}
	for _, c := range cases {
		rec := do(t, s, http.MethodPost, "/v0/plugins/"+c.id+"/refresh", "test-token")
		if rec.Code != c.status {
			t.Fatalf("%s: status %d, want %d", c.id, rec.Code, c.status)
		}
		if !strings.Contains(rec.Body.String(), "error") {
			t.Fatalf("%s: body = %s", c.id, rec.Body)
		}
	}
}

func TestRefreshIsPostOnly(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, http.MethodGet, "/v0/plugins/com.example.gas/refresh", "test-token")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestStartBindsLoopback(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	addr := s.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("addr = %q", addr)
	}
	res, err := http.Get("http://" + addr + "/v0/portfolio")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d without a token", res.StatusCode)
	}
}

func TestNewToken(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) < 32 {
		t.Fatalf("tokens %q and %q", a, b)
	}
}

func TestRefreshWrapsDataErrors(t *testing.T) {
	s, data := newTestServer(t)
	data.errs["com.example.bad"] = errors.New("fetch refused")
	rec := do(t, s, http.MethodPost, "/v0/plugins/com.example.bad/refresh", "test-token")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
}

// TestRebindSamePort restarts the server on the port it just released, as
// rotating the token or changing the port does.
func TestRebindSamePort(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Start(0); err != nil {
		t.Fatal(err)
	}
	port := s.ln.Addr().(*net.TCPAddr).Port
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestServer(t)
	if err := next.Start(port); err != nil {
		t.Fatalf("rebind %d: %v", port, err)
	}
	next.Close()
}
