package app

import "github.com/egoist/mygo/ui"

// palette holds the app's colors: graphite, paper and sage. The shell is a
// quiet instrument — warm paper in the light, graphite in the dark, one
// sage accent — rather than a neon dashboard.
type palette struct {
	dark        bool
	bg          ui.Color // the content's background
	sidebar     ui.Color // under the sidebar where no material shows
	card        ui.Color // cards and fields on the content
	inset       ui.Color // wells inside a card, such as token chips
	text        ui.Color
	muted       ui.Color
	faint       ui.Color
	hairline    ui.Color
	hover       ui.Color
	press       ui.Color
	accent      ui.Color
	accentSoft  ui.Color
	onAccent    ui.Color
	danger      ui.Color
	dangerSoft  ui.Color
	success     ui.Color
	successSoft ui.Color
	shadow      ui.Color
}

func paletteFor(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			dark:        true,
			bg:          ui.Hex("#141513"),
			sidebar:     ui.Hex("#1a1b18"),
			card:        ui.Hex("#1f211d"),
			inset:       ui.Hex("#191a17"),
			text:        ui.Hex("#eceae3"),
			muted:       ui.RGBA(236, 234, 227, 0.78),
			faint:       ui.RGBA(236, 234, 227, 0.50),
			hairline:    ui.RGBA(255, 255, 255, 0.09),
			hover:       ui.RGBA(255, 255, 255, 0.06),
			press:       ui.RGBA(255, 255, 255, 0.10),
			accent:      ui.Hex("#9db89a"),
			accentSoft:  ui.RGBA(157, 184, 154, 0.14),
			onAccent:    ui.Hex("#14160f"),
			danger:      ui.Hex("#d98a76"),
			dangerSoft:  ui.RGBA(217, 138, 118, 0.12),
			success:     ui.Hex("#8fbc95"),
			successSoft: ui.RGBA(143, 188, 149, 0.14),
			shadow:      ui.RGBA(0, 0, 0, 0.35),
		}
	}
	return palette{
		bg:          ui.Hex("#f6f4ef"),
		sidebar:     ui.Hex("#ece9e1"),
		card:        ui.Hex("#fffefa"),
		inset:       ui.Hex("#f1efe8"),
		text:        ui.Hex("#1b1c18"),
		muted:       ui.RGBA(27, 28, 24, 0.60),
		faint:       ui.RGBA(27, 28, 24, 0.36),
		hairline:    ui.RGBA(27, 28, 24, 0.10),
		hover:       ui.RGBA(27, 28, 24, 0.05),
		press:       ui.RGBA(27, 28, 24, 0.09),
		accent:      ui.Hex("#4e6b52"),
		accentSoft:  ui.RGBA(78, 107, 82, 0.12),
		onAccent:    ui.Hex("#fbfcf8"),
		danger:      ui.Hex("#a34a3a"),
		dangerSoft:  ui.RGBA(163, 74, 58, 0.10),
		success:     ui.Hex("#4f7d5a"),
		successSoft: ui.RGBA(79, 125, 90, 0.12),
		shadow:      ui.RGBA(30, 30, 25, 0.08),
	}
}

// useAccent makes the app's accent the theme's, with its hover and press:
// left alone, those stay MyGo's blue, which primary buttons turn under the
// pointer. They go darker in light mode; in dark mode hover goes lighter.
func useAccent(t *ui.Theme, p palette) {
	// 业务卡片和官方控件使用同一份语义主题，统一输入框、按钮和焦点的颜色。
	t.Background, t.Surface = p.bg, p.inset
	t.SurfaceHover, t.SurfacePressed = p.hover, p.press
	t.Border, t.Text, t.TextMuted = p.hairline, p.text, p.muted
	t.Danger, t.Success, t.Selection = p.danger, p.success, p.accentSoft
	t.Focus = p.accent
	black, white := ui.Hex("#000000"), ui.Hex("#ffffff")
	t.Accent, t.AccentText = p.accent, p.onAccent
	t.AccentHover, t.AccentPressed = p.accent.MixOklab(black, 0.1), p.accent.MixOklab(black, 0.2)
	if p.dark {
		t.AccentHover, t.AccentPressed = p.accent.MixOklab(white, 0.15), p.accent.MixOklab(black, 0.15)
	}
}
