package render

import (
	"bytes"
	"encoding/xml"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/matrix"
)

func testScene(t *testing.T, s Style) (*matrix.Matrix, *Scene) {
	t.Helper()
	m, err := matrix.Build("https://example.com", matrix.Options{Level: matrix.M})
	if err != nil {
		t.Fatal(err)
	}
	sc, err := BuildScene(m, s)
	if err != nil {
		t.Fatal(err)
	}
	return m, sc
}

func TestBuildSceneCoversDarkModules(t *testing.T) {
	m, sc := testScene(t, DefaultStyle())
	if sc.Size != float64(m.Size+8) {
		t.Fatalf("scene size %v, want %d", sc.Size, m.Size+8)
	}
	covered := map[[2]int]bool{}
	for _, r := range sc.Layers[0].Rects {
		for x := 0; x < int(r.W); x++ {
			covered[[2]int{int(r.X) - 4 + x, int(r.Y) - 4}] = true
		}
	}
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			if covered[[2]int{x, y}] != m.Dark(x, y) {
				t.Fatalf("module (%d,%d): covered=%v dark=%v", x, y, covered[[2]int{x, y}], m.Dark(x, y))
			}
		}
	}
}

func TestRasterizePixels(t *testing.T) {
	s := DefaultStyle()
	s.Foreground = color.NRGBA{0x12, 0x34, 0x56, 255}
	_, sc := testScene(t, s)
	size := int(sc.Size) * 10
	img := Rasterize(sc, size)
	// Quiet zone corner is background, finder pattern corner is foreground.
	if got := img.RGBAAt(5, 5); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("quiet zone pixel = %v", got)
	}
	if got := img.RGBAAt(45, 45); got != (color.RGBA{0x12, 0x34, 0x56, 255}) {
		t.Errorf("finder pixel = %v", got)
	}
}

func TestRasterizeTransparent(t *testing.T) {
	s := DefaultStyle()
	s.Background = color.NRGBA{}
	_, sc := testScene(t, s)
	img := Rasterize(sc, 256)
	if got := img.RGBAAt(1, 1); got.A != 0 {
		t.Errorf("background alpha = %d, want 0", got.A)
	}
}

func TestExportPNG(t *testing.T) {
	_, sc := testScene(t, DefaultStyle())
	var buf bytes.Buffer
	if err := Export(&buf, sc, PNG, 300); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 300 {
		t.Fatalf("bounds %v, want 300x300", b)
	}
}

func TestExportSVG(t *testing.T) {
	s := DefaultStyle()
	s.Foreground = color.NRGBA{0, 0, 0x80, 0x80}
	_, sc := testScene(t, s)
	var buf bytes.Buffer
	if err := Export(&buf, sc, SVG, 512); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`width="512"`, `viewBox="0 0 33 33"`, `fill="#000080"`, `fill-opacity="0.502"`, `fill="#ffffff"`} {
		if !strings.Contains(out, want) {
			t.Errorf("SVG missing %s", want)
		}
	}
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() != "EOF" {
				t.Fatalf("SVG is not well-formed XML: %v", err)
			}
			break
		}
	}
}

func TestExportSizeLimits(t *testing.T) {
	_, sc := testScene(t, DefaultStyle())
	for _, size := range []int{MinSize - 1, MaxSize + 1} {
		if err := Export(&bytes.Buffer{}, sc, PNG, size); err == nil {
			t.Errorf("size %d: expected error", size)
		}
	}
}

func TestParseColor(t *testing.T) {
	tests := map[string]color.NRGBA{
		"#000":        {0, 0, 0, 255},
		"#FF8800":     {255, 136, 0, 255},
		"1a2b3c":      {0x1a, 0x2b, 0x3c, 255},
		"#11223344":   {0x11, 0x22, 0x33, 0x44},
		"transparent": {},
	}
	for in, want := range tests {
		if got, err := ParseColor(in); err != nil || got != want {
			t.Errorf("ParseColor(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "#12", "#gggggg", "red"} {
		if _, err := ParseColor(bad); err == nil {
			t.Errorf("ParseColor(%q): expected error", bad)
		}
	}
}

func TestFormatFromPath(t *testing.T) {
	if f, err := FormatFromPath("out/code.SVG"); err != nil || f != SVG {
		t.Errorf("got %v, %v", f, err)
	}
	if _, err := FormatFromPath("code"); err == nil {
		t.Error("expected error without extension")
	}
	if _, err := FormatFromPath("code.gif"); err == nil {
		t.Error("expected error for gif")
	}
}

func TestStyleValidate(t *testing.T) {
	s := DefaultStyle()
	s.QuietZone = 11
	if s.Validate() == nil {
		t.Error("expected quiet zone error")
	}
	s = DefaultStyle()
	s.Foreground = color.NRGBA{}
	if s.Validate() == nil {
		t.Error("expected transparent foreground error")
	}
}

// BenchmarkExportPNG1024 tracks the spec target: a 1024 px PNG in < 50 ms.
func BenchmarkExportPNG1024(b *testing.B) {
	m, err := matrix.Build(strings.Repeat("https://example.com/", 10), matrix.Options{Level: matrix.H})
	if err != nil {
		b.Fatal(err)
	}
	sc, err := BuildScene(m, DefaultStyle())
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if err := Export(io.Discard, sc, PNG, 1024); err != nil {
			b.Fatal(err)
		}
	}
}
