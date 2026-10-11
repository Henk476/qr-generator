package render

// neighbors says which orthogonal neighbors of a module are dark.
type neighbors struct{ up, right, down, left bool }

// modulePath draws one dark module at (x, y) in the given style. Connected
// styles round only the corners whose two adjacent sides are both exposed,
// so neighboring modules merge into smooth shapes.
func modulePath(shape string, x, y float64, n neighbors) Path {
	exposed := func(a, b bool) bool { return !a && !b }
	tl, tr := exposed(n.up, n.left), exposed(n.up, n.right)
	br, bl := exposed(n.down, n.right), exposed(n.down, n.left)
	r := func(on bool, v float64) float64 {
		if on {
			return v
		}
		return 0
	}
	switch shape {
	case "rounded":
		return RoundRect(x, y, 1, 1, r(tl, .3), r(tr, .3), r(br, .3), r(bl, .3))
	case "extra-rounded":
		return RoundRect(x, y, 1, 1, r(tl, .5), r(tr, .5), r(br, .5), r(bl, .5))
	case "classy":
		return RoundRect(x, y, 1, 1, r(tl, .5), 0, r(br, .5), 0)
	case "classy-rounded":
		return RoundRect(x, y, 1, 1, r(tl, .5), r(tr, .2), r(br, .5), r(bl, .2))
	case "dots":
		return Circle(x+.5, y+.5, .45)
	case "diamond":
		return Polygon(Point{x + .5, y}, Point{x + 1, y + .5}, Point{x + .5, y + 1}, Point{x, y + .5})
	case "vertical":
		return RoundRect(x+.1, y, .8, 1, r(!n.up, .4), r(!n.up, .4), r(!n.down, .4), r(!n.down, .4))
	case "horizontal":
		return RoundRect(x, y+.1, 1, .8, r(!n.left, .4), r(!n.right, .4), r(!n.right, .4), r(!n.left, .4))
	}
	return RectPath(x, y, 1, 1)
}

// eyeOuterPath draws the 7x7 finder ring with its top-left at (x, y).
func eyeOuterPath(shape string, x, y float64) Path {
	switch shape {
	case "rounded":
		return Ring(RoundRectAll(x, y, 7, 7, 2), RoundRectAll(x+1, y+1, 5, 5, 1.2))
	case "circle":
		return Ring(Circle(x+3.5, y+3.5, 3.5), Circle(x+3.5, y+3.5, 2.5))
	case "leaf":
		return Ring(RoundRect(x, y, 7, 7, 3, 0, 3, 0), RoundRect(x+1, y+1, 5, 5, 2, 0, 2, 0))
	case "cut":
		return Ring(chamfer(x, y, 7, 1.8), chamfer(x+1, y+1, 5, 1.2))
	}
	return Ring(RectPath(x, y, 7, 7), RectPath(x+1, y+1, 5, 5))
}

// eyeInnerPath draws the 3x3 finder center with its top-left at (x, y).
func eyeInnerPath(shape string, x, y float64) Path {
	switch shape {
	case "rounded":
		return RoundRectAll(x, y, 3, 3, .8)
	case "circle":
		return Circle(x+1.5, y+1.5, 1.5)
	case "diamond":
		return Polygon(Point{x + 1.5, y - .2}, Point{x + 3.2, y + 1.5}, Point{x + 1.5, y + 3.2}, Point{x - .2, y + 1.5})
	case "leaf":
		return RoundRect(x, y, 3, 3, 1.5, 0, 1.5, 0)
	case "star":
		return Star(x+1.5, y+1.5, 1.9, 1.15, 5)
	}
	return RectPath(x, y, 3, 3)
}

// chamfer is a square with its corners cut at 45 degrees.
func chamfer(x, y, size, c float64) Path {
	return Polygon(
		Point{x + c, y}, Point{x + size - c, y}, Point{x + size, y + c}, Point{x + size, y + size - c},
		Point{x + size - c, y + size}, Point{x + c, y + size}, Point{x, y + size - c}, Point{x, y + c},
	)
}
