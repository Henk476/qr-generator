package render

import (
	"cmp"
	"image"
	"image/color"
	"math"

	"github.com/Henk476/qr-generator/internal/matrix"
)

// Scene is a resolution-independent drawing in module units. The origin is
// the top-left corner of the canvas.
type Scene struct {
	Width, Height float64
	Background    color.NRGBA // whole canvas; alpha 0 = transparent
	Items         []Item      // painted in order over the background

	// Facts about the code itself, used by the scannability check.
	Code           Rect        // the code box: symbol plus quiet zone
	QuietZone      float64     // in modules
	Foreground     Paint       // data module paint
	CodeBackground color.NRGBA // what the modules sit on
	// Symbology is "" for QR codes, else a barcode.Symbology.
	Symbology string
}

// Item is a filled path, or an image clipped to a path.
type Item struct {
	Path  Path
	Paint Paint
	// Snap marks paths made only of axis-aligned lines on the module grid;
	// rasterizers snap them to whole pixels to keep edges crisp.
	Snap bool

	Image     image.Image // when set, drawn into ImageRect, clipped by Path if non-empty
	ImageRect Rect
}

var white = color.NRGBA{255, 255, 255, 255}

// BuildScene lays out the code: frame, modules, finder patterns ("eyes")
// and logo.
func BuildScene(m *matrix.Matrix, s Style) (*Scene, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	q := float64(s.QuietZone)
	size := float64(m.Size) + 2*q
	fl := layoutFrame(s.Frame, size)

	sc := &Scene{
		Width: fl.W, Height: fl.H, Background: s.Background,
		Code: Rect{fl.CodeX, fl.CodeY, size, size}, QuietZone: q,
		CodeBackground: s.Background,
	}
	sc.Items = append(sc.Items, fl.Back...)
	if fl.Plate {
		plate := s.Background
		if plate.A == 0 {
			plate = white // a transparent hole in a filled frame would hide the code
		}
		sc.Items = append(sc.Items, Item{Path: RoundRectAll(fl.CodeX, fl.CodeY, size, size, size*0.03), Paint: SolidPaint(plate)})
		sc.CodeBackground = plate
	}

	ox, oy := fl.CodeX+q, fl.CodeY+q // symbol origin
	n := float64(m.Size)
	fg := resolvePaint(s.Foreground, s.Gradient, Rect{ox, oy, n, n})
	sc.Foreground = fg

	// Logo placement and knockout.
	var logoBox Rect
	knocked := func(x, y int) bool { return false }
	if l := s.Logo; l != nil {
		side := l.Size * n
		logoBox = Rect{ox + (n-side)/2, oy + (n-side)/2, side, side}
		if l.Knockout {
			reach := side/2 + 0.5
			knocked = func(x, y int) bool {
				dx, dy := float64(x)+.5-n/2, float64(y)+.5-n/2
				if l.Circle {
					return math.Hypot(dx, dy) <= reach
				}
				return math.Abs(dx) <= reach && math.Abs(dy) <= reach
			}
		}
	}

	// Data modules. Finder patterns are drawn separately as eyes.
	dark := func(x, y int) bool {
		return m.Dark(x, y) && m.RegionAt(x, y) == matrix.Data && !knocked(x, y)
	}
	var mod Path
	square := s.Module == "" || s.Module == "square"
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if !dark(x, y) {
				continue
			}
			if square {
				// Merge horizontal runs to keep vector output small.
				start := x
				for x+1 < m.Size && dark(x+1, y) {
					x++
				}
				mod.Append(RectPath(ox+float64(start), oy+float64(y), float64(x-start+1), 1))
				continue
			}
			nb := neighbors{up: dark(x, y-1), right: dark(x+1, y), down: dark(x, y+1), left: dark(x-1, y)}
			mod.Append(modulePath(s.Module, ox+float64(x), oy+float64(y), nb))
		}
	}
	sc.Items = append(sc.Items, Item{Path: mod, Paint: fg, Snap: square})

	// Eyes.
	outerPaint, innerPaint := fg, fg
	if s.EyeOuterColor != nil {
		outerPaint = SolidPaint(*s.EyeOuterColor)
	}
	if s.EyeInnerColor != nil {
		innerPaint = SolidPaint(*s.EyeInnerColor)
	}
	for i, o := range m.FinderOrigins() {
		x, y := ox+float64(o[0]), oy+float64(o[1])
		e := s.PerEye[i]
		outerShape, innerShape := cmp.Or(e.Outer, s.EyeOuter), cmp.Or(e.Inner, s.EyeInner)
		op, ip := outerPaint, innerPaint
		if e.OuterColor != nil {
			op = SolidPaint(*e.OuterColor)
		}
		if e.InnerColor != nil {
			ip = SolidPaint(*e.InnerColor)
		}
		sc.Items = append(sc.Items,
			Item{Path: eyeOuterPath(outerShape, x, y), Paint: op, Snap: outerShape == "" || outerShape == "square"},
			Item{Path: eyeInnerPath(innerShape, x+2, y+2), Paint: ip, Snap: innerShape == "" || innerShape == "square"},
		)
	}

	if l := s.Logo; l != nil {
		sc.Items = append(sc.Items, logoItems(l, logoBox, fg, sc.CodeBackground)...)
	}
	sc.Items = append(sc.Items, fl.Front...)
	return sc, nil
}

// logoItems draws a logo in box: built-in icons sit on a plate in the
// background color; images are fitted and optionally clipped to a circle.
func logoItems(l *Logo, box Rect, fg Paint, bg color.NRGBA) []Item {
	shape := func(b Rect) Path {
		if l.Circle {
			return Circle(b.X+b.W/2, b.Y+b.H/2, b.W/2)
		}
		return RoundRectAll(b.X, b.Y, b.W, b.H, b.W*0.18)
	}
	if l.Image == nil {
		plate := bg
		if plate.A == 0 {
			plate = white
		}
		inset := box.W * 0.1
		return []Item{
			{Path: shape(box), Paint: SolidPaint(plate)},
			{Path: iconPath(l.Icon, Rect{box.X + inset, box.Y + inset, box.W - 2*inset, box.H - 2*inset}), Paint: fg},
		}
	}
	b := l.Image.Bounds()
	iw, ih := float64(b.Dx()), float64(b.Dy())
	fit := min(box.W/iw, box.H/ih)
	w, h := iw*fit, ih*fit
	r := Rect{box.X + (box.W-w)/2, box.Y + (box.H-h)/2, w, h}
	var clip Path
	if l.Circle {
		clip = shape(box)
	}
	return []Item{{Image: l.Image, ImageRect: r, Path: clip}}
}
