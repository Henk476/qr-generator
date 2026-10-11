// Package render turns a QR matrix into a vector scene and exports it to
// image formats. All formats are drawn from the same scene so they look
// identical.
package render

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// Style controls how a matrix is drawn.
type Style struct {
	Foreground color.NRGBA
	Background color.NRGBA // alpha 0 = transparent
	QuietZone  int         // light border width in modules
}

// DefaultStyle is black on white with the standard 4-module quiet zone.
func DefaultStyle() Style {
	return Style{
		Foreground: color.NRGBA{0, 0, 0, 255},
		Background: color.NRGBA{255, 255, 255, 255},
		QuietZone:  4,
	}
}

// Validate checks the style's numeric ranges.
func (s Style) Validate() error {
	if s.QuietZone < 0 || s.QuietZone > 10 {
		return fmt.Errorf("quiet zone must be 0-10 modules, got %d", s.QuietZone)
	}
	if s.Foreground.A == 0 {
		return fmt.Errorf("foreground color must not be transparent")
	}
	return nil
}

// ParseColor parses "#rgb", "#rrggbb", "#rrggbbaa" (the # is optional) or
// "transparent".
func ParseColor(s string) (color.NRGBA, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "transparent" || s == "none" {
		return color.NRGBA{}, nil
	}
	h := strings.TrimPrefix(s, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) == 6 {
		h += "ff"
	}
	if len(h) != 8 {
		return color.NRGBA{}, fmt.Errorf("invalid color %q (use #rrggbb, #rrggbbaa or transparent)", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("invalid color %q (use #rrggbb, #rrggbbaa or transparent)", s)
	}
	return color.NRGBA{uint8(v >> 24), uint8(v >> 16), uint8(v >> 8), uint8(v)}, nil
}

// hexRGB formats the color as #rrggbb, ignoring alpha.
func hexRGB(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
