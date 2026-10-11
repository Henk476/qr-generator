package render

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// Format is an output image format.
type Format string

const (
	PNG  Format = "png"
	JPEG Format = "jpeg"
	SVG  Format = "svg"
	PDF  Format = "pdf"
	EPS  Format = "eps"
)

// Size limits for raster output, in pixels.
const (
	MinSize = 128
	MaxSize = 4096
)

// Print size limits for PDF and EPS, in millimetres.
const (
	MinPrintMM = 10
	MaxPrintMM = 1000
)

// Page sizes for PDF output.
var Pages = []Option{
	{"fit", "Fit to code"},
	{"a4", "A4"},
	{"letter", "US Letter"},
}

// Formats lists the supported formats.
func Formats() []Format { return []Format{PNG, JPEG, SVG, PDF, EPS} }

// Vector reports whether the format is resolution-independent.
func (f Format) Vector() bool { return f == SVG || f == PDF || f == EPS }

// Ext is the file extension, without the dot.
func (f Format) Ext() string {
	if f == JPEG {
		return "jpg"
	}
	return string(f)
}

// ContentType is the MIME type.
func (f Format) ContentType() string {
	return map[Format]string{
		PNG: "image/png", JPEG: "image/jpeg", SVG: "image/svg+xml",
		PDF: "application/pdf", EPS: "application/postscript",
	}[f]
}

// ParseFormat parses a format name such as "png" or "jpg".
func ParseFormat(s string) (Format, error) {
	f := Format(strings.ToLower(strings.TrimPrefix(s, ".")))
	if f == "jpg" {
		f = JPEG
	}
	for _, known := range Formats() {
		if f == known {
			return f, nil
		}
	}
	return "", fmt.Errorf("unsupported format %q (supported: png, jpeg, svg, pdf, eps)", s)
}

// FormatFromPath infers the format from a file extension.
func FormatFromPath(path string) (Format, error) {
	ext := filepath.Ext(path)
	if ext == "" {
		return "", fmt.Errorf("cannot infer format from %q: no file extension", path)
	}
	return ParseFormat(ext)
}

// Options controls export.
type Options struct {
	Format Format
	// Size is the output width in pixels for PNG and JPEG, and the nominal
	// width for SVG.
	Size int
	// Quality is the JPEG quality, 1-100 (0 = 90).
	Quality int
	// Page is the PDF page: "fit" (default), "a4" or "letter".
	Page string
	// PrintMM is the printed width of the image in PDF and EPS (0 = 50 mm).
	PrintMM float64
}

func (o Options) withDefaults() Options {
	if o.Quality == 0 {
		o.Quality = 90
	}
	if o.Page == "" {
		o.Page = "fit"
	}
	if o.PrintMM == 0 {
		o.PrintMM = 50
	}
	return o
}

// Validate checks the options for o.Format.
func (o Options) Validate() error {
	o = o.withDefaults()
	switch o.Format {
	case PNG, JPEG, SVG:
		if o.Size < MinSize || o.Size > MaxSize {
			return fmt.Errorf("size must be %d-%d px, got %d", MinSize, MaxSize, o.Size)
		}
		if o.Quality < 1 || o.Quality > 100 {
			return fmt.Errorf("JPEG quality must be 1-100, got %d", o.Quality)
		}
	case PDF, EPS:
		if o.PrintMM < MinPrintMM || o.PrintMM > MaxPrintMM {
			return fmt.Errorf("print size must be %d-%d mm, got %g", MinPrintMM, MaxPrintMM, o.PrintMM)
		}
		if _, _, ok := pageSize(o.Page); !ok {
			return fmt.Errorf("unknown page %q (valid: fit, a4, letter)", o.Page)
		}
	default:
		return fmt.Errorf("unsupported format %q", o.Format)
	}
	return nil
}

// Export writes the scene in o.Format.
func Export(w io.Writer, sc *Scene, o Options) error {
	if err := o.Validate(); err != nil {
		return err
	}
	o = o.withDefaults()
	switch o.Format {
	case PNG:
		return writePNG(w, sc, o.Size)
	case JPEG:
		return writeJPEG(w, sc, o.Size, o.Quality)
	case SVG:
		return WriteSVG(w, sc, o.Size)
	case PDF:
		return writePDF(w, sc, o.Page, o.PrintMM)
	case EPS:
		return writeEPS(w, sc, o.PrintMM)
	}
	return nil
}

const ptPerMM = 72 / 25.4

// pageSize returns the page in points; 0x0 means "fit to the drawing".
func pageSize(page string) (w, h float64, ok bool) {
	switch page {
	case "", "fit":
		return 0, 0, true
	case "a4":
		return 595.276, 841.89, true
	case "letter":
		return 612, 792, true
	}
	return 0, 0, false
}

func fmtNum(v float64) string {
	v = math.Round(v*1000) / 1000
	if v == 0 {
		return "0" // avoid "-0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}
