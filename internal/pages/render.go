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
	})
}

// statCard is the face of one reading: its label over its value, on the
// theme's surface.
func statCard(c *ui.Context, s Stat) {
	t := c.Theme()
	ui.Column(c).Gap(4).Padding(14).Radius(10).
		Background(t.Surface).Border(1, t.Border).Children(func() {
		ui.Text(c, s.Label).FontSize(11).FontWeight(600).TextColor(t.TextMuted).LetterSpacing(0.3)
		ui.Text(c, s.Value).FontSize(24).FontWeight(700).LetterSpacing(-0.4)
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
				area.Draw(func(p *ui.Painter, r ui.Rect) {
					for i := 1; i < len(values); i++ {
						p.Line(
							chart.X(r, x.At(i-1)), chart.Y(r, y.At(values[i-1])),
							chart.X(r, x.At(i)), chart.Y(r, y.At(values[i])),
							2, t.Accent,
						)
					}
				})
				return
			}
			for i, v := range values {
				chart.Rect(c, x.Band(i), y.Span(0, v)).Background(t.Accent).Radius(2)
			}
		}).Height(150).Gap(6)
	})
}

// xTicks labels at most five points, so a 60-point chart stays readable.
func xTicks(x chart.Band) []chart.Tick {
	n := len(x.Labels)
	if n <= 5 {
		return x.Ticks()
	}
	step := (n - 1) / 4
	out := make([]chart.Tick, 0, 5)
	for i := 0; i < n; i += step {
		out = append(out, chart.Tick{At: x.At(i), Label: x.Labels[i]})
	}
	if last := n - 1; (n-1)%step != 0 {
		out = append(out, chart.Tick{At: x.At(last), Label: x.Labels[last]})
	}
	return out
}
