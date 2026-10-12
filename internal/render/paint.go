package render

import (
	"image/color"
	"math"
)

// PaintKind selects how a shape is filled.
type PaintKind uint8

const (
	Solid PaintKind = iota
	Linear
	Radial
)

// Paint is a resolved fill in scene coordinates.
type Paint struct {
	Kind   PaintKind
	Color  color.NRGBA // solid color, or the gradient's start
	Color2 color.NRGBA // gradient end
	// Linear: the gradient runs from (X1, Y1) to (X2, Y2).
	// Radial: centered at (X1, Y1) with radius R.
	X1, Y1, X2, Y2, R float64
}

// SolidPaint is a single-color paint.
func SolidPaint(c color.NRGBA) Paint { return Paint{Kind: Solid, Color: c} }

// Colors returns the colors the paint can produce.
func (p Paint) Colors() []color.NRGBA {
	if p.Kind == Solid {
		return []color.NRGBA{p.Color}
	}
	return []color.NRGBA{p.Color, p.Color2}
}

// T returns the gradient position (0 to 1) at (x, y).
func (p Paint) T(x, y float64) float64 {
	var t float64
	switch p.Kind {
	case Linear:
		dx, dy := p.X2-p.X1, p.Y2-p.Y1
		if l := dx*dx + dy*dy; l > 0 {
			t = ((x-p.X1)*dx + (y-p.Y1)*dy) / l
		}
	case Radial:
		if p.R > 0 {
			t = math.Hypot(x-p.X1, y-p.Y1) / p.R
		}
	}
	return min(max(t, 0), 1)
}

// At returns the color at (x, y).
func (p Paint) At(x, y float64) color.NRGBA {
	if p.Kind == Solid {
		return p.Color
	}
	return lerp(p.Color, p.Color2, p.T(x, y))
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), m(a.A, b.A)}
}

// GradientKind is the user-facing gradient choice.
type GradientKind string

const (
	NoGradient     GradientKind = ""
	LinearGradient GradientKind = "linear"
	RadialGradient GradientKind = "radial"
)

// Gradient describes a two-color gradient relative to a box.
type Gradient struct {
	Kind   GradientKind
	Color2 color.NRGBA
	Angle  float64 // degrees for linear: 0 = left to right, 90 = top to bottom
}

// resolve turns a color plus optional gradient into a paint spanning box.
func resolvePaint(c color.NRGBA, g Gradient, box Rect) Paint {
	cx, cy := box.X+box.W/2, box.Y+box.H/2
	switch g.Kind {
	case LinearGradient:
		a := g.Angle * math.Pi / 180
		dx, dy := math.Cos(a), math.Sin(a)
		half := (box.W*math.Abs(dx) + box.H*math.Abs(dy)) / 2
		return Paint{Kind: Linear, Color: c, Color2: g.Color2,
			X1: cx - dx*half, Y1: cy - dy*half, X2: cx + dx*half, Y2: cy + dy*half}
	case RadialGradient:
		return Paint{Kind: Radial, Color: c, Color2: g.Color2, X1: cx, Y1: cy, R: math.Hypot(box.W, box.H) / 2}
	}
	return SolidPaint(c)
}
