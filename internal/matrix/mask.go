package matrix

import (
	"image"
	"slices"

	"github.com/makiuchi-d/gozxing"
	zxqr "github.com/makiuchi-d/gozxing/qrcode"
)

// Mask re-selection.
//
// The underlying encoder sometimes picks a data mask that leaves finder-like
// 1:1:3:1:1 runs in the data area, which can make detectors lock onto a false
// finder pattern. After encoding, we score all eight masks with the ISO/IEC
// 18004 penalty rules and switch to the best one, rewriting the format
// information to match.

// maskFuncs are the eight QR data masks; i is the row, j the column.
var maskFuncs = [8]func(i, j int) bool{
	func(i, j int) bool { return (i+j)%2 == 0 },
	func(i, j int) bool { return i%2 == 0 },
	func(i, j int) bool { return j%3 == 0 },
	func(i, j int) bool { return (i+j)%3 == 0 },
	func(i, j int) bool { return (i/2+j/3)%2 == 0 },
	func(i, j int) bool { return (i*j)%2+(i*j)%3 == 0 },
	func(i, j int) bool { return ((i*j)%2+(i*j)%3)%2 == 0 },
	func(i, j int) bool { return ((i+j)%2+(i*j)%3)%2 == 0 },
}

// alignmentCenters lists alignment pattern center coordinates per version.
var alignmentCenters = [41][]int{
	2: {6, 18}, 3: {6, 22}, 4: {6, 26}, 5: {6, 30}, 6: {6, 34},
	7: {6, 22, 38}, 8: {6, 24, 42}, 9: {6, 26, 46}, 10: {6, 28, 50},
	11: {6, 30, 54}, 12: {6, 32, 58}, 13: {6, 34, 62},
	14: {6, 26, 46, 66}, 15: {6, 26, 48, 70}, 16: {6, 26, 50, 74},
	17: {6, 30, 54, 78}, 18: {6, 30, 56, 82}, 19: {6, 30, 58, 86}, 20: {6, 34, 62, 90},
	21: {6, 28, 50, 72, 94}, 22: {6, 26, 50, 74, 98}, 23: {6, 30, 54, 78, 102},
	24: {6, 28, 54, 80, 106}, 25: {6, 32, 58, 84, 110}, 26: {6, 30, 58, 86, 114},
	27: {6, 34, 62, 90, 118},
	28: {6, 26, 50, 74, 98, 122}, 29: {6, 30, 54, 78, 102, 126}, 30: {6, 26, 52, 78, 104, 130},
	31: {6, 30, 56, 82, 108, 134}, 32: {6, 34, 60, 86, 112, 138}, 33: {6, 30, 58, 86, 114, 142},
	34: {6, 34, 62, 90, 118, 146},
	35: {6, 30, 54, 78, 102, 126, 150}, 36: {6, 24, 50, 76, 102, 128, 154},
	37: {6, 28, 54, 80, 106, 132, 158}, 38: {6, 32, 58, 84, 110, 136, 162},
	39: {6, 26, 54, 82, 110, 138, 166}, 40: {6, 30, 58, 86, 114, 142, 170},
}

// functionModules marks every module that is not data: finders with their
// separators and format areas, timing patterns, alignment patterns, version
// information and the dark module. Indexed [y][x].
func functionModules(version, size int) [][]bool {
	f := make([][]bool, size)
	for i := range f {
		f[i] = make([]bool, size)
	}
	rect := func(x0, y0, x1, y1 int) {
		for y := max(y0, 0); y <= min(y1, size-1); y++ {
			for x := max(x0, 0); x <= min(x1, size-1); x++ {
				f[y][x] = true
			}
		}
	}
	rect(0, 0, 8, 8)           // top-left finder, separator, format info
	rect(size-8, 0, size-1, 8) // top-right
	rect(0, size-8, 8, size-1) // bottom-left (includes the dark module)
	rect(6, 0, 6, size-1)      // vertical timing pattern
	rect(0, 6, size-1, 6)      // horizontal timing pattern
	centers := alignmentCenters[version]
	for _, cy := range centers {
		for _, cx := range centers {
			if f[cy][cx] { // overlaps a finder
				continue
			}
			rect(cx-2, cy-2, cx+2, cy+2)
		}
	}
	if version >= 7 {
		rect(size-11, 0, size-9, 5) // version info, top-right
		rect(0, size-11, 5, size-9) // version info, bottom-left
	}
	return f
}

// formatBits returns the 15-bit format information for level and mask.
func formatBits(l Level, mask int) int {
	eccBits := [...]int{L: 1, M: 0, Q: 3, H: 2}[l]
	data := eccBits<<3 | mask
	rem := data << 10
	for i := 14; i >= 10; i-- {
		if rem&(1<<i) != 0 {
			rem ^= 0x537 << (i - 10)
		}
	}
	return (data<<10 | rem) ^ 0x5412
}

// formatPositions returns the two copies of the format information as
// (x, y) positions, most significant bit first (the order decoders read).
func formatPositions(size int) (first, second [15][2]int) {
	k := 0
	for x := 0; x <= 5; x++ {
		first[k] = [2]int{x, 8}
		k++
	}
	first[6], first[7], first[8] = [2]int{7, 8}, [2]int{8, 8}, [2]int{8, 7}
	k = 9
	for y := 5; y >= 0; y-- {
		first[k] = [2]int{8, y}
		k++
	}
	k = 0
	for y := size - 1; y >= size-7; y-- {
		second[k] = [2]int{8, y}
		k++
	}
	for x := size - 8; x < size; x++ {
		second[k] = [2]int{x, 8}
		k++
	}
	return first, second
}

// currentMask identifies the mask from the format information.
func currentMask(dark [][]bool, l Level) int {
	first, _ := formatPositions(len(dark))
	got := 0
	for _, p := range first {
		got <<= 1
		if dark[p[1]][p[0]] {
			got |= 1
		}
	}
	for m := 0; m < 8; m++ {
		if formatBits(l, m) == got {
			return m
		}
	}
	return -1
}

// remask switches dark from mask from to mask to and rewrites the format
// information.
func remask(dark [][]bool, fn [][]bool, l Level, from, to int) {
	for y := range dark {
		for x := range dark[y] {
			if !fn[y][x] && maskFuncs[from](y, x) != maskFuncs[to](y, x) {
				dark[y][x] = !dark[y][x]
			}
		}
	}
	bits := formatBits(l, to)
	first, second := formatPositions(len(dark))
	for k := 0; k < 15; k++ {
		on := bits&(1<<(14-k)) != 0
		dark[first[k][1]][first[k][0]] = on
		dark[second[k][1]][second[k][0]] = on
	}
}

// penalty scores a symbol with the four ISO/IEC 18004 mask evaluation rules.
func penalty(dark [][]bool) int {
	n := len(dark)
	score := 0

	// Rule 1: runs of five or more same-colored modules.
	for _, horiz := range []bool{true, false} {
		for a := 0; a < n; a++ {
			run := 1
			for b := 1; b <= n; b++ {
				same := false
				if b < n {
					if horiz {
						same = dark[a][b] == dark[a][b-1]
					} else {
						same = dark[b][a] == dark[b-1][a]
					}
				}
				if same {
					run++
					continue
				}
				if run >= 5 {
					score += 3 + run - 5
				}
				run = 1
			}
		}
	}

	// Rule 2: 2x2 blocks of one color.
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			c := dark[y][x]
			if dark[y][x+1] == c && dark[y+1][x] == c && dark[y+1][x+1] == c {
				score += 3
			}
		}
	}

	score += finderPenalty(dark)

	// Rule 4: deviation of the dark share from 50%.
	darkCount := 0
	for y := range dark {
		for x := range dark[y] {
			if dark[y][x] {
				darkCount++
			}
		}
	}
	pct := darkCount * 100 / (n * n)
	dev := pct - 50
	if dev < 0 {
		dev = -dev
	}
	score += dev / 5 * 10
	return score
}

// finderPenalty is rule 3: 40 points per finder-like 1:1:3:1:1 pattern with
// four light modules on one side (the quiet zone counts as light).
func finderPenalty(dark [][]bool) int {
	n := len(dark)
	at := func(x, y int) bool { return x >= 0 && y >= 0 && x < n && y < n && dark[y][x] }
	score := 0
	patterns := [2][11]bool{
		{true, false, true, true, true, false, true, false, false, false, false},
		{false, false, false, false, true, false, true, true, true, false, true},
	}
	for a := 0; a < n; a++ {
		for b := -4; b < n; b++ {
			for _, p := range patterns {
				matchH, matchV := true, true
				for k := 0; k < 11; k++ {
					if at(b+k, a) != p[k] {
						matchH = false
					}
					if at(a, b+k) != p[k] {
						matchV = false
					}
				}
				if matchH {
					score += 40
				}
				if matchV {
					score += 40
				}
			}
		}
	}
	return score
}

// chooseBestMask re-masks dark in place. Masks are tried from the lowest
// ISO penalty up, and the first whose plain rendering a ZXing-equivalent
// reader decodes back to payload wins. ZXing's detector (used by many
// Android scanners) misreads some otherwise valid mask choices, so this
// check keeps those out of the output. If no mask decodes, the lowest
// penalty mask is kept.
func chooseBestMask(dark [][]bool, version int, l Level, payload string) {
	cur := currentMask(dark, l)
	if cur < 0 {
		return // unexpected format information: leave the symbol alone
	}
	fn := functionModules(version, len(dark))
	var scores [8]int
	at := cur
	for m := 0; m < 8; m++ {
		remask(dark, fn, l, at, m)
		at = m
		scores[m] = penalty(dark)
	}
	order := []int{0, 1, 2, 3, 4, 5, 6, 7}
	slices.SortStableFunc(order, func(a, b int) int { return scores[a] - scores[b] })
	for _, m := range order {
		remask(dark, fn, l, at, m)
		at = m
		if readable(dark, payload) {
			return
		}
	}
	remask(dark, fn, l, at, order[0])
}

// readable reports whether a plain black-on-white rendering of dark decodes
// to payload with the ZXing detector and decoder.
func readable(dark [][]bool, payload string) bool {
	const ppm, quiet = 4, 4
	n := len(dark)
	side := (n + 2*quiet) * ppm
	img := image.NewGray(image.Rect(0, 0, side, side))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := range dark {
		for x := range dark[y] {
			if !dark[y][x] {
				continue
			}
			for dy := 0; dy < ppm; dy++ {
				row := img.Pix[((y+quiet)*ppm+dy)*img.Stride:]
				for dx := 0; dx < ppm; dx++ {
					row[(x+quiet)*ppm+dx] = 0
				}
			}
		}
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return false
	}
	res, err := zxqr.NewQRCodeReader().Decode(bmp, map[gozxing.DecodeHintType]any{
		gozxing.DecodeHintType_TRY_HARDER:    true,
		gozxing.DecodeHintType_CHARACTER_SET: "UTF-8",
	})
	return err == nil && res.GetText() == payload
}
