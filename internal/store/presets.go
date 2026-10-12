package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Henk476/qr-generator/internal/generate"
)

// Preset is a named, reusable design.
type Preset struct {
	ID      string
	Name    string
	Design  generate.Design
	Builtin bool
}

// builtinPresets ship with the app. Their IDs are stable; their designs are
// refreshed on every start so improvements reach existing installs.
var builtinPresets = []Preset{
	{ID: "classic", Name: "Classic", Design: generate.DefaultDesign()},
	{ID: "midnight", Name: "Midnight", Design: generate.Design{ECC: "M", QuietZone: 4, Foreground: "#1b2a49", Background: "#ffffff"}},
	{ID: "ocean", Name: "Ocean", Design: generate.Design{ECC: "M", QuietZone: 4, Foreground: "#0b5d7a", Background: "#eef7fa"}},
	{ID: "forest", Name: "Forest", Design: generate.Design{ECC: "M", QuietZone: 4, Foreground: "#1f4d2b", Background: "#f3f7ef"}},
	{ID: "crimson", Name: "Crimson", Design: generate.Design{ECC: "M", QuietZone: 4, Foreground: "#8b1a2b", Background: "#fff8f5"}},
	{ID: "parchment", Name: "Parchment", Design: generate.Design{ECC: "M", QuietZone: 4, Foreground: "#3b2f2a", Background: "#faf3e0"}},
	{ID: "transparent", Name: "Transparent", Design: generate.Design{ECC: "M", QuietZone: 4, Foreground: "#000000", Background: "transparent"}},
	{ID: "print", Name: "Print (high ECC)", Design: generate.Design{ECC: "H", QuietZone: 4, Foreground: "#000000", Background: "#ffffff"}},
	{ID: "soft-dots", Name: "Soft dots", Design: generate.Design{ECC: "Q", QuietZone: 4, Foreground: "#1b4f8a", Background: "#ffffff",
		Module: "dots", EyeOuter: "circle", EyeInner: "circle"}},
	{ID: "tide", Name: "Tide (gradient)", Design: generate.Design{ECC: "Q", QuietZone: 4, Foreground: "#0b5d7a", Background: "#ffffff",
		Gradient: "linear", Foreground2: "#1b2a49", GradientAngle: 45, Module: "rounded", EyeOuter: "rounded", EyeInner: "rounded"}},
	{ID: "scan-me", Name: "Scan me badge", Design: generate.Design{ECC: "Q", QuietZone: 3, Foreground: "#1b2a49", Background: "#ffffff",
		Module: "classy-rounded", EyeOuter: "leaf", EyeInner: "leaf", Frame: "banner-bottom"}},
}

func (s *Store) syncBuiltinPresets(ctx context.Context) error {
	for _, p := range builtinPresets {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO presets (id, name, style, builtin) VALUES (?, ?, ?, 1)
			 ON CONFLICT(id) DO UPDATE SET name = excluded.name, style = excluded.style, builtin = 1`,
			p.ID, p.Name, mustJSON(p.Design))
		if err != nil {
			return err
		}
	}
	return nil
}

func scanPreset(row interface{ Scan(...any) error }) (*Preset, error) {
	var p Preset
	var style string
	if err := row.Scan(&p.ID, &p.Name, &style, &p.Builtin); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(style), &p.Design); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListPresets returns built-in presets first, then the user's in creation
// order.
func (s *Store) ListPresets(ctx context.Context) ([]Preset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, style, builtin FROM presets ORDER BY builtin DESC, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Preset
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPreset loads one preset.
func (s *Store) GetPreset(ctx context.Context, id string) (*Preset, error) {
	p, err := scanPreset(s.db.QueryRowContext(ctx, `SELECT id, name, style, builtin FROM presets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// CreatePreset saves a user preset, filling in its ID.
func (s *Store) CreatePreset(ctx context.Context, p *Preset) error {
	p.ID, p.Builtin = newID(), false
	_, err := s.db.ExecContext(ctx, `INSERT INTO presets (id, name, style, builtin) VALUES (?, ?, ?, 0)`,
		p.ID, p.Name, mustJSON(p.Design))
	return err
}

// ErrBuiltin is returned when trying to delete a built-in preset.
var ErrBuiltin = errors.New("built-in presets cannot be deleted")

// DeletePreset removes a user preset.
func (s *Store) DeletePreset(ctx context.Context, id string) error {
	p, err := s.GetPreset(ctx, id)
	if err != nil {
		return err
	}
	if p.Builtin {
		return ErrBuiltin
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM presets WHERE id = ?`, id)
	return err
}
