package pages

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

func TestXTicksKeepsTheEndsApart(t *testing.T) {
	labels := make([]string, 30)
	for i := range labels {
		labels[i] = fmt.Sprintf("d%02d", i)
	}
	ticks := xTicks(chart.NewBand(labels...))
	if len(ticks) != 3 {
		t.Fatalf("30 points got %d ticks, want first, middle and last", len(ticks))
	}
	if ticks[0].Label != "d00" || ticks[2].Label != "d29" {
		t.Fatalf("ends = %q %q", ticks[0].Label, ticks[2].Label)
	}
	if ticks[1].Label == "d28" {
		t.Fatal("the middle tick sits on the neighbour of the last label")
	}
}

func TestRender(t *testing.T) {
	p, err := Decode([]byte(validPage))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var linked string
	tt := ui.NewTester(func(c *ui.Context) {
		Render(c, p, func(page string) { linked = page })
	}, 380, 620)
	for _, want := range []string{
		"Portfolio", "Base and Ethereum", "Total", "Value", "$12,480.20",
		"Tokens", "ETH", "3.204", "USDC", "1,200.00",
		"Pools", "ETH/USDC", "APR 12.4%",
		"More", "History", "Plain text", "Mon", "Tue",
	} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("History"); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if linked != "history" {
		t.Fatalf("linked = %q", linked)
	}
}

// TestRenderKeepsMarkupAsText is the §13 fixture: a plugin's strings are
// text, never markup, so a page cannot draw its own chrome.
func TestRenderKeepsMarkupAsText(t *testing.T) {
	p := Page{Title: `<b>bold</b>`, Sections: []Section{{Text: `<script>alert(1)</script>`}}}
	tt := ui.NewTester(func(c *ui.Context) { Render(c, p, nil) }, 380, 300)
	for _, want := range []string{"<b>bold</b>", "<script>alert(1)</script>"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestRenderBars(t *testing.T) {
	p := Page{Sections: []Section{{
		Chart: &Chart{Kind: "bar", Points: []Point{
			{Label: "a", Value: 1},
			{Label: "b", Value: 4},
			{Label: "c", Value: 2},
		}},
	}}}
	tt := ui.NewTester(func(c *ui.Context) { Render(c, p, nil) }, 380, 300)
	for _, want := range []string{"a", "b", "c"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}
