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
		{"bad color", []string{"generate", "--data", "a.com", "--fg", "blue", "-o", "x.png"}, 1, "foreground"},
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

func TestIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true, "": false, "0.0.0.0": false, "192.168.1.5": false} {
		if got := isLoopback(host); got != want {
			t.Errorf("isLoopback(%q) = %v", host, got)
		}
	}
}

func TestGenerateStyledFormats(t *testing.T) {
	dir := t.TempDir()
	logoPath := filepath.Join(dir, "logo.svg")
	os.WriteFile(logoPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="5" fill="#c02020"/></svg>`), 0o644)
	style := []string{"generate", "--data", "example.com", "--shape", "classy-rounded", "--eye-border", "leaf",
		"--eye-center", "circle", "--gradient", "radial", "--fg", "#1b2a49", "--fg2", "#0b5d7a",
		"--frame", "banner-bottom", "--frame-text", "Scan the tide", "--strict"}
	for _, tc := range []struct {
		file, magic string
		extra       []string
	}{
		{"a.png", "\x89PNG", []string{"--logo", "icon:wifi"}},
		{"a.jpg", "\xff\xd8", []string{"--logo", logoPath, "--logo-circle"}},
		{"a.svg", "<svg", []string{"--logo", "icon:star"}},
		{"a.pdf", "%PDF", []string{"--logo", logoPath, "--page", "a4", "--print-size", "80"}},
		{"a.eps", "%!PS", []string{"--logo", "icon:home"}},
	} {
		out := filepath.Join(dir, tc.file)
		code, _, stderr := runCLI(append(append(style, tc.extra...), "-o", out)...)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", tc.file, code, stderr)
		}
		b, _ := os.ReadFile(out)
		if !strings.HasPrefix(string(b), tc.magic) {
			t.Errorf("%s starts with %q", tc.file, b[:min(len(b), 8)])
		}
		if !strings.Contains(stderr, "ECC H") {
			t.Errorf("%s: a logo should force ECC H: %s", tc.file, stderr)
		}
	}
	code, _, stderr := runCLI("generate", "--data", "a.com", "--shape", "blob", "-o", filepath.Join(dir, "x.png"))
	if code != 1 || !strings.Contains(stderr, "unknown module shape") {
		t.Errorf("bad shape: exit %d %s", code, stderr)
	}
	code, _, stderr = runCLI("generate", "--data", "a.com", "--logo", filepath.Join(dir, "missing.png"), "-o", filepath.Join(dir, "x.png"))
	if code != 1 {
		t.Errorf("missing logo file: exit %d %s", code, stderr)
	}
}

func TestStyles(t *testing.T) {
	code, stdout, _ := runCLI("styles")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"Module shapes (--shape)", "classy-rounded", "icon:wifi", "banner-bottom"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("styles output missing %q", want)
		}
	}
}

func TestGenerateBarcodes(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--symbology", "ean13", "--data", "590123412345"}, "EAN-13, 95 modules wide"},
		{[]string{"--symbology", "datamatrix", "--value", "HELLO"}, "Data Matrix"},
		{[]string{"--symbology", "code128", "--data", "SHIP-1"}, "Code 128"},
	} {
		args := append([]string{"generate", "--type", "barcode", "--strict", "-o", filepath.Join(dir, "b.png")}, tc.args...)
		code, _, stderr := runCLI(args...)
		if code != 0 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d %s", tc.args, code, stderr)
		}
	}
	code, _, stderr := runCLI("generate", "--type", "barcode", "--symbology", "ean13", "--data", "5901234123458", "-o", filepath.Join(dir, "b.png"))
	if code != 1 || !strings.Contains(stderr, "check digit") {
		t.Errorf("bad check digit: exit %d %s", code, stderr)
	}
}
