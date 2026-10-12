package render

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/matrix"
)

func testMatrix(t testing.TB) *matrix.Matrix {
	t.Helper()
	m, err := matrix.Build("https://example.com", matrix.Options{Level: matrix.H})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func testScene(t testing.TB, s Style) *Scene {
	t.Helper()
	sc, err := BuildScene(testMatrix(t), s)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

// fancyStyle exercises every feature at once.
func fancyStyle() Style {
	s := DefaultStyle()
	s.Foreground = color.NRGBA{0x1b, 0x2a, 0x49, 255}
	s.Gradient = Gradient{Kind: LinearGradient, Color2: color.NRGBA{0x0b, 0x5d, 0x7a, 255}, Angle: 45}
	s.Module, s.EyeOuter, s.EyeInner = "rounded", "circle", "star"
	red := color.NRGBA{0xc0, 0x20, 0x20, 255}
	s.EyeInnerColor = &red
	s.Logo = &Logo{Icon: "wifi", Size: 0.22, Knockout: true, Circle: true}
	s.Frame = &Frame{Kind: "banner-bottom", Text: "SCAN ME", Font: "sans", Color: s.Foreground, TextColor: white}
	return s
}

func TestPlainSceneCoversDarkModules(t *testing.T) {
	m := testMatrix(t)
	sc := testScene(t, DefaultStyle())
	if sc.Width != float64(m.Size+8) || sc.Height != sc.Width {
		t.Fatalf("scene %vx%v, want %d square", sc.Width, sc.Height, m.Size+8)
	}
	img := Rasterize(sc, int(sc.Width)*4)
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; x++ {
			c := img.RGBAAt((x+4)*4+2, (y+4)*4+2)
			if dark := c.R < 128; dark != m.Dark(x, y) {
				t.Fatalf("module (%d,%d): dark=%v want %v", x, y, dark, m.Dark(x, y))
			}
		}
	}
}

func TestRasterizeColorsAndTransparency(t *testing.T) {
	s := DefaultStyle()
	s.Foreground = color.NRGBA{0x12, 0x34, 0x56, 255}
	s.Background = color.NRGBA{}
	sc := testScene(t, s)
	img := Rasterize(sc, int(sc.Width)*10)
	if got := img.RGBAAt(5, 5); got.A != 0 {
		t.Errorf("quiet zone alpha = %d, want 0", got.A)
	}
	if got := img.RGBAAt(45, 45); got != (color.RGBA{0x12, 0x34, 0x56, 255}) {
		t.Errorf("finder pixel = %v", got)
	}
}

func TestGradient(t *testing.T) {
	s := DefaultStyle()
	s.Foreground = color.NRGBA{255, 0, 0, 255}
	s.Gradient = Gradient{Kind: LinearGradient, Color2: color.NRGBA{0, 0, 255, 255}}
	sc := testScene(t, s)
	img := Rasterize(sc, int(sc.Width)*10)
	// Top-left finder is near the red end, top-right near the blue end.
	left, right := img.RGBAAt(45, 45), img.RGBAAt(int(sc.Width)*10-45, 45)
	if !(left.R > left.B && right.B > right.R) {
		t.Errorf("gradient left %v right %v", left, right)
	}
	p := resolvePaint(s.Foreground, Gradient{Kind: RadialGradient, Color2: s.Gradient.Color2}, Rect{0, 0, 10, 10})
	if c := p.At(5, 5); c != s.Foreground {
		t.Errorf("radial center = %v", c)
	}
}

func TestPathReverse(t *testing.T) {
	// A ring must leave its hole empty when rasterized.
	sc := &Scene{Width: 10, Height: 10, Items: []Item{{
		Path:  Ring(RectPath(0, 0, 10, 10), Circle(5, 5, 3)),
		Paint: SolidPaint(color.NRGBA{0, 0, 0, 255}),
	}}}
	img := Rasterize(sc, 100)
	if img.RGBAAt(50, 50).A != 0 || img.RGBAAt(5, 5).A != 255 {
		t.Errorf("ring hole %v, ring body %v", img.RGBAAt(50, 50), img.RGBAAt(5, 5))
	}
	if r := Circle(5, 5, 3).Reverse().Reverse(); len(r.Segs) != len(Circle(5, 5, 3).Segs) {
		t.Error("double reverse changed segment count")
	}
}

func TestFramesGrowCanvas(t *testing.T) {
	m := testMatrix(t)
	for _, f := range Frames {
		t.Run("frame="+f.Value, func(t *testing.T) {
			s := DefaultStyle()
			s.Frame = &Frame{Kind: f.Value, Text: "SCAN ME", Font: "sans", Color: color.NRGBA{0, 0, 0, 255}, TextColor: white}
			sc, err := BuildScene(m, s)
			if err != nil {
				t.Fatal(err)
			}
			size := float64(m.Size + 8)
			if f.Value == "" {
				if sc.Width != size || sc.Height != size {
					t.Errorf("no frame changed size to %vx%v", sc.Width, sc.Height)
				}
				return
			}
			if sc.Height <= size {
				t.Errorf("frame %s did not add room for text: %vx%v", f.Value, sc.Width, sc.Height)
			}
			c := sc.Code
			if c.X < 0 || c.Y < 0 || c.X+c.W > sc.Width+1e-9 || c.Y+c.H > sc.Height+1e-9 {
				t.Errorf("code box %+v outside canvas %vx%v", c, sc.Width, sc.Height)
			}
		})
	}
}

func TestTextOutline(t *testing.T) {
	p := textInBox("SCAN ME", fontFor("sans"), Rect{0, 0, 100, 20}, 0.5)
	b := p.Bounds()
	if b.W <= 0 || b.X < 0 || b.X+b.W > 100.01 || b.Y < 0 || b.Y+b.H > 20.01 {
		t.Fatalf("text bounds %+v outside box", b)
	}
	if long := textInBox(strings.Repeat("W", 30), fontFor("mono"), Rect{0, 0, 50, 20}, 0.5); long.Bounds().W > 50.01 {
		t.Errorf("long text not shrunk to fit: %+v", long.Bounds())
	}
}

func TestIconsDrawInsideBox(t *testing.T) {
	for _, name := range IconNames() {
		p := iconPath(name, Rect{10, 10, 24, 24})
		b := p.Bounds()
		if p.Empty() || b.X < 9.5 || b.Y < 9.5 || b.X+b.W > 34.5 || b.Y+b.H > 34.5 {
			t.Errorf("icon %s bounds %+v", name, b)
		}
	}
}

func TestKnockout(t *testing.T) {
	m := testMatrix(t)
	s := DefaultStyle()
	s.Logo = &Logo{Icon: "star", Size: 0.3, Knockout: true}
	with, _ := BuildScene(m, s)
	s.Logo.Knockout = false
	without, _ := BuildScene(m, s)
	// Item 0 is the data modules.
	if len(with.Items[0].Path.Segs) >= len(without.Items[0].Path.Segs) {
		t.Error("knockout did not remove modules")
	}
}

func TestLogoImage(t *testing.T) {
	logo := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for i := range logo.Pix {
		logo.Pix[i] = 200
	}
	s := DefaultStyle()
	s.Logo = &Logo{Image: logo, Size: 0.25, Knockout: true, Circle: true}
	sc := testScene(t, s)
	last := sc.Items[len(sc.Items)-1]
	if last.Image == nil || last.Path.Empty() {
		t.Fatal("expected a circle-clipped image item last")
	}
	if r := last.ImageRect; r.W != 2*r.H {
		t.Errorf("image aspect not preserved: %+v", r)
	}
	for _, f := range Formats() {
		if err := Export(io.Discard, sc, Options{Format: f, Size: 512}); err != nil {
			t.Errorf("%s with image logo: %v", f, err)
		}
	}
}

func TestExportPNGAndJPEG(t *testing.T) {
	sc := testScene(t, fancyStyle())
	for _, f := range []Format{PNG, JPEG} {
		var buf bytes.Buffer
		if err := Export(&buf, sc, Options{Format: f, Size: 400}); err != nil {
			t.Fatal(err)
		}
		var img image.Image
		var err error
		if f == PNG {
			img, err = png.Decode(&buf)
		} else {
			img, err = jpeg.Decode(&buf)
		}
		if err != nil {
			t.Fatal(err)
		}
		if b := img.Bounds(); b.Dx() != 400 || b.Dy() <= 400 {
			t.Errorf("%s bounds %v, want 400 wide and taller (frame)", f, b)
		}
	}
}

func TestExportSVG(t *testing.T) {
	s := fancyStyle()
	s.Foreground = color.NRGBA{0, 0, 0x80, 0x80}
	var buf bytes.Buffer
	if err := Export(&buf, testScene(t, s), Options{Format: SVG, Size: 512}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`width="512"`, "<linearGradient", `stop-color="#000080" stop-opacity="0.502"`, `fill="url(#cqr-g0)"`, `fill="#c02020"`} {
		if !strings.Contains(out, want) {
			t.Errorf("SVG missing %s", want)
		}
	}
	dec := xml.NewDecoder(strings.NewReader(out))
	for {
		if _, err := dec.Token(); err != nil {
			if err != io.EOF {
				t.Fatalf("SVG is not well-formed XML: %v", err)
			}
			break
		}
	}
}

// TestExportPDF checks the structure: header, every xref offset points at
// its object, and the trailer finds the xref table.
func TestExportPDF(t *testing.T) {
	logo := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	s := fancyStyle()
	s.Logo = &Logo{Image: logo, Size: 0.2, Knockout: true, Circle: true}
	for _, page := range []string{"fit", "a4", "letter"} {
		var buf bytes.Buffer
		if err := Export(&buf, testScene(t, s), Options{Format: PDF, Page: page, PrintMM: 60}); err != nil {
			t.Fatal(err)
		}
		pdf := buf.Bytes()
		if !bytes.HasPrefix(pdf, []byte("%PDF-1.4")) || !bytes.HasSuffix(pdf, []byte("%%EOF\n")) {
			t.Fatal("bad PDF header or trailer")
		}
		m := regexp.MustCompile(`startxref\n(\d+)\n`).FindSubmatch(pdf)
		if m == nil {
			t.Fatal("no startxref")
		}
		xref, _ := strconv.Atoi(string(m[1]))
		if !bytes.HasPrefix(pdf[xref:], []byte("xref\n")) {
			t.Fatalf("startxref %d does not point at xref", xref)
		}
		entries := regexp.MustCompile(`(\d{10}) 00000 n `).FindAllSubmatch(pdf[xref:], -1)
		for i, e := range entries {
			off, _ := strconv.Atoi(string(e[1]))
			if want := strconv.Itoa(i+1) + " 0 obj"; !bytes.HasPrefix(pdf[off:], []byte(want)) {
				t.Fatalf("xref entry %d points at %q", i+1, pdf[off:off+10])
			}
		}
		for _, want := range []string{"/ShadingType 2", "/SMask", "/Subtype /Image"} {
			if !bytes.Contains(pdf, []byte(want)) {
				t.Errorf("PDF missing %s", want)
			}
		}
		if page == "a4" && !bytes.Contains(pdf, []byte("/MediaBox [0 0 595.276 841.89]")) {
			t.Error("A4 media box missing")
		}
	}
	if err := Export(io.Discard, testScene(t, s), Options{Format: PDF, Page: "a4", PrintMM: 300}); err == nil {
		t.Error("expected error for a code larger than the page")
	}
}

func TestExportEPS(t *testing.T) {
	var buf bytes.Buffer
	if err := Export(&buf, testScene(t, fancyStyle()), Options{Format: EPS, PrintMM: 25.4}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"%!PS-Adobe-3.0 EPSF-3.0", "%%BoundingBox: 0 0 72 ", "shfill", "%%EOF"} {
		if !strings.Contains(out, want) {
			t.Errorf("EPS missing %q", want)
		}
	}
	if strings.Count(out, "gsave") != strings.Count(out, "grestore") {
		t.Error("unbalanced gsave/grestore")
	}
}

func TestExportOptionLimits(t *testing.T) {
	sc := testScene(t, DefaultStyle())
	bad := []Options{
		{Format: PNG, Size: MinSize - 1},
		{Format: SVG, Size: MaxSize + 1},
		{Format: JPEG, Size: 512, Quality: 101},
		{Format: PDF, PrintMM: 5},
		{Format: EPS, PrintMM: 2000},
		{Format: PDF, Page: "a3"},
		{Format: "gif", Size: 512},
	}
	for _, o := range bad {
		if err := Export(io.Discard, sc, o); err == nil {
			t.Errorf("%+v: expected error", o)
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

func TestFormats(t *testing.T) {
	if f, err := FormatFromPath("out/code.JPG"); err != nil || f != JPEG || f.Ext() != "jpg" {
		t.Errorf("got %v, %v", f, err)
	}
	if _, err := FormatFromPath("code"); err == nil {
		t.Error("expected error without extension")
	}
	if !PDF.Vector() || PNG.Vector() {
		t.Error("Vector() wrong")
	}
}

func TestStyleValidate(t *testing.T) {
	bad := []func(*Style){
		func(s *Style) { s.QuietZone = 11 },
		func(s *Style) { s.Foreground = color.NRGBA{} },
		func(s *Style) { s.Module = "hexagon" },
		func(s *Style) { s.EyeOuter = "star" },
		func(s *Style) { s.Gradient.Kind = "conic" },
		func(s *Style) { s.Logo = &Logo{Icon: "nope", Size: 0.2} },
		func(s *Style) { s.Logo = &Logo{Icon: "star", Size: 0.5} },
		func(s *Style) { s.Frame = &Frame{Kind: "neon"} },
		func(s *Style) { s.Frame = &Frame{Kind: "label", Text: strings.Repeat("x", 31)} },
	}
	for i, mod := range bad {
		s := DefaultStyle()
		mod(&s)
		if s.Validate() == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

// BenchmarkExportPNG1024 tracks the spec target: a 1024 px PNG in < 50 ms.
func BenchmarkExportPNG1024(b *testing.B) {
	for _, tc := range []struct {
		name  string
		style Style
	}{{"plain", DefaultStyle()}, {"fancy", fancyStyle()}} {
		b.Run(tc.name, func(b *testing.B) {
			sc := testScene(b, tc.style)
			for b.Loop() {
				if err := Export(io.Discard, sc, Options{Format: PNG, Size: 1024}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestEveryFontLoadsAndDiffers(t *testing.T) {
	widths := map[float64]string{}
	for _, f := range Fonts {
		if fontFor(f.Value) == nil {
			t.Fatalf("font %s did not load", f.Value)
		}
		p, w, capH := textOutline("Scan Me", fontFor(f.Value))
		if p.Empty() || w <= 0 || capH <= 0 {
			t.Errorf("font %s: empty outline (w=%v cap=%v)", f.Value, w, capH)
		}
		if other, dup := widths[w]; dup {
			t.Errorf("fonts %s and %s produce identical widths: fallback in use?", f.Value, other)
		}
		widths[w] = f.Value
	}
}

func TestPerEyeOverrides(t *testing.T) {
	m := testMatrix(t)
	red := color.NRGBA{0xc0, 0x20, 0x20, 255}
	s := DefaultStyle()
	s.EyeOuter = "circle"
	s.PerEye[1] = EyeStyle{Outer: "square", Inner: "star", InnerColor: &red}
	sc, err := BuildScene(m, s)
	if err != nil {
		t.Fatal(err)
	}
	// Items: modules, then outer+inner for each of the three eyes.
	eyes := sc.Items[1:7]
	if eyes[0].Snap || !eyes[2].Snap { // top-left inherits circle, top-right overrides to square
		t.Error("eye border overrides not applied")
	}
	if eyes[3].Paint.Color != red || eyes[1].Paint.Color == red {
		t.Error("eye center color override applied to the wrong eye")
	}
	s.PerEye[2].Outer = "hexagon"
	if s.Validate() == nil || !strings.Contains(s.Validate().Error(), "bottom-left") {
		t.Errorf("bad override error = %v", s.Validate())
	}
}

func TestParseFont(t *testing.T) {
	f, err := ParseFont(libreBaskervilleBold)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Name(), "Baskerville") {
		t.Errorf("name %q", f.Name())
	}
	s := DefaultStyle()
	s.Frame = &Frame{Kind: "label", Text: "Scan", Font: "custom", Custom: f, Color: color.NRGBA{0, 0, 0, 255}}
	if _, err := BuildScene(testMatrix(t), s); err != nil {
		t.Fatalf("custom font frame: %v", err)
	}
	for name, data := range map[string][]byte{
		"garbage":   []byte("not a font at all"),
		"too large": make([]byte, MaxFontBytes+1),
		"truncated": libreBaskervilleBold[:2000],
	} {
		if _, err := ParseFont(data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
