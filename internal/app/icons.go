package app

import (
	"embed"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

// Icons are one set. On macOS, sfsymbols.Symbol draws the system SF Symbol
// at the size and weight asked for. Everywhere else, and on a macOS that
// lacks that symbol, it draws the Lucide SVG embedded beside this file
// (icons/LICENSE). ui.SidebarItem cannot host a Symbol — it only takes an
// SVG, at a fixed 16pt — so the sidebar builds its own rows and calls
// drawNav, which is larger and heavier on the icon rail.

//go:embed icons/*.svg
var iconFiles embed.FS

// glyph is one icon: an SF Symbol name and the Lucide document that stands
// in for it.
type glyph struct {
	symbol string
	svg    *ui.SVG
}

func mustGlyph(symbol, file string) *glyph {
	b, err := iconFiles.ReadFile("icons/" + file)
	if err != nil {
		panic("app: icon " + file + ": " + err.Error())
	}
	return &glyph{symbol: symbol, svg: ui.MustParseSVG(b)}
}

var (
	iconPulse       = mustGlyph("waveform.path.ecg", "activity.svg")
	iconChart       = mustGlyph("chart.line.uptrend.xyaxis", "chart-line.svg")
	iconWallet      = mustGlyph("wallet.bifold", "wallet.svg")
	iconLock        = mustGlyph("lock", "lock.svg")
	iconGrid        = mustGlyph("square.grid.2x2", "layout-grid.svg")
	iconGear        = mustGlyph("gearshape", "cog.svg")
	iconSparkle     = mustGlyph("sparkles", "sparkles.svg")
	iconRefresh     = mustGlyph("arrow.clockwise", "refresh-cw.svg")
	iconTrash       = mustGlyph("trash", "trash.svg")
	iconTerminal    = mustGlyph("terminal", "terminal.svg")
	iconStore       = mustGlyph("storefront", "store.svg")
	iconShield      = mustGlyph("checkmark.shield", "shield-check.svg")
	iconKey         = mustGlyph("key", "key.svg")
	iconCopy        = mustGlyph("doc.on.doc", "copy.svg")
	iconLockOpen    = mustGlyph("lock.open", "lock-open.svg")
	iconChevronLeft = mustGlyph("chevron.left", "chevron-left.svg")
	iconSidebar     = mustGlyph("sidebar.leading", "panel-left.svg")
	iconGlobe       = mustGlyph("globe", "globe.svg")
	iconClose       = mustGlyph("xmark", "x.svg")
	iconSwap        = mustGlyph("arrow.left.arrow.right", "arrow-left-right.svg")
	iconFingerprint = mustGlyph("touchid", "fingerprint-pattern.svg")
)

// allGlyphs is every icon the shell draws, for the test that they parsed.
func allGlyphs() []*glyph {
	return []*glyph{
		iconPulse, iconChart, iconWallet, iconLock, iconGrid, iconGear,
		iconSparkle, iconRefresh, iconTrash, iconTerminal, iconStore,
		iconShield, iconKey, iconCopy, iconLockOpen, iconChevronLeft,
		iconSidebar, iconGlobe, iconClose, iconSwap,
		iconFingerprint,
	}
}

// draw paints the icon size DIPs square in color. Larger sizes take a
// slightly heavier SF Symbol weight; the Lucide fallback keeps its own stroke.
func (g *glyph) draw(c *ui.Context, size float32, color ui.Color) {
	w := sfsymbols.Regular
	switch {
	case size >= 22:
		w = sfsymbols.Semibold
	case size >= 18:
		w = sfsymbols.Medium
	}
	g.paint(c, size, color, w, false)
}

// drawNav paints a sidebar icon. The rail uses a larger, heavier symbol
// that fills its box; the expanded row uses a medium symbol at its natural
// size, so the glyphs share one cap height beside their labels.
func (g *glyph) drawNav(c *ui.Context, size float32, color ui.Color, rail bool) {
	w := sfsymbols.Medium
	if rail {
		w = sfsymbols.Semibold
	}
	g.paint(c, size, color, w, !rail)
}

func (g *glyph) paint(c *ui.Context, size float32, color ui.Color, w sfsymbols.Weight, natural bool) {
	if g == nil {
		return
	}
	el := sfsymbols.Symbol(c, g.symbol).Size(size).Color(color).Weight(w).Fallback(g.svg)
	if natural {
		el.Natural()
	}
}
