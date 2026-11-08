package app

import "github.com/egoist/mygo/ui"

// palette holds the app's colors, for the light and the dark appearance. The
// theme's own grays are close, but the app wants a violet accent and cards
// that sit on the content rather than controls that sit on a surface.
type palette struct {
	dark       bool
	bg         ui.Color // the content's background
	sidebar    ui.Color // under the sidebar where no material shows
	card       ui.Color // cards and fields on the content
	text       ui.Color
	muted      ui.Color
	faint      ui.Color
	hairline   ui.Color
	hover      ui.Color
	press      ui.Color
	accent     ui.Color
	accentSoft ui.Color
	onAccent   ui.Color
	danger     ui.Color
	success    ui.Color
}

func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			dark:       true,
			bg:         ui.Hex("#141416"),
			sidebar:    ui.Hex("#1c1c1f"),
			card:       ui.Hex("#1f1f23"),
			text:       ui.Hex("#f5f5f7"),
			muted:      ui.RGBA(235, 235, 245, 0.56),
			faint:      ui.RGBA(235, 235, 245, 0.32),
			hairline:   ui.RGBA(255, 255, 255, 0.08),
			hover:      ui.RGBA(255, 255, 255, 0.06),
			press:      ui.RGBA(255, 255, 255, 0.1),
			accent:     ui.Oklch(0.7, 0.17, 285),
			accentSoft: ui.RGBA(139, 122, 255, 0.16),
			onAccent:   ui.Hex("#ffffff"),
			danger:     ui.Oklch(0.68, 0.19, 25),
			success:    ui.Oklch(0.72, 0.16, 155),
		}
	}
	return palette{
		bg:         ui.Hex("#ffffff"),
		sidebar:    ui.Hex("#f4f4f6"),
		card:       ui.Hex("#f7f7f9"),
		text:       ui.Hex("#1d1d1f"),
		muted:      ui.RGBA(60, 60, 67, 0.62),
		faint:      ui.RGBA(60, 60, 67, 0.32),
		hairline:   ui.RGBA(0, 0, 0, 0.07),
		hover:      ui.RGBA(0, 0, 0, 0.04),
		press:      ui.RGBA(0, 0, 0, 0.08),
		accent:     ui.Oklch(0.55, 0.22, 285),
		accentSoft: ui.RGBA(107, 78, 240, 0.1),
		onAccent:   ui.Hex("#ffffff"),
		danger:     ui.Oklch(0.58, 0.2, 25),
		success:    ui.Oklch(0.6, 0.15, 155),
	}
}

// useAccent makes the app's accent the theme's, with its hover and press:
// left alone, those stay MyGo's blue, which primary buttons turn under the
// pointer. They go darker in light mode; in dark mode hover goes lighter.
func useAccent(t *ui.Theme, p palette) {
	black, white := ui.Hex("#000000"), ui.Hex("#ffffff")
	t.Accent, t.AccentText = p.accent, p.onAccent
	t.AccentHover, t.AccentPressed = p.accent.MixOklab(black, 0.1), p.accent.MixOklab(black, 0.2)
	if p.dark {
		t.AccentHover, t.AccentPressed = p.accent.MixOklab(white, 0.15), p.accent.MixOklab(black, 0.15)
	}
}
