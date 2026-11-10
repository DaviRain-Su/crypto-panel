package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

// Render draws page with native UI in the app's theme. onLink is called
// with the page name of a clicked link; nil disables links.
func Render(c *ui.Context, page Page, onLink func(page string)) {
	t := c.Theme()
	ui.Column(c).Gap(12).Children(func() {
		if page.Title != "" || page.Subtitle != "" {
			ui.Column(c).Gap(3).Children(func() {
				if page.Title != "" {
					ui.Text(c, page.Title).FontSize(22).FontWeight(700).LetterSpacing(-0.4)
				}
				if page.Subtitle != "" {
					ui.Text(c, page.Subtitle).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}
		for _, section := range page.Sections {
			renderSection(c, section, onLink)
		}
	})
}

// RenderBody draws page's subtitle and sections, without its title, for a
// shell that already names the page above it.
func RenderBody(c *ui.Context, page Page, onLink func(page string)) {
	t := c.Theme()
	ui.Column(c).Gap(12).Children(func() {
		if page.Subtitle != "" {
			ui.Text(c, page.Subtitle).FontSize(12).TextColor(t.TextMuted)
		}
		for _, section := range page.Sections {
			renderSection(c, section, onLink)
		}
	})
}

func renderSection(c *ui.Context, s Section, onLink func(string)) {
	t := c.Theme()
	ui.Column(c).Gap(10).Children(func() {
		if s.Title != "" {
			ui.Text(c, s.Title).FontSize(15).FontWeight(700).LetterSpacing(-0.2).Margin(8, 0, 0, 0)
		}
		if len(s.Stats) > 0 {
			renderStats(c, s.Stats)
		}
		if s.Table != nil {
			renderTable(c, s.Table)
		}
		if len(s.Cards) > 0 {
			ui.Column(c).Gap(8).Children(func() {
				for _, card := range s.Cards {
					renderCard(c, card)
				}
			})
		}
		if len(s.Links) > 0 && onLink != nil {
			ui.Row(c).Gap(8).Children(func() {
				for _, link := range s.Links {
					page := link.Page
					ui.Button(c, link.Title).OnClick(func() { onLink(page) })
				}
			})
		}
		if s.Text != "" {
			ui.Text(c, s.Text).FontSize(12.5).TextColor(t.Text)
		}
		if s.Divider {
			ui.Divider(c)
		}
		if s.Chart != nil {
			renderChart(c, s.Chart)
		}
		if s.Gauge != nil {
			renderGauge(c, s.Gauge)
		}
	})
}

// statCard is the face of one reading: its label over its value, on the
// theme's surface. The value takes the trend's color, and the shape behind
// the number draws under it when the stat carries a sparkline.
func statCard(c *ui.Context, s Stat) {
	t := c.Theme()
	trend := trendColor(c, s.Trend)
	ui.Column(c).Gap(4).Padding(14).Radius(10).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Text(c, s.Label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).LetterSpacing(0.3)
		ui.Text(c, s.Value).FontSize(24).FontWeight(700).LetterSpacing(-0.4).TextColor(trend)
		if len(s.Sparkline) > 1 {
			drawSparkline(c, s.Sparkline, trend)
		}
	})
}

// trendColor turns a stat's trend into the color its value reads in: up
// into the theme's success, down into its danger, and no trend keeps the
// text color.
func trendColor(c *ui.Context, trend string) ui.Color {
	t := c.Theme()
	switch trend {
	case "up":
		return t.Success
	case "down":
		return t.Danger
	}
	return ui.Color{}
}

// drawSparkline draws the shape behind a number: a hairline through the
// values, oldest first, with the area under it faintly filled. No axes —
// a sparkline is a shape, not a chart.
func drawSparkline(c *ui.Context, values []float64, color ui.Color) {
	t := c.Theme()
	if color == (ui.Color{}) {
		color = t.Accent
	}
	xs, ys := sparkFractions(values)
	ui.Box(c).Height(26).Grow(1).Draw(func(p *ui.Painter, r ui.Rect) {
		line := linePath(r, xs, ys)
		p.FillPath(areaPath(r, xs, ys), color.Alpha(0.14))
		p.StrokePath(line, 1.5, color)
	})
}

// sparkFractions maps a series onto a unit box: x runs left to right in
// the series' order, y runs bottom to top between the series' own low and
// high, so a flat series still draws as a line.
func sparkFractions(values []float64) (xs, ys []float32) {
	if len(values) < 2 {
		return nil, nil
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo = min(lo, v)
		hi = max(hi, v)
	}
	n := float32(len(values) - 1)
	xs = make([]float32, len(values))
	ys = make([]float32, len(values))
	if hi == lo {
		// A flat series has no range to span; its line draws across the
		// middle, where the stroke stays whole.
		for i := range values {
			xs[i] = float32(i) / n
			ys[i] = 0.5
		}
		return xs, ys
	}
	for i, v := range values {
		xs[i] = float32(i) / n
		ys[i] = 1 - float32((v-lo)/(hi-lo))
	}
	return xs, ys
}

// renderGauge draws one reading against its scale: the label and short
// reading above the meter's empty-to-full bar.
func renderGauge(c *ui.Context, g *Gauge) {
	t := c.Theme()
	ui.Column(c).Gap(7).Padding(14).Radius(10).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		if g.Label != "" || g.Text != "" {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				if g.Label != "" {
					ui.Text(c, g.Label).FontSize(11).FontWeight(600).
						TextColor(t.TextMuted).LetterSpacing(0.3).Grow(1)
				}
				if g.Text != "" {
					ui.Text(c, g.Text).FontSize(12.5).FontWeight(700)
				}
			})
		}
		ui.Meter(c, g.Value, 0, 100, nil)
	})
}

func renderStats(c *ui.Context, stats []Stat) {
	if len(stats) <= 3 {
		ui.Row(c).Gap(10).AlignItems(ui.Stretch).Children(func() {
			for _, s := range stats {
				ui.Column(c).Grow(1).Children(func() { statCard(c, s) })
			}
		})
		return
	}
	ui.Grid(c).Columns(2).Gap(10).Children(func() {
		for _, s := range stats {
			statCard(c, s)
		}
	})
}

func renderTable(c *ui.Context, table *Table) {
	t := c.Theme()
	align := func(a string) ui.Align {
		if a == "end" {
			return ui.End
		}
		return ui.Start
	}
	ui.Column(c).Gap(0).Padding(14).Radius(10).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(10).Padding(0, 0, 6, 0).Children(func() {
			for _, col := range table.Columns {
				ui.Text(c, col.Title).FontSize(11).FontWeight(600).TextColor(t.TextMuted).
					LetterSpacing(0.3).Grow(1).TextAlign(align(col.Align))
			}
		})
		for i, row := range table.Rows {
			if i > 0 {
				ui.Divider(c)
			}
			ui.Row(c).Gap(10).Padding(6, 0).Children(func() {
				for j, cell := range row {
					ui.Text(c, cell).FontSize(12.5).Grow(1).TextAlign(align(table.Columns[j].Align))
				}
			})
		}
	})
}

func renderCard(c *ui.Context, card Card) {
	t := c.Theme()
	ui.Column(c).Gap(3).Padding(14).Radius(10).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, card.Title).FontSize(13).FontWeight(600).Grow(1)
			if card.Subtitle != "" {
				ui.Text(c, card.Subtitle).FontSize(11).TextColor(t.TextMuted)
			}
		})
		if card.Body != "" {
			ui.Text(c, card.Body).FontSize(12.5).TextColor(t.TextMuted)
		}
	})
}

func renderChart(c *ui.Context, spec *Chart) {
	t := c.Theme()
	labels := make([]string, len(spec.Points))
	values := make([]float64, len(spec.Points))
	lo, hi := 0.0, 0.0
	for i, p := range spec.Points {
		labels[i] = p.Label
		values[i] = p.Value
		lo = min(lo, p.Value)
		hi = max(hi, p.Value)
	}
	if lo == hi {
		hi = lo + 1
	}
	x := chart.NewBand(labels...).Padding(0.2, 0.1)
	y := chart.NewLinear(lo, hi).Nice(3)
	axes := chart.Axes{Left: y.Ticks(3), Bottom: xTicks(x)}
	ui.Column(c).Padding(14).Radius(10).Background(t.Surface).Border(1, t.Border).Children(func() {
		chart.Plot(c, axes, func(area ui.Element) {
			if spec.Kind == "line" {
				xs, ys := lineFractions(x, y, values)
				area.Draw(func(p *ui.Painter, r ui.Rect) {
					// StrokePath follows every sample. Painter.Line only
					// paints the horizontal or vertical edge of a box, so
					// a diagonal becomes a right angle.
					p.FillPath(areaPath(r, xs, ys), t.Accent.Alpha(0.16))
					p.StrokePath(linePath(r, xs, ys), 2, t.Accent)
				})
				return
			}
			for i, v := range values {
				chart.Rect(c, x.Band(i), y.Span(0, v)).Background(t.Accent).Radius(2)
			}
		}).Height(220).Gap(6)
	})
}

// lineFractions maps each sample onto the plot, in the series' order.
// x grows from the first sample to the last and never steps backward.
func lineFractions(x chart.Band, y chart.Linear, values []float64) (xs, ys []float32) {
	xs = make([]float32, len(values))
	ys = make([]float32, len(values))
	for i, v := range values {
		xs[i] = x.At(i)
		ys[i] = y.At(v)
	}
	return xs, ys
}

// lineVertices is the line through every sample, in order.
func lineVertices(r ui.Rect, xs, ys []float32) [][2]float32 {
	n := min(len(xs), len(ys))
	out := make([][2]float32, n)
	for i := 0; i < n; i++ {
		out[i] = [2]float32{chart.X(r, xs[i]), chart.Y(r, ys[i])}
	}
	return out
}

// areaVertices is one polygon under that line: the samples from first to
// last, then the baseline back from the last sample to the first. No
// sample in between returns to the baseline.
func areaVertices(r ui.Rect, xs, ys []float32) [][2]float32 {
	line := lineVertices(r, xs, ys)
	if len(line) == 0 {
		return nil
	}
	base := chart.Y(r, 0)
	out := make([][2]float32, 0, len(line)+2)
	out = append(out, line...)
	out = append(out, [2]float32{line[len(line)-1][0], base})
	out = append(out, [2]float32{line[0][0], base})
	return out
}

func linePath(r ui.Rect, xs, ys []float32) *ui.Path {
	pts := lineVertices(r, xs, ys)
	path := &ui.Path{}
	if len(pts) == 0 {
		return path
	}
	path.MoveTo(pts[0][0], pts[0][1])
	for _, pt := range pts[1:] {
		path.LineTo(pt[0], pt[1])
	}
	return path
}

func areaPath(r ui.Rect, xs, ys []float32) *ui.Path {
	pts := areaVertices(r, xs, ys)
	path := &ui.Path{}
	if len(pts) == 0 {
		return path
	}
	path.MoveTo(pts[0][0], pts[0][1])
	for _, pt := range pts[1:] {
		path.LineTo(pt[0], pt[1])
	}
	path.Close()
	return path
}

// xTicks labels a chart's horizontal axis. A long series keeps the first,
// middle and last labels so two neighbours cannot collide. A shorter one
// keeps at most five, and replaces the previous tick with the last point
// when they would sit on adjacent samples.
func xTicks(x chart.Band) []chart.Tick {
	n := len(x.Labels)
	if n == 0 {
		return nil
	}
	if n <= 5 {
		return x.Ticks()
	}
	if n > 12 {
		at := []int{0, n / 2, n - 1}
		out := make([]chart.Tick, len(at))
		for i, idx := range at {
			out[i] = chart.Tick{At: x.At(idx), Label: x.Labels[idx]}
		}
		return out
	}
	step := (n - 1) / 4
	if step < 1 {
		step = 1
	}
	out := make([]chart.Tick, 0, 5)
	prev := 0
	for i := 0; i < n; i += step {
		out = append(out, chart.Tick{At: x.At(i), Label: x.Labels[i]})
		prev = i
	}
	last := n - 1
	if prev == last {
		return out
	}
	tick := chart.Tick{At: x.At(last), Label: x.Labels[last]}
	if last-prev < step {
		out[len(out)-1] = tick
		return out
	}
	return append(out, tick)
}
