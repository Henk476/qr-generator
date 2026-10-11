package render

import (
	"math"

	"github.com/Henk476/qr-generator/internal/barcode"
)

// minLinearQuiet is the narrowest side quiet zone for linear barcodes;
// scanners need about ten modules of white either side.
const minLinearQuiet = 10

// BuildBarcodeScene lays out a non-QR barcode. Only colors (including a
// gradient) and the quiet zone apply; shapes, eyes, logos and frames are
// QR features. Linear barcodes get bar height and their text underneath.
func BuildBarcodeScene(g *barcode.Grid, text string, s Style) (*Scene, error) {
	plain := Style{Foreground: s.Foreground, Gradient: s.Gradient, Background: s.Background, QuietZone: s.QuietZone}
	if err := plain.Validate(); err != nil {
		return nil, err
	}
	q := float64(s.QuietZone)
	qx, barH, textH := q, float64(g.H), 0.0
	if g.Linear {
		qx = max(q, minLinearQuiet)
		barH = math.Round(max(25, 0.4*float64(g.W)))
		textH = math.Round(barH * 0.22)
	}
	w := float64(g.W) + 2*qx
	h := barH + textH + 2*q
	sc := &Scene{
		Width: w, Height: h, Background: s.Background,
		Code: Rect{0, 0, w, h}, QuietZone: min(qx, q), CodeBackground: s.Background,
		Symbology: string(g.Symbology),
	}
	area := Rect{qx, q, float64(g.W), barH}
	fg := resolvePaint(s.Foreground, s.Gradient, area)
	sc.Foreground = fg

	var p Path
	if g.Linear {
		for x := 0; x < g.W; {
			if !g.Dark(x, 0) {
				x++
				continue
			}
			start := x
			for x < g.W && g.Dark(x, 0) {
				x++
			}
			p.Append(RectPath(qx+float64(start), q, float64(x-start), barH))
		}
	} else {
		for y := 0; y < g.H; y++ {
			for x := 0; x < g.W; {
				if !g.Dark(x, y) {
					x++
					continue
				}
				start := x
				for x < g.W && g.Dark(x, y) {
					x++
				}
				p.Append(RectPath(qx+float64(start), q+float64(y), float64(x-start), 1))
			}
		}
	}
	sc.Items = append(sc.Items, Item{Path: p, Paint: fg, Snap: true})
	if g.Linear && text != "" {
		band := Rect{qx, q + barH + textH*0.15, float64(g.W), textH * 0.85}
		sc.Items = append(sc.Items, Item{Path: textInBox(text, "mono", band, 0.85), Paint: SolidPaint(s.Foreground)})
	}
	return sc, nil
}
