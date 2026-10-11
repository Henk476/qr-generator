package render

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"strings"
)

// maxEmbeddedImage caps logo resolution inside vector files.
const maxEmbeddedImage = 1024

// WriteSVG writes the scene as a standalone SVG document. Text is already
// outlined and logos are embedded, so the file needs nothing else.
func WriteSVG(w io.Writer, sc *Scene, size int) error {
	bw := bufio.NewWriter(w)
	height := int(float64(size) * sc.Height / sc.Width)
	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %s %s">`+"\n",
		size, height, fmtNum(sc.Width), fmtNum(sc.Height))

	// Gradient and clip definitions.
	var defs strings.Builder
	gradIDs := map[Paint]string{}
	for _, it := range sc.Items {
		if it.Image == nil && it.Paint.Kind != Solid && gradIDs[it.Paint] == "" {
			id := fmt.Sprintf("cqr-g%d", len(gradIDs))
			gradIDs[it.Paint] = id
			writeSVGGradient(&defs, id, it.Paint)
		}
	}
	clipIDs := map[int]string{}
	for i, it := range sc.Items {
		if it.Image != nil && !it.Path.Empty() {
			id := fmt.Sprintf("cqr-c%d", len(clipIDs))
			clipIDs[i] = id
			fmt.Fprintf(&defs, `<clipPath id="%s"><path d="%s"/></clipPath>`+"\n", id, svgPathData(it.Path))
		}
	}
	if defs.Len() > 0 {
		bw.WriteString("<defs>\n" + defs.String() + "</defs>\n")
	}

	if sc.Background.A > 0 {
		fmt.Fprintf(bw, `<rect width="%s" height="%s"%s/>`+"\n", fmtNum(sc.Width), fmtNum(sc.Height), svgFill(sc.Background))
	}
	for i, it := range sc.Items {
		if it.Image != nil {
			if err := writeSVGImage(bw, it, clipIDs[i]); err != nil {
				return err
			}
			continue
		}
		if it.Path.Empty() {
			continue
		}
		fill := svgFill(it.Paint.Color)
		if id := gradIDs[it.Paint]; id != "" {
			fill = fmt.Sprintf(` fill="url(#%s)"`, id)
		}
		crisp := ""
		if it.Snap {
			crisp = ` shape-rendering="crispEdges"`
		}
		fmt.Fprintf(bw, `<path d="%s"%s%s/>`+"\n", svgPathData(it.Path), fill, crisp)
	}
	bw.WriteString("</svg>\n")
	return bw.Flush()
}

func writeSVGGradient(b *strings.Builder, id string, p Paint) {
	stops := fmt.Sprintf(`<stop offset="0" stop-color="%s"%s/><stop offset="1" stop-color="%s"%s/>`,
		hexRGB(p.Color), stopOpacity(p.Color), hexRGB(p.Color2), stopOpacity(p.Color2))
	if p.Kind == Linear {
		fmt.Fprintf(b, `<linearGradient id="%s" gradientUnits="userSpaceOnUse" x1="%s" y1="%s" x2="%s" y2="%s">%s</linearGradient>`+"\n",
			id, fmtNum(p.X1), fmtNum(p.Y1), fmtNum(p.X2), fmtNum(p.Y2), stops)
	} else {
		fmt.Fprintf(b, `<radialGradient id="%s" gradientUnits="userSpaceOnUse" cx="%s" cy="%s" r="%s">%s</radialGradient>`+"\n",
			id, fmtNum(p.X1), fmtNum(p.Y1), fmtNum(p.R), stops)
	}
}

func stopOpacity(c color.NRGBA) string {
	if c.A == 255 {
		return ""
	}
	return fmt.Sprintf(` stop-opacity="%s"`, fmtNum(float64(c.A)/255))
}

func writeSVGImage(w io.Writer, it Item, clipID string) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, limitImage(it.Image, maxEmbeddedImage)); err != nil {
		return err
	}
	clip := ""
	if clipID != "" {
		clip = fmt.Sprintf(` clip-path="url(#%s)"`, clipID)
	}
	r := it.ImageRect
	_, err := fmt.Fprintf(w, `<image x="%s" y="%s" width="%s" height="%s" preserveAspectRatio="none"%s href="data:image/png;base64,%s"/>`+"\n",
		fmtNum(r.X), fmtNum(r.Y), fmtNum(r.W), fmtNum(r.H), clip, base64.StdEncoding.EncodeToString(buf.Bytes()))
	return err
}

func svgPathData(p Path) string {
	var b strings.Builder
	for _, s := range p.Segs {
		switch s.Op {
		case MoveTo:
			fmt.Fprintf(&b, "M%s %s", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y))
		case LineTo:
			fmt.Fprintf(&b, "L%s %s", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y))
		case CubicTo:
			fmt.Fprintf(&b, "C%s %s %s %s %s %s", fmtNum(s.Pts[0].X), fmtNum(s.Pts[0].Y),
				fmtNum(s.Pts[1].X), fmtNum(s.Pts[1].Y), fmtNum(s.Pts[2].X), fmtNum(s.Pts[2].Y))
		case Close:
			b.WriteString("Z")
		}
	}
	return b.String()
}

func svgFill(c color.NRGBA) string {
	s := fmt.Sprintf(` fill="%s"`, hexRGB(c))
	if c.A < 255 {
		s += fmt.Sprintf(` fill-opacity="%s"`, fmtNum(float64(c.A)/255))
	}
	return s
}
