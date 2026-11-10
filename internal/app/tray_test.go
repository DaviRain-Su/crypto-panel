package app

import (
	"bytes"
	"errors"
	"image/png"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/plugin"
)

// nav is one declared page, the shape trayCandidates looks for.
func nav(page string) []plugin.NavItem { return []plugin.NavItem{{Page: page}} }

// fakeTray records what the app paints onto the native tray.
type fakeTray struct {
	title     string
	tip       string
	menus     int
	menu      *mygo.Menu
	destroyed int
	// painted is every string handed to SetTitle, SetToolTip, or a menu item.
	painted []string
}

func (f *fakeTray) SetTitle(title string) {
	f.title = title
	f.painted = append(f.painted, title)
}
func (f *fakeTray) SetToolTip(tip string) {
	f.tip = tip
	f.painted = append(f.painted, tip)
}
func (f *fakeTray) SetMenu(menu *mygo.Menu) {
	f.menus++
	f.menu = menu
	if menu == nil {
		return
	}
	for _, it := range menu.Items() {
		f.painted = append(f.painted, it.Label)
	}
}
func (f *fakeTray) Destroy() { f.destroyed++ }

// stubTray returns a newTray hook that hands back fake.
func stubTray(fake *fakeTray) func(mygo.TrayOptions) (trayHandle, error) {
	return func(mygo.TrayOptions) (trayHandle, error) { return fake, nil }
}

func pageWithStats(label, value string, more ...pages.Section) pages.Page {
	secs := append([]pages.Section{{Stats: []pages.Stat{{Label: label, Value: value}}}}, more...)
	return pages.Page{Title: "Overview", Sections: secs}
}

func TestTrayStat(t *testing.T) {
	p := pageWithStats("Gas", "12 gwei", pages.Section{Stats: []pages.Stat{{Label: "Block", Value: "42"}}})
	stat, ok := trayStat(p)
	if !ok || stat.Label != "Gas" || stat.Value != "12 gwei" {
		t.Fatalf("trayStat picked %+v (ok=%v), want the first stat", stat, ok)
	}
	if _, ok := trayStat(pages.Page{Title: "Empty"}); ok {
		t.Fatal("a page without sections must not yield a stat")
	}
	if _, ok := trayStat(pages.Page{Sections: []pages.Section{{}}}); ok {
		t.Fatal("a section without stats must not yield a stat")
	}
}

func TestTrayCandidates(t *testing.T) {
	items := []plugin.Installed{
		{Manifest: plugin.Manifest{ID: "a", Name: "Alpha", Navigation: nav("overview")}, Enabled: true},
		{Manifest: plugin.Manifest{ID: "b", Name: "Broken", Navigation: nav("overview")}, Enabled: true, Err: "missing files"},
		{Manifest: plugin.Manifest{ID: "c", Name: "Disabled", Navigation: nav("overview")}, Enabled: false},
		{Manifest: plugin.Manifest{ID: "d", Name: "No page"}, Enabled: true},
	}
	got := trayCandidates(items)
	if len(got) != 1 || got[0].Manifest.ID != "a" {
		t.Fatalf("trayCandidates = %+v, want only the enabled app with a page", got)
	}
}

func TestTrayCandidatesOrderedByName(t *testing.T) {
	items := []plugin.Installed{
		{Manifest: plugin.Manifest{ID: "z", Name: "Zeta"}, Enabled: true},
		{Manifest: plugin.Manifest{ID: "a", Name: "Alpha"}, Enabled: true},
	}
	got := trayCandidates(items)
	names := make([]string, 0, len(got))
	for _, it := range got {
		names = append(names, it.Manifest.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("candidates not sorted by name: %q", names)
	}
}

func TestTrayLayoutOf(t *testing.T) {
	entries := []trayEntry{
		{id: "a", name: "Alpha", page: "overview", label: "Gas", value: "12 gwei"},
		{id: "b", name: "Beta", page: "main", label: "Fear", value: "39"},
	}
	layout := trayLayoutOf(entries)
	if layout.title != "12 gwei" {
		t.Fatalf("title = %q, want the first app's value", layout.title)
	}
	if layout.tip != "Alpha · Gas" {
		t.Fatalf("tip = %q, want \"Alpha · Gas\"", layout.tip)
	}
	labels := make([]string, 0, len(layout.items))
	for _, it := range layout.items {
		labels = append(labels, it.label)
	}
	want := []string{"Alpha · 12 gwei", "Beta · 39"}
	if len(labels) != len(want) || labels[0] != want[0] || labels[1] != want[1] {
		t.Fatalf("menu labels = %q, want %q", labels, want)
	}
	if sig := trayLayoutSig(layout); sig == "" {
		t.Fatal("layout signature must not be empty")
	}
	// A value change must change the signature so the menu is rebuilt.
	entries[1].value = "40"
	if trayLayoutSig(trayLayoutOf(entries)) == trayLayoutSig(layout) {
		t.Fatal("signature must change when a stat changes")
	}
}

func TestTrayIconIsPNG(t *testing.T) {
	data := trayIconPNG()
	if len(data) == 0 {
		t.Fatal("tray icon is empty")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("tray icon is not a decodable PNG: %v", err)
	}
}

func TestCreateTrayRegistersOnce(t *testing.T) {
	a := newTestApp(t)
	fake := &fakeTray{}
	var opts *mygo.TrayOptions
	calls := 0
	a.newTray = func(o mygo.TrayOptions) (trayHandle, error) {
		calls++
		opts = &o
		return fake, nil
	}
	a.createTray()
	if a.tray == nil {
		t.Fatal("createTray left a.tray unset")
	}
	if opts.ID != "lintel" {
		t.Fatalf("tray id = %q", opts.ID)
	}
	if !opts.IconIsTemplate || len(opts.Icon) == 0 {
		t.Fatal("tray icon must be a non-empty template image")
	}
	if opts.ToolTip != mygo.App.Name() {
		t.Fatalf("tooltip = %q, want the app name", opts.ToolTip)
	}
	if fake.menus != 1 {
		t.Fatalf("createTray painted the menu %d times", fake.menus)
	}
	// A second call must not create another tray. The menu is not repainted
	// when nothing changed, so the creation count is what shows the leak.
	a.createTray()
	if calls != 1 {
		t.Fatalf("newTray calls = %d, want 1", calls)
	}
	if fake.menus != 1 {
		t.Fatal("createTray recreated an existing tray")
	}
}

func TestCreateTrayFailureIsSilent(t *testing.T) {
	a := newTestApp(t)
	a.newTray = func(mygo.TrayOptions) (trayHandle, error) { return nil, errors.New("no tray here") }
	a.createTray()
	if a.tray != nil {
		t.Fatal("a failed createTray must leave a.tray nil")
	}
	a.updateTray() // must not panic without a tray
}

func TestUpdateTrayWithoutTrayIsQuiet(t *testing.T) {
	a := newTestApp(t)
	a.updateTray()
	if len(a.trayEntries) != 0 {
		t.Fatalf("entries = %v without a tray", a.trayEntries)
	}
}

// installTrayApp installs one local app whose overview page answers a
// single stat, the shape every tray entry comes from.
func installTrayApp(t *testing.T, a *App) {
	t.Helper()
	removeAllPlugins(t, a)
	dir := filepath.Join(t.TempDir(), "tray")
	writeAgentPlugin(t, dir, "com.example.tray", `export function page() {
  return { title: 'Overview', sections: [{ stats: [{ label: 'Index', value: '42' }] }] };
}`)
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatalf("installing: %v", err)
	}
	a.refreshPlugins()
}

// syncTrayApp makes the tray pipeline synchronous and hands the app a fake
// tray, so a test can assert on what the menu bar would show.
func syncTrayApp(t *testing.T, a *App) *fakeTray {
	t.Helper()
	a.runOnMain = func(f func()) { f() }
	a.goAsync = func(f func()) { f() }
	fake := &fakeTray{}
	a.newTray = stubTray(fake)
	a.createTray()
	return fake
}

func TestUpdateTrayShowsFirstStat(t *testing.T) {
	a := newTestApp(t)
	installTrayApp(t, a)
	fake := syncTrayApp(t, a)
	a.updateTray()

	if fake.title != "42" {
		t.Fatalf("tray title = %q, want the app's first stat value", fake.title)
	}
	if fake.tip != "Agent test · Index" {
		t.Fatalf("tray tooltip = %q", fake.tip)
	}
	entry := a.trayEntries["com.example.tray"]
	if entry.page != "overview" || entry.value != "42" {
		t.Fatalf("tray entry = %+v", entry)
	}
	if fake.menus < 2 {
		t.Fatal("updateTray did not repaint the menu")
	}
}

func TestUpdateTrayKeepsBusyValue(t *testing.T) {
	a := newTestApp(t)
	installTrayApp(t, a)
	fake := syncTrayApp(t, a)
	a.updateTray()
	stale := a.trayEntries["com.example.tray"]

	// While the page is being re-rendered for the window the tray keeps
	// the value it already shows instead of dropping the app.
	if _, err := a.pluginManager.TryExecution("com.example.tray"); err != nil {
		t.Fatalf("leasing the runner: %v", err)
	}
	a.updateTray()
	if got := a.trayEntries["com.example.tray"]; got != stale {
		t.Fatalf("busy refresh changed the entry from %+v to %+v", stale, got)
	}
	if fake.title != "42" {
		t.Fatalf("tray title = %q, want the retained value", fake.title)
	}
}

func TestUpdateTrayDropsBrokenApp(t *testing.T) {
	a := newTestApp(t)
	installTrayApp(t, a)
	fake := syncTrayApp(t, a)
	a.updateTray()
	if fake.title != "42" {
		t.Fatalf("tray title = %q, want the app's stat", fake.title)
	}

	// A source that stops answering leaves the tray with no value rather
	// than a stale one.
	removeAllPlugins(t, a)
	a.updateTray()
	if _, ok := a.trayEntries["com.example.tray"]; ok {
		t.Fatal("a removed app stayed in the tray entries")
	}
	if fake.title != "" {
		t.Fatalf("tray title = %q, want empty after the app went away", fake.title)
	}
}

func TestTrayMenuOpensAppPage(t *testing.T) {
	a := newTestApp(t)
	a.openTrayApp("com.example.tray", "overview")
	if a.route.kind != "plugin" || a.route.id != "com.example.tray" || a.route.page != "overview" {
		t.Fatalf("openTrayApp routed to %+v", a.route)
	}
}

func TestTrayFoldsNewlinesBeforeTheNativeTray(t *testing.T) {
	a := newTestApp(t)
	removeAllPlugins(t, a)
	dir := filepath.Join(t.TempDir(), "tray")
	// The page layer accepts these. The tray must not.
	writeAgentPlugin(t, dir, "com.example.tray", `export function page() {
  return { title: 'Overview', sections: [{ stats: [{ label: 'Inde\tx', value: '42\nClick\tAllow\u2028now' }] }] };
}`)
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatalf("installing: %v", err)
	}
	a.refreshPlugins()
	fake := syncTrayApp(t, a)
	a.updateTray()
	for _, s := range fake.painted {
		for _, r := range s {
			if r == '\n' || r == '\t' || r == '\r' || r == '\u2028' || r == '\u2029' {
				t.Fatalf("native tray text contains %q in %q", r, s)
			}
		}
	}
	if fake.title != "42 Click Allow now" {
		t.Fatalf("title = %q", fake.title)
	}
	if fake.tip != "Agent test · Inde x" {
		t.Fatalf("tooltip = %q", fake.tip)
	}
	if fake.menu == nil || len(fake.menu.Items()) == 0 {
		t.Fatal("the tray has no menu")
	}
	item := fake.menu.Items()[0]
	if strings.Contains(item.Label, "\n") || strings.Contains(item.Label, "\t") {
		t.Fatalf("menu label = %q", item.Label)
	}
	// The click target is the manifest id and the declared page, captured
	// when the menu was built, not the stat text or the display name.
	item.Click(item, nil)
	if a.route.kind != "plugin" || a.route.id != "com.example.tray" || a.route.page != "overview" {
		t.Fatalf("menu click opened %+v", a.route)
	}
}

func TestTrayTitleIsShorterThanAPageValue(t *testing.T) {
	a := newTestApp(t)
	removeAllPlugins(t, a)
	dir := filepath.Join(t.TempDir(), "tray")
	value := strings.Repeat("a", 80)
	writeAgentPlugin(t, dir, "com.example.tray", `export function page() {
  return { title: 'Overview', sections: [{ stats: [{ label: 'Index', value: '`+value+`' }] }] };
}`)
	if _, err := a.plugins.Install(dir); err != nil {
		t.Fatalf("installing: %v", err)
	}
	a.refreshPlugins()
	fake := syncTrayApp(t, a)
	a.updateTray()
	if utf8.RuneCountInString(fake.title) != trayLineLimit || fake.title != value[:trayLineLimit] {
		t.Fatalf("title = %q, want %d characters", fake.title, trayLineLimit)
	}
	for _, s := range fake.painted {
		if utf8.RuneCountInString(s) > trayLineLimit {
			t.Fatalf("tray text is %d characters: %q", utf8.RuneCountInString(s), s)
		}
	}
}

func TestStaleTrayCallbackDoesNotPaint(t *testing.T) {
	a := newTestApp(t)
	installTrayApp(t, a)
	fake := syncTrayApp(t, a)
	a.updateTray()
	a.trayEntries["com.example.tray"] = trayEntry{
		id: "com.example.tray", name: "Agent test", page: "overview", label: "Index", value: "kept",
	}
	a.applyTray()
	if fake.title != "kept" {
		t.Fatalf("title = %q", fake.title)
	}
	var queued []func()
	a.goAsync = func(fn func()) { queued = append(queued, fn) }
	a.updateTray()
	if len(queued) != 1 {
		t.Fatalf("queued refreshes = %d", len(queued))
	}
	a.trayGen++
	painted := len(fake.painted)
	queued[0]()
	if fake.title != "kept" || len(fake.painted) != painted {
		t.Fatalf("stale callback painted %q", fake.title)
	}
}

func TestCloseTrayDestroysAndBlocksLatePaint(t *testing.T) {
	a := newTestApp(t)
	installTrayApp(t, a)
	fake := syncTrayApp(t, a)
	a.updateTray()
	var queued []func()
	a.goAsync = func(fn func()) { queued = append(queued, fn) }
	a.updateTray()
	gen := a.trayGen
	a.closeTray()
	if a.tray != nil {
		t.Fatal("closeTray left the handle set")
	}
	if fake.destroyed != 1 {
		t.Fatalf("Destroy calls = %d, want 1", fake.destroyed)
	}
	if a.trayGen == gen {
		t.Fatal("closeTray did not retire in-flight callbacks")
	}
	painted := len(fake.painted)
	for _, fn := range queued {
		fn()
	}
	if len(fake.painted) != painted {
		t.Fatal("a callback painted the tray after it was closed")
	}
}
