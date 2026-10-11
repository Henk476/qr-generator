package render

import (
	_ "embed"
	"errors"
	"fmt"
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
func textOutline(s string, f *sfnt.Font) (p Path, width, capHeight float64) {
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
func textInBox(s string, f *sfnt.Font, box Rect, capFrac float64) Path {
	if s == "" {
		return Path{}
	}
	outline, width, capH := textOutline(s, f)
	scale := box.H * capFrac / capH
	if width*scale > box.W {
		scale = box.W / width
	}
	dx := box.X + (box.W-width*scale)/2
	baseline := box.Y + (box.H+capH*scale)/2
	return outline.Transform(func(q Point) Point { return Point{dx + q.X*scale, baseline + q.Y*scale} })
}

// Font is a user-supplied TrueType or OpenType font for frame text.
type Font struct {
	sf *sfnt.Font
}

// MaxFontBytes limits uploaded font files.
const MaxFontBytes = 10 << 20

// ParseFont reads a TTF or OTF file. It rejects fonts that cannot draw
// basic Latin text, since frame phrases need it.
func ParseFont(data []byte) (font *Font, err error) {
	if len(data) > MaxFontBytes {
		return nil, fmt.Errorf("font is %d MB; the limit is %d MB", len(data)>>20, MaxFontBytes>>20)
	}
	defer func() {
		if r := recover(); r != nil { // malformed input must not crash the server
			font, err = nil, errors.New("unreadable font file")
		}
	}()
	sf, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("not a TrueType or OpenType font (TTF or OTF; WOFF and font collections are not supported): %w", err)
	}
	var buf sfnt.Buffer
	for _, r := range "SCANme0" {
		if i, err := sf.GlyphIndex(&buf, r); err != nil || i == 0 {
			return nil, fmt.Errorf("font has no glyph for %q; choose a font with Latin letters", r)
		}
	}
	if _, w, _ := textOutline("SCAN", sf); w <= 0 {
		return nil, errors.New("font draws nothing")
	}
	return &Font{sf: sf}, nil
}

// Name is the font's full name, when it has one.
func (f *Font) Name() string {
	var buf sfnt.Buffer
	if n, err := f.sf.Name(&buf, sfnt.NameIDFull); err == nil && n != "" {
		return n
	}
	return "Custom font"
}

// face is the font that draws the frame phrase.
func (f *Frame) face() *sfnt.Font {
	if f.Custom != nil {
		return f.Custom.sf
	}
	return fontFor(f.Font)
}
