// Package render turns a QR matrix into a vector scene and exports it to
// image formats. All formats are drawn from the same scene so they look
// identical.
package render

import (
	"fmt"
	"image"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Color is a non-premultiplied RGBA color.
type Color = color.NRGBA

// Option is a selectable style value with a human label.
type Option struct{ Value, Label string }

func values(opts []Option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Value
	}
	return out
}

// ModuleShapes lists the data module styles.
var ModuleShapes = []Option{
	{"square", "Square"},
	{"rounded", "Rounded"},
	{"extra-rounded", "Extra rounded"},
	{"dots", "Dots"},
	{"classy", "Classy"},
	{"classy-rounded", "Classy rounded"},
	{"diamond", "Diamond"},
	{"vertical", "Vertical bars"},
	{"horizontal", "Horizontal bars"},
}

// EyeOuterShapes lists the finder pattern border styles.
var EyeOuterShapes = []Option{
	{"square", "Square"},
	{"rounded", "Rounded"},
	{"circle", "Circle"},
	{"leaf", "Leaf"},
	{"cut", "Cut corners"},
}

// EyeInnerShapes lists the finder pattern center styles.
var EyeInnerShapes = []Option{
	{"square", "Square"},
	{"rounded", "Rounded"},
	{"circle", "Circle"},
	{"diamond", "Diamond"},
	{"leaf", "Leaf"},
	{"star", "Star"},
}

// Frames lists the frame templates.
var Frames = []Option{
	{"", "No frame"},
	{"label", "Text only"},
	{"border", "Border"},
	{"banner-bottom", "Banner below"},
	{"banner-top", "Banner above"},
	{"bubble", "Speech bubble"},
	{"badge", "Badge"},
	{"ticket", "Ticket"},
	{"corners", "Corner brackets"},
	{"phone", "Phone"},
}

// Fonts lists the frame fonts.
var Fonts = []Option{
	{"sans", "Sans-serif"},
	{"serif", "Serif"},
	{"mono", "Monospace"},
}

// MaxFrameText is the longest frame phrase, in characters.
const MaxFrameText = 30

// Logo is an image or built-in icon in the middle of the code.
type Logo struct {
	Image image.Image // an uploaded image, or nil
	Icon  string      // a built-in icon name, used when Image is nil
	// Size is the logo width as a fraction of the symbol width (0.1-0.3).
	Size float64
	// Knockout clears the modules behind the logo.
	Knockout bool
	Circle   bool // clip to a circle instead of a square
}

// Frame is a decorative frame with a call-to-action phrase.
type Frame struct {
	Kind      string
	Text      string
	Font      string
	Color     color.NRGBA
	TextColor color.NRGBA
}

// Style controls how a matrix is drawn.
type Style struct {
	Foreground color.NRGBA
	Gradient   Gradient    // optional foreground gradient
	Background color.NRGBA // alpha 0 = transparent
	QuietZone  int         // light border width in modules

	Module   string // one of ModuleShapes; "" = square
	EyeOuter string // one of EyeOuterShapes; "" = square
	EyeInner string // one of EyeInnerShapes; "" = square
	// Eye colors; nil means "same as the foreground" (gradient included).
	EyeOuterColor *color.NRGBA
	EyeInnerColor *color.NRGBA

	Logo  *Logo
	Frame *Frame
}

// DefaultStyle is black on white with the standard 4-module quiet zone.
func DefaultStyle() Style {
	return Style{
		Foreground: color.NRGBA{0, 0, 0, 255},
		Background: color.NRGBA{255, 255, 255, 255},
		QuietZone:  4,
	}
}

func checkOption(what, v string, opts []Option) error {
	if v != "" && !slices.Contains(values(opts), v) {
		return fmt.Errorf("unknown %s %q (valid: %s)", what, v, strings.Join(values(opts), ", "))
	}
	return nil
}

// Validate checks the style's values and ranges.
func (s Style) Validate() error {
	if s.QuietZone < 0 || s.QuietZone > 10 {
		return fmt.Errorf("quiet zone must be 0-10 modules, got %d", s.QuietZone)
	}
	if s.Foreground.A == 0 {
		return fmt.Errorf("foreground color must not be transparent")
	}
	switch s.Gradient.Kind {
	case NoGradient, LinearGradient, RadialGradient:
	default:
		return fmt.Errorf("unknown gradient %q (valid: linear, radial)", s.Gradient.Kind)
	}
	for _, c := range []struct {
		what, v string
		opts    []Option
	}{
		{"module shape", s.Module, ModuleShapes},
		{"eye border shape", s.EyeOuter, EyeOuterShapes},
		{"eye center shape", s.EyeInner, EyeInnerShapes},
	} {
		if err := checkOption(c.what, c.v, c.opts); err != nil {
			return err
		}
	}
	if l := s.Logo; l != nil {
		if l.Image == nil && !slices.Contains(IconNames(), l.Icon) {
			return fmt.Errorf("unknown icon %q (valid: %s)", l.Icon, strings.Join(IconNames(), ", "))
		}
		if l.Size < 0.1 || l.Size > 0.3 {
			return fmt.Errorf("logo size must be 10-30%% of the code width, got %.0f%%", l.Size*100)
		}
	}
	if f := s.Frame; f != nil {
		if err := checkOption("frame", f.Kind, Frames); err != nil {
			return err
		}
		if err := checkOption("font", f.Font, Fonts); err != nil {
			return err
		}
		if n := utf8.RuneCountInString(f.Text); n > MaxFrameText {
			return fmt.Errorf("frame text must be at most %d characters, got %d", MaxFrameText, n)
		}
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
