package render

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"image/color"
	"io"
	"math"
)

// maxEPSImage caps logo resolution in EPS, where images are hex-encoded.
const maxEPSImage = 512

// writeEPS writes the scene as Encapsulated PostScript (Level 3, for
// gradients) printed printMM wide. PostScript has no transparency, so
// translucent colors and logo pixels are flattened onto the code background
// (white if that is transparent too).
func writeEPS(w io.Writer, sc *Scene, printMM float64) error {
	drawW := printMM * ptPerMM
	scale := drawW / sc.Width
	drawH := sc.Height * scale
	base := white
	if sc.CodeBackground.A == 255 {
		base = sc.CodeBackground
	}

	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "%%!PS-Adobe-3.0 EPSF-3.0\n%%%%Creator: Canute's QR Generator\n")
	fmt.Fprintf(bw, "%%%%BoundingBox: 0 0 %d %d\n%%%%HiResBoundingBox: 0 0 %s %s\n", int(math.Ceil(drawW)), int(math.Ceil(drawH)), fmtNum(drawW), fmtNum(drawH))
	bw.WriteString("%%LanguageLevel: 3\n%%Pages: 1\n%%EndComments\n")
	bw.WriteString("save\n/m {moveto} bind def /l {lineto} bind def /c {curveto} bind def /h {closepath} bind def\n")
	// Scene units, y down.
	fmt.Fprintf(bw, "0 %s translate %s %s scale\n", fmtNum(drawH), fmtNum(scale), fmtNum(-scale))

	setColor := func(col color.NRGBA) {
		col = flatten(col, base)
		fmt.Fprintf(bw, "%s %s %s setrgbcolor\n", fmtNum(float64(col.R)/255), fmtNum(float64(col.G)/255), fmtNum(float64(col.B)/255))
	}
	pathOps := func(p Path) {
		bw.WriteString("newpath\n")
		for _, s := range p.Segs {
			switch s.Op {
			case MoveTo:
				fmt.Fprintf(bw, "%s %s m\n", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y))
			case LineTo:
				fmt.Fprintf(bw, "%s %s l\n", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y))
			case CubicTo:
				fmt.Fprintf(bw, "%s %s %s %s %s %s c\n", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y),
					fmtNum(s.Pts[1].X), fmtNum(s.Pts[1].Y), fmtNum(s.Pts[2].X), fmtNum(s.Pts[2].Y))
			case Close:
				bw.WriteString("h\n")
			}
		}
	}

	if sc.Background.A > 0 {
		setColor(sc.Background)
		fmt.Fprintf(bw, "0 0 %s %s rectfill\n", fmtNum(sc.Width), fmtNum(sc.Height))
	}
	for _, it := range sc.Items {
		switch {
		case it.Image != nil:
			bw.WriteString("gsave\n")
			if !it.Path.Empty() {
				pathOps(it.Path)
				bw.WriteString("clip\n")
			}
			img := limitImage(it.Image, maxEPSImage)
			b := img.Bounds()
			r := it.ImageRect
			fmt.Fprintf(bw, "%s %s translate %s %s scale\n", fmtNum(r.X), fmtNum(r.Y), fmtNum(r.W), fmtNum(r.H))
			fmt.Fprintf(bw, "/picstr %d string def\n%d %d 8 [%d 0 0 %d 0 0] {currentfile picstr readhexstring pop} false 3 colorimage\n",
				b.Dx()*3, b.Dx(), b.Dy(), b.Dx(), b.Dy())
			row := make([]byte, 0, b.Dx()*3)
			for y := b.Min.Y; y < b.Max.Y; y++ {
				row = row[:0]
				for x := b.Min.X; x < b.Max.X; x++ {
					c := flatten(color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA), base)
					row = append(row, c.R, c.G, c.B)
				}
				bw.WriteString(hex.EncodeToString(row) + "\n")
			}
			bw.WriteString("grestore\n")
		case it.Path.Empty():
		case it.Paint.Kind == Solid:
			setColor(it.Paint.Color)
			pathOps(it.Path)
			bw.WriteString("fill\n")
		default:
			p := it.Paint
			p.Color, p.Color2 = flatten(p.Color, base), flatten(p.Color2, base)
			bw.WriteString("gsave\n")
			pathOps(it.Path)
			fmt.Fprintf(bw, "clip\n%s shfill\ngrestore\n", shadingDict(p))
		}
	}
	bw.WriteString("restore\n%%EOF\n")
	return bw.Flush()
}
