package barcode

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	ok := []struct {
		sym      Symbology
		in, want string
	}{
		{EAN13, "590123412345", "5901234123457"},
		{EAN13, "590-1234-12345 7", "5901234123457"},
		{UPCA, "03600029145", "036000291452"},
		{UPCA, "036000291452", "036000291452"},
		{Code128, "ABC-123 x", "ABC-123 x"},
		{DataMatrix, "Hello, Canute!", "Hello, Canute!"},
	}
	for _, tt := range ok {
		got, err := Normalize(tt.sym, tt.in)
		if err != nil || got != tt.want {
			t.Errorf("Normalize(%s, %q) = %q, %v; want %q", tt.sym, tt.in, got, err, tt.want)
		}
	}
	bad := []struct {
		sym     Symbology
		in, err string
	}{
		{EAN13, "5901234123458", "check digit"},
		{EAN13, "12345", "12 or 13 digits"},
		{EAN13, "59012341234a", "digits only"},
		{UPCA, "0360002914", "11 or 12"},
		{Code128, "héllo", "ASCII"},
		{Code128, strings.Repeat("x", 81), "at most 80"},
		{Aztec, strings.Repeat("x", MaxText+1), "at most"},
		{"ean8", "1234567", "unknown symbology"},
	}
	for _, tt := range bad {
		if _, err := Normalize(tt.sym, tt.in); err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("Normalize(%s, %q) err = %v, want %q", tt.sym, tt.in, err, tt.err)
		}
	}
}

func TestEncodeShapes(t *testing.T) {
	for _, in := range Symbologies() {
		payload := "HELLO123"
		switch in.Symbology {
		case EAN13:
			payload = "5901234123457"
		case UPCA:
			payload = "036000291452"
		}
		g, err := Encode(in.Symbology, payload)
		if err != nil {
			t.Fatalf("%s: %v", in.Symbology, err)
		}
		if in.Linear != (g.H == 1) {
			t.Errorf("%s: linear=%v but height %d", in.Symbology, in.Linear, g.H)
		}
		dark := 0
		for y := 0; y < g.H; y++ {
			for x := 0; x < g.W; x++ {
				if g.Dark(x, y) {
					dark++
				}
			}
		}
		if dark == 0 || dark == g.W*g.H {
			t.Errorf("%s: %d of %d modules dark", in.Symbology, dark, g.W*g.H)
		}
	}
}
