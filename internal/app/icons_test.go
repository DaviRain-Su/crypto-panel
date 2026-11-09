package app

import "testing"

func TestGlyphsParsed(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range allGlyphs() {
		if g == nil || g.symbol == "" || g.svg == nil {
			t.Fatal("incomplete glyph")
		}
		if seen[g.symbol] {
			t.Fatalf("duplicate symbol %q", g.symbol)
		}
		seen[g.symbol] = true
		w, h := g.svg.Size()
		if w != 24 || h != 24 {
			t.Fatalf("%s fallback is %gx%g, want 24x24", g.symbol, w, h)
		}
	}
	if len(seen) != 21 {
		t.Fatalf("got %d glyphs", len(seen))
	}
}
