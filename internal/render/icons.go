package render

import "math"

// Icons are drawn on a 24x24 grid, clockwise, with holes reversed.
type icon struct {
	name, label string
	draw        func() Path
}

var icons = []icon{
	{"link", "Link", iconLink},
	{"location", "Location", iconLocation},
	{"email", "E-mail", iconEmail},
	{"phone", "Phone", iconPhone},
	{"chat", "Chat (WhatsApp, SMS)", iconChat},
	{"wifi", "Wi-Fi", iconWiFi},
	{"vcard", "Contact card", iconVCard},
	{"calendar", "Calendar", iconCalendar},
	{"payment", "Payment card", iconPayment},
	{"bitcoin", "Bitcoin", iconBitcoin},
	{"scan", "Scan me", iconScan},
	{"menu", "Menu", iconMenu},
	{"food", "Fork and knife", iconFood},
	{"home", "Home", iconHome},
	{"star", "Star", iconStar},
}

// IconNames lists the built-in logo icons.
func IconNames() []string {
	out := make([]string, len(icons))
	for i, ic := range icons {
		out[i] = ic.name
	}
	return out
}

// Icons lists the built-in logo icons with labels.
func Icons() []Option {
	out := make([]Option, len(icons))
	for i, ic := range icons {
		out[i] = Option{ic.name, ic.label}
	}
	return out
}

// iconPath returns the named icon scaled into box.
func iconPath(name string, box Rect) Path {
	for _, ic := range icons {
		if ic.name == name {
			s := min(box.W, box.H) / 24
			ox, oy := box.X+(box.W-24*s)/2, box.Y+(box.H-24*s)/2
			return ic.draw().Transform(func(q Point) Point { return Point{ox + q.X*s, oy + q.Y*s} })
		}
	}
	return Path{}
}

func rotate(p Path, cx, cy, deg float64) Path {
	a := deg * math.Pi / 180
	c, s := math.Cos(a), math.Sin(a)
	return p.Transform(func(q Point) Point {
		dx, dy := q.X-cx, q.Y-cy
		return Point{cx + dx*c - dy*s, cy + dx*s + dy*c}
	})
}

func union(ps ...Path) Path {
	var out Path
	for _, p := range ps {
		out.Append(p)
	}
	return out
}

func iconLink() Path {
	a := Ring(RoundRectAll(0.5, 8, 13, 8, 4), RoundRectAll(2.7, 10.2, 8.6, 3.6, 1.8))
	b := Ring(RoundRectAll(10.5, 8, 13, 8, 4), RoundRectAll(12.7, 10.2, 8.6, 3.6, 1.8))
	return rotate(union(a, b), 12, 12, -45)
}

func iconLocation() Path {
	return union(
		Circle(12, 9, 7),
		Polygon(Point{5.9, 12.4}, Point{18.1, 12.4}, Point{12, 22.5}),
		Circle(12, 9, 3).Reverse(),
	)
}

func iconEmail() Path {
	return union(
		Ring(RoundRectAll(2, 5, 20, 14, 2), RectPath(4, 7, 16, 10)),
		Polygon(Point{4, 7}, Point{12, 12.5}, Point{20, 7}, Point{20, 9.4}, Point{12, 15}, Point{4, 9.4}),
	)
}

func iconPhone() Path {
	return union(
		Ring(RoundRectAll(6, 1.5, 12, 21, 2.5), RoundRectAll(8, 4.5, 8, 13.5, .5)),
		Circle(12, 20, .9).Reverse(),
	)
}

func iconChat() Path {
	return union(
		RoundRectAll(2, 3, 20, 14, 4),
		Polygon(Point{6, 16}, Point{11, 16}, Point{5, 21.5}),
		Circle(7.5, 10, 1.4).Reverse(),
		Circle(12, 10, 1.4).Reverse(),
		Circle(16.5, 10, 1.4).Reverse(),
	)
}

func iconWiFi() Path {
	a0, a1 := -3*math.Pi/4, -math.Pi/4
	return union(
		Circle(12, 18.5, 2),
		ArcBand(12, 18.5, 4.8, 7, a0, a1),
		ArcBand(12, 18.5, 9, 11.2, a0, a1),
		ArcBand(12, 18.5, 13.2, 15.4, a0, a1),
	)
}

func iconVCard() Path {
	return union(
		Ring(RoundRectAll(1, 4, 22, 16, 2), RoundRectAll(3, 6, 18, 12, 1)),
		Circle(8.5, 10, 2.2),
		RoundRect(5, 13.3, 7, 3.4, 2.5, 2.5, 0, 0),
		RectPath(14, 8.8, 5.5, 1.6), RectPath(14, 12, 5.5, 1.6), RectPath(14, 15.2, 3.5, 1.6),
	)
}

func iconCalendar() Path {
	p := union(
		Ring(RoundRectAll(2, 4, 20, 18, 2), RectPath(4, 9, 16, 11)),
		RoundRectAll(6.2, 1.5, 2.4, 5, 1.2), RoundRectAll(15.4, 1.5, 2.4, 5, 1.2),
	)
	for _, y := range []float64{11, 15.5} {
		for _, x := range []float64{6, 10.5, 15} {
			p.Append(RectPath(x, y, 3, 3))
		}
	}
	return p
}

func iconPayment() Path {
	return union(
		RoundRectAll(1, 4.5, 22, 15, 2),
		RectPath(1, 8, 22, 3).Reverse(),
		RoundRectAll(4, 14, 6, 2.5, .5).Reverse(),
	)
}

func iconBitcoin() Path {
	b := textInBox("B", fontFor("sans"), Rect{3, 3.5, 18, 17}, 1)
	bb := b.Bounds()
	x1, x2 := bb.X+bb.W*.3, bb.X+bb.W*.55
	return union(b,
		RectPath(x1, 1, 1.6, bb.Y-1+.5), RectPath(x2, 1, 1.6, bb.Y-1+.5),
		RectPath(x1, bb.Y+bb.H-.5, 1.6, 23-(bb.Y+bb.H)+.5), RectPath(x2, bb.Y+bb.H-.5, 1.6, 23-(bb.Y+bb.H)+.5),
	)
}

func iconScan() Path {
	l := Polygon(Point{2, 2}, Point{9, 2}, Point{9, 4.2}, Point{4.2, 4.2}, Point{4.2, 9}, Point{2, 9})
	mx := func(q Point) Point { return Point{24 - q.X, q.Y} }
	my := func(q Point) Point { return Point{q.X, 24 - q.Y} }
	mxy := func(q Point) Point { return Point{24 - q.X, 24 - q.Y} }
	return union(l, l.Transform(mx).Reverse(), l.Transform(my).Reverse(), l.Transform(mxy), RoundRectAll(5, 10.9, 14, 2.2, 1.1))
}

func iconMenu() Path {
	return union(
		RoundRectAll(3, 5, 18, 2.6, 1.3),
		RoundRectAll(3, 10.7, 18, 2.6, 1.3),
		RoundRectAll(3, 16.4, 18, 2.6, 1.3),
	)
}

func iconFood() Path {
	return union(
		RectPath(4.5, 2, 1.3, 7), RectPath(7.1, 2, 1.3, 7), RectPath(9.7, 2, 1.3, 7),
		RoundRect(4.5, 8, 6.5, 3.2, 0, 0, 2.6, 2.6),
		RoundRectAll(6.95, 10, 1.6, 12, .8),
		RoundRect(14.5, 2, 4.5, 11, 0, 4.5, 0, 0),
		RoundRectAll(15.5, 12, 2.3, 10, 1.1),
	)
}

func iconHome() Path {
	return union(
		Polygon(Point{12, 2}, Point{23, 12}, Point{20, 12}, Point{20, 22}, Point{4, 22}, Point{4, 12}, Point{1, 12}),
		RectPath(10, 15, 4, 7).Reverse(),
	)
}

func iconStar() Path { return Star(12, 12.8, 11, 4.6, 5) }
