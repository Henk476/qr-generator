package matrix

import (
	"fmt"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode/decoder"
)

func rawDecode(m [][]bool) (string, error) {
	bm, _ := gozxing.NewBitMatrix(len(m), len(m))
	for y := range m {
		for x := range m[y] {
			if m[y][x] {
				bm.Set(x, y)
			}
		}
	}
	res, err := decoder.NewDecoder().Decode(bm, nil)
	if err != nil {
		return "", err
	}
	return res.GetText(), nil
}

// TestRemaskEveryVersionAndMask proves the function-pattern map, mask
// functions and format information are right: every symbol re-masked to
// every mask still decodes to its payload.
func TestRemaskEveryVersionAndMask(t *testing.T) {
	for v := 1; v <= 40; v++ {
		for _, lvl := range []Level{L, H} {
			payload := fmt.Sprintf("v%d-%s-", v, lvl)
			m, err := Build(payload, Options{Level: lvl, Version: v})
			if err != nil {
				t.Fatalf("v%d: %v", v, err)
			}
			cur := currentMask(m.dark, lvl)
			if cur < 0 {
				t.Fatalf("v%d %s: format information not recognized", v, lvl)
			}
			fn := functionModules(v, m.Size)
			at := cur
			for mask := 0; mask < 8; mask++ {
				remask(m.dark, fn, lvl, at, mask)
				at = mask
				if got, err := rawDecode(m.dark); err != nil || got != payload {
					t.Fatalf("v%d %s mask %d: decoded %q, %v", v, lvl, mask, got, err)
				}
			}
		}
	}
}

func TestChosenMaskIsBestReadable(t *testing.T) {
	payloads := []string{"tel:+442079460000", "geo:50.8304,-0.8579?q=50.8304,-0.8579(Bosham%20shore)"}
	for _, n := range []int{72, 99, 104, 118} {
		payloads = append(payloads, strings.Repeat("abcdefghij", 20)[:n])
	}
	for _, p := range payloads {
		for _, lvl := range []Level{M, H} {
			m, err := Build(p, Options{Level: lvl})
			if err != nil {
				t.Fatal(err)
			}
			if !readable(m.dark, p) {
				t.Fatalf("%q %s: chosen mask is not readable", p, lvl)
			}
			chosen := penalty(m.dark)
			fn := functionModules(m.Version, m.Size)
			at := currentMask(m.dark, lvl)
			for mask := 0; mask < 8; mask++ {
				remask(m.dark, fn, lvl, at, mask)
				at = mask
				if penalty(m.dark) < chosen && readable(m.dark, p) {
					t.Errorf("%q %s: readable mask %d scores below the chosen one", p, lvl, mask)
				}
			}
		}
	}
}

func TestFinderPenalty(t *testing.T) {
	n := 21
	grid := make([][]bool, n)
	for i := range grid {
		grid[i] = make([]bool, n)
	}
	if got := finderPenalty(grid); got != 0 {
		t.Fatalf("blank grid scores %d", got)
	}
	// A 1:1:3:1:1 run with light on both sides matches both orientations
	// of the rule: 4 light modules before it, and 4 after.
	for x, on := range []bool{true, false, true, true, true, false, true} {
		grid[10][7+x] = on
	}
	if got := finderPenalty(grid); got != 80 {
		t.Errorf("finder-like run scores %d, want 80", got)
	}
}
