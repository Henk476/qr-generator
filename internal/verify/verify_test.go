package verify

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/barcode"
	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/matrix"
	"github.com/Henk476/qr-generator/internal/render"
)

// samples has one realistic input per content type.
var samples = map[string]map[string]string{
	"url":      {"url": "example.com/some/path?q=1"},
	"text":     {"text": "Canute bade the tide turn back. Ünïcödé ✓ 日本"},
	"email":    {"to": "canute@example.com", "subject": "The tide", "body": "It did not listen."},
	"call":     {"phone": "+44 20 7946 0000"},
	"sms":      {"phone": "+31612345678", "message": "Running late"},
	"whatsapp": {"phone": "+31612345678", "message": "Hello!"},
	"wifi":     {"ssid": "Bosham;Shore", "password": `tide:"out"`, "hidden": "true"},
	"vcard": {"first": "Canute", "last": "Knutsson", "org": "North Sea Empire", "title": "King",
		"mobile": "+4512345678", "email": "canute@example.com", "url": "https://example.com",
		"street": "1 Shore Rd", "city": "Bosham", "country": "England", "note": "Prefers dry feet"},
	"event": {"title": "Tide demonstration", "start": "2026-10-11 10:00", "tz": "Europe/London",
		"location": "Bosham beach", "description": "Bring towels"},
	"barcode":  {"symbology": "datamatrix", "value": "CANUTE-1016"},
	"location": {"latitude": "50.8304", "longitude": "-0.8579", "label": "Bosham shore"},
	"paypal":   {"username": "canute", "amount": "12.50", "currency": "GBP"},
	"crypto":   {"currency": "bitcoin", "address": "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", "amount": "0.001", "message": "Tide fund"},
}

func render1(t *testing.T, payload string, opts matrix.Options, s render.Style) *render.Scene {
	t.Helper()
	m, err := matrix.Build(payload, opts)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := render.BuildScene(m, s)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// TestRoundTrip renders every content type at every ECC level in several
// styles and checks the decoded text matches the payload exactly.
func TestRoundTrip(t *testing.T) {
	if len(samples) != len(encode.Types()) {
		t.Fatalf("samples cover %d types, registry has %d", len(samples), len(encode.Types()))
	}
	styles := map[string]render.Style{
		"default":     render.DefaultStyle(),
		"navy-cream":  {Foreground: color.NRGBA{0x1b, 0x2a, 0x49, 255}, Background: color.NRGBA{0xfa, 0xf3, 0xe0, 255}, QuietZone: 4},
		"transparent": {Foreground: color.NRGBA{0x20, 0x20, 0x20, 255}, QuietZone: 2},
	}
	for typ, in := range samples {
		payload, err := encode.Build(typ, in)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		for _, lvl := range []matrix.Level{matrix.L, matrix.M, matrix.Q, matrix.H} {
			for sname, s := range styles {
				t.Run(fmt.Sprintf("%s/%s/%s", typ, lvl, sname), func(t *testing.T) {
					r := Check(payload, render1(t, payload, matrix.Options{Level: lvl}, s), 1024)
					if !r.Match {
						t.Fatalf("decode mismatch: %+v", r)
					}
					if !r.OK() {
						t.Errorf("unexpected warnings: %v", r.Warnings)
					}
				})
			}
		}
	}
}

// TestExportedPNGDecodes decodes the actual exported PNG bytes, not just the
// in-memory raster, at a typical small size.
func TestExportedPNGDecodes(t *testing.T) {
	payload := "https://example.com/hello"
	sc := render1(t, payload, matrix.Options{Level: matrix.M}, render.DefaultStyle())
	var buf bytes.Buffer
	if err := render.Export(&buf, sc, render.Options{Format: render.PNG, Size: 200}); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(img)
	if err != nil || got != payload {
		t.Fatalf("Decode = %q, %v", got, err)
	}
}

func TestWarnings(t *testing.T) {
	payload := "hello"
	tests := []struct {
		name     string
		style    render.Style
		outPx    int
		wantWarn string
	}{
		{"low contrast", render.Style{Foreground: color.NRGBA{0xaa, 0xaa, 0xaa, 255}, Background: color.NRGBA{255, 255, 255, 255}, QuietZone: 4}, 1024, "low contrast"},
		{"inverted", render.Style{Foreground: color.NRGBA{255, 255, 255, 255}, Background: color.NRGBA{0, 0, 0, 255}, QuietZone: 4}, 1024, "light-on-dark"},
		{"no quiet zone", render.Style{Foreground: color.NRGBA{0, 0, 0, 255}, Background: color.NRGBA{255, 255, 255, 255}, QuietZone: 0}, 1024, "quiet zone"},
		{"tiny output", render.DefaultStyle(), 40, "px per module"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Check(payload, render1(t, payload, matrix.Options{Level: matrix.M}, tt.style), tt.outPx)
			if !strings.Contains(strings.Join(r.Warnings, "\n"), tt.wantWarn) {
				t.Fatalf("warnings %v, want one containing %q", r.Warnings, tt.wantWarn)
			}
		})
	}
}

func TestContrast(t *testing.T) {
	black, white := color.NRGBA{0, 0, 0, 255}, color.NRGBA{255, 255, 255, 255}
	if c := contrast(black, white); c < 20.99 || c > 21.01 {
		t.Errorf("black/white contrast = %v, want 21", c)
	}
	if c := contrast(white, white); c != 1 {
		t.Errorf("white/white contrast = %v, want 1", c)
	}
}

// TestStyledRoundTrip decodes codes in every module shape, eye shape,
// frame and logo setup. Styling must never cost scannability.
func TestStyledRoundTrip(t *testing.T) {
	payload := "https://example.com/canute?tide=out"
	navy := color.NRGBA{0x1b, 0x2a, 0x49, 255}
	base := func() render.Style {
		s := render.DefaultStyle()
		s.Foreground = navy
		return s
	}
	type tc struct {
		name  string
		level matrix.Level
		style render.Style
	}
	var cases []tc
	for _, m := range render.ModuleShapes {
		s := base()
		s.Module = m.Value
		cases = append(cases, tc{"module/" + m.Value, matrix.M, s})
	}
	for _, o := range render.EyeOuterShapes {
		for _, i := range render.EyeInnerShapes {
			s := base()
			s.EyeOuter, s.EyeInner = o.Value, i.Value
			cases = append(cases, tc{"eye/" + o.Value + "+" + i.Value, matrix.M, s})
		}
	}
	for _, f := range render.Frames {
		s := base()
		s.Frame = &render.Frame{Kind: f.Value, Text: "SCAN ME", Font: "sans", Color: navy, TextColor: color.NRGBA{255, 255, 255, 255}}
		cases = append(cases, tc{"frame/" + f.Value, matrix.M, s})
	}
	for _, icon := range render.IconNames() {
		for _, circle := range []bool{false, true} {
			s := base()
			s.Logo = &render.Logo{Icon: icon, Size: 0.3, Knockout: true, Circle: circle}
			cases = append(cases, tc{fmt.Sprintf("logo/%s/circle=%v", icon, circle), matrix.H, s})
		}
	}
	for _, g := range []render.GradientKind{render.LinearGradient, render.RadialGradient} {
		s := base()
		s.Gradient = render.Gradient{Kind: g, Color2: color.NRGBA{0x0b, 0x5d, 0x7a, 255}, Angle: 30}
		s.Module = "dots"
		cases = append(cases, tc{"gradient/" + string(g), matrix.M, s})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Check(payload, render1(t, payload, matrix.Options{Level: c.level}, c.style), 1024)
			if !r.Match {
				t.Fatalf("decode failed: %v", r.Warnings)
			}
		})
	}
}

func TestGradientContrastUsesWeakestEnd(t *testing.T) {
	s := render.DefaultStyle()
	s.Gradient = render.Gradient{Kind: render.LinearGradient, Color2: color.NRGBA{0xcc, 0xcc, 0xcc, 255}}
	r := Check("hello", render1(t, "hello", matrix.Options{Level: matrix.M}, s), 1024)
	if r.Contrast > 2 || !strings.Contains(strings.Join(r.Warnings, "\n"), "low contrast") {
		t.Errorf("contrast %.1f, warnings %v", r.Contrast, r.Warnings)
	}
}

// TestBarcodeRoundTrip decodes every barcode symbology that has an
// offline reader, in plain and styled colors.
func TestBarcodeRoundTrip(t *testing.T) {
	values := map[barcode.Symbology]string{
		barcode.DataMatrix: "Canute bade the tide turn back",
		barcode.PDF417:     "BOARDING PASS CNT 1016",
		barcode.Aztec:      "TICKET 42 / BOSHAM",
		barcode.Code128:    "SHIP-1016-A",
		barcode.EAN13:      "590123412345",
		barcode.UPCA:       "03600029145",
	}
	styles := map[string]render.Style{
		"plain": render.DefaultStyle(),
		"gradient": {Foreground: color.NRGBA{0x1b, 0x2a, 0x49, 255}, Background: color.NRGBA{0xfa, 0xf3, 0xe0, 255}, QuietZone: 4,
			Gradient: render.Gradient{Kind: render.LinearGradient, Color2: color.NRGBA{0x0b, 0x5d, 0x7a, 255}}},
	}
	for _, info := range barcode.Symbologies() {
		for sname, s := range styles {
			t.Run(string(info.Symbology)+"/"+sname, func(t *testing.T) {
				payload, err := barcode.Normalize(info.Symbology, values[info.Symbology])
				if err != nil {
					t.Fatal(err)
				}
				g, err := barcode.Encode(info.Symbology, payload)
				if err != nil {
					t.Fatal(err)
				}
				sc, err := render.BuildBarcodeScene(g, payload, s)
				if err != nil {
					t.Fatal(err)
				}
				r := Check(payload, sc, 1024)
				if info.Symbology == barcode.PDF417 {
					if len(r.Notes) == 0 || r.Decoded {
						t.Errorf("PDF417 should be reported as not checked: %+v", r)
					}
					return
				}
				if !r.Match {
					t.Fatalf("decode failed: %+v", r)
				}
			})
		}
	}
}
