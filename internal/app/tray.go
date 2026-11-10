package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/plugin"
)

// The menu bar tray is the glanceable face of the apps: one icon whose text
// is the first stat of the first app's first page, and a menu that lists
// every enabled app's first stat and opens the app the item names. A plugin
// declares nothing new: any app with a page of stats shows up.

// trayHandle is the part of the native tray the shell paints: the text
// beside the icon, the hover text, and the menu. Tests stub it.
type trayHandle interface {
	SetTitle(title string)
	SetToolTip(tip string)
	SetMenu(menu *mygo.Menu)
	Destroy()
}

// trayLineLimit is how much of a stat the menu bar will show. It is shorter
// than the 80-character value and 40-character label a page may carry.
const trayLineLimit = 32

// trayEntry is one app's glanceable value, from the first stat of its first
// page.
type trayEntry struct {
	id    string
	name  string
	page  string
	label string
	value string
}

// trayMenuItem is one row of the tray's menu: the text and the app it opens.
type trayMenuItem struct {
	label string
	id    string
	page  string
}

// trayLayout is everything the tray shows for a set of entries: the text
// beside the icon, the hover text, and the menu rows above the fixed ones.
type trayLayout struct {
	title string
	tip   string
	items []trayMenuItem
}

// trayStat returns the page's first stat, in reading order: the tray shows
// the number the app itself leads with.
func trayStat(p pages.Page) (pages.Stat, bool) {
	for _, sec := range p.Sections {
		if len(sec.Stats) > 0 {
			return sec.Stats[0], true
		}
	}
	return pages.Stat{}, false
}

// trayCandidates lists the apps a tray value can come from: installed,
// on, and with a declared first page.
func trayCandidates(items []plugin.Installed) []plugin.Installed {
	var out []plugin.Installed
	for _, it := range items {
		if it.Err == "" && it.Enabled && len(it.Manifest.Navigation) > 0 {
			out = append(out, it)
		}
	}
	return out
}

// breaksTrayLine reports a character that would put a menu-bar string onto
// a second line. pages.PlainText allows these, so the tray folds them itself.
func breaksTrayLine(r rune) bool {
	switch r {
	case '\n', '\r', '\t', '\v', '\f', '\u0085', '\u2028', '\u2029':
		return true
	}
	return unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// trayLine makes s one short line: line breaks become a space, other
// control characters are dropped, and the result is cut to trayLineLimit.
func trayLine(s string) string {
	s = strings.Join(strings.FieldsFunc(s, breaksTrayLine), " ")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= trayLineLimit {
		return s
	}
	return string([]rune(s)[:trayLineLimit])
}

// trayLayoutOf arranges entries the way the tray shows them: entries sorted
// by name, the first one's value beside the icon, and a menu row per entry.
// Every string is a single short line before it can reach the native tray.
func trayLayoutOf(entries []trayEntry) trayLayout {
	sorted := append([]trayEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	var out trayLayout
	if len(sorted) > 0 {
		out.title = trayLine(sorted[0].value)
		out.tip = trayLine(sorted[0].name + " · " + sorted[0].label)
	}
	for _, e := range sorted {
		out.items = append(out.items, trayMenuItem{
			label: trayLine(fmt.Sprintf("%s · %s", e.name, e.value)),
			id:    e.id,
			page:  e.page,
		})
	}
	return out
}

// trayIconPNG draws the tray's mark once and keeps it: the tray holds the
// same image for its whole life.
func trayIconPNG() []byte {
	trayIconOnce.Do(func() {
		trayIcon = drawTrayIconPNG()
	})
	return trayIcon
}

var (
	trayIconOnce sync.Once
	trayIcon     []byte
)

func drawTrayIconPNG() []byte {
	const s = 36
	const radius = 9
	img := image.NewRGBA(image.Rect(0, 0, s, s))
	black := color.RGBA{A: 0xff}
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			if roundedSquare(x, y, s, radius) {
				img.Set(x, y, black)
			}
		}
	}
	transparent := color.RGBA{}
	for y := 25; y < 29; y++ {
		for x := 9; x < 27; x++ {
			img.Set(x, y, transparent)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// roundedSquare reports whether the pixel sits in an s×s square whose
// corners are rounded to radius.
func roundedSquare(x, y, s, radius int) bool {
	dx := min(x, s-1-x)
	dy := min(y, s-1-y)
	if dx >= radius || dy >= radius {
		return true
	}
	// The pixel is in a corner quarter: it belongs to the square when it
	// lies inside the quarter circle centered at (radius, radius).
	ox, oy := dx-radius, dy-radius
	return ox*ox+oy*oy <= radius*radius
}

// createTray puts the icon in the menu bar. It runs on the main thread once
// the app is ready. A second call leaves the existing tray in place.
func (a *App) createTray() {
	if a.tray != nil {
		return
	}
	if a.newTray == nil {
		a.newTray = func(opts mygo.TrayOptions) (trayHandle, error) { return mygo.NewTray(opts) }
	}
	tray, err := a.newTray(mygo.TrayOptions{
		ID:             "lintel",
		Icon:           trayIconPNG(),
		IconIsTemplate: true,
		ToolTip:        mygo.App.Name(),
	})
	if err != nil {
		log.Printf("lintel: tray: %v", err)
		return
	}
	a.tray = tray
	a.applyTray()
}

// closeTray removes the icon and drops any refresh that has not painted
// yet. A late callback still holds the old generation, and the handle is
// gone, so it cannot SetTitle or SetMenu after the app has closed the tray.
func (a *App) closeTray() {
	a.trayGen++
	tray := a.tray
	a.tray = nil
	a.trayLayoutSig = ""
	if tray != nil {
		tray.Destroy()
	}
}

// updateTray re-reads every candidate app's first page and refreshes the
// tray with each app's first stat. It runs on the main thread and hands the
// page calls their own goroutines; an app that is busy running for the
// window keeps the value it last showed.
func (a *App) updateTray() {
	if a.tray == nil || a.plugins == nil {
		return
	}
	if a.trayEntries == nil {
		a.trayEntries = map[string]trayEntry{}
	}
	a.trayGen++
	gen := a.trayGen
	items, err := a.plugins.List()
	if err != nil {
		return
	}
	// An app that is gone, off, or broken leaves the menu with its next
	// answer; its old value goes now.
	keep := make(map[string]bool, len(items))
	for _, inst := range trayCandidates(items) {
		keep[inst.Manifest.ID] = true
	}
	changed := false
	for id := range a.trayEntries {
		if !keep[id] {
			delete(a.trayEntries, id)
			changed = true
		}
	}
	if changed {
		a.applyTray()
	}
	for _, inst := range trayCandidates(items) {
		inst := inst
		release := func() {}
		if a.pluginManager != nil {
			release, err = a.pluginManager.TryExecution(inst.Manifest.ID)
			if err != nil {
				continue // busy: keep the old value
			}
		}
		hash, err := a.plugins.PackageHash(inst.Manifest.ID)
		if err != nil {
			release()
			a.dropTrayEntry(inst.Manifest.ID)
			continue
		}
		runner, err := a.pluginRunner(inst, hash)
		if err != nil {
			release()
			a.dropTrayEntry(inst.Manifest.ID)
			continue
		}
		page := inst.Manifest.Navigation[0].Page
		id := inst.Manifest.ID
		a.goAsync(func() {
			rendered, perr := runner.Page(page)
			release()
			a.runOnMain(func() {
				if gen != a.trayGen {
					return
				}
				if perr != nil {
					a.dropTrayEntry(id)
					return
				}
				stat, ok := trayStat(rendered)
				if !ok {
					a.dropTrayEntry(id)
					return
				}
				a.trayEntries[id] = trayEntry{
					id:    id,
					name:  inst.Manifest.Name,
					page:  page,
					label: stat.Label,
					value: stat.Value,
				}
				a.applyTray()
			})
		})
	}
}

// dropTrayEntry forgets an app's tray value. It runs on the main thread.
func (a *App) dropTrayEntry(id string) {
	if _, ok := a.trayEntries[id]; ok {
		delete(a.trayEntries, id)
		a.applyTray()
	}
}

// trayLayoutSig identifies the menu a set of entries draws, so an unchanged
// reading does not rebuild the native menu every refresh.
func trayLayoutSig(layout trayLayout) string {
	sig := layout.title + "\n" + layout.tip
	for _, it := range layout.items {
		sig += "\n" + it.label + "|" + it.id + "|" + it.page
	}
	return sig
}

// applyTray draws the layout the current entries make. It runs on the main
// thread.
func (a *App) applyTray() {
	if a.tray == nil {
		return
	}
	entries := make([]trayEntry, 0, len(a.trayEntries))
	for _, e := range a.trayEntries {
		entries = append(entries, e)
	}
	layout := trayLayoutOf(entries)
	a.tray.SetToolTip(layout.tip)
	a.tray.SetTitle(layout.title)
	if sig := trayLayoutSig(layout); sig != a.trayLayoutSig {
		a.trayLayoutSig = sig
		items := make([]*mygo.MenuItem, 0, len(layout.items)+3)
		for _, it := range layout.items {
			it := it
			items = append(items, &mygo.MenuItem{
				Label: it.label,
				Click: func(*mygo.MenuItem, *mygo.Window) { a.openTrayApp(it.id, it.page) },
			})
		}
		if len(layout.items) > 0 {
			items = append(items, mygo.Separator())
		}
		items = append(items,
			&mygo.MenuItem{Label: a.t(i18n.TrayOpenApp), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
			&mygo.MenuItem{Label: a.t(i18n.TrayRefresh), Click: func(*mygo.MenuItem, *mygo.Window) {
				a.requestRefresh()
				a.updateTray()
			}},
		)
		a.tray.SetMenu(mygo.NewMenu(items))
	}
}

// openTrayApp opens the app a tray item names, in the main window.
func (a *App) openTrayApp(id, page string) {
	a.showWindow()
	a.openPluginPage(id, page)
}
