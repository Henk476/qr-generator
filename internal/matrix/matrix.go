// Package matrix builds the module grid of a QR code. It hides the
// underlying QR library so it can be swapped for an in-house encoder later.
package matrix

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Level is the error-correction level.
type Level int

const (
	L Level = iota // ~7% recovery
	M              // ~15% recovery
	Q              // ~25% recovery
	H              // ~30% recovery
)

func (l Level) String() string { return [...]string{"L", "M", "Q", "H"}[l] }

// ParseLevel parses "L", "M", "Q" or "H" (case-insensitive).
func ParseLevel(s string) (Level, error) {
	switch strings.ToUpper(s) {
	case "L":
		return L, nil
	case "M":
		return M, nil
	case "Q":
		return Q, nil
	case "H":
		return H, nil
	}
	return 0, fmt.Errorf("error correction level must be L, M, Q or H, got %q", s)
}

func (l Level) lib() qrcode.RecoveryLevel {
	return [...]qrcode.RecoveryLevel{qrcode.Low, qrcode.Medium, qrcode.High, qrcode.Highest}[l]
}

// Options controls matrix generation.
type Options struct {
	Level Level
	// Version forces a symbol version (1-40). 0 picks the smallest that fits.
	Version int
}

// Matrix is a QR symbol without quiet zone.
type Matrix struct {
	Version int
	Level   Level
	Size    int      // modules per side: 17 + 4*Version
	dark    [][]bool // [y][x]
}

// Build encodes payload into a QR matrix.
func Build(payload string, opts Options) (*Matrix, error) {
	if payload == "" {
		return nil, fmt.Errorf("nothing to encode")
	}
	if opts.Level < L || opts.Level > H {
		return nil, fmt.Errorf("invalid error correction level %d", opts.Level)
	}
	var (
		q   *qrcode.QRCode
		err error
	)
	if opts.Version == 0 {
		q, err = qrcode.New(payload, opts.Level.lib())
	} else {
		if opts.Version < 1 || opts.Version > 40 {
			return nil, fmt.Errorf("version must be 1-40, got %d", opts.Version)
		}
		q, err = qrcode.NewWithForcedVersion(payload, opts.Version, opts.Level.lib())
	}
	if err != nil {
		return nil, fmt.Errorf("content does not fit in a QR code at level %s: %w", opts.Level, err)
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	return &Matrix{Version: q.VersionNumber, Level: opts.Level, Size: len(bm), dark: bm}, nil
}

// Dark reports whether the module at column x, row y is dark. Coordinates
// outside the symbol are light (quiet zone).
func (m *Matrix) Dark(x, y int) bool {
	if x < 0 || y < 0 || x >= m.Size || y >= m.Size {
		return false
	}
	return m.dark[y][x]
}

// Region classifies a module so renderers can style parts differently.
type Region int

const (
	Data        Region = iota // data, timing, alignment, format/version info
	FinderOuter               // 7x7 ring of a finder pattern ("eye border")
	FinderInner               // 3x3 center of a finder pattern ("eye center")
	Separator                 // light ring around a finder pattern
)

// RegionAt classifies the module at (x, y).
func (m *Matrix) RegionAt(x, y int) Region {
	for _, o := range m.FinderOrigins() {
		dx, dy := x-o[0], y-o[1]
		if dx < -1 || dy < -1 || dx > 7 || dy > 7 {
			continue
		}
		switch {
		case dx == -1 || dy == -1 || dx == 7 || dy == 7:
			return Separator
		case dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4:
			return FinderInner
		default:
			return FinderOuter
		}
	}
	return Data
}

// FinderOrigins returns the top-left module of the three finder patterns:
// top-left, top-right, bottom-left.
func (m *Matrix) FinderOrigins() [3][2]int {
	return [3][2]int{{0, 0}, {m.Size - 7, 0}, {0, m.Size - 7}}
}
