package logo

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestDecodePNGAndJPEG(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 30, 20))
	src.Set(1, 1, color.NRGBA{255, 0, 0, 255})
	var p, j bytes.Buffer
	png.Encode(&p, src)
	jpeg.Encode(&j, src, nil)
	for name, data := range map[string][]byte{"png": p.Bytes(), "jpeg": j.Bytes()} {
		img, err := Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := img.Bounds(); b.Dx() != 30 || b.Dy() != 20 {
			t.Errorf("%s bounds %v", name, b)
		}
	}
	out, err := EncodePNG(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Error(err)
	}
}

func TestDecodeSVG(t *testing.T) {
	svg := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100">
		<script>alert(1)</script><rect width="200" height="100" fill="#0b5d7a"/></svg>`
	img, err := Decode([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != SVGSide || b.Dy() != SVGSide/2 {
		t.Errorf("bounds %v", b)
	}
	if c := color.NRGBAModel.Convert(img.At(500, 250)).(color.NRGBA); c.B < 100 || c.A != 255 {
		t.Errorf("center pixel %v, want the fill color", c)
	}
}

func TestDecodeRejects(t *testing.T) {
	tests := map[string][]byte{
		"text":      []byte("hello, not an image"),
		"gif":       []byte("GIF89a......"),
		"too large": bytes.Repeat([]byte{0}, MaxBytes+1),
		"no size":   []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"bad svg":   []byte(`<svg viewBox="0 0 10 10"><path d="M 0 0 L"`),
	}
	for name, data := range tests {
		if _, err := Decode(data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// A huge declared size is refused before decoding the pixels.
	var buf bytes.Buffer
	png.Encode(&buf, image.NewGray(image.Rect(0, 0, MaxSide+1, 1)))
	if _, err := Decode(buf.Bytes()); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("oversized image: %v", err)
	}
}
