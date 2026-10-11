// Package barcode encodes non-QR symbologies (2D and linear barcodes) into
// a module grid the renderer can draw.
package barcode

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"unicode/utf8"

	bc "github.com/boombuler/barcode"
	"github.com/boombuler/barcode/aztec"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/ean"
	"github.com/boombuler/barcode/pdf417"
)

// Symbology identifies a barcode format.
type Symbology string

const (
	DataMatrix Symbology = "datamatrix"
	PDF417     Symbology = "pdf417"
	Aztec      Symbology = "aztec"
	Code128    Symbology = "code128"
	EAN13      Symbology = "ean13"
	UPCA       Symbology = "upca"
)

// Info describes a symbology.
type Info struct {
	Symbology Symbology
	Label     string
	Linear    bool // a 1D barcode: bars, not a module grid
	Hint      string
}

var infos = []Info{
	{DataMatrix, "Data Matrix", false, "text or numbers (ASCII)"},
	{PDF417, "PDF417", false, "text (ASCII), common on IDs and boarding passes"},
	{Aztec, "Aztec", false, "text (ASCII), common on transport tickets"},
	{Code128, "Code 128", true, "letters, digits and symbols (ASCII), up to 80 characters"},
	{EAN13, "EAN-13", true, "12 digits (the check digit is added) or 13 digits"},
	{UPCA, "UPC-A", true, "11 digits (the check digit is added) or 12 digits"},
}

// Symbologies lists the supported barcode formats.
func Symbologies() []Info { return slices.Clone(infos) }

// Lookup returns the description of a symbology.
func Lookup(s Symbology) (Info, bool) {
	for _, in := range infos {
		if in.Symbology == s {
			return in, true
		}
	}
	return Info{}, false
}

// MaxText limits free-text symbologies, keeping symbols a printable size.
const MaxText = 500

// Normalize validates data for sym and returns the exact text the barcode
// will hold (for EAN-13 and UPC-A, including the check digit).
func Normalize(sym Symbology, data string) (string, error) {
	info, ok := Lookup(sym)
	if !ok {
		names := make([]string, len(infos))
		for i, in := range infos {
			names[i] = string(in.Symbology)
		}
		return "", fmt.Errorf("unknown symbology %q (valid: %s)", sym, strings.Join(names, ", "))
	}
	switch sym {
	case EAN13:
		return checkDigits(data, 12, info.Label)
	case UPCA:
		return checkDigits(data, 11, info.Label)
	}
	for _, r := range data {
		if r < 32 || r > 126 {
			return "", fmt.Errorf("%s supports plain ASCII text only, not %q", info.Label, r)
		}
	}
	max := MaxText
	if sym == Code128 {
		max = 80
	}
	if n := utf8.RuneCountInString(data); n > max {
		return "", fmt.Errorf("%s holds at most %d characters here, got %d", info.Label, max, n)
	}
	return data, nil
}

// checkDigits accepts n digits (appending the GS1 check digit) or n+1
// digits (verifying it).
func checkDigits(data string, n int, label string) (string, error) {
	data = strings.NewReplacer(" ", "", "-", "").Replace(data)
	for _, r := range data {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("%s takes digits only", label)
		}
	}
	switch len(data) {
	case n:
		return data + string(rune('0'+gs1Check(data))), nil
	case n + 1:
		if int(data[n]-'0') != gs1Check(data[:n]) {
			return "", fmt.Errorf("%s check digit is wrong: expected %d", label, gs1Check(data[:n]))
		}
		return data, nil
	}
	return "", fmt.Errorf("%s needs %d or %d digits, got %d", label, n, n+1, len(data))
}

// gs1Check computes the GS1 mod-10 check digit.
func gs1Check(digits string) int {
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if (len(digits)-1-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return (10 - sum%10) % 10
}

// Grid is an encoded barcode: W x H modules. Linear barcodes have H = 1;
// the renderer gives their bars height.
type Grid struct {
	Symbology Symbology
	Linear    bool
	W, H      int
	dark      []bool
}

// Dark reports whether module (x, y) is dark.
func (g *Grid) Dark(x, y int) bool {
	if x < 0 || y < 0 || x >= g.W || y >= g.H {
		return false
	}
	return g.dark[y*g.W+x]
}

// Encode builds the barcode for a payload already checked by Normalize.
func Encode(sym Symbology, payload string) (*Grid, error) {
	info, ok := Lookup(sym)
	if !ok {
		return nil, fmt.Errorf("unknown symbology %q", sym)
	}
	var b bc.Barcode
	var err error
	switch sym {
	case DataMatrix:
		b, err = datamatrix.Encode(payload)
	case PDF417:
		b, err = pdf417.Encode(payload, 2)
	case Aztec:
		b, err = aztec.Encode([]byte(payload), 33, 0)
	case Code128:
		b, err = code128.Encode(payload)
	case EAN13:
		b, err = ean.Encode(payload)
	case UPCA:
		b, err = ean.Encode("0" + payload) // UPC-A is EAN-13 with a leading zero
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", info.Label, err)
	}
	if b == nil {
		return nil, errors.New("barcode encoder returned nothing")
	}
	r := b.Bounds()
	g := &Grid{Symbology: sym, Linear: info.Linear, W: r.Dx(), H: r.Dy(), dark: make([]bool, r.Dx()*r.Dy())}
	for y := 0; y < g.H; y++ {
		for x := 0; x < g.W; x++ {
			gray := color.GrayModel.Convert(b.At(r.Min.X+x, r.Min.Y+y)).(color.Gray)
			g.dark[y*g.W+x] = gray.Y < 128
		}
	}
	return g, nil
}
