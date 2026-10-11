// Command canute-qr generates static QR codes locally.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/matrix"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/verify"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const usageText = `Canute's QR Generator: static QR codes, made locally. No ads, no tracking.

Usage:
  canute-qr generate [flags]   create a QR code image
  canute-qr types              list content types and their fields
  canute-qr version            print the version

Run "canute-qr generate -h" for generate flags.
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "generate":
		return runGenerate(args[1:], stdout, stderr)
	case "types":
		printTypes(stdout)
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
var generalFlags = []string{"type", "data", "o", "format", "size", "ecc", "qr-version", "quiet-zone", "fg", "bg", "no-verify", "strict"}

func runGenerate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		typeName  = fs.String("type", "url", `content type (see "canute-qr types")`)
		data      = fs.String("data", "", "value for the type's main field, e.g. the URL, text or phone number")
		out       = fs.String("o", "", `output file, or "-" for stdout (required)`)
		formatStr = fs.String("format", "", "png or svg (default: from the -o extension)")
		size      = fs.Int("size", 1024, fmt.Sprintf("image width and height in px (%d-%d)", render.MinSize, render.MaxSize))
		eccStr    = fs.String("ecc", "M", "error correction level: L, M, Q or H")
		qrVersion = fs.Int("qr-version", 0, "force QR version 1-40 (default: smallest that fits)")
		quiet     = fs.Int("quiet-zone", 4, "light border width in modules (0-10)")
		fgStr     = fs.String("fg", "#000000", "foreground color (#rrggbb or #rrggbbaa)")
		bgStr     = fs.String("bg", "#ffffff", `background color (#rrggbb, #rrggbbaa or "transparent")`)
		noVerify  = fs.Bool("no-verify", false, "skip the scannability check")
		strict    = fs.Bool("strict", false, "fail instead of warning when the code may not scan")
	)
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

	level, err := matrix.ParseLevel(*eccStr)
	if err != nil {
		return fail(err)
	}
	style := render.Style{QuietZone: *quiet}
	if style.Foreground, err = render.ParseColor(*fgStr); err != nil {
		return fail(fmt.Errorf("--fg: %w", err))
	}
	if style.Background, err = render.ParseColor(*bgStr); err != nil {
		return fail(fmt.Errorf("--bg: %w", err))
	}

	payload, err := typ.Build(values)
	if err != nil {
		return fail(err)
	}
	m, err := matrix.Build(payload, matrix.Options{Level: level, Version: *qrVersion})
	if err != nil {
		return fail(err)
	}
	scene, err := render.BuildScene(m, style)
	if err != nil {
		return fail(err)
	}
	var buf bytes.Buffer
	if err := render.Export(&buf, scene, format, *size); err != nil {
		return fail(err)
	}

	if !*noVerify {
		report := verify.Check(payload, scene, *size)
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
		fmt.Fprintf(stderr, "wrote %s (%s, QR version %d, ECC %s, %d bytes encoded)\n", *out, format, m.Version, m.Level, len(payload))
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
