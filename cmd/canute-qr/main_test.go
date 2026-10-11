package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/verify"
)

func runCLI(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func TestGeneratePNG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "wifi.png")
	code, _, stderr := runCLI("generate", "--type", "wifi", "--ssid", "Home", "--password", "secret", "--size", "400", "-o", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 400 {
		t.Errorf("width %d, want 400", img.Bounds().Dx())
	}
	if got, err := verify.Decode(img); err != nil || got != "WIFI:T:WPA;S:Home;P:secret;;" {
		t.Errorf("decoded %q, %v", got, err)
	}
	if strings.Contains(stderr, "warning") {
		t.Errorf("unexpected warning: %s", stderr)
	}
}

func TestGenerateSVGToStdout(t *testing.T) {
	code, stdout, stderr := runCLI("generate", "--data", "example.com", "--format", "svg", "--fg", "#1b2a49", "--bg", "transparent", "-o", "-")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "<svg") || !strings.Contains(stdout, `fill="#1b2a49"`) {
		t.Errorf("unexpected SVG: %.200s", stdout)
	}
	if strings.Contains(stdout, "<rect") {
		t.Error("transparent background should not draw a background rect")
	}
}

func TestGenerateErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no output", []string{"generate", "--data", "a.com"}, 1, "-o is required"},
		{"unknown type", []string{"generate", "--type", "fax", "-o", "x.png"}, 1, "unknown content type"},
		{"field of other type", []string{"generate", "--data", "a.com", "--ssid", "x", "-o", "x.png"}, 1, "unknown field"},
		{"data without primary", []string{"generate", "--type", "vcard", "--data", "x", "-o", "x.png"}, 1, "--data is not supported"},
		{"data and primary", []string{"generate", "--data", "a.com", "--url", "b.com", "-o", "x.png"}, 1, "both set"},
		{"bad format", []string{"generate", "--data", "a.com", "-o", "x.gif"}, 1, "unsupported format"},
		{"stdout needs format", []string{"generate", "--data", "a.com", "-o", "-"}, 1, "--format is required"},
		{"bad ecc", []string{"generate", "--data", "a.com", "--ecc", "Z", "-o", "x.png"}, 1, "L, M, Q or H"},
		{"bad color", []string{"generate", "--data", "a.com", "--fg", "blue", "-o", "x.png"}, 1, "--fg"},
		{"bad size", []string{"generate", "--data", "a.com", "--size", "50", "-o", "x.png"}, 1, "size must be"},
		{"stray arg", []string{"generate", "--data", "a.com", "-o", "x.png", "extra"}, 2, "unexpected argument"},
		{"unknown command", []string{"frobnicate"}, 2, "unknown command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(tt.args...)
			if code != tt.code || !strings.Contains(stderr, tt.want) {
				t.Fatalf("exit %d, stderr %q; want exit %d containing %q", code, stderr, tt.code, tt.want)
			}
		})
	}
}

func TestStrictBlocksUnscannable(t *testing.T) {
	out := filepath.Join(t.TempDir(), "x.png")
	code, _, stderr := runCLI("generate", "--data", "a.com", "--fg", "#dddddd", "--strict", "-o", out)
	if code != 1 || !strings.Contains(stderr, "low contrast") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("--strict should not write the file")
	}
	// Without --strict the same code is written with a warning.
	code, _, stderr = runCLI("generate", "--data", "a.com", "--fg", "#dddddd", "-o", out)
	if code != 0 || !strings.Contains(stderr, "warning:") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestTypesAndVersion(t *testing.T) {
	code, stdout, _ := runCLI("types")
	if code != 0 || !strings.Contains(stdout, "wifi  (Wi-Fi)") || !strings.Contains(stdout, "--ssid, --data") {
		t.Errorf("types output:\n%s", stdout)
	}
	code, stdout, _ = runCLI("version")
	if code != 0 || !strings.HasPrefix(stdout, "canute-qr ") {
		t.Errorf("version output %q", stdout)
	}
	code, _, stderr := runCLI("generate", "-h")
	if code != 0 || !strings.Contains(stderr, "--quiet-zone") {
		t.Errorf("generate -h: exit %d\n%s", code, stderr)
	}
}
