package app

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/pages"
	"github.com/davirain-su/crypto-panel/internal/plugin"
)

var live = flag.Bool("live", false, "hit the public endpoints in TestLiveGasPlugin")

func TestPluginsEmptyView(t *testing.T) {
	a := newTestApp(t)
	removeAllPlugins(t, a)
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"No apps yet", "Install from folder…", "Install from zip…", "plugin.json"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

// installExamplePlugin returns one example from the app's store, installing
// the testdata folder only when first launch has not already seeded it.
func installExamplePlugin(t *testing.T, a *App, name string) string {
	t.Helper()
	src := filepath.Join("..", "plugin", "testdata", "examples", name)
	m, err := plugin.LoadManifest(src)
	if err != nil {
		t.Fatalf("manifest %s: %v", name, err)
	}
	if _, err := a.plugins.Get(m.ID); err == nil {
		a.refreshPlugins()
		return m.ID
	}
	m, err = a.plugins.Install(src)
	if err != nil {
		t.Fatalf("installing %s: %v", name, err)
	}
	a.refreshPlugins()
	return m.ID
}

// removeAllPlugins deletes every installed app, including the official
// examples a first launch seeds.
func removeAllPlugins(t *testing.T, a *App) {
	t.Helper()
	items, err := a.plugins.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Manifest.ID != "" {
			if err := a.plugins.Remove(item.Manifest.ID); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := a.plugins.RemoveAt(item.Dir); err != nil {
			t.Fatal(err)
		}
	}
	a.refreshPlugins()
}

func TestOfficialExamplesOnFirstLaunch(t *testing.T) {
	a := newTestApp(t)
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Gas", "Fear & Greed", "Official", "v1.0.0", "Open", "App settings", "2 APPS · 2 ON"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Unsigned") || tt.HasText("No apps yet") {
		t.Fatalf("official examples are labeled as a local install: %q", tt.Texts())
	}
	for _, id := range []string{"com.example.gas", "com.example.feargreed"} {
		inst, ok := a.installedPlugin(id)
		if !ok || !inst.Official || inst.Signed || inst.Source != plugin.OfficialSource {
			t.Fatalf("%s = %+v, ok %v", id, inst, ok)
		}
		if inst.Manifest.Permissions.Sign || len(inst.Manifest.Permissions.Read) != 0 {
			t.Fatalf("%s permissions = %+v", id, inst.Manifest.Permissions)
		}
	}
	gas, _ := a.installedPlugin("com.example.gas")
	if len(gas.Manifest.Permissions.Chains) != 2 || len(gas.Manifest.Permissions.Network) != 0 {
		t.Fatalf("gas permissions = %+v", gas.Manifest.Permissions)
	}
	fg, _ := a.installedPlugin("com.example.feargreed")
	if len(fg.Manifest.Permissions.Network) != 1 || fg.Manifest.Permissions.Network[0] != "api.alternative.me" ||
		len(fg.Manifest.Permissions.Chains) != 0 {
		t.Fatalf("feargreed permissions = %+v", fg.Manifest.Permissions)
	}
	a.openPluginDetail("com.example.gas")
	tt = ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"All apps", "com.example.gas", "Official", "Logs"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Unsigned") {
		t.Fatalf("app settings calls the official example unsigned: %q", tt.Texts())
	}
	a.openPluginApp("com.example.gas")
	if a.route.kind != "plugin" || a.route.id != "com.example.gas" || a.route.page != "overview" {
		t.Fatalf("Open went to %+v", a.route)
	}
}

func TestFolderInstallStaysUnsigned(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(t.TempDir(), "local")
	writeAgentPlugin(t, dir, "com.example.local", "export function page() { return { title: 'Local' }; }")
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("Unsigned") || !tt.HasText("Official") {
		t.Fatalf("folder and official installs are not both labeled: %q", tt.Texts())
	}
	inst, ok := a.installedPlugin("com.example.local")
	if !ok || inst.Official || inst.Signed || inst.Source != "" {
		t.Fatalf("folder install = %+v, ok %v", inst, ok)
	}
}

func TestTamperedOfficialExampleDoesNotOpen(t *testing.T) {
	a := newTestApp(t)
	inst, err := a.plugins.Get("com.example.gas")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst.Dir, "index.js"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("changed") {
		t.Fatalf("the tampered example is not shown as broken: %q", tt.Texts())
	}
	a.openPluginApp("com.example.gas")
	if a.route.kind == "plugin" {
		t.Fatalf("Open ran a tampered official example: %+v", a.route)
	}
	a.openPluginPage("com.example.gas", "overview")
	if a.pluginView.err == "" || a.pluginView.official || a.pluginView.page.Title != "" {
		t.Fatalf("page = %+v", a.pluginView)
	}
}

func TestPluginsAppDetail(t *testing.T) {
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "gas")
	a.openPluginDetail(id)
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"All apps", id, "Remove", "Logs", "Gas", "Official"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Unsigned") {
		t.Fatalf("official example is unsigned: %q", tt.Texts())
	}
	if err := tt.Click("All apps"); err != nil {
		t.Fatal(err)
	}
	if a.pluginDetail != "" {
		t.Fatalf("All apps left the detail open: %q", a.pluginDetail)
	}
}

func TestPluginsBrokenFolder(t *testing.T) {
	a := newTestApp(t)
	dir := filepath.Join(a.plugins.Dir(), "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("broken") {
		t.Fatalf("the broken folder is not shown: %q", tt.Texts())
	}
}

func TestOfficialPageHeader(t *testing.T) {
	a := newTestApp(t)
	a.route = route{kind: "plugin", id: "com.example.gas", page: "overview"}
	a.pluginView = pluginPageState{
		key:      a.pluginKey(),
		name:     "Gas",
		official: true,
		page: pages.Page{
			Title: "Gas",
			Sections: []pages.Section{{
				Stats: []pages.Stat{{Label: "Ethereum gas", Value: "1.00 gwei"}},
			}},
		},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Gas", "Official", "Ethereum gas", "1.00 gwei"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Unsigned") {
		t.Fatalf("the official page is marked unsigned: %q", tt.Texts())
	}
}

func TestPluginPageView(t *testing.T) {
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "gas")
	a.route = route{kind: "plugin", id: id, page: "overview"}
	a.pluginView = pluginPageState{
		key:      a.pluginKey(),
		name:     "Gas",
		unsigned: true,
		page: pages.Page{
			Title: "Gas",
			Sections: []pages.Section{{
				Stats: []pages.Stat{{Label: "Ethereum gas", Value: "1.00 gwei"}},
			}},
		},
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Gas", "Unsigned", "Ethereum gas", "1.00 gwei"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestPluginPageError(t *testing.T) {
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "gas")
	a.route = route{kind: "plugin", id: id, page: "overview"}
	a.pluginView = pluginPageState{
		key:      a.pluginKey(),
		name:     "Gas",
		unsigned: true,
		err:      "fetch: evil.example is not in the plugin's network allowlist",
	}
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("not in the plugin's network allowlist") {
		t.Fatalf("the refusal is not shown: %q", tt.Texts())
	}
}

// TestLiveGasPlugin walks the whole path a third-party plugin takes: the
// example folder installed by hand, the manifest's chains permission, the
// host function, the app's pinned reader and the public endpoint.
func TestLiveGasPlugin(t *testing.T) {
	if !*live {
		t.Skip("run with -live")
	}
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "gas")
	inst, err := a.plugins.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(inst.Dir, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	host := plugin.NewHost(a.pluginHostDeps(inst.Manifest))
	r, err := plugin.NewRunner(inst.Manifest, source, host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("overview")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	rows := page.Sections[1].Table.Rows
	if len(rows) == 0 || rows[0][0] != "Ethereum" {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0][1] == "—" {
		t.Fatalf("Ethereum gas is unavailable: %v", rows[0])
	}
	t.Logf("ethereum gas %s gwei at block %s", rows[0][1], rows[0][2])
}

// TestLiveFearGreedPlugin checks the other half of the host: an allowlisted
// fetch, JSON.parse in the plugin, and a chart the shell renders.
func TestLiveFearGreedPlugin(t *testing.T) {
	if !*live {
		t.Skip("run with -live")
	}
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "fear-greed")
	inst, err := a.plugins.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(inst.Dir, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	host := plugin.NewHost(a.pluginHostDeps(inst.Manifest))
	r, err := plugin.NewRunner(inst.Manifest, source, host, "en-US")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer r.Close()
	page, err := r.Page("index")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	stats := page.Sections[0].Stats
	if len(stats) != 3 || stats[0].Value == "" {
		t.Fatalf("stats = %+v", stats)
	}
	chart := page.Sections[1].Chart
	if chart == nil || len(chart.Points) != 30 {
		t.Fatalf("chart = %+v", chart)
	}
	t.Logf("fear & greed now %s, %d points", stats[0].Value, len(chart.Points))
}

func TestPluginDisabledPage(t *testing.T) {
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "gas")
	if err := a.plugins.SetEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	a.refreshPlugins()
	a.openPluginPage(id, "overview")
	if a.pluginView.err != "This app is turned off." {
		t.Fatalf("err = %q", a.pluginView.err)
	}
}
