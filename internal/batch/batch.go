// Package batch generates many codes from a CSV file into a ZIP archive.
//
// The CSV has a header row. Columns are named after content fields (url,
// ssid, password, ...), plus two optional special columns: "type" (the
// content type for that row) and "name" (the file name and library name).
// A rename map lets other column headers stand for field names.
package batch

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/render"
)

// MaxRows limits one batch.
const MaxRows = 5000

// Row is one code to generate.
type Row struct {
	Line   int // CSV line number, for messages
	Name   string
	Type   string
	Fields map[string]string
}

// ParseOptions controls CSV parsing.
type ParseOptions struct {
	DefaultType string            // used when there is no "type" column or the cell is empty
	Rename      map[string]string // lower-case CSV header -> field name
}

// ParseRename parses "header=field,header2=field2".
func ParseRename(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		from, to, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
			return nil, fmt.Errorf("column mapping %q must look like header=field", pair)
		}
		out[strings.ToLower(strings.TrimSpace(from))] = strings.TrimSpace(to)
	}
	return out, nil
}

// knownFields is every field name of any content type.
func knownFields() map[string]bool {
	known := map[string]bool{}
	for _, t := range encode.Types() {
		for _, f := range t.Fields {
			known[f.Name] = true
		}
	}
	return known
}

// Parse reads rows from CSV. Header problems (unknown columns, no rows)
// are errors; problems in individual rows are reported by Generate.
func Parse(r io.Reader, opts ParseOptions) ([]Row, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // tolerate ragged rows
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("the CSV file is empty")
	} else if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}
	known := knownFields()
	cols := make([]string, len(header))
	var unknown []string
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))) // strip a UTF-8 BOM
		if to, ok := opts.Rename[h]; ok {
			h = to
		}
		if h != "type" && h != "name" && h != "" && !known[h] {
			unknown = append(unknown, header[i])
		}
		cols[i] = h
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown CSV columns: %s (use field names such as url, ssid or phone, or map them)", strings.Join(unknown, ", "))
	}

	var rows []Row
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading CSV: %w", err) // csv.ParseError includes the line
		}
		line, _ := cr.FieldPos(0)
		row := Row{Line: line, Type: opts.DefaultType, Fields: map[string]string{}}
		empty := true
		for i, v := range rec {
			v = strings.TrimSpace(v)
			if i >= len(cols) || v == "" || cols[i] == "" {
				continue
			}
			empty = false
			switch cols[i] {
			case "type":
				row.Type = strings.ToLower(v)
			case "name":
				row.Name = v
			default:
				row.Fields[cols[i]] = v
			}
		}
		if empty {
			continue
		}
		if len(rows) == MaxRows {
			return nil, fmt.Errorf("the CSV has more than %d rows; split it into smaller files", MaxRows)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, errors.New("the CSV has a header but no rows")
	}
	return rows, nil
}

// Result is the outcome for one row.
type Result struct {
	Row      Row
	File     string // path inside the archive; empty on error
	Err      error
	Warnings []string
	Type     encode.Type
	Fields   map[string]string // the fields used, for saving to the library
}

// Options controls generation.
type Options struct {
	Design generate.Design
	Assets generate.AssetLoader
	Fonts  generate.FontLoader
	Export render.Options
	Verify bool // run the scannability check on each code
}

// Generate renders every row and writes the images plus report.csv to w.
// Rows render in parallel but are written in CSV order. A failing row is
// reported, not fatal.
func Generate(w io.Writer, rows []Row, opts Options) ([]Result, error) {
	if err := opts.Export.Validate(); err != nil {
		return nil, err
	}
	data := make([][]byte, len(rows))
	results := make([]Result, len(rows))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				data[i], results[i] = renderRow(rows[i], opts)
			}
		}()
	}
	for i := range rows {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	zw := zip.NewWriter(w)
	used := map[string]int{}
	for i := range results {
		if results[i].Err != nil {
			continue
		}
		results[i].File = uniqueName(used, results[i].File, opts.Export.Format.Ext())
		f, err := zw.Create(results[i].File)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(data[i]); err != nil {
			return nil, err
		}
	}
	if err := writeReport(zw, results); err != nil {
		return nil, err
	}
	return results, zw.Close()
}

func renderRow(row Row, opts Options) ([]byte, Result) {
	res := Result{Row: row, Fields: row.Fields}
	t, ok := encode.Lookup(row.Type)
	if !ok {
		if row.Type == "" {
			res.Err = errors.New(`no content type: add a "type" column or choose a default type`)
		} else {
			_, res.Err = encode.Build(row.Type, nil)
		}
		return nil, res
	}
	res.Type = t
	code, err := generate.Prepare(generate.Request{Type: t.Name, Fields: row.Fields, Design: opts.Design, Assets: opts.Assets, Fonts: opts.Fonts})
	if err != nil {
		res.Err = err
		return nil, res
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, opts.Export); err != nil {
		res.Err = err
		return nil, res
	}
	if opts.Verify {
		px := opts.Export.Size
		if opts.Export.Format == render.PDF || opts.Export.Format == render.EPS {
			px = render.MaxSize // printed: resolution is not the bottleneck
		}
		res.Warnings = code.Verify(px).Warnings
	}
	name := row.Name
	if name == "" {
		name = generate.DefaultName(t, row.Fields)
	}
	res.File = generate.Slug(name)
	return buf.Bytes(), res
}

// uniqueName returns stem.ext, or stem-2.ext, stem-3.ext ... if taken.
func uniqueName(used map[string]int, stem, ext string) string {
	used[stem]++
	if n := used[stem]; n > 1 {
		return fmt.Sprintf("%s-%d.%s", stem, n, ext)
	}
	return stem + "." + ext
}

func writeReport(zw *zip.Writer, results []Result) error {
	f, err := zw.Create("report.csv")
	if err != nil {
		return err
	}
	cw := csv.NewWriter(f)
	cw.Write([]string{"line", "name", "type", "file", "status", "message"})
	for _, r := range results {
		status, msg := "ok", strings.Join(r.Warnings, "; ")
		switch {
		case r.Err != nil:
			status, msg = "error", r.Err.Error()
		case len(r.Warnings) > 0:
			status = "warning"
		}
		cw.Write([]string{fmt.Sprint(r.Row.Line), r.Row.Name, r.Row.Type, r.File, status, msg})
	}
	cw.Flush()
	return cw.Error()
}

// Summary counts results by status.
func Summary(results []Result) (ok, warnings, failed int) {
	for _, r := range results {
		switch {
		case r.Err != nil:
			failed++
		case len(r.Warnings) > 0:
			warnings++
		default:
			ok++
		}
	}
	return ok, warnings, failed
}
