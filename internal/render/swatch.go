package render

import (
	"fmt"
	"strings"
)

// swatchPattern is a small module pattern for previewing module shapes.
var swatchPattern = [5][5]bool{
	{true, true, false, true, true},
	{true, false, false, false, true},
	{false, true, true, true, false},
	{true, true, false, true, false},
	{true, false, true, true, true},
}

// ModuleSwatchSVG is a small inline SVG previewing a module shape. It
// fills with currentColor so CSS can color it.
func ModuleSwatchSVG(shape string) string {
	dark := func(x, y int) bool { return x >= 0 && y >= 0 && x < 5 && y < 5 && swatchPattern[y][x] }
	var p Path
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			if dark(x, y) {
				p.Append(modulePath(shape, float64(x), float64(y),
					neighbors{up: dark(x, y-1), right: dark(x+1, y), down: dark(x, y+1), left: dark(x-1, y)}))
			}
		}
	}
	return swatchSVG(p, 5)
}

// EyeSwatchSVG is a small inline SVG previewing a finder pattern.
func EyeSwatchSVG(outer, inner string) string {
	p := eyeOuterPath(outer, 0, 0)
	p.Append(eyeInnerPath(inner, 2, 2))
	return swatchSVG(p, 7)
}

// IconSwatchSVG is a small inline SVG of a built-in logo icon.
func IconSwatchSVG(name string) string { return swatchSVG(iconPath(name, Rect{0, 0, 24, 24}), 24) }

func swatchSVG(p Path, size float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-0.5 -0.5 %s %s" aria-hidden="true" focusable="false"><path fill="currentColor" d="%s"/></svg>`,
		fmtNum(size+1), fmtNum(size+1), svgPathData(p))
	return b.String()
}
