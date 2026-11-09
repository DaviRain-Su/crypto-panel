package app

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

// writeStorePlugin writes a plugin folder for the store tests.
func writeStorePlugin(t *testing.T, dir, id, version, source string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "id": %q,
  "name": "Remote",
  "version": %q,
  "main": "index.js",
  "permissions": { "network": ["api.example.com"] }
}`, id, version)
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// storeEntry signs a plugin folder the way a registry would.
func storeEntry(t *testing.T, dir, url string, key ed25519.PrivateKey) plugin.RegistryEntry {
	t.Helper()
	m, err := plugin.LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plugin.DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return plugin.RegistryEntry{
		ID:          m.ID,
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		URL:         url,
		Hash:        digest,
		Signature:   plugin.SignDigest(key, digest),
		Publisher:   plugin.PublicKeyString(key.Public().(ed25519.PublicKey)),
	}
}

func TestStoreStartsWithNoRegistry(t *testing.T) {
	a := newTestApp(t)
	a.route = routePlugins
	a.setPluginTab("store")
	if a.storeErr != "" {
		t.Fatalf("store: %v", a.storeErr)
	}
	if len(a.storeItems) != 0 {
		t.Fatalf("the app ships a catalog: %+v", a.storeItems)
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Installed", "Store", "No plugin registry yet", "Open Settings"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestStoreInstallsFromRegistry(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeStorePlugin(t, src, "com.example.remote", "1.0.0", "export function page() { return {}; }")
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := plugin.ZipDir(src)
	if err != nil {
		t.Fatal(err)
	}
	var entry plugin.RegistryEntry
	mux := http.NewServeMux()
	mux.HandleFunc("/registry.json", func(w http.ResponseWriter, r *http.Request) {
		entry.URL = "http://" + r.Host + "/p.zip"
		reg := plugin.Registry{Version: 1, Name: "Test", Plugins: []plugin.RegistryEntry{entry}}
		json.NewEncoder(w).Encode(reg)
	})
	mux.HandleFunc("/p.zip", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	entry = storeEntry(t, src, srv.URL+"/p.zip", key)

	a := newTestApp(t)
	a.settings.PluginStoreURL = srv.URL + "/registry.json"
	a.storeURLDraft = a.settings.PluginStoreURL
	a.route = routePlugins
	a.setPluginTab("store")
	if a.storeErr != "" {
		t.Fatalf("store: %v", a.storeErr)
	}
	if len(a.storeItems) != 1 || a.storeItems[0].ID != entry.ID {
		t.Fatalf("items = %+v", a.storeItems)
	}
	a.installFromStore(a.storeItems[0])
	inst, err := a.plugins.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !inst.Signed {
		t.Fatal("the registry install is not signed")
	}
	if inst.Source != entry.URL {
		t.Fatalf("source = %q", inst.Source)
	}
}

func TestStoreInstallRefusedIsShown(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeStorePlugin(t, src, "com.example.bad", "1.0.0", "export function page() { return {}; }")
	_, key, _ := ed25519.GenerateKey(nil)
	archive, err := plugin.ZipDir(src)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer srv.Close()
	entry := storeEntry(t, src, srv.URL+"/p.zip", key)
	entry.Hash = "sha256:" + strings.Repeat("00", 32)
	a := newTestApp(t)
	a.route = routePlugins
	a.setPluginTab("store")
	a.storeItems = []plugin.RegistryEntry{entry}
	a.installFromStore(entry)
	if !strings.Contains(a.pluginNotice, "Install failed") {
		t.Fatalf("notice = %q", a.pluginNotice)
	}
	if _, err := a.plugins.Get(entry.ID); err == nil {
		t.Fatal("a plugin with a bad hash was installed")
	}
}

func TestInstallPromptView(t *testing.T) {
	m := plugin.Manifest{
		ID:      "com.example.signer",
		Name:    "Signer",
		Version: "1.0.0",
		Author:  "Ada",
		Permissions: plugin.Permissions{
			Network:   []string{"api.example.com"},
			Chains:    []string{"eip155:1", "eip155:8453"},
			Read:      []string{"balances"},
			Sign:      true,
			StorageKb: 64,
		},
		Settings: []plugin.SettingField{{Key: "k", Label: "K"}},
	}
	ip := &installPrompt{
		t:   func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		m:   m,
		req: installRequest{source: "https://example.com/registry.json"},
	}
	tt := ui.NewTester(ip.view, 460, 620)
	for _, want := range []string{
		"Install Signer?", "This plugin can",
		"Network: api.example.com", "Chains: Ethereum, Base",
		"See your watched addresses and balances",
		"Ask you to sign transactions", "Store 64 KB of data",
		"Settings: 1", "Cancel", "Install",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestInstallPromptMarksNewPermissions(t *testing.T) {
	old := plugin.Manifest{
		ID:          "com.example.signer",
		Name:        "Signer",
		Version:     "1.0.0",
		Permissions: plugin.Permissions{Chains: []string{"eip155:1"}},
	}
	next := old
	next.Version = "1.1.0"
	next.Permissions = plugin.Permissions{
		Chains: []string{"eip155:1", "eip155:8453"},
		Sign:   true,
	}
	ip := &installPrompt{
		t:   func(k i18n.Key, args ...any) string { return i18n.T(i18n.English, k, args...) },
		m:   next,
		req: installRequest{update: true, old: &old},
	}
	tt := ui.NewTester(ip.view, 460, 620)
	for _, want := range []string{"Update Signer to v1.1.0?", "New permissions:", "Chains: Base", "Ask you to sign transactions", "Update"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Install Signer?") {
		t.Error("an update is titled as a fresh install")
	}
}

func TestStoreURLValidation(t *testing.T) {
	a := newTestApp(t)
	a.storeURLDraft = "http://example.com/registry.json"
	a.applyStoreURL()
	if a.storeFormErr == "" {
		t.Fatal("a remote http registry was accepted")
	}
	if a.settings.PluginStoreURL != "" {
		t.Fatalf("settings = %q", a.settings.PluginStoreURL)
	}
	a.storeURLDraft = "https://example.com/registry.json"
	a.applyStoreURL()
	if a.storeFormErr != "" {
		t.Fatalf("err = %q", a.storeFormErr)
	}
	if a.settings.PluginStoreURL != "https://example.com/registry.json" {
		t.Fatalf("settings = %q", a.settings.PluginStoreURL)
	}
}

func TestSettingsShowsTheStoreField(t *testing.T) {
	a := newTestApp(t)
	a.route = routeSettings
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Plugin registry", "Leave empty for no store", "Apply"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

// TestStoreSearchFilters checks the store's search box: a word of a name, an
// id, an author or a description narrows the catalog, and a word that matches
// nothing says so.
func TestStoreSearchFilters(t *testing.T) {
	a := newTestApp(t)
	a.settings.PluginStoreURL = "https://example.com/registry.json"
	a.storeLoaded = true
	a.storeItems = []plugin.RegistryEntry{
		{ID: "com.example.gas", Name: "Gas prices", Description: "Ethereum gas tracker"},
		{ID: "com.example.fear", Name: "Fear & Greed", Author: "Ada", Description: "Market sentiment"},
	}
	a.route = routePlugins
	a.pluginTab = "store"
	a.storeQuery = "sentiment"
	if got := a.filteredStoreItems(); len(got) != 1 || got[0].ID != "com.example.fear" {
		t.Fatalf("filtered = %+v", got)
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Fear & Greed") {
		t.Errorf("the match is not shown: %q", tt.Texts())
	}
	if tt.HasText("Gas prices") {
		t.Errorf("the non-match is shown: %q", tt.Texts())
	}
	a.storeQuery = "nothing matches this"
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("No app matches") {
		t.Errorf("the empty filter does not say so: %q", tt.Texts())
	}
}
