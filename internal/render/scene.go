package render

import (
	"image/color"

	"github.com/Henk476/qr-generator/internal/matrix"
)

// Scene is a resolution-independent drawing in module units. The origin is
// the top-left corner of the quiet zone.
type Scene struct {
	Size       float64 // width and height, quiet zone included
	QuietZone  float64
	Background color.NRGBA
	Layers     []Layer // painted in order, over the background
}

// Layer is a set of shapes sharing one fill.
type Layer struct {
	Fill  color.NRGBA
	Rects []Rect
}

// Rect is an axis-aligned rectangle in module units.
type Rect struct{ X, Y, W, H float64 }

// BuildScene lays out the dark modules of m. Horizontal runs of dark modules
// are merged into a single rectangle to keep vector output small.
func BuildScene(m *matrix.Matrix, s Style) (*Scene, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	q := float64(s.QuietZone)
	fg := Layer{Fill: s.Foreground}
	for y := 0; y < m.Size; y++ {
		for x := 0; x < m.Size; {
			if !m.Dark(x, y) {
				x++
				continue
			}
			start := x
			for x < m.Size && m.Dark(x, y) {
				x++
			}
			fg.Rects = append(fg.Rects, Rect{X: q + float64(start), Y: q + float64(y), W: float64(x - start), H: 1})
		}
	}
	return &Scene{
		Size:       float64(m.Size) + 2*q,
		QuietZone:  q,
		Background: s.Background,
		Layers:     []Layer{fg},
	}, nil
}
