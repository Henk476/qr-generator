package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Henk476/qr-generator/internal/generate"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCodeCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)

	c := &Code{Name: "Home Wi-Fi", Type: "wifi", Fields: map[string]string{"ssid": "Home", "password": "x"},
		Design: generate.DefaultDesign(), Tags: []string{" home ", "Home", "", "network"}}
	if err := s.CreateCode(ctx, c); err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.CreatedAt.IsZero() {
		t.Fatalf("ID/timestamps not set: %+v", c)
	}
	if !slices.Equal(c.Tags, []string{"home", "network"}) {
		t.Errorf("tags = %q", c.Tags)
	}

	got, err := s.GetCode(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != c.Name || got.Fields["ssid"] != "Home" || got.Design != c.Design {
		t.Errorf("round trip mismatch: %+v", got)
	}

	got.Name = "Office Wi-Fi"
	got.Design.Foreground = "#123456"
	if err := s.UpdateCode(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetCode(ctx, c.ID)
	if again.Name != "Office Wi-Fi" || again.Design.Foreground != "#123456" || again.UpdatedAt.Before(again.CreatedAt) {
		t.Errorf("update not persisted: %+v", again)
	}

	if err := s.DeleteCode(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCode(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := s.DeleteCode(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete: %v", err)
	}
	if err := s.UpdateCode(ctx, &Code{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing: %v", err)
	}
}

func TestListCodes(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	for _, c := range []*Code{
		{Name: "Beta site", Type: "url", Fields: map[string]string{"url": "beta.example.com"}, Tags: []string{"work"}},
		{Name: "alpha wifi", Type: "wifi", Fields: map[string]string{"ssid": "Alpha_Net"}, Tags: []string{"home"}},
		{Name: "Gamma 100%", Type: "url", Fields: map[string]string{"url": "gamma.example.com"}, Tags: []string{"Work", "promo"}},
	} {
		c.Design = generate.DefaultDesign()
		if err := s.CreateCode(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	names := func(opts ListOptions) []string {
		cs, err := s.ListCodes(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, c := range cs {
			n = append(n, c.Name)
		}
		return n
	}
	tests := []struct {
		name string
		opts ListOptions
		want []string
	}{
		{"default newest first", ListOptions{}, []string{"Gamma 100%", "alpha wifi", "Beta site"}},
		{"by name", ListOptions{Sort: SortName}, []string{"alpha wifi", "Beta site", "Gamma 100%"}},
		{"type", ListOptions{Type: "url", Sort: SortName}, []string{"Beta site", "Gamma 100%"}},
		{"query name", ListOptions{Query: "WIFI"}, []string{"alpha wifi"}},
		{"query content", ListOptions{Query: "gamma.example"}, []string{"Gamma 100%"}},
		{"query literal percent", ListOptions{Query: "100%"}, []string{"Gamma 100%"}},
		{"query literal underscore", ListOptions{Query: "a_n"}, []string{"alpha wifi"}},
		{"tag case-insensitive", ListOptions{Tag: "work", Sort: SortName}, []string{"Beta site", "Gamma 100%"}},
		{"no match", ListOptions{Query: "zzz"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(tt.opts); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	tags, err := s.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tags, []string{"home", "promo", "work", "Work"}) {
		t.Errorf("tags = %q", tags)
	}
}

func TestPresets(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	ps, err := s.ListPresets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != len(builtinPresets) || ps[0].ID != "classic" || !ps[0].Builtin {
		t.Fatalf("builtin presets: %+v", ps)
	}
	for _, p := range ps {
		if err := p.Design.Validate(); err != nil {
			t.Errorf("builtin preset %s invalid: %v", p.ID, err)
		}
	}

	p := &Preset{Name: "Mine", Design: generate.Design{ECC: "Q", QuietZone: 2, Foreground: "#111111", Background: "#eeeeee"}}
	if err := s.CreatePreset(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPreset(ctx, p.ID)
	if err != nil || got.Design != p.Design || got.Builtin {
		t.Fatalf("GetPreset = %+v, %v", got, err)
	}
	ps, _ = s.ListPresets(ctx)
	if ps[len(ps)-1].ID != p.ID {
		t.Error("user preset should be listed after built-ins")
	}

	if err := s.DeletePreset(ctx, "classic"); !errors.Is(err, ErrBuiltin) {
		t.Errorf("delete builtin: %v", err)
	}
	if err := s.DeletePreset(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPreset(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
}

func TestSettingsAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.GetSetting(ctx, "size", "1024"); v != "1024" {
		t.Errorf("default = %q", v)
	}
	if err := s.SetSetting(ctx, "size", "512"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "size", "768"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopening must not re-run migrations or duplicate built-in presets.
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.GetSetting(ctx, "size", ""); v != "768" {
		t.Errorf("after reopen = %q", v)
	}
	ps, _ := s.ListPresets(ctx)
	if len(ps) != len(builtinPresets) {
		t.Errorf("%d presets after reopen", len(ps))
	}
}
