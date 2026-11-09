package app

import (
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/wallet"
)

// writeAgentPlugin writes a plugin folder for the local API tests.
func writeAgentPlugin(t *testing.T, dir, id, source string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "id": %q,
  "name": "Agent test",
  "version": "1.0.0",
  "main": "index.js",
  "permissions": { "sign": true, "chains": ["eip155:1"] },
  "navigation": [{ "title": "Overview", "page": "overview" }]
}`, id)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentPortfolio(t *testing.T) {
	a := newTestApp(t)
	a.settings.Accounts = []wallet.Address{{Addr: viewAddr, Label: "Main"}}
	a.lastUpdated = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	a.snap = &snapshot{
		at:    a.lastUpdated,
		block: 42,
		accounts: []accountSnapshot{{
			addr:   viewAddr,
			native: big.NewInt(1_500_000_000_000_000_000),
			tokens: []chain.TokenBalance{{
				Token:  chains.Ethereum.Tokens[0], // USDC, 6 decimals
				Amount: big.NewInt(1_200_000_000),
			}},
		}},
	}
	p := a.Portfolio()
	if p.Network == "" || p.ChainID != a.settings.NetworkID || p.Block != 42 {
		t.Fatalf("portfolio = %+v", p)
	}
	if len(p.Accounts) != 1 {
		t.Fatalf("accounts = %+v", p.Accounts)
	}
	acc := p.Accounts[0]
	if acc.Address != viewAddr || acc.Label != "Main" {
		t.Fatalf("account = %+v", acc)
	}
	if acc.Native.Symbol != "ETH" || acc.Native.Decimals != 18 ||
		acc.Native.Amount != "1500000000000000000" {
		t.Fatalf("native = %+v", acc.Native)
	}
	if len(acc.Tokens) != 1 || acc.Tokens[0].Symbol != "USDC" ||
		acc.Tokens[0].Amount != "1200000000" || acc.Tokens[0].Contract == "" {
		t.Fatalf("tokens = %+v", acc.Tokens)
	}
}

func TestAgentPlugins(t *testing.T) {
	a := newTestApp(t)
	plugins := a.Plugins()
	if len(plugins) != 2 {
		t.Fatalf("plugins = %+v", plugins)
	}
	byID := map[string]agent.Plugin{}
	for _, p := range plugins {
		byID[p.ID] = p
	}
	p, ok := byID["com.example.gas"]
	if !ok || p.Name != "Gas" || !p.Enabled || !p.Official || p.Signed {
		t.Fatalf("gas = %+v", p)
	}
	if len(p.Permissions.Chains) != 2 || len(p.Pages) != 1 || p.Pages[0] != "overview" || p.Permissions.Sign {
		t.Fatalf("gas = %+v", p)
	}
	fg, ok := byID["com.example.feargreed"]
	if !ok || fg.Name != "Fear & Greed" || !fg.Official || fg.Signed || fg.Permissions.Sign {
		t.Fatalf("feargreed = %+v", fg)
	}
	if len(fg.Permissions.Network) != 1 || fg.Permissions.Network[0] != "api.alternative.me" {
		t.Fatalf("feargreed = %+v", fg)
	}
}

func TestAgentPluginsShowsBrokenFolders(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(a.plugins.Dir(), "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	plugins := a.Plugins()
	var broken agent.Plugin
	found := false
	for _, p := range plugins {
		if p.Folder == "broken" {
			broken = p
			found = true
		}
	}
	if !found || broken.Broken == "" || len(plugins) != 3 {
		t.Fatalf("plugins = %+v", plugins)
	}
}

func TestAgentRefreshRunsThePage(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "static")
	writeAgentPlugin(t, dir, "com.example.static",
		`export function page() {
  return { title: "Static", sections: [{ stats: [{ label: "Value", value: "42" }] }] };
}`)
	m, err := a.plugins.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	got, err := a.Refresh(m.ID)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	page, ok := got.(pages.Page)
	if !ok || page.Title != "Static" || page.Sections[0].Stats[0].Value != "42" {
		t.Fatalf("page = %+v", got)
	}
}

// TestAgentRefreshRefusesSigning checks the API's read-only promise: a page
// that asks to sign is refused before any approval window can open.
func TestAgentRefreshRefusesSigning(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "signer")
	writeAgentPlugin(t, dir, "com.example.signer",
		`export function page() {
  host.requestSign({ chainId: "eip155:1", to: "0x2c7536e3605d9c16a7a3d7b1898e529396a65c23", value: "1" });
  return { title: "Signed", sections: [] };
}`)
	m, err := a.plugins.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	if _, err := a.Refresh(m.ID); err == nil ||
		!strings.Contains(err.Error(), "not available over the local API") {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentRefreshErrors(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.Refresh("com.example.missing"); err == nil {
		t.Fatal("a missing app was refreshed")
	}
	dir := filepath.Join(t.TempDir(), "static")
	writeAgentPlugin(t, dir, "com.example.off",
		`export function page() { return { title: "Off", sections: [] }; }`)
	m, err := a.plugins.Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.plugins.SetEnabled(m.ID, false); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	if _, err := a.Refresh(m.ID); err == nil || !strings.Contains(err.Error(), "turned off") {
		t.Fatalf("err = %v", err)
	}
}

// freePort finds a port nothing listens on, for the API's test server.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestAgentServerStartsAndStops(t *testing.T) {
	a := newTestApp(t)
	secrets := memStore{}
	a.agentSecrets = secrets
	a.settings.AgentPort = freePort(t)
	a.setAgentEnabled(true)
	if a.agentSrv == nil || a.agentAddr == "" {
		t.Fatalf("the API did not start: %q", a.agentErr)
	}
	defer a.stopAgent()

	res, err := http.Get("http://" + a.agentAddr + "/v0/portfolio")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d without a token", res.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+a.agentAddr+"/v0/portfolio", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+a.agentToken)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d with the token", res.StatusCode)
	}

	a.setAgentEnabled(false)
	if a.agentSrv != nil || a.agentAddr != "" {
		t.Fatal("the API did not stop")
	}
	if _, ok := secrets["token"]; !ok {
		t.Fatal("the token is not in the keychain")
	}
}

func TestAgentRotateToken(t *testing.T) {
	a := newTestApp(t)
	secrets := memStore{}
	a.agentSecrets = secrets
	a.settings.AgentPort = freePort(t)
	a.setAgentEnabled(true)
	defer a.stopAgent()
	old := a.agentToken
	a.rotateAgentToken()
	if a.agentToken == "" || a.agentToken == old {
		t.Fatalf("token = %q, old = %q, err = %q", a.agentToken, old, a.agentErr)
	}
	if string(secrets["token"]) != a.agentToken {
		t.Fatal("the keychain still holds the old token")
	}
}

func TestSettingsShowsTheAgentAPI(t *testing.T) {
	a := newTestApp(t)
	a.agentSecrets = memStore{}
	a.settings.AgentEnabled = true
	a.settings.AgentPort = freePort(t)
	a.startAgent()
	defer a.stopAgent()
	a.route = routeSettings
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"LOCAL API", "Local agent API", "Bearer token", a.agentToken, "Rotate token"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}
