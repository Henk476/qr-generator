package generate

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/render"
)

func TestPrepareAndExport(t *testing.T) {
	code, err := Prepare(Request{Type: "url", Fields: map[string]string{"url": "example.com"}, Design: DefaultDesign()})
	if err != nil {
		t.Fatal(err)
	}
	if code.Payload != "https://example.com" {
		t.Errorf("payload %q", code.Payload)
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, render.Options{Format: render.SVG, Size: 256}); err != nil {
		t.Fatal(err)
	}
	if r := code.Verify(256); !r.OK() {
		t.Errorf("warnings: %v", r.Warnings)
	}
}

func TestDesignValidate(t *testing.T) {
	tests := []struct {
		name    string
		mod     func(*Design)
		wantErr string
	}{
		{"ok", func(*Design) {}, ""},
		{"transparent bg", func(d *Design) { d.Background = "transparent" }, ""},
		{"bad ecc", func(d *Design) { d.ECC = "X" }, "L, M, Q or H"},
		{"bad version", func(d *Design) { d.Version = 41 }, "version"},
		{"bad fg", func(d *Design) { d.Foreground = "red" }, "foreground"},
		{"bad bg", func(d *Design) { d.Background = "#12" }, "background"},
		{"bad quiet zone", func(d *Design) { d.QuietZone = -1 }, "quiet zone"},
		{"transparent fg", func(d *Design) { d.Foreground = "transparent" }, "transparent"},
		{"full design", func(d *Design) {
			d.Gradient, d.Foreground2, d.GradientAngle = "radial", "#0b5d7a", 30
			d.Module, d.EyeOuter, d.EyeInner, d.EyeInnerColor = "dots", "leaf", "star", "#c02020"
			d.Logo, d.LogoSize, d.LogoCircle = "icon:wifi", 25, true
			d.Frame, d.FrameFont = "badge", "mono"
		}, ""},
		{"bad gradient color", func(d *Design) { d.Gradient = "linear" }, "gradient end color"},
		{"bad gradient", func(d *Design) { d.Gradient, d.Foreground2 = "spiral", "#000" }, "gradient"},
		{"bad module", func(d *Design) { d.Module = "blob" }, "module shape"},
		{"bad eye color", func(d *Design) { d.EyeOuterColor = "x" }, "eye border color"},
		{"bad logo ref", func(d *Design) { d.Logo = "http://x" }, "logo must be"},
		{"bad icon", func(d *Design) { d.Logo = "icon:unicorn" }, "unknown icon"},
		{"logo too big", func(d *Design) { d.Logo, d.LogoSize = "icon:star", 40 }, "logo size"},
		{"bad frame", func(d *Design) { d.Frame = "neon" }, "frame"},
		{"bad frame color", func(d *Design) { d.Frame, d.FrameColor = "border", "#1" }, "frame color"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DefaultDesign()
			tt.mod(&d)
			err := d.Validate()
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLogoForcesHighECCAndLoadsAssets(t *testing.T) {
	d := DefaultDesign()
	d.ECC, d.Logo = "L", "asset:abc"
	var asked string
	code, err := Prepare(Request{
		Type: "url", Fields: map[string]string{"url": "example.com"}, Design: d,
		Assets: func(id string) (image.Image, error) {
			asked = id
			return image.NewNRGBA(image.Rect(0, 0, 10, 10)), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != "abc" {
		t.Errorf("asset loader asked for %q", asked)
	}
	if code.Matrix.Level.String() != "H" {
		t.Errorf("ECC %s, want H with a logo", code.Matrix.Level)
	}
	_, err = Prepare(Request{Type: "url", Fields: map[string]string{"url": "example.com"}, Design: d,
		Assets: func(string) (image.Image, error) { return nil, errors.New("gone") }})
	if err == nil || !strings.Contains(err.Error(), "logo: gone") {
		t.Errorf("err = %v", err)
	}
}

func TestOldDesignJSONStillLoads(t *testing.T) {
	// A design saved by M2, before shapes, logos and frames existed.
	var d Design
	if err := json.Unmarshal([]byte(`{"ecc":"Q","quietZone":4,"fg":"#1b2a49","bg":"#ffffff"}`), &d); err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(DefaultDesign())
	if string(b) != `{"ecc":"M","quietZone":4,"fg":"#000000","bg":"#ffffff"}` {
		t.Errorf("default design JSON grew: %s", b)
	}
}

func TestNormalize(t *testing.T) {
	d := DefaultDesign()
	d.Foreground2, d.GradientAngle = "#123456", 45 // no gradient selected
	d.Module, d.EyeOuter = "square", "square"
	d.LogoSize, d.LogoCircle = 25, true         // no logo
	d.FrameText, d.FrameColor = "HI", "#ff0000" // no frame
	if got := d.Normalize(); got != DefaultDesign() {
		t.Errorf("Normalize = %+v", got)
	}
	d = DefaultDesign()
	d.Frame, d.FrameText, d.FrameFont, d.FrameColor, d.FrameTextColor = "border", "SCAN ME", "sans", "#000000", "#FFFFFF"
	d.Logo, d.LogoSize = "icon:star", DefaultLogoSize
	want := DefaultDesign()
	want.Frame, want.Logo = "border", "icon:star"
	if got := d.Normalize(); got != want {
		t.Errorf("Normalize = %+v, want %+v", got, want)
	}
}
