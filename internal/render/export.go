package render

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/vector"
)

// Format is an output image format.
type Format string

const (
	PNG Format = "png"
	SVG Format = "svg"
)

// Size limits for raster output, in pixels.
const (
	MinSize = 128
	MaxSize = 4096
)

// Formats lists the supported formats.
func Formats() []Format { return []Format{PNG, SVG} }

// ParseFormat parses a format name such as "png".
func ParseFormat(s string) (Format, error) {
	f := Format(strings.ToLower(strings.TrimPrefix(s, ".")))
	for _, known := range Formats() {
		if f == known {
			return f, nil
		}
	}
	return "", fmt.Errorf("unsupported format %q (supported: png, svg)", s)
}

// FormatFromPath infers the format from a file extension.
func FormatFromPath(path string) (Format, error) {
	ext := filepath.Ext(path)
	if ext == "" {
		return "", fmt.Errorf("cannot infer format from %q: no file extension", path)
	}
	return ParseFormat(ext)
}

// Export writes the scene in the given format. size is the output width and
// height in pixels (for SVG it sets the nominal width/height attributes).
func Export(w io.Writer, sc *Scene, f Format, size int) error {
	if size < MinSize || size > MaxSize {
		return fmt.Errorf("size must be %d-%d px, got %d", MinSize, MaxSize, size)
	}
	switch f {
	case PNG:
		return png.Encode(w, Rasterize(sc, size))
	case SVG:
		return WriteSVG(w, sc, size)
	}
	return fmt.Errorf("unsupported format %q", f)
}

// Rasterize draws the scene into a size x size image. Rectangle edges are
// snapped to whole pixels so modules stay crisp at any size.
func Rasterize(sc *Scene, size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	if sc.Background.A > 0 {
		draw.Draw(img, img.Bounds(), image.NewUniform(sc.Background), image.Point{}, draw.Src)
	}
	scale := float64(size) / sc.Size
	snap := func(v float64) float32 { return float32(math.Round(v * scale)) }
	for _, l := range sc.Layers {
		z := vector.NewRasterizer(size, size)
		for _, r := range l.Rects {
			x0, y0, x1, y1 := snap(r.X), snap(r.Y), snap(r.X+r.W), snap(r.Y+r.H)
			z.MoveTo(x0, y0)
			z.LineTo(x1, y0)
			z.LineTo(x1, y1)
			z.LineTo(x0, y1)
			z.ClosePath()
		}
		z.Draw(img, img.Bounds(), image.NewUniform(l.Fill), image.Point{})
	}
	return img
}

// WriteSVG writes the scene as a standalone SVG document.
func WriteSVG(w io.Writer, sc *Scene, size int) error {
	bw := bufio.NewWriter(w)
	n := fmtNum(sc.Size)
	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %s %s" shape-rendering="crispEdges">`+"\n", size, size, n, n)
	if sc.Background.A > 0 {
		fmt.Fprintf(bw, `<rect width="%s" height="%s"%s/>`+"\n", n, n, svgFill(sc.Background))
	}
	for _, l := range sc.Layers {
		if len(l.Rects) == 0 {
			continue
		}
		bw.WriteString(`<path d="`)
		for _, r := range l.Rects {
			fmt.Fprintf(bw, "M%s %sh%sv%sh-%sz", fmtNum(r.X), fmtNum(r.Y), fmtNum(r.W), fmtNum(r.H), fmtNum(r.W))
		}
		fmt.Fprintf(bw, `"%s/>`+"\n", svgFill(l.Fill))
	}
	bw.WriteString("</svg>\n")
	return bw.Flush()
}

func svgFill(c color.NRGBA) string {
	s := fmt.Sprintf(` fill="%s"`, hexRGB(c))
	if c.A < 255 {
		s += fmt.Sprintf(` fill-opacity="%s"`, fmtNum(float64(c.A)/255))
	}
	return s
}

func fmtNum(v float64) string {
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}
