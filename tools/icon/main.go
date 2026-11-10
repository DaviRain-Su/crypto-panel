// Command icon draws the app's icon: an ink rounded square with three brass
// bars, the app's mark. It writes the 1024×1024 PNG mygo.json names, and
// the copy the shell embeds for the Dock when the binary runs outside a
// bundle (`go run .`).
//
//	go run ./tools/icon
package main

import (
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/vector"
)

const size = 1024

// targets are the two copies of the art: the bundled app's icon, and the
// PNG internal/app embeds for the Dock in development runs.
var targets = []string{"resources/icon.png", "internal/app/logo.png"}

func main() {
	out := flag.String("out", "", "where to write the PNG instead of both default copies")
	flag.Parse()
	img := drawIcon()
	outs := targets
	if *out != "" {
		outs = []string{*out}
	}
	for _, target := range outs {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			log.Fatal(err)
		}
		f, err := os.Create(target)
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
	}
}

func drawIcon() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	// The rounded square, inset from the canvas the way macOS expects.
	const inset, radius = 64.0, 208.0
	plate := roundedRectMask(inset, inset, size-2*inset, size-2*inset, radius)

	// The plate: a vertical graphite gradient with a sage glow behind the
	// bars.
	plateColor := func(x, y int) color.NRGBA {
		t := float64(y) / size
		base := lerpColor(color.NRGBA{0x20, 0x23, 0x1e, 0xff}, color.NRGBA{0x0c, 0x0e, 0x0b, 0xff}, t)
		dx, dy := float64(x)-size/2, float64(y)-size*0.46
		d := math.Hypot(dx, dy) / (size * 0.46)
		glow := math.Max(0, 1-d)
		glow *= glow
		return blend(base, color.NRGBA{0x9d, 0xb8, 0x96, 0xff}, glow*0.18)
	}
	fillMasked(img, plate, plateColor)

	// A hairline inside the plate, so the edge reads on dark and light docks.
	inner := roundedRectMask(inset+1.5, inset+1.5, size-2*inset-3, size-2*inset-3, radius-1.5)
	edge := roundedRectMask(inset, inset, size-2*inset, size-2*inset, radius)
	subtractMask(edge, inner)
	fillMasked(img, edge, func(x, y int) color.NRGBA {
		return color.NRGBA{0xff, 0xff, 0xff, 0x1f}
	})

	// Three bars, the tallest in the middle: the app's mark.
	bars := []struct {
		h     float64
		shade float64
	}{
		{300, 0.72},
		{456, 1.0},
		{364, 0.86},
	}
	const barW, gap = 96.0, 48.0
	total := float64(len(bars))*barW + float64(len(bars)-1)*gap
	x := (size - total) / 2
	baseline := size*0.5 + 228
	for _, b := range bars {
		top := baseline - b.h
		mask := roundedRectMask(x, top, barW, b.h, barW/2)
		light := color.NRGBA{0xdc, 0xe6, 0xd3, 0xff}
		deep := color.NRGBA{0x6f, 0x8f, 0x6d, 0xff}
		hi, lo := light, deep
		if b.shade < 1 {
			hi = blend(light, deep, 1-b.shade)
			lo = blend(deep, color.NRGBA{0x3f, 0x55, 0x3e, 0xff}, 1-b.shade)
		}
		fillMasked(img, mask, func(px, py int) color.NRGBA {
			t := (float64(py) - top) / b.h
			return lerpColor(hi, lo, t)
		})
		x += barW + gap
	}

	// A thin baseline under the bars.
	line := roundedRectMask(size*0.30, baseline+30, size*0.40, 6, 3)
	fillMasked(img, line, func(x, y int) color.NRGBA {
		return color.NRGBA{0xff, 0xff, 0xff, 0x2e}
	})
	return img
}

// roundedRectMask returns the coverage of a rounded rectangle, one byte per
// pixel.
func roundedRectMask(x, y, w, h, r float64) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, size, size))
	ras := vector.NewRasterizer(size, size)
	ras.MoveTo(float32(x+r), float32(y))
	ras.LineTo(float32(x+w-r), float32(y))
	ras.QuadTo(float32(x+w), float32(y), float32(x+w), float32(y+r))
	ras.LineTo(float32(x+w), float32(y+h-r))
	ras.QuadTo(float32(x+w), float32(y+h), float32(x+w-r), float32(y+h))
	ras.LineTo(float32(x+r), float32(y+h))
	ras.QuadTo(float32(x), float32(y+h), float32(x), float32(y+h-r))
	ras.LineTo(float32(x), float32(y+r))
	ras.QuadTo(float32(x), float32(y), float32(x+r), float32(y))
	ras.ClosePath()
	ras.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return mask
}

// subtractMask leaves a minus b in a.
func subtractMask(a, b *image.Alpha) {
	for i := range a.Pix {
		av := int(a.Pix[i]) - int(b.Pix[i])
		if av < 0 {
			av = 0
		}
		a.Pix[i] = uint8(av)
	}
}

// fillMasked paints colorAt through mask, over what is already in img.
func fillMasked(img *image.RGBA, mask *image.Alpha, colorAt func(x, y int) color.NRGBA) {
	draw.DrawMask(img, img.Bounds(), colorSource{colorAt}, image.Point{}, mask, image.Point{}, draw.Over)
}

// colorSource is an image whose pixels come from a function.
type colorSource struct{ at func(x, y int) color.NRGBA }

func (s colorSource) ColorModel() color.Model { return color.NRGBAModel }
func (s colorSource) Bounds() image.Rectangle { return image.Rect(0, 0, size, size) }
func (s colorSource) At(x, y int) color.Color { return s.at(x, y) }

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	return color.NRGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: uint8(float64(a.A) + (float64(b.A)-float64(a.A))*t),
	}
}

// blend mixes b into a by t, keeping a's alpha.
func blend(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	return color.NRGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: a.A,
	}
}
