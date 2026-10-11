package matrix

import (
	"strings"
	"testing"
)

func TestBuildSizeAndFinders(t *testing.T) {
	m, err := Build("https://example.com", Options{Level: M})
	if err != nil {
		t.Fatal(err)
	}
	if m.Size != 17+4*m.Version {
		t.Fatalf("size %d does not match version %d", m.Size, m.Version)
	}
	// Every finder pattern: dark 7x7 ring, light 5x5 ring, dark 3x3 center.
	for _, o := range m.FinderOrigins() {
		for dy := 0; dy < 7; dy++ {
			for dx := 0; dx < 7; dx++ {
				ring := max(abs(dx-3), abs(dy-3))
				wantDark := ring != 2
				if got := m.Dark(o[0]+dx, o[1]+dy); got != wantDark {
					t.Fatalf("finder at %v module (%d,%d): dark=%v want %v", o, dx, dy, got, wantDark)
				}
			}
		}
	}
}

func TestRegionAt(t *testing.T) {
	m, err := Build("x", Options{Level: L})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		x, y int
		want Region
	}{
		{0, 0, FinderOuter}, {3, 3, FinderInner}, {7, 3, Separator},
		{m.Size - 1, 0, FinderOuter}, {m.Size - 5, 4, FinderInner}, {m.Size - 8, 2, Separator},
		{2, m.Size - 3, FinderInner}, {10, 10, Data}, {m.Size - 1, m.Size - 1, Data},
	}
	for _, c := range cases {
		if got := m.RegionAt(c.x, c.y); got != c.want {
			t.Errorf("RegionAt(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestForcedVersion(t *testing.T) {
	m, err := Build("hi", Options{Level: Q, Version: 10})
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 10 || m.Size != 57 {
		t.Fatalf("version %d size %d, want 10/57", m.Version, m.Size)
	}
	if _, err := Build(strings.Repeat("x", 100), Options{Level: H, Version: 1}); err == nil {
		t.Fatal("expected error for content too large for version 1")
	}
	if _, err := Build("x", Options{Version: 41}); err == nil {
		t.Fatal("expected error for version 41")
	}
}

func TestTooLong(t *testing.T) {
	if _, err := Build(strings.Repeat("é", 3000), Options{Level: H}); err == nil {
		t.Fatal("expected capacity error")
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{"l": L, "M": M, "q": Q, "H": H} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("X"); err == nil {
		t.Error("expected error for X")
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
