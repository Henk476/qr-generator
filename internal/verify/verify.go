// Package verify checks that a rendered code actually scans: it decodes a
// rasterized copy and compares the result with the intended payload, and it
// flags styles that many phone scanners struggle with.
package verify

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"

	"github.com/Henk476/qr-generator/internal/render"
)

// MinContrast is the lowest foreground/background contrast ratio accepted
// without a warning.
const MinContrast = 4.0

// decodePxPerModule is the resolution used for the decode check. It is
// independent of the output size so SVG output is checked the same way.
const decodePxPerModule = 8

// Report is the outcome of a scannability check. Warnings are advisory: the
// caller decides whether to block on them.
type Report struct {
	Decoded  bool    // a QR code was found and decoded
	Match    bool    // the decoded text equals the payload
	Contrast float64 // WCAG contrast ratio between foreground and background
	Warnings []string
}

// OK reports whether the check produced no warnings.
func (r Report) OK() bool { return len(r.Warnings) == 0 }

// Check verifies the scene encodes payload. outputPx is the size the code
// will be exported at, used to warn about modules that are too small.
func Check(payload string, sc *render.Scene, outputPx int) Report {
	var r Report
	warn := func(format string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(format, a...)) }

	text, err := Decode(onWhite(render.Rasterize(sc, int(math.Ceil(sc.Width*decodePxPerModule)))))
	switch {
	case err != nil:
		warn("the code could not be decoded: increase contrast, raise the quiet zone or simplify the content")
	case text != payload:
		r.Decoded = true
		warn("the code decodes to different content than intended")
	default:
		r.Decoded, r.Match = true, true
	}

	// With a gradient, the weakest end decides.
	bg := flatten(sc.CodeBackground, color.NRGBA{255, 255, 255, 255})
	r.Contrast = math.Inf(1)
	inverted := false
	for _, c := range sc.Foreground.Colors() {
		fg := flatten(c, bg)
		r.Contrast = min(r.Contrast, contrast(fg, bg))
		inverted = inverted || luminance(fg) > luminance(bg)
	}
	if r.Contrast < MinContrast {
		warn("low contrast %.1f:1 between foreground and background (aim for at least %.0f:1)", r.Contrast, MinContrast)
	}
	if inverted {
		warn("light-on-dark codes are not read by many scanners: use a darker foreground than background")
	}
	if sc.QuietZone < 2 {
		warn("a quiet zone below 2 modules can stop some scanners finding the code (4 is standard)")
	}
	if px := float64(outputPx) / sc.Width; px < 2 {
		warn("only %.1f px per module at %d px: export larger for reliable scanning", px, outputPx)
	}
	return r
}

// Decode reads a QR code from img.
func Decode(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, map[gozxing.DecodeHintType]any{
		gozxing.DecodeHintType_TRY_HARDER:    true,
		gozxing.DecodeHintType_CHARACTER_SET: "UTF-8",
	})
	if err != nil {
		return "", err
	}
	return res.GetText(), nil
}

// onWhite composites img over white, the way a transparent code looks when
// printed on paper.
func onWhite(img image.Image) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Over)
	return out
}

// flatten composites c over an opaque base.
func flatten(c, base color.NRGBA) color.NRGBA {
	a := float64(c.A) / 255
	mix := func(f, b uint8) uint8 { return uint8(math.Round(float64(f)*a + float64(b)*(1-a))) }
	return color.NRGBA{mix(c.R, base.R), mix(c.G, base.G), mix(c.B, base.B), 255}
}

// luminance is the WCAG 2 relative luminance of an opaque color.
func luminance(c color.NRGBA) float64 {
	ch := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

func contrast(a, b color.NRGBA) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
