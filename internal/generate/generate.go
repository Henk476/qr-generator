// Package generate is the content-to-image pipeline shared by the CLI and the
// web UI: build the payload, lay out the matrix, style it, export it.
package generate

import (
	"fmt"
	"io"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/matrix"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/verify"
)

// Design is the serializable look of a code. It is what presets store.
type Design struct {
	ECC        string `json:"ecc"`               // L, M, Q or H
	Version    int    `json:"version,omitempty"` // 0 = smallest that fits
	QuietZone  int    `json:"quietZone"`
	Foreground string `json:"fg"` // #rrggbb or #rrggbbaa
	Background string `json:"bg"` // #rrggbb, #rrggbbaa or "transparent"
}

// DefaultDesign is black on white, ECC M, standard quiet zone.
func DefaultDesign() Design {
	return Design{ECC: "M", QuietZone: 4, Foreground: "#000000", Background: "#ffffff"}
}

// Validate checks every design value without rendering anything.
func (d Design) Validate() error {
	_, _, err := d.parse()
	return err
}

func (d Design) parse() (matrix.Options, render.Style, error) {
	lvl, err := matrix.ParseLevel(d.ECC)
	if err != nil {
		return matrix.Options{}, render.Style{}, err
	}
	if d.Version < 0 || d.Version > 40 {
		return matrix.Options{}, render.Style{}, fmt.Errorf("version must be 0 (auto) or 1-40, got %d", d.Version)
	}
	s := render.Style{QuietZone: d.QuietZone}
	if s.Foreground, err = render.ParseColor(d.Foreground); err != nil {
		return matrix.Options{}, render.Style{}, fmt.Errorf("foreground: %w", err)
	}
	if s.Background, err = render.ParseColor(d.Background); err != nil {
		return matrix.Options{}, render.Style{}, fmt.Errorf("background: %w", err)
	}
	if err := s.Validate(); err != nil {
		return matrix.Options{}, render.Style{}, err
	}
	return matrix.Options{Level: lvl, Version: d.Version}, s, nil
}

// Request is everything needed to produce a code.
type Request struct {
	Type   string
	Fields map[string]string
	Design Design
}

// Code is a prepared code, ready to export in any format.
type Code struct {
	Payload string
	Matrix  *matrix.Matrix
	Scene   *render.Scene
}

// Prepare builds the payload and lays out the code.
func Prepare(req Request) (*Code, error) {
	opts, style, err := req.Design.parse()
	if err != nil {
		return nil, err
	}
	payload, err := encode.Build(req.Type, req.Fields)
	if err != nil {
		return nil, err
	}
	m, err := matrix.Build(payload, opts)
	if err != nil {
		return nil, err
	}
	sc, err := render.BuildScene(m, style)
	if err != nil {
		return nil, err
	}
	return &Code{Payload: payload, Matrix: m, Scene: sc}, nil
}

// Export writes the code as format at size px.
func (c *Code) Export(w io.Writer, f render.Format, size int) error {
	return render.Export(w, c.Scene, f, size)
}

// Verify runs the scannability check for output at size px.
func (c *Code) Verify(size int) verify.Report {
	return verify.Check(c.Payload, c.Scene, size)
}
