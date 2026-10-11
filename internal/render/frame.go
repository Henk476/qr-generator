package render

import "image/color"

// frameLayout places the code box inside a frame.
type frameLayout struct {
	W, H         float64 // canvas size
	CodeX, CodeY float64 // top-left of the code box (symbol + quiet zone)
	// Plate means the frame paints behind the code, so the code needs its
	// own background plate.
	Plate bool
	Back  []Item // drawn before the code
	Front []Item // drawn after the code
}

// layoutFrame lays out frame f around a code box of size s. Sizes scale
// with s so frames look the same at every QR version.
func layoutFrame(f *Frame, s float64) frameLayout {
	if f == nil || f.Kind == "" {
		return frameLayout{W: s, H: s}
	}
	t := s * 0.045  // stroke thickness
	th := s * 0.16  // text band height
	r := s * 0.06   // corner radius
	capFrac := 0.55 // text cap height as a fraction of its band
	fill := SolidPaint(f.Color)
	text := func(box Rect, c color.NRGBA) Item {
		return Item{Path: textInBox(f.Text, f.face(), box, capFrac), Paint: SolidPaint(c)}
	}
	ring := func(x, y, w, h, radius float64) Path {
		return Ring(RoundRectAll(x, y, w, h, radius), RoundRectAll(x+t, y+t, w-2*t, h-2*t, max(radius-t, 0)))
	}

	var l frameLayout
	switch f.Kind {
	case "label":
		l = frameLayout{W: s, H: s + th}
		l.Front = []Item{text(Rect{0, s, s, th}, f.Color)}

	case "border":
		l = frameLayout{W: s + 2*t, H: s + 2*t + th, CodeX: t, CodeY: t}
		l.Back = []Item{{Path: ring(0, 0, l.W, l.H, r), Paint: fill}}
		l.Front = []Item{text(Rect{t, t + s, s, th}, f.Color)}

	case "banner-bottom":
		l = frameLayout{W: s + 2*t, H: s + 2*t + th, CodeX: t, CodeY: t, Plate: true}
		l.Back = []Item{{Path: RoundRectAll(0, 0, l.W, l.H, r), Paint: fill}}
		l.Front = []Item{text(Rect{t, t + s, s, th}, f.TextColor)}

	case "banner-top":
		l = frameLayout{W: s + 2*t, H: s + 2*t + th, CodeX: t, CodeY: t + th, Plate: true}
		l.Back = []Item{{Path: RoundRectAll(0, 0, l.W, l.H, r), Paint: fill}}
		l.Front = []Item{text(Rect{t, t, s, th}, f.TextColor)}

	case "badge":
		band := th * 1.25
		l = frameLayout{W: s + 4*t, H: s + 4*t + band, CodeX: 2 * t, CodeY: 2 * t, Plate: true}
		l.Back = []Item{{Path: RoundRectAll(0, 0, l.W, l.H, s*0.14), Paint: fill}}
		l.Front = []Item{text(Rect{2 * t, 2*t + s, s, band}, f.TextColor)}

	case "bubble":
		bh, tail := th*1.3, t*2.5
		l = frameLayout{W: s, H: bh + tail + s, CodeY: bh + tail}
		bubble := RoundRectAll(0, 0, s, bh, bh/2)
		bubble.Append(Polygon(Point{s/2 - 1.6*t, bh - 0.01}, Point{s/2 + 1.6*t, bh - 0.01}, Point{s / 2, bh + tail}))
		l.Back = []Item{{Path: bubble, Paint: fill}}
		l.Front = []Item{text(Rect{bh / 3, 0, s - bh*2/3, bh}, f.TextColor)}

	case "ticket":
		tw, tabH := s*0.62, th*1.1
		body := s + 2*t
		l = frameLayout{W: s + 2*t, H: body + tabH/2, CodeX: t, CodeY: t}
		tab := Rect{(l.W - tw) / 2, body - tabH/2, tw, tabH}
		l.Back = []Item{
			{Path: ring(0, 0, l.W, body, r), Paint: fill},
			{Path: RoundRectAll(tab.X, tab.Y, tab.W, tab.H, tabH/2), Paint: fill},
		}
		l.Front = []Item{text(Rect{tab.X + tabH/3, tab.Y, tab.W - tabH*2/3, tab.H}, f.TextColor)}

	case "corners":
		l = frameLayout{W: s + 2*t, H: s + 2*t + th, CodeX: t, CodeY: t}
		a := s * 0.2
		corner := Polygon(Point{0, 0}, Point{a, 0}, Point{a, t}, Point{t, t}, Point{t, a}, Point{0, a})
		w, h := l.W, s+2*t
		var p Path
		p.Append(corner)
		p.Append(corner.Transform(func(q Point) Point { return Point{w - q.X, q.Y} }).Reverse())
		p.Append(corner.Transform(func(q Point) Point { return Point{q.X, h - q.Y} }).Reverse())
		p.Append(corner.Transform(func(q Point) Point { return Point{w - q.X, h - q.Y} }))
		l.Back = []Item{{Path: p, Paint: fill}}
		l.Front = []Item{text(Rect{t, h, s, th}, f.Color)}

	case "phone":
		side, top, bottom := s*0.06, s*0.17, s*0.15
		l = frameLayout{W: s + 2*side, H: top + s + th + bottom, CodeX: side, CodeY: top}
		body := Ring(RoundRectAll(0, 0, l.W, l.H, s*0.13), RoundRectAll(side, top, s, s+th, s*0.03))
		body.Append(RoundRectAll(l.W/2-s*0.12, top*0.42, s*0.24, s*0.035, s*0.0175).Reverse())
		body.Append(Circle(l.W/2, top+s+th+bottom/2, bottom*0.28).Reverse())
		l.Back = []Item{{Path: body, Paint: fill}}
		l.Front = []Item{text(Rect{side, top + s, s, th}, f.Color)}
	}
	return l
}
