package render

import (
	_ "embed"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed fonts/LibreBaskerville-Bold.ttf
var libreBaskervilleBold []byte

var fonts = struct {
	once sync.Once
	byID map[string]*sfnt.Font
}{}

// fontFor returns an embedded font; text never depends on system fonts.
func fontFor(name string) *sfnt.Font {
	fonts.once.Do(func() {
		fonts.byID = map[string]*sfnt.Font{}
		for id, ttf := range map[string][]byte{"sans": gobold.TTF, "serif": libreBaskervilleBold, "mono": gomonobold.TTF} {
			f, err := sfnt.Parse(ttf)
			if err != nil {
				panic("render: embedded font: " + err.Error())
			}
			fonts.byID[id] = f
		}
	})
	if f, ok := fonts.byID[name]; ok {
		return f
	}
	return fonts.byID["sans"]
}

// textUnitsPerEm is the scale glyphs are loaded at before converting to
// scene units; large enough that 26.6 rounding is invisible.
const textUnitsPerEm = 1024

// textOutline returns the outline of s set in fontName at 1 unit per em,
// with the baseline at y = 0 and the text starting at x = 0. It also
// returns the advance width and the font's cap height (both per em).
func textOutline(s, fontName string) (p Path, width, capHeight float64) {
	f := fontFor(fontName)
	var buf sfnt.Buffer
	ppem := fixed.I(textUnitsPerEm)
	toUnits := func(v fixed.Int26_6) float64 { return float64(v) / 64 / textUnitsPerEm }

	if m, err := f.Metrics(&buf, ppem, font.HintingNone); err == nil {
		capHeight = toUnits(m.CapHeight)
	}
	if capHeight <= 0 {
		capHeight = 0.7
	}

	var x float64
	var prev sfnt.GlyphIndex
	for i, r := range s {
		idx, err := f.GlyphIndex(&buf, r)
		if err != nil {
			continue
		}
		if i > 0 {
			if k, err := f.Kern(&buf, prev, idx, ppem, font.HintingNone); err == nil {
				x += toUnits(k)
			}
		}
		segs, err := f.LoadGlyph(&buf, idx, ppem, nil)
		if err == nil {
			pt := func(a fixed.Point26_6) (float64, float64) { return x + toUnits(a.X), toUnits(a.Y) }
			open := false
			for _, sg := range segs {
				switch sg.Op {
				case sfnt.SegmentOpMoveTo:
					if open {
						p.Close()
					}
					p.MoveTo(pt(sg.Args[0]))
					open = true
				case sfnt.SegmentOpLineTo:
					p.LineTo(pt(sg.Args[0]))
				case sfnt.SegmentOpQuadTo:
					cx, cy := pt(sg.Args[0])
					ex, ey := pt(sg.Args[1])
					p.QuadTo(cx, cy, ex, ey)
				case sfnt.SegmentOpCubeTo:
					x1, y1 := pt(sg.Args[0])
					x2, y2 := pt(sg.Args[1])
					ex, ey := pt(sg.Args[2])
					p.CubicTo(x1, y1, x2, y2, ex, ey)
				}
			}
			if open {
				p.Close()
			}
		}
		if adv, err := f.GlyphAdvance(&buf, idx, ppem, font.HintingNone); err == nil {
			x += toUnits(adv)
		}
		prev = idx
	}
	return p, x, capHeight
}

// textInBox lays out s centered in box: cap height fills capFrac of the
// box height, shrunk if needed so the text fits the box width.
func textInBox(s, fontName string, box Rect, capFrac float64) Path {
	if s == "" {
		return Path{}
	}
	outline, width, capH := textOutline(s, fontName)
	scale := box.H * capFrac / capH
	if width*scale > box.W {
		scale = box.W / width
	}
	dx := box.X + (box.W-width*scale)/2
	baseline := box.Y + (box.H+capH*scale)/2
	return outline.Transform(func(q Point) Point { return Point{dx + q.X*scale, baseline + q.Y*scale} })
}
