package render

import "math"

// Point is a position in scene units (modules).
type Point struct{ X, Y float64 }

// Op is a path segment operation.
type Op uint8

const (
	MoveTo  Op = iota // Pts[0]
	LineTo            // Pts[0]
	CubicTo           // Pts[0], Pts[1] control points, Pts[2] end
	Close
)

// Seg is one path segment.
type Seg struct {
	Op  Op
	Pts [3]Point
}

// Path is a sequence of subpaths, filled with the nonzero winding rule.
// Shape helpers draw clockwise (on screen, y down); holes are added
// reversed so they cut out under the nonzero rule.
type Path struct {
	Segs []Seg
}

func (p *Path) MoveTo(x, y float64) { p.Segs = append(p.Segs, Seg{Op: MoveTo, Pts: [3]Point{{x, y}}}) }
func (p *Path) LineTo(x, y float64) { p.Segs = append(p.Segs, Seg{Op: LineTo, Pts: [3]Point{{x, y}}}) }
func (p *Path) CubicTo(x1, y1, x2, y2, x, y float64) {
	p.Segs = append(p.Segs, Seg{Op: CubicTo, Pts: [3]Point{{x1, y1}, {x2, y2}, {x, y}}})
}
func (p *Path) Close() { p.Segs = append(p.Segs, Seg{Op: Close}) }

// QuadTo adds a quadratic curve, converted to a cubic.
func (p *Path) QuadTo(cx, cy, x, y float64) {
	c := p.current()
	p.CubicTo(c.X+2.0/3*(cx-c.X), c.Y+2.0/3*(cy-c.Y), x+2.0/3*(cx-x), y+2.0/3*(cy-y), x, y)
}

// Append adds all subpaths of q.
func (p *Path) Append(q Path) { p.Segs = append(p.Segs, q.Segs...) }

// Empty reports whether the path has no segments.
func (p Path) Empty() bool { return len(p.Segs) == 0 }

func (p *Path) current() Point {
	start := Point{}
	var cur Point
	for _, s := range p.Segs {
		switch s.Op {
		case MoveTo:
			start, cur = s.Pts[0], s.Pts[0]
		case LineTo:
			cur = s.Pts[0]
		case CubicTo:
			cur = s.Pts[2]
		case Close:
			cur = start
		}
	}
	return cur
}

// Transform returns a copy with every point mapped through f.
func (p Path) Transform(f func(Point) Point) Path {
	out := Path{Segs: make([]Seg, len(p.Segs))}
	for i, s := range p.Segs {
		out.Segs[i] = s
		n := s.Op.points()
		for j := 0; j < n; j++ {
			out.Segs[i].Pts[j] = f(s.Pts[j])
		}
	}
	return out
}

// Translate returns a copy moved by (dx, dy).
func (p Path) Translate(dx, dy float64) Path {
	return p.Transform(func(q Point) Point { return Point{q.X + dx, q.Y + dy} })
}

// Reverse returns the path with every subpath drawn in the opposite
// direction, which turns a shape into a hole under the nonzero rule.
func (p Path) Reverse() Path {
	var out Path
	var sub []Seg
	flush := func(closed bool) {
		if len(sub) == 0 {
			return
		}
		// Points visited: start, then each segment's end.
		start := sub[0].Pts[0]
		ends := []Point{start}
		for _, s := range sub[1:] {
			if s.Op == CubicTo {
				ends = append(ends, s.Pts[2])
			} else {
				ends = append(ends, s.Pts[0])
			}
		}
		last := ends[len(ends)-1]
		if closed && last != start {
			// The implicit closing line becomes the first explicit line.
			out.MoveTo(start.X, start.Y)
			out.LineTo(last.X, last.Y)
		} else {
			out.MoveTo(last.X, last.Y)
		}
		for i := len(sub) - 1; i >= 1; i-- {
			s, to := sub[i], ends[i-1]
			if s.Op == CubicTo {
				out.CubicTo(s.Pts[1].X, s.Pts[1].Y, s.Pts[0].X, s.Pts[0].Y, to.X, to.Y)
			} else {
				out.LineTo(to.X, to.Y)
			}
		}
		if closed {
			out.Close()
		}
		sub = nil
	}
	for _, s := range p.Segs {
		switch s.Op {
		case MoveTo:
			flush(false)
			sub = []Seg{s}
		case Close:
			flush(true)
		default:
			sub = append(sub, s)
		}
	}
	flush(false)
	return out
}

// Bounds returns the bounding box of the path's points (control points
// included, so it may be slightly larger than the curve).
func (p Path) Bounds() Rect {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, s := range p.Segs {
		n := s.Op.points()
		for j := 0; j < n; j++ {
			q := s.Pts[j]
			minX, minY, maxX, maxY = min(minX, q.X), min(minY, q.Y), max(maxX, q.X), max(maxY, q.Y)
		}
	}
	if minX > maxX {
		return Rect{}
	}
	return Rect{minX, minY, maxX - minX, maxY - minY}
}

// Rect is an axis-aligned rectangle.
type Rect struct{ X, Y, W, H float64 }

// kappa is the cubic control distance for a quarter circle of radius 1.
const kappa = 0.5522847498

// RectPath is a clockwise rectangle.
func RectPath(x, y, w, h float64) Path {
	var p Path
	p.MoveTo(x, y)
	p.LineTo(x+w, y)
	p.LineTo(x+w, y+h)
	p.LineTo(x, y+h)
	p.Close()
	return p
}

// RoundRect is a clockwise rectangle with per-corner radii: top-left,
// top-right, bottom-right, bottom-left. Radii are clamped to half the
// shorter side.
func RoundRect(x, y, w, h, tl, tr, br, bl float64) Path {
	lim := min(w, h) / 2
	tl, tr, br, bl = min(tl, lim), min(tr, lim), min(br, lim), min(bl, lim)
	var p Path
	p.MoveTo(x+tl, y)
	p.LineTo(x+w-tr, y)
	if tr > 0 {
		p.CubicTo(x+w-tr+tr*kappa, y, x+w, y+tr-tr*kappa, x+w, y+tr)
	}
	p.LineTo(x+w, y+h-br)
	if br > 0 {
		p.CubicTo(x+w, y+h-br+br*kappa, x+w-br+br*kappa, y+h, x+w-br, y+h)
	}
	p.LineTo(x+bl, y+h)
	if bl > 0 {
		p.CubicTo(x+bl-bl*kappa, y+h, x, y+h-bl+bl*kappa, x, y+h-bl)
	}
	p.LineTo(x, y+tl)
	if tl > 0 {
		p.CubicTo(x, y+tl-tl*kappa, x+tl-tl*kappa, y, x+tl, y)
	}
	p.Close()
	return p
}

// RoundRectAll is RoundRect with one radius for every corner.
func RoundRectAll(x, y, w, h, r float64) Path { return RoundRect(x, y, w, h, r, r, r, r) }

// Circle is a clockwise circle.
func Circle(cx, cy, r float64) Path { return RoundRectAll(cx-r, cy-r, 2*r, 2*r, r) }

// Polygon is a closed polygon through pts (give them clockwise).
func Polygon(pts ...Point) Path {
	var p Path
	for i, q := range pts {
		if i == 0 {
			p.MoveTo(q.X, q.Y)
		} else {
			p.LineTo(q.X, q.Y)
		}
	}
	p.Close()
	return p
}

// Star is a clockwise star with n points, pointing up.
func Star(cx, cy, outer, inner float64, n int) Path {
	pts := make([]Point, 0, 2*n)
	for i := 0; i < 2*n; i++ {
		r := outer
		if i%2 == 1 {
			r = inner
		}
		a := -math.Pi/2 + float64(i)*math.Pi/float64(n)
		pts = append(pts, Point{cx + r*math.Cos(a), cy + r*math.Sin(a)})
	}
	return Polygon(pts...)
}

// arcTo appends a circular arc around (cx, cy) from angle a0 to a1
// (radians; increasing angle is clockwise on screen). The current point
// must already be at the arc's start.
func (p *Path) arcTo(cx, cy, r, a0, a1 float64) {
	n := int(math.Ceil(math.Abs(a1-a0) / (math.Pi / 2)))
	if n == 0 {
		return
	}
	step := (a1 - a0) / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	for i := 0; i < n; i++ {
		s, e := a0+float64(i)*step, a0+float64(i+1)*step
		x0, y0 := cx+r*math.Cos(s), cy+r*math.Sin(s)
		x1, y1 := cx+r*math.Cos(e), cy+r*math.Sin(e)
		p.CubicTo(x0-k*r*math.Sin(s), y0+k*r*math.Cos(s), x1+k*r*math.Sin(e), y1-k*r*math.Cos(e), x1, y1)
	}
}

// ArcBand is a ring segment between radii inner and outer from angle a0 to
// a1 (radians, a0 < a1), drawn clockwise.
func ArcBand(cx, cy, inner, outer, a0, a1 float64) Path {
	var p Path
	p.MoveTo(cx+outer*math.Cos(a0), cy+outer*math.Sin(a0))
	p.arcTo(cx, cy, outer, a0, a1)
	p.LineTo(cx+inner*math.Cos(a1), cy+inner*math.Sin(a1))
	p.arcTo(cx, cy, inner, a1, a0)
	p.Close()
	return p
}

// Ring is a filled shape with a hole: outer minus inner.
func Ring(outer, inner Path) Path {
	p := Path{Segs: append([]Seg(nil), outer.Segs...)}
	p.Append(inner.Reverse())
	return p
}

// points is how many entries of Seg.Pts the op uses.
func (o Op) points() int {
	switch o {
	case MoveTo, LineTo:
		return 1
	case CubicTo:
		return 3
	}
	return 0
}
