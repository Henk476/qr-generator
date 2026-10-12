// Command canute-qr generates static QR codes and barcodes locally.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/render"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func init() {
	// "go install ...@v1.2.3" builds carry the module version instead.
	if info, ok := debug.ReadBuildInfo(); ok && version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `Canute's QR Generator: static QR codes, made locally. No ads, no tracking.

Usage:
  canute-qr serve [flags]      start the web UI (default http://127.0.0.1:8080)
  canute-qr generate [flags]   create one QR code or barcode image
  canute-qr batch [flags]      create many codes from a CSV file into a ZIP
  canute-qr backup -o FILE     back up the library, presets and settings
  canute-qr restore FILE       replace the library, presets and settings from a backup
  canute-qr types              list content types and their fields
  canute-qr styles             list shapes, frames, fonts and logo icons
  canute-qr version            print the version

Run "canute-qr COMMAND -h" for a command's flags.
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
	case "batch":
		return runBatch(args[1:], stdout, stderr)
	case "backup":
		return runBackup(args[1:], stdout, stderr)
	case "restore":
		return runRestore(args[1:], stdout, stderr)
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

// parseFlags parses args, mapping -h to exit code 0 and errors to 2.
// It returns -1 when the command should continue.
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) int {
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
	return -1
}

func failer(stderr io.Writer) func(error) int {
	return func(err error) int {
		fmt.Fprintln(stderr, "canute-qr:", err)
		return 1
	}
}

func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		typeName  = fs.String("type", "url", `content type (see "canute-qr types")`)
		data      = fs.String("data", "", "value for the type's main field, e.g. the URL, text or phone number")
		out       = fs.String("o", "", `output file, or "-" for stdout (required)`)
		formatStr = fs.String("format", "", "png, jpeg, svg, pdf or eps (default: from the -o extension)")
	)
	style := addStyleFlags(fs)
	fieldFlags := map[string]*string{}
	for name, usage := range fieldUsages() {
		fieldFlags[name] = fs.String(name, "", usage)
	}
	fs.Usage = func() { generateUsage(fs) }
	if code := parseFlags(fs, args, stderr); code >= 0 {
		return code
	}
	fail := failer(stderr)

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

	res, err := style.resolve()
	if err != nil {
		return fail(err)
	}
	code, err := generate.Prepare(generate.Request{Type: typ.Name, Fields: values, Design: res.design, Assets: res.assets, Fonts: res.fonts})
	if err != nil {
		return fail(err)
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, style.export(format)); err != nil {
		return fail(err)
	}

	if !*style.noVerify {
		report := code.Verify(style.checkPx(format))
		for _, w := range report.Warnings {
			fmt.Fprintln(stderr, "warning:", w)
		}
		for _, n := range report.Notes {
			fmt.Fprintln(stderr, "note:", n)
		}
		if *style.strict && !report.OK() {
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
		fmt.Fprintf(stderr, "wrote %s (%s, %s, %d bytes encoded)\n", *out, format, code.Summary(), len(code.Payload))
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
	fmt.Fprint(w, "  canute-qr generate --type barcode --symbology ean13 --data 590123412345 -o product.pdf\n\n")
	fmt.Fprint(w, "Flags:\n")
	printFlags(fs, []string{"type", "data", "o", "format"})
	printFlags(fs, styleFlagNames)
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
