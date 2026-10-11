package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Henk476/qr-generator/internal/batch"
	"github.com/Henk476/qr-generator/internal/render"
)

func runBatch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		csvPath   = fs.String("csv", "", `CSV file with a header row, or "-" for stdin (required)`)
		out       = fs.String("o", "", "output ZIP file (required)")
		typeName  = fs.String("type", "", `content type for rows without a "type" column`)
		mapping   = fs.String("map", "", `rename CSV columns to fields, e.g. "Website=url,Network=ssid"`)
		formatStr = fs.String("format", "png", "image format: png, jpeg, svg, pdf or eps")
	)
	style := addStyleFlags(fs)
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprint(w, `Usage: canute-qr batch --csv FILE -o FILE.zip [flags]

Creates one code per CSV row. Name columns after content fields (url, ssid,
password, phone, ... see "canute-qr types"). Optional columns: "type" (the
content type of the row) and "name" (the image file name). The ZIP also
contains report.csv with the result of every row.

Example CSV:
  type,name,url,ssid,password
  url,Shop,example.com/shop,,
  wifi,Guest Wi-Fi,,Guest,secret

Flags:
`)
		printFlags(fs, []string{"csv", "o", "type", "map", "format"})
		printFlags(fs, styleFlagNames)
	}
	if code := parseFlags(fs, args, stderr); code >= 0 {
		return code
	}
	fail := failer(stderr)
	if *csvPath == "" || *out == "" {
		return fail(errors.New("--csv and -o are required"))
	}
	format, err := render.ParseFormat(*formatStr)
	if err != nil {
		return fail(err)
	}
	rename, err := batch.ParseRename(*mapping)
	if err != nil {
		return fail(err)
	}
	res, err := style.resolve()
	if err != nil {
		return fail(err)
	}

	var in io.Reader = os.Stdin
	if *csvPath != "-" {
		f, err := os.Open(*csvPath)
		if err != nil {
			return fail(err)
		}
		defer f.Close()
		in = f
	}
	rows, err := batch.Parse(in, batch.ParseOptions{DefaultType: *typeName, Rename: rename})
	if err != nil {
		return fail(err)
	}

	var buf bytes.Buffer
	results, err := batch.Generate(&buf, rows, batch.Options{
		Design: res.design, Assets: res.assets, Fonts: res.fonts, Export: style.export(format), Verify: !*style.noVerify,
	})
	if err != nil {
		return fail(err)
	}
	ok, warned, failed := batch.Summary(results)
	for _, r := range results {
		switch {
		case r.Err != nil:
			fmt.Fprintf(stderr, "line %d: error: %v\n", r.Row.Line, r.Err)
		case len(r.Warnings) > 0:
			fmt.Fprintf(stderr, "line %d: warning: %s\n", r.Row.Line, r.Warnings[0])
		}
	}
	if *style.strict && (failed > 0 || warned > 0) {
		return fail(fmt.Errorf("%d rows failed and %d may not scan (--strict); nothing written", failed, warned))
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stderr, "wrote %s: %d codes, %d with warnings, %d failed (see report.csv in the ZIP)\n", *out, ok+warned, warned, failed)
	if failed > 0 {
		return 1
	}
	return 0
}
