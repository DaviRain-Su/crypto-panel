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

func TestLineAreaFollowsEveryPoint(t *testing.T) {
	values := []float64{
		29, 31, 28, 34, 38, 36, 41, 45, 43, 49,
		52, 48, 55, 58, 54, 60, 64, 61, 66, 63,
		59, 62, 68, 71, 67, 70, 74, 69, 66, 72,
	}
	labels := make([]string, len(values))
	for i := range labels {
		labels[i] = fmt.Sprintf("d%02d", i)
	}
	x := chart.NewBand(labels...).Padding(0.2, 0.1)
	y := chart.NewLinear(0, 80).Nice(3)
	xs, ys := lineFractions(x, y, values)
	r := ui.Rect{X: 8, Y: 12, W: 640, H: 220}
	line := lineVertices(r, xs, ys)
	area := areaVertices(r, xs, ys)
	if len(line) != len(values) {
		t.Fatalf("line has %d points, want %d", len(line), len(values))
	}
	if len(area) != len(values)+2 {
		t.Fatalf("area has %d points, want the samples plus the two baseline corners", len(area))
	}
	for i := range line {
		if area[i] != line[i] {
			t.Fatalf("area[%d] = %v, line point %v", i, area[i], line[i])
		}
		if ys[i] != y.At(values[i]) {
			t.Fatalf("sample %d is not in series order", i)
		}
		if i > 0 && line[i][0] <= line[i-1][0] {
			t.Fatalf("line x went backwards at %d: %v then %v", i, line[i-1], line[i])
		}
	}
	baseY := chart.Y(r, 0)
	last, first := area[len(values)], area[len(values)+1]
	if last != [2]float32{line[len(line)-1][0], baseY} || first != [2]float32{line[0][0], baseY} {
		t.Fatalf("baseline = %v %v, want the last sample back to the first", last, first)
	}
	for i := 0; i < len(values); i++ {
		if area[i][1] == baseY {
			t.Fatalf("sample %d dropped to the baseline", i)
		}
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
