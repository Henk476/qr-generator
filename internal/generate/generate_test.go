package generate

import (
	"bytes"
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
	if err := code.Export(&buf, render.SVG, 256); err != nil {
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
