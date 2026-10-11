package render

import (
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/vector"
)

// Rasterize draws the scene at width px (height follows the aspect
// ratio). Snap items are aligned to whole pixels so modules stay crisp.
func Rasterize(sc *Scene, width int) *image.RGBA {
	scale := float64(width) / sc.Width
	height := max(1, int(math.Round(sc.Height*scale)))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if sc.Background.A > 0 {
		draw.Draw(img, img.Bounds(), image.NewUniform(sc.Background), image.Point{}, draw.Src)
	}
	for _, it := range sc.Items {
		if it.Image != nil {
			drawImageItem(img, it, scale)
			continue
		}
		r := pixelBounds(it.Path, scale, img.Bounds())
		if r.Empty() {
			continue
		}
		// Rasterize only the item's bounding box: a full-canvas rasterizer
		// per item dominated the render time.
		z := vector.NewRasterizer(r.Dx(), r.Dy())
		addPath(z, it.Path, scale, it.Snap, r.Min)
		if it.Paint.Kind == Solid {
			z.Draw(img, r, image.NewUniform(it.Paint.Color), image.Point{})
			continue
		}
		mask := image.NewAlpha(image.Rect(0, 0, r.Dx(), r.Dy()))
		z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
		fillGradient(img, r, mask, it.Paint, scale)
	}
	return img
}

// pixelBounds is the pixel rectangle covering p, clipped to canvas.
func pixelBounds(p Path, scale float64, canvas image.Rectangle) image.Rectangle {
	if p.Empty() {
		return image.Rectangle{}
	}
	b := p.Bounds()
	return image.Rect(
		int(math.Floor(b.X*scale))-1, int(math.Floor(b.Y*scale))-1,
		int(math.Ceil((b.X+b.W)*scale))+1, int(math.Ceil((b.Y+b.H)*scale))+1,
	).Intersect(canvas)
}

// gradientSteps is the resolution of the gradient color lookup table;
// finer than 8-bit color, so banding is not introduced.
const gradientSteps = 1024

// fillGradient blends paint p into dst through mask, which covers rect r.
func fillGradient(dst *image.RGBA, r image.Rectangle, mask *image.Alpha, p Paint, scale float64) {
	var lut [gradientSteps + 1]color.NRGBA
	for i := range lut {
		lut[i] = lerp(p.Color, p.Color2, float64(i)/gradientSteps)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		mrow := mask.Pix[(y-r.Min.Y)*mask.Stride:]
		sy := (float64(y) + .5) / scale
		for x := r.Min.X; x < r.Max.X; x++ {
			m := uint32(mrow[x-r.Min.X])
			if m == 0 {
				continue
			}
			c := lut[int(p.T((float64(x)+.5)/scale, sy)*gradientSteps)]
			a := uint32(c.A) * m / 255 // coverage-weighted alpha
			i := dst.PixOffset(x, y)
			px := dst.Pix[i : i+4 : i+4]
			inv := 255 - a
			px[0] = uint8((uint32(c.R)*a + uint32(px[0])*inv) / 255)
			px[1] = uint8((uint32(c.G)*a + uint32(px[1])*inv) / 255)
			px[2] = uint8((uint32(c.B)*a + uint32(px[2])*inv) / 255)
			px[3] = uint8(a + uint32(px[3])*inv/255)
		}
	}
}

// addPath feeds p to z, scaled to pixels and shifted so origin lands at
// the rasterizer's (0, 0).
func addPath(z *vector.Rasterizer, p Path, scale float64, snap bool, origin image.Point) {
	fx := func(v float64) float32 {
		if snap {
			return float32(math.Round(v*scale) - float64(origin.X))
		}
		return float32(v*scale - float64(origin.X))
	}
	fy := func(v float64) float32 {
		if snap {
			return float32(math.Round(v*scale) - float64(origin.Y))
		}
		return float32(v*scale - float64(origin.Y))
	}
	for _, s := range p.Segs {
		switch s.Op {
		case MoveTo:
			z.MoveTo(fx(s.Pts[0].X), fy(s.Pts[0].Y))
		case LineTo:
			z.LineTo(fx(s.Pts[0].X), fy(s.Pts[0].Y))
		case CubicTo:
			z.CubeTo(fx(s.Pts[0].X), fy(s.Pts[0].Y), fx(s.Pts[1].X), fy(s.Pts[1].Y), fx(s.Pts[2].X), fy(s.Pts[2].Y))
		case Close:
			z.ClosePath()
		}
	}
}

func drawImageItem(dst *image.RGBA, it Item, scale float64) {
	r := image.Rect(
		int(math.Round(it.ImageRect.X*scale)), int(math.Round(it.ImageRect.Y*scale)),
		int(math.Round((it.ImageRect.X+it.ImageRect.W)*scale)), int(math.Round((it.ImageRect.Y+it.ImageRect.H)*scale)),
	)
	if r.Empty() {
		return
	}
	scaled := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), it.Image, it.Image.Bounds(), draw.Src, nil)
	if it.Path.Empty() {
		draw.Draw(dst, r, scaled, image.Point{}, draw.Over)
		return
	}
	// Clip mask over the image rectangle only.
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	addPath(z, it.Path, scale, false, r.Min)
	mask := image.NewAlpha(image.Rect(0, 0, r.Dx(), r.Dy()))
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(dst, r, scaled, image.Point{}, mask, image.Point{}, draw.Over)
}

func writePNG(w io.Writer, sc *Scene, size int) error {
	return png.Encode(w, Rasterize(sc, size))
}

// writeJPEG flattens onto white, since JPEG has no transparency.
func writeJPEG(w io.Writer, sc *Scene, size, quality int) error {
	img := Rasterize(sc, size)
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Over)
	return jpeg.Encode(w, out, &jpeg.Options{Quality: quality})
}

// flatten composites c over an opaque base.
func flatten(c, base color.NRGBA) color.NRGBA {
	a := float64(c.A) / 255
	mix := func(f, b uint8) uint8 { return uint8(math.Round(float64(f)*a + float64(b)*(1-a))) }
	return color.NRGBA{mix(c.R, base.R), mix(c.G, base.G), mix(c.B, base.B), 255}
}

// limitImage downscales img so neither side exceeds maxSide, for embedding
// in vector formats.
func limitImage(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxSide && b.Dy() <= maxSide {
		return img
	}
	f := float64(maxSide) / float64(max(b.Dx(), b.Dy()))
	out := image.NewNRGBA(image.Rect(0, 0, max(1, int(float64(b.Dx())*f)), max(1, int(float64(b.Dy())*f))))
	xdraw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Src, nil)
	return out
}
