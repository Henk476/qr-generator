package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/logo"
	"github.com/Henk476/qr-generator/internal/render"
)

// styleFlags are the design, export and check flags shared by generate and
// batch.
type styleFlags struct {
	design   generate.Design
	logo     *string
	size     *int
	quality  *int
	page     *string
	printMM  *float64
	noVerify *bool
	strict   *bool
}

func optionValues(opts []render.Option) string {
	var vs []string
	for _, o := range opts {
		if o.Value != "" {
			vs = append(vs, o.Value)
		}
	}
	return strings.Join(vs, ", ")
}

func addStyleFlags(fs *flag.FlagSet) *styleFlags {
	f := &styleFlags{design: generate.DefaultDesign()}
	d := &f.design
	f.size = fs.Int("size", 1024, fmt.Sprintf("image width in px for png, jpeg and svg (%d-%d)", render.MinSize, render.MaxSize))
	f.quality = fs.Int("quality", 90, "JPEG quality (1-100)")
	f.page = fs.String("page", "fit", "PDF page: fit, a4 or letter")
	f.printMM = fs.Float64("print-size", 50, fmt.Sprintf("printed width in mm for pdf and eps (%d-%d)", render.MinPrintMM, render.MaxPrintMM))
	f.logo = fs.String("logo", "", `logo: a PNG, JPEG or SVG file, or icon:NAME (see "canute-qr styles")`)
	f.noVerify = fs.Bool("no-verify", false, "skip the scannability check")
	f.strict = fs.Bool("strict", false, "fail instead of warning when a code may not scan")

	fs.StringVar(&d.ECC, "ecc", d.ECC, "error correction level: L, M, Q or H (H is used automatically with a logo)")
	fs.IntVar(&d.Version, "qr-version", 0, "force QR version 1-40 (default: smallest that fits)")
	fs.IntVar(&d.QuietZone, "quiet-zone", d.QuietZone, "light border width in modules (0-10)")
	fs.StringVar(&d.Foreground, "fg", d.Foreground, "foreground color (#rrggbb or #rrggbbaa)")
	fs.StringVar(&d.Background, "bg", d.Background, `background color (#rrggbb, #rrggbbaa or "transparent")`)
	fs.StringVar(&d.Gradient, "gradient", "", "foreground gradient: linear or radial (from --fg to --fg2)")
	fs.StringVar(&d.Foreground2, "fg2", "", "gradient end color")
	fs.IntVar(&d.GradientAngle, "gradient-angle", 0, "linear gradient angle in degrees (0 = left to right, 90 = top to bottom)")
	fs.StringVar(&d.Module, "shape", "", "module shape: "+optionValues(render.ModuleShapes))
	fs.StringVar(&d.EyeOuter, "eye-border", "", "finder border shape: "+optionValues(render.EyeOuterShapes))
	fs.StringVar(&d.EyeInner, "eye-center", "", "finder center shape: "+optionValues(render.EyeInnerShapes))
	fs.StringVar(&d.EyeOuterColor, "eye-border-color", "", "finder border color (default: foreground)")
	fs.StringVar(&d.EyeInnerColor, "eye-center-color", "", "finder center color (default: foreground)")
	fs.IntVar(&d.LogoSize, "logo-size", generate.DefaultLogoSize, "logo width as a percentage of the code (10-30)")
	fs.BoolVar(&d.LogoCircle, "logo-circle", false, "clip the logo to a circle")
	fs.BoolVar(&d.LogoKeepModules, "logo-keep-modules", false, "draw modules behind the logo instead of clearing them")
	fs.StringVar(&d.Frame, "frame", "", "frame: "+optionValues(render.Frames))
	fs.StringVar(&d.FrameText, "frame-text", "", fmt.Sprintf("frame phrase, up to %d characters (default %q)", render.MaxFrameText, generate.DefaultFrameText))
	fs.StringVar(&d.FrameFont, "frame-font", "", "frame font: "+optionValues(render.Fonts))
	fs.StringVar(&d.FrameColor, "frame-color", "", "frame color (default: foreground)")
	fs.StringVar(&d.FrameTextColor, "frame-text-color", "", "color of text on a filled frame band (default: white)")
	return f
}

// styleFlagNames lists the shared flags in help order.
var styleFlagNames = []string{
	"size", "quality", "page", "print-size",
	"ecc", "qr-version", "quiet-zone", "fg", "bg", "gradient", "fg2", "gradient-angle",
	"shape", "eye-border", "eye-center", "eye-border-color", "eye-center-color",
	"logo", "logo-size", "logo-circle", "logo-keep-modules",
	"frame", "frame-text", "frame-font", "frame-color", "frame-text-color",
	"no-verify", "strict",
}

// resolve returns the design and, for a logo file, a loader for it.
func (f *styleFlags) resolve() (generate.Design, generate.AssetLoader, error) {
	d := f.design
	if *f.logo == "" {
		return d, nil, nil
	}
	if strings.HasPrefix(*f.logo, "icon:") {
		d.Logo = *f.logo
		return d, nil, nil
	}
	b, err := os.ReadFile(*f.logo)
	if err != nil {
		return d, nil, err
	}
	img, err := logo.Decode(b)
	if err != nil {
		return d, nil, fmt.Errorf("--logo: %w", err)
	}
	d.Logo = "asset:file"
	return d, func(string) (image.Image, error) { return img, nil }, nil
}

// export returns the export options for format.
func (f *styleFlags) export(format render.Format) render.Options {
	return render.Options{Format: format, Size: *f.size, Quality: *f.quality, Page: *f.page, PrintMM: *f.printMM}
}

// checkPx is the output size the scan check should assume.
func (f *styleFlags) checkPx(format render.Format) int {
	if format == render.PDF || format == render.EPS {
		return render.MaxSize // printed: resolution is not the bottleneck
	}
	return *f.size
}

// printFlags prints the named flags with their defaults.
func printFlags(fs *flag.FlagSet, names []string) {
	w := fs.Output()
	for _, name := range names {
		f := fs.Lookup(name)
		fmt.Fprintf(w, "  --%s\n    \t%s", f.Name, f.Usage)
		if f.DefValue != "" && f.DefValue != "0" && f.DefValue != "false" {
			fmt.Fprintf(w, " (default %s)", f.DefValue)
		}
		fmt.Fprintln(w)
	}
}
