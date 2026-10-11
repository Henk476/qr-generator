// Command canute-qr generates static QR codes locally.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/logo"
	"github.com/Henk476/qr-generator/internal/render"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `Canute's QR Generator: static QR codes, made locally. No ads, no tracking.

Usage:
  canute-qr serve [flags]      start the web UI (default http://127.0.0.1:8080)
  canute-qr generate [flags]   create a QR code image
  canute-qr types              list content types and their fields
  canute-qr styles             list shapes, frames, fonts and logo icons
  canute-qr version            print the version

Run "canute-qr generate -h" for generate flags.
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "types":
		printTypes(stdout)
		return 0
	case "styles":
		printStyles(stdout)
		return 0
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "canute-qr", version)
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	}
	fmt.Fprintf(stderr, "canute-qr: unknown command %q\n\n%s", args[0], usageText)
	return 2
}

// generalFlags are listed first in "generate -h", before the content fields.
var generalFlags = []string{
	"type", "data", "o", "format", "size", "quality", "page", "print-size",
	"ecc", "qr-version", "quiet-zone", "fg", "bg", "gradient", "fg2", "gradient-angle",
	"shape", "eye-border", "eye-center", "eye-border-color", "eye-center-color",
	"logo", "logo-size", "logo-circle", "logo-keep-modules",
	"frame", "frame-text", "frame-font", "frame-color", "frame-text-color",
	"no-verify", "strict",
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

func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	d := generate.DefaultDesign()
	var (
		typeName  = fs.String("type", "url", `content type (see "canute-qr types")`)
		data      = fs.String("data", "", "value for the type's main field, e.g. the URL, text or phone number")
		out       = fs.String("o", "", `output file, or "-" for stdout (required)`)
		formatStr = fs.String("format", "", "png, jpeg, svg, pdf or eps (default: from the -o extension)")
		size      = fs.Int("size", 1024, fmt.Sprintf("image width in px for png, jpeg and svg (%d-%d)", render.MinSize, render.MaxSize))
		quality   = fs.Int("quality", 90, "JPEG quality (1-100)")
		page      = fs.String("page", "fit", "PDF page: fit, a4 or letter")
		printMM   = fs.Float64("print-size", 50, fmt.Sprintf("printed width in mm for pdf and eps (%d-%d)", render.MinPrintMM, render.MaxPrintMM))
		logoArg   = fs.String("logo", "", `logo: a PNG, JPEG or SVG file, or icon:NAME (see "canute-qr styles")`)
		noVerify  = fs.Bool("no-verify", false, "skip the scannability check")
		strict    = fs.Bool("strict", false, "fail instead of warning when the code may not scan")
	)
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
	fieldFlags := map[string]*string{}
	for name, usage := range fieldUsages() {
		fieldFlags[name] = fs.String(name, "", usage)
	}
	fs.Usage = func() { generateUsage(fs) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "canute-qr: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	fail := func(err error) int {
		fmt.Fprintln(stderr, "canute-qr:", err)
		return 1
	}

	typ, ok := encode.Lookup(*typeName)
	if !ok {
		_, err := encode.Build(*typeName, nil)
		return fail(err)
	}
	values := map[string]string{}
	fs.Visit(func(f *flag.Flag) {
		if p, ok := fieldFlags[f.Name]; ok {
			values[f.Name] = *p
		}
	})
	if *data != "" {
		if typ.Primary == "" {
			return fail(fmt.Errorf("--data is not supported for %s; use the field flags (see \"canute-qr types\")", typ.Name))
		}
		if _, dup := values[typ.Primary]; dup {
			return fail(fmt.Errorf("--data and --%s both set", typ.Primary))
		}
		values[typ.Primary] = *data
	}

	if *out == "" {
		return fail(errors.New("-o is required (a file path, or - for stdout)"))
	}
	var format render.Format
	var err error
	switch {
	case *formatStr != "":
		format, err = render.ParseFormat(*formatStr)
	case *out == "-":
		err = errors.New("--format is required when writing to stdout")
	default:
		format, err = render.FormatFromPath(*out)
	}
	if err != nil {
		return fail(err)
	}

	req := generate.Request{Type: typ.Name, Fields: values, Design: d}
	if *logoArg != "" {
		if strings.HasPrefix(*logoArg, "icon:") {
			req.Design.Logo = *logoArg
		} else {
			b, err := os.ReadFile(*logoArg)
			if err != nil {
				return fail(err)
			}
			img, err := logo.Decode(b)
			if err != nil {
				return fail(fmt.Errorf("--logo: %w", err))
			}
			req.Design.Logo = "asset:file"
			req.Assets = func(string) (image.Image, error) { return img, nil }
		}
	}

	code, err := generate.Prepare(req)
	if err != nil {
		return fail(err)
	}
	opts := render.Options{Format: format, Size: *size, Quality: *quality, Page: *page, PrintMM: *printMM}
	var buf bytes.Buffer
	if err := code.Export(&buf, opts); err != nil {
		return fail(err)
	}

	if !*noVerify {
		checkPx := *size
		if format == render.PDF || format == render.EPS {
			checkPx = render.MaxSize // print output: resolution is not the bottleneck
		}
		report := code.Verify(checkPx)
		for _, w := range report.Warnings {
			fmt.Fprintln(stderr, "warning:", w)
		}
		if *strict && !report.OK() {
			return fail(errors.New("scannability check failed (--strict); nothing written"))
		}
	}

	if *out == "-" {
		_, err = stdout.Write(buf.Bytes())
	} else {
		err = os.WriteFile(*out, buf.Bytes(), 0o644)
	}
	if err != nil {
		return fail(err)
	}
	if *out != "-" {
		fmt.Fprintf(stderr, "wrote %s (%s, QR version %d, ECC %s, %d bytes encoded)\n", *out, format, code.Matrix.Version, code.Matrix.Level, len(code.Payload))
	}
	return 0
}

// fieldUsages merges the fields of all content types into one flag each.
func fieldUsages() map[string]string {
	byField := map[string][]string{}
	for _, t := range encode.Types() {
		for _, f := range t.Fields {
			byField[f.Name] = append(byField[f.Name], t.Name+": "+f.Usage)
		}
	}
	usages := map[string]string{}
	for name, us := range byField {
		usages[name] = strings.Join(us, "; ")
	}
	return usages
}

func generateUsage(fs *flag.FlagSet) {
	w := fs.Output()
	fmt.Fprint(w, "Usage: canute-qr generate --type TYPE [content fields] -o FILE [flags]\n\nExamples:\n")
	fmt.Fprint(w, "  canute-qr generate --data https://example.com -o link.png\n")
	fmt.Fprint(w, "  canute-qr generate --type wifi --ssid Home --password secret -o wifi.svg\n")
	fmt.Fprint(w, "  canute-qr generate --type vcard --first Canute --last Knutsson --mobile +4512345678 -o me.png\n\n")
	fmt.Fprint(w, "Flags:\n")
	for _, name := range generalFlags {
		f := fs.Lookup(name)
		fmt.Fprintf(w, "  --%s\n    \t%s", f.Name, f.Usage)
		if f.DefValue != "" && f.DefValue != "0" && f.DefValue != "false" {
			fmt.Fprintf(w, " (default %s)", f.DefValue)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprint(w, "\nContent fields are flags too, e.g. --ssid, --password. Run \"canute-qr types\" for the full list.\n")
}

func printTypes(w io.Writer) {
	for _, t := range encode.Types() {
		fmt.Fprintf(w, "%s  (%s)\n", t.Name, t.Title)
		fields := append([]encode.Field(nil), t.Fields...)
		sort.SliceStable(fields, func(i, j int) bool { return fields[i].Required && !fields[j].Required })
		for _, f := range fields {
			flagName := "--" + f.Name
			if f.Name == t.Primary {
				flagName += ", --data"
			}
			req := ""
			if f.Required {
				req = " (required)"
			}
			fmt.Fprintf(w, "    %-22s %s%s\n", flagName, f.Usage, req)
		}
		fmt.Fprintln(w)
	}
}

func printStyles(w io.Writer) {
	section := func(title, flagName string, opts []render.Option) {
		fmt.Fprintf(w, "%s (%s)\n", title, flagName)
		for _, o := range opts {
			v := o.Value
			if v == "" {
				v = `""`
			}
			fmt.Fprintf(w, "    %-16s %s\n", v, o.Label)
		}
		fmt.Fprintln(w)
	}
	section("Module shapes", "--shape", render.ModuleShapes)
	section("Finder border shapes", "--eye-border", render.EyeOuterShapes)
	section("Finder center shapes", "--eye-center", render.EyeInnerShapes)
	section("Frames", "--frame", render.Frames)
	section("Frame fonts", "--frame-font", render.Fonts)
	icons := render.Icons()
	for i := range icons {
		icons[i].Value = "icon:" + icons[i].Value
	}
	section("Logo icons", "--logo", icons)
}
