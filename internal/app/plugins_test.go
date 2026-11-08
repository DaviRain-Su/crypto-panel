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
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Install from folder…", "No plugins installed", "plugin.json"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

// installExamplePlugin copies one shipped example into the app's store.
func installExamplePlugin(t *testing.T, a *App, name string) string {
	t.Helper()
	m, err := a.plugins.Install(filepath.Join("..", "..", "examples", "plugins", name))
	if err != nil {
		t.Fatalf("installing %s: %v", name, err)
	}
	a.refreshPlugins()
	return m.ID
}

func TestPluginsManagerShowsInstalled(t *testing.T) {
	a := newTestApp(t)
	id := installExamplePlugin(t, a, "gas")
	a.route = routePlugins
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	for _, want := range []string{"Gas", "Unsigned", "v1.0.0", id, "Remove", "Logs"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
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
	if a.pluginView.err != "This plugin is turned off." {
		t.Fatalf("err = %q", a.pluginView.err)
	}
}
