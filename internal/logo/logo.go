// Package logo decodes user-supplied logo files into images. Every logo is
// normalized to a raster image here, so uploaded SVG markup is never served
// or embedded anywhere.
package logo

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // register decoder
	"image/png"
	"net/http"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

// Limits on logo files.
const (
	MaxBytes = 5 << 20 // upload size
	MaxSide  = 4096    // pixels, checked before decoding the whole image
	SVGSide  = 1024    // SVG logos are rasterized at this size
)

// Decode reads a PNG, JPEG or SVG logo.
func Decode(data []byte) (image.Image, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("logo is %d MB; the limit is %d MB", len(data)>>20, MaxBytes>>20)
	}
	switch ct := http.DetectContentType(data); {
	case ct == "image/png" || ct == "image/jpeg":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("unreadable image: %w", err)
		}
		if cfg.Width > MaxSide || cfg.Height > MaxSide || cfg.Width == 0 || cfg.Height == 0 {
			return nil, fmt.Errorf("logo is %dx%d px; the limit is %dx%d", cfg.Width, cfg.Height, MaxSide, MaxSide)
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("unreadable image: %w", err)
		}
		return img, nil
	case looksLikeSVG(data):
		return decodeSVG(data)
	default:
		return nil, fmt.Errorf("unsupported logo type %s: use PNG, JPEG or SVG", ct)
	}
}

func looksLikeSVG(data []byte) bool {
	head := strings.ToLower(string(data[:min(len(data), 1024)]))
	return strings.Contains(head, "<svg")
}

func decodeSVG(data []byte) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil { // oksvg can panic on malformed input
			img, err = nil, errors.New("unreadable SVG")
		}
	}()
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, fmt.Errorf("unreadable SVG: %w", err)
	}
	vb := icon.ViewBox
	if vb.W <= 0 || vb.H <= 0 {
		return nil, errors.New("SVG has no size (add a viewBox)")
	}
	w, h := SVGSide, SVGSide
	if vb.W > vb.H {
		h = max(1, int(float64(SVGSide)*vb.H/vb.W))
	} else {
		w = max(1, int(float64(SVGSide)*vb.W/vb.H))
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, out, out.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1)
	return out, nil
}

// EncodePNG re-encodes a logo as PNG, which also strips any metadata.
func EncodePNG(img image.Image) ([]byte, error) {
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
