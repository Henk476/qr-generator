// Package generate is the content-to-image pipeline shared by the CLI and the
// web UI: build the payload, lay out the matrix, style it, export it.
package generate

import (
	"errors"
	"fmt"
	"image"
	"io"
	"strings"

	"github.com/Henk476/qr-generator/internal/barcode"
	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/matrix"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/verify"
)

// Design is the serializable look of a code. It is what presets store.
// Fields added after M2 are omitempty, and their zero values mean "the
// plain default", so older saved designs keep rendering the same.
type Design struct {
	ECC        string `json:"ecc"`               // L, M, Q or H
	Version    int    `json:"version,omitempty"` // 0 = smallest that fits
	QuietZone  int    `json:"quietZone"`
	Foreground string `json:"fg"` // #rrggbb or #rrggbbaa
	Background string `json:"bg"` // #rrggbb, #rrggbbaa or "transparent"

	Gradient      string `json:"gradient,omitempty"` // "", "linear" or "radial"
	Foreground2   string `json:"fg2,omitempty"`      // gradient end color
	GradientAngle int    `json:"gradientAngle,omitempty"`

	Module        string `json:"module,omitempty"`        // render.ModuleShapes
	EyeOuter      string `json:"eyeOuter,omitempty"`      // render.EyeOuterShapes
	EyeInner      string `json:"eyeInner,omitempty"`      // render.EyeInnerShapes
	EyeOuterColor string `json:"eyeOuterColor,omitempty"` // "" = same as foreground
	EyeInnerColor string `json:"eyeInnerColor,omitempty"`
	// Eyes overrides the top-left, top-right and bottom-left finder patterns.
	Eyes [3]EyeOverride `json:"eyes,omitzero"`

	Logo            string `json:"logo,omitempty"`     // "icon:<name>" or "asset:<id>"
	LogoSize        int    `json:"logoSize,omitempty"` // percent of code width, 10-30 (0 = 22)
	LogoKeepModules bool   `json:"logoKeepModules,omitempty"`
	LogoCircle      bool   `json:"logoCircle,omitempty"`

	Frame          string `json:"frame,omitempty"`          // render.Frames
	FrameText      string `json:"frameText,omitempty"`      // "" = "SCAN ME"
	FrameFont      string `json:"frameFont,omitempty"`      // render.Fonts
	FrameColor     string `json:"frameColor,omitempty"`     // "" = foreground
	FrameTextColor string `json:"frameTextColor,omitempty"` // "" = white
}

// EyeOverride restyles one finder pattern; empty fields inherit the
// design's eye settings.
type EyeOverride struct {
	Outer      string `json:"outer,omitempty"`
	Inner      string `json:"inner,omitempty"`
	OuterColor string `json:"outerColor,omitempty"`
	InnerColor string `json:"innerColor,omitempty"`
}

// Defaults for optional design fields.
const (
	DefaultLogoSize  = 22
	DefaultFrameText = "SCAN ME"
)

// DefaultDesign is black on white, ECC M, standard quiet zone.
func DefaultDesign() Design {
	return Design{ECC: "M", QuietZone: 4, Foreground: "#000000", Background: "#ffffff"}
}

// AssetLoader returns an uploaded logo image by asset ID.
type AssetLoader func(id string) (image.Image, error)

// FontLoader returns an uploaded font by asset ID.
type FontLoader func(id string) (*render.Font, error)

// Validate checks every design value without rendering anything. Uploaded
// logos are only checked for a well-formed reference.
func (d Design) Validate() error {
	_, _, err := d.parse(nil, nil)
	return err
}

// HasLogo reports whether the design places a logo.
func (d Design) HasLogo() bool { return d.Logo != "" }

func (d Design) parse(assets AssetLoader, fonts FontLoader) (matrix.Options, render.Style, error) {
	fail := func(err error) (matrix.Options, render.Style, error) { return matrix.Options{}, render.Style{}, err }
	lvl, err := matrix.ParseLevel(d.ECC)
	if err != nil {
		return fail(err)
	}
	if d.Version < 0 || d.Version > 40 {
		return fail(fmt.Errorf("version must be 0 (auto) or 1-40, got %d", d.Version))
	}
	s := render.Style{
		QuietZone: d.QuietZone,
		Module:    d.Module,
		EyeOuter:  d.EyeOuter,
		EyeInner:  d.EyeInner,
	}
	for i, e := range d.Eyes {
		s.PerEye[i].Outer, s.PerEye[i].Inner = e.Outer, e.Inner
	}
	if s.Foreground, err = render.ParseColor(d.Foreground); err != nil {
		return fail(fmt.Errorf("foreground: %w", err))
	}
	if s.Background, err = render.ParseColor(d.Background); err != nil {
		return fail(fmt.Errorf("background: %w", err))
	}
	if d.Gradient != "" {
		s.Gradient = render.Gradient{Kind: render.GradientKind(d.Gradient), Angle: float64(d.GradientAngle)}
		if s.Gradient.Color2, err = render.ParseColor(d.Foreground2); err != nil {
			return fail(fmt.Errorf("gradient end color: %w", err))
		}
	}
	for _, ec := range []struct {
		what, v string
		dst     **render.Color
	}{{"eye border color", d.EyeOuterColor, &s.EyeOuterColor}, {"eye center color", d.EyeInnerColor, &s.EyeInnerColor},
		{"top-left eye border color", d.Eyes[0].OuterColor, &s.PerEye[0].OuterColor}, {"top-left eye center color", d.Eyes[0].InnerColor, &s.PerEye[0].InnerColor},
		{"top-right eye border color", d.Eyes[1].OuterColor, &s.PerEye[1].OuterColor}, {"top-right eye center color", d.Eyes[1].InnerColor, &s.PerEye[1].InnerColor},
		{"bottom-left eye border color", d.Eyes[2].OuterColor, &s.PerEye[2].OuterColor}, {"bottom-left eye center color", d.Eyes[2].InnerColor, &s.PerEye[2].InnerColor},
	} {
		if ec.v == "" {
			continue
		}
		c, err := render.ParseColor(ec.v)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", ec.what, err))
		}
		*ec.dst = &c
	}

	if d.Logo != "" {
		size := d.LogoSize
		if size == 0 {
			size = DefaultLogoSize
		}
		logo := &render.Logo{Size: float64(size) / 100, Knockout: !d.LogoKeepModules, Circle: d.LogoCircle}
		kind, ref, _ := strings.Cut(d.Logo, ":")
		switch {
		case kind == "icon":
			logo.Icon = ref
		case kind == "asset" && ref != "":
			if assets != nil {
				img, err := assets(ref)
				if err != nil {
					return fail(fmt.Errorf("logo: %w", err))
				}
				logo.Image = img
			} else {
				logo.Icon = render.IconNames()[0] // validation only: the asset is checked when loaded
			}
		default:
			return fail(fmt.Errorf("logo must be icon:<name> or asset:<id>, got %q", d.Logo))
		}
		s.Logo = logo
		lvl = matrix.H // a logo hides modules; the highest error correction compensates
	}

	if d.Frame != "" {
		f := &render.Frame{Kind: d.Frame, Text: d.FrameText, Font: d.FrameFont, Color: s.Foreground,
			TextColor: render.Color{R: 255, G: 255, B: 255, A: 255}}
		if f.Text == "" {
			f.Text = DefaultFrameText
		}
		if ref, ok := strings.CutPrefix(d.FrameFont, "asset:"); ok {
			switch {
			case ref == "":
				return fail(errors.New("frame font must be sans, serif, mono or asset:<id>"))
			case fonts != nil:
				font, err := fonts(ref)
				if err != nil {
					return fail(fmt.Errorf("frame font: %w", err))
				}
				f.Font, f.Custom = "custom", font
			default:
				f.Font = "sans" // validation only: the font is checked when loaded
			}
		}
		if d.FrameColor != "" {
			if f.Color, err = render.ParseColor(d.FrameColor); err != nil {
				return fail(fmt.Errorf("frame color: %w", err))
			}
		}
		if d.FrameTextColor != "" {
			if f.TextColor, err = render.ParseColor(d.FrameTextColor); err != nil {
				return fail(fmt.Errorf("frame text color: %w", err))
			}
		}
		s.Frame = f
	}

	if err := s.Validate(); err != nil {
		return fail(err)
	}
	return matrix.Options{Level: lvl, Version: d.Version}, s, nil
}

// Request is everything needed to produce a code.
type Request struct {
	Type   string
	Fields map[string]string
	Design Design
	Assets AssetLoader // resolves "asset:" logos; may be nil if none are used
	Fonts  FontLoader  // resolves "asset:" frame fonts; may be nil if none are used
}

// Code is a prepared code, ready to export in any format.
type Code struct {
	Payload string
	Matrix  *matrix.Matrix // nil for non-QR barcodes
	Barcode *barcode.Grid  // nil for QR codes
	Scene   *render.Scene
}

// Summary describes the symbol, e.g. "QR version 3, 29x29 modules, ECC M".
func (c *Code) Summary() string {
	if c.Barcode != nil {
		info, _ := barcode.Lookup(c.Barcode.Symbology)
		if c.Barcode.Linear {
			return fmt.Sprintf("%s, %d modules wide", info.Label, c.Barcode.W)
		}
		return fmt.Sprintf("%s, %dx%d modules", info.Label, c.Barcode.W, c.Barcode.H)
	}
	return fmt.Sprintf("QR version %d, %dx%d modules, ECC %s", c.Matrix.Version, c.Matrix.Size, c.Matrix.Size, c.Matrix.Level)
}

// Prepare builds the payload and lays out the code.
func Prepare(req Request) (*Code, error) {
	opts, style, err := req.Design.parse(req.Assets, req.Fonts)
	if err != nil {
		return nil, err
	}
	payload, err := encode.Build(req.Type, req.Fields)
	if err != nil {
		return nil, err
	}
	if req.Type == "barcode" {
		g, err := barcode.Encode(barcode.Symbology(req.Fields["symbology"]), payload)
		if err != nil {
			return nil, err
		}
		sc, err := render.BuildBarcodeScene(g, payload, style)
		if err != nil {
			return nil, err
		}
		return &Code{Payload: payload, Barcode: g, Scene: sc}, nil
	}
	m, err := matrix.Build(payload, opts)
	if err != nil {
		return nil, err
	}
	sc, err := render.BuildScene(m, style)
	if err != nil {
		return nil, err
	}
	return &Code{Payload: payload, Matrix: m, Scene: sc}, nil
}

// Export writes the code with the given export options.
func (c *Code) Export(w io.Writer, o render.Options) error {
	return render.Export(w, c.Scene, o)
}

// Verify runs the scannability check for output at size px.
func (c *Code) Verify(size int) verify.Report {
	return verify.Check(c.Payload, c.Scene, size)
}

// Normalize clears fields that have no effect or hold the default, so saved
// designs stay minimal and equal designs compare equal.
func (d Design) Normalize() Design {
	if d.Gradient == "" {
		d.Foreground2, d.GradientAngle = "", 0
	}
	if d.Module == "square" {
		d.Module = ""
	}
	if d.EyeOuter == "square" {
		d.EyeOuter = ""
	}
	if d.EyeInner == "square" {
		d.EyeInner = ""
	}
	if d.Logo == "" {
		d.LogoSize, d.LogoKeepModules, d.LogoCircle = 0, false, false
	} else if d.LogoSize == DefaultLogoSize {
		d.LogoSize = 0
	}
	if d.Frame == "" {
		d.FrameText, d.FrameFont, d.FrameColor, d.FrameTextColor = "", "", "", ""
	} else {
		if d.FrameText == DefaultFrameText {
			d.FrameText = ""
		}
		if d.FrameFont == "sans" {
			d.FrameFont = ""
		}
		if strings.EqualFold(d.FrameColor, d.Foreground) {
			d.FrameColor = ""
		}
		if strings.EqualFold(d.FrameTextColor, "#ffffff") {
			d.FrameTextColor = ""
		}
	}
	return d
}
