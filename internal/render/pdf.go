package render

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
)

// pdfWriter assembles a minimal single-page PDF 1.4 document.
type pdfWriter struct {
	objects [][]byte // object bodies; object n is objects[n-1]
}

func (p *pdfWriter) add(body []byte) int {
	p.objects = append(p.objects, body)
	return len(p.objects)
}

func (p *pdfWriter) reserve() int { return p.add(nil) }

func (p *pdfWriter) set(n int, body []byte) { p.objects[n-1] = body }

func deflate(b []byte) []byte {
	var buf bytes.Buffer
	z := zlib.NewWriter(&buf)
	z.Write(b)
	z.Close()
	return buf.Bytes()
}

func pdfStream(dict string, data []byte) []byte {
	return []byte(fmt.Sprintf("<< %s /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", dict, len(data), data))
}

// writePDF writes the scene printed printMM wide, centered on page.
func writePDF(w io.Writer, sc *Scene, page string, printMM float64) error {
	drawW := printMM * ptPerMM
	scale := drawW / sc.Width
	drawH := sc.Height * scale
	pw, ph, _ := pageSize(page)
	if pw == 0 {
		pw, ph = drawW, drawH
	}
	if drawW > pw+0.01 || drawH > ph+0.01 {
		return fmt.Errorf("a %g mm wide code does not fit on a %s page", printMM, page)
	}

	var doc pdfWriter
	catalog, pages, pageObj := doc.reserve(), doc.reserve(), doc.reserve()

	var c strings.Builder // content stream
	var extG, shadings, xobjects []string
	alphaGS := map[uint8]string{}
	setFill := func(col color.NRGBA) {
		fmt.Fprintf(&c, "%s %s %s rg\n", fmtNum(float64(col.R)/255), fmtNum(float64(col.G)/255), fmtNum(float64(col.B)/255))
		name, ok := alphaGS[col.A]
		if !ok {
			name = fmt.Sprintf("GS%d", len(alphaGS))
			alphaGS[col.A] = name
			a := fmtNum(float64(col.A) / 255)
			extG = append(extG, fmt.Sprintf("/%s << /ca %s /CA %s >>", name, a, a))
		}
		fmt.Fprintf(&c, "/%s gs\n", name)
	}
	pathOps := func(path Path) {
		for _, s := range path.Segs {
			switch s.Op {
			case MoveTo:
				fmt.Fprintf(&c, "%s %s m\n", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y))
			case LineTo:
				fmt.Fprintf(&c, "%s %s l\n", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y))
			case CubicTo:
				fmt.Fprintf(&c, "%s %s %s %s %s %s c\n", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y),
					fmtNum(s.Pts[1].X), fmtNum(s.Pts[1].Y), fmtNum(s.Pts[2].X), fmtNum(s.Pts[2].Y))
			case Close:
				c.WriteString("h\n")
			}
		}
	}

	// Map scene units (y down) to page points (y up), centered.
	fmt.Fprintf(&c, "%s 0 0 %s %s %s cm\n", fmtNum(scale), fmtNum(-scale), fmtNum((pw-drawW)/2), fmtNum((ph+drawH)/2))
	if sc.Background.A > 0 {
		setFill(sc.Background)
		fmt.Fprintf(&c, "0 0 %s %s re f\n", fmtNum(sc.Width), fmtNum(sc.Height))
	}
	shadingNames := map[Paint]string{}
	for _, it := range sc.Items {
		switch {
		case it.Image != nil:
			name := fmt.Sprintf("Im%d", len(xobjects))
			xobjects = append(xobjects, fmt.Sprintf("/%s %d 0 R", name, pdfImage(&doc, it.Image)))
			c.WriteString("q\n")
			if !it.Path.Empty() {
				pathOps(it.Path)
				c.WriteString("W n\n")
			}
			r := it.ImageRect
			fmt.Fprintf(&c, "%s 0 0 %s %s %s cm /%s Do\nQ\n", fmtNum(r.W), fmtNum(-r.H), fmtNum(r.X), fmtNum(r.Y+r.H), name)
		case it.Path.Empty():
		case it.Paint.Kind == Solid:
			setFill(it.Paint.Color)
			pathOps(it.Path)
			c.WriteString("f\n")
		default:
			name, ok := shadingNames[it.Paint]
			if !ok {
				name = fmt.Sprintf("Sh%d", len(shadingNames))
				shadingNames[it.Paint] = name
				shadings = append(shadings, fmt.Sprintf("/%s %d 0 R", name, doc.add([]byte(shadingDict(it.Paint)))))
			}
			c.WriteString("q\n")
			pathOps(it.Path)
			fmt.Fprintf(&c, "W n /%s sh\nQ\n", name)
		}
	}

	content := doc.add(pdfStream("", deflate([]byte(c.String()))))
	res := "/ProcSet [/PDF /ImageC]"
	if len(extG) > 0 {
		res += " /ExtGState << " + strings.Join(extG, " ") + " >>"
	}
	if len(shadings) > 0 {
		res += " /Shading << " + strings.Join(shadings, " ") + " >>"
	}
	if len(xobjects) > 0 {
		res += " /XObject << " + strings.Join(xobjects, " ") + " >>"
	}
	doc.set(catalog, []byte(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pages)))
	doc.set(pages, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", pageObj)))
	doc.set(pageObj, []byte(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Resources << %s >> /Contents %d 0 R >>",
		pages, fmtNum(pw), fmtNum(ph), res, content)))
	info := doc.add([]byte("<< /Producer (Canute's QR Generator) >>"))

	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(doc.objects))
	for i, body := range doc.objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(doc.objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(doc.objects)+1, catalog, info, xref)
	_, err := w.Write(out.Bytes())
	return err
}

// shadingDict is an axial or radial shading for p (PDF and PostScript
// share the syntax).
func shadingDict(p Paint) string {
	rgb := func(c color.NRGBA) string {
		return fmt.Sprintf("[%s %s %s]", fmtNum(float64(c.R)/255), fmtNum(float64(c.G)/255), fmtNum(float64(c.B)/255))
	}
	fn := fmt.Sprintf("<< /FunctionType 2 /Domain [0 1] /C0 %s /C1 %s /N 1 >>", rgb(p.Color), rgb(p.Color2))
	if p.Kind == Linear {
		return fmt.Sprintf("<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [%s %s %s %s] /Function %s /Extend [true true] >>",
			fmtNum(p.X1), fmtNum(p.Y1), fmtNum(p.X2), fmtNum(p.Y2), fn)
	}
	return fmt.Sprintf("<< /ShadingType 3 /ColorSpace /DeviceRGB /Coords [%s %s 0 %s %s %s] /Function %s /Extend [true true] >>",
		fmtNum(p.X1), fmtNum(p.Y1), fmtNum(p.X1), fmtNum(p.Y1), fmtNum(p.R), fn)
}

// pdfImage adds img as an RGB image XObject with an alpha soft mask and
// returns its object number.
func pdfImage(doc *pdfWriter, img image.Image) int {
	img = limitImage(img, maxEmbeddedImage)
	b := img.Bounds()
	rgb := make([]byte, 0, b.Dx()*b.Dy()*3)
	alpha := make([]byte, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			rgb = append(rgb, c.R, c.G, c.B)
			alpha = append(alpha, c.A)
		}
	}
	dims := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /BitsPerComponent 8", b.Dx(), b.Dy())
	smask := doc.add(pdfStream(dims+" /ColorSpace /DeviceGray", deflate(alpha)))
	return doc.add(pdfStream(fmt.Sprintf("%s /ColorSpace /DeviceRGB /SMask %d 0 R", dims, smask), deflate(rgb)))
}
