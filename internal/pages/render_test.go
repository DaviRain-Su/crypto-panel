package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

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
