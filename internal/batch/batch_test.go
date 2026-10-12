package batch

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/verify"
)

// sample has a BOM, a renamed column, a default-type row, a duplicate
// name, a failing row and a blank line.
const sample = "\ufefftype,name,url,ssid,password,Phone Number\n" +
	"url,Shop,example.com/shop,,,\n" +
	"wifi,Guest Wi-Fi,,Guest,secret,\n" +
	",Shop,example.com/shop2,,,\n" +
	"call,,,,,+31 6 1234 5678\n" +
	"url,Broken,javascript://x,,,\n" +
	",,,,,\n"

var sampleOpts = ParseOptions{DefaultType: "url", Rename: map[string]string{"phone number": "phone"}}

func TestParse(t *testing.T) {
	rows, err := Parse(strings.NewReader(sample), sampleOpts)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("%d rows, want 5", len(rows))
	}
	if r := rows[1]; r.Type != "wifi" || r.Name != "Guest Wi-Fi" || r.Fields["ssid"] != "Guest" || r.Line != 3 {
		t.Errorf("row 2 = %+v", r)
	}
	if rows[2].Type != "url" || rows[3].Fields["phone"] != "+31 6 1234 5678" {
		t.Errorf("default type or rename not applied: %+v %+v", rows[2], rows[3])
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"":                              "empty",
		"url\n":                         "no rows",
		"url,colour\nexample.com,red\n": "unknown CSV columns: colour",
		"url\n\"unterminated\n":         "parse error on line 2",
	}
	for in, want := range tests {
		if _, err := Parse(strings.NewReader(in), ParseOptions{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) err = %v, want %q", in, err, want)
		}
	}
	if _, err := ParseRename("a=b, c"); err == nil {
		t.Error("expected mapping error")
	}
	if m, err := ParseRename("Web Site=url, Net=ssid"); err != nil || m["web site"] != "url" || m["net"] != "ssid" {
		t.Errorf("ParseRename = %v, %v", m, err)
	}
}

func TestGenerate(t *testing.T) {
	rows, err := Parse(strings.NewReader(sample), sampleOpts)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	results, err := Generate(&buf, rows, Options{
		Design: generate.DefaultDesign(),
		Export: render.Options{Format: render.PNG, Size: 256},
		Verify: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, warn, failed := Summary(results); ok != 4 || warn != 0 || failed != 1 {
		t.Errorf("summary ok=%d warn=%d failed=%d", ok, warn, failed)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*zip.File{}
	var names []string
	for _, f := range zr.File {
		files[f.Name] = f
		names = append(names, f.Name)
	}
	want := []string{"shop.png", "guest-wi-fi.png", "shop-2.png", "call-31-6-1234-5678.png", "report.csv"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("archive has %v, want %v", names, want)
	}

	// Each image decodes to its row's content.
	rc, _ := files["guest-wi-fi.png"].Open()
	img, err := png.Decode(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := verify.Decode(img); err != nil || got != "WIFI:T:WPA;S:Guest;P:secret;;" {
		t.Errorf("decoded %q, %v", got, err)
	}

	rc, _ = files["report.csv"].Open()
	report, _ := csv.NewReader(rc).ReadAll()
	rc.Close()
	if len(report) != 6 || report[5][4] != "error" || !strings.Contains(report[5][5], "scheme") || report[5][0] != "6" {
		t.Errorf("report = %v", report)
	}
}

func TestGenerateRejectsBadExport(t *testing.T) {
	rows := []Row{{Line: 2, Type: "url", Fields: map[string]string{"url": "a.com"}}}
	if _, err := Generate(io.Discard, rows, Options{Design: generate.DefaultDesign(), Export: render.Options{Format: render.PNG, Size: 5}}); err == nil {
		t.Error("expected export option error")
	}
}

// BenchmarkThousandPNGs tracks the spec target: 1,000 PNGs in under 30 s.
func BenchmarkThousandPNGs(b *testing.B) {
	rows := make([]Row, 1000)
	for i := range rows {
		rows[i] = Row{Line: i + 2, Type: "url", Fields: map[string]string{"url": "example.com/item/" + strings.Repeat("x", i%40)}}
	}
	for b.Loop() {
		if _, err := Generate(io.Discard, rows, Options{Design: generate.DefaultDesign(), Export: render.Options{Format: render.PNG, Size: 1024}}); err != nil {
			b.Fatal(err)
		}
	}
}
