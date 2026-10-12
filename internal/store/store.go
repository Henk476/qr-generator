// Package store persists saved codes, style presets and settings in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"

	"github.com/Henk476/qr-generator/internal/generate"
)

// ErrNotFound is returned when a code or preset does not exist.
var ErrNotFound = errors.New("not found")

// Store is a handle to the database.
type Store struct {
	db *sql.DB
}

// migrations are applied in order; PRAGMA user_version records progress.
// Never edit an applied migration: append a new one.
var migrations = []string{
	`CREATE TABLE codes (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL,
		content    TEXT NOT NULL, -- JSON object of field values
		style      TEXT NOT NULL, -- JSON generate.Design
		tags       TEXT NOT NULL DEFAULT '[]', -- JSON array of strings
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE INDEX codes_updated ON codes(updated_at);
	CREATE TABLE presets (
		id      TEXT PRIMARY KEY,
		name    TEXT NOT NULL,
		style   TEXT NOT NULL, -- JSON generate.Design
		builtin INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
	`CREATE TABLE assets (
		id         TEXT PRIMARY KEY,
		kind       TEXT NOT NULL, -- "logo"
		mime       TEXT NOT NULL,
		sha256     TEXT NOT NULL UNIQUE,
		data       BLOB NOT NULL,
		created_at TEXT NOT NULL
	);`,
}

// Open opens (creating if needed) the database at path, migrates it and
// refreshes the built-in presets.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection: this is a single-user local app, and it keeps the
	// per-connection pragmas below in force.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.init(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) init(ctx context.Context) error {
	for _, p := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err := s.db.ExecContext(ctx, p); err != nil {
			return err
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("database schema v%d is newer than this program supports (v%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return s.syncBuiltinPresets(ctx)
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // only plain maps, slices and structs are marshalled
	}
	return string(b)
}

// Code is a saved code.
type Code struct {
	ID        string
	Name      string
	Type      string
	Fields    map[string]string
	Design    generate.Design
	Tags      []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NormalizeTags trims, drops empties and de-duplicates (case-insensitively).
func NormalizeTags(tags []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}

// CreateCode saves a new code, filling in its ID and timestamps.
func (s *Store) CreateCode(ctx context.Context, c *Code) error {
	c.ID = newID()
	ts := now()
	c.Tags = NormalizeTags(c.Tags)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO codes (id, name, type, content, style, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Type, mustJSON(c.Fields), mustJSON(c.Design), mustJSON(c.Tags), ts, ts)
	if err != nil {
		return err
	}
	c.CreatedAt, c.UpdatedAt = parseTime(ts), parseTime(ts)
	return nil
}

// UpdateCode overwrites a saved code's name, content, design and tags.
func (s *Store) UpdateCode(ctx context.Context, c *Code) error {
	ts := now()
	c.Tags = NormalizeTags(c.Tags)
	res, err := s.db.ExecContext(ctx,
		`UPDATE codes SET name = ?, type = ?, content = ?, style = ?, tags = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Type, mustJSON(c.Fields), mustJSON(c.Design), mustJSON(c.Tags), ts, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	c.UpdatedAt = parseTime(ts)
	return nil
}

// DeleteCode removes a saved code.
func (s *Store) DeleteCode(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM codes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const codeColumns = `id, name, type, content, style, tags, created_at, updated_at`

func scanCode(row interface{ Scan(...any) error }) (*Code, error) {
	var c Code
	var content, style, tags, created, updated string
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &content, &style, &tags, &created, &updated); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(content), &c.Fields); err != nil {
		return nil, fmt.Errorf("code %s content: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(style), &c.Design); err != nil {
		return nil, fmt.Errorf("code %s style: %w", c.ID, err)
	}
	if err := json.Unmarshal([]byte(tags), &c.Tags); err != nil {
		return nil, fmt.Errorf("code %s tags: %w", c.ID, err)
	}
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return &c, nil
}

// GetCode loads one saved code.
func (s *Store) GetCode(ctx context.Context, id string) (*Code, error) {
	c, err := scanCode(s.db.QueryRowContext(ctx, `SELECT `+codeColumns+` FROM codes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// Sort orders for ListCodes.
const (
	SortUpdated = "updated"
	SortCreated = "created"
	SortName    = "name"
)

// ListOptions filters and orders ListCodes.
type ListOptions struct {
	Query string // matched against name and content
	Type  string
	Tag   string
	Sort  string // SortUpdated (default), SortCreated or SortName
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// ListCodes returns saved codes matching opts.
func (s *Store) ListCodes(ctx context.Context, opts ListOptions) ([]Code, error) {
	order := "updated_at DESC"
	switch opts.Sort {
	case SortCreated:
		order = "created_at DESC"
	case SortName:
		order = "name COLLATE NOCASE ASC"
	}
	like := "%" + likeEscaper.Replace(opts.Query) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT `+codeColumns+` FROM codes
		WHERE (?1 = '' OR name LIKE ?2 ESCAPE '\' OR content LIKE ?2 ESCAPE '\')
		  AND (?3 = '' OR type = ?3)
		  AND (?4 = '' OR EXISTS (SELECT 1 FROM json_each(codes.tags) WHERE value = ?4 COLLATE NOCASE))
		ORDER BY `+order+`, rowid DESC`,
		opts.Query, like, opts.Type, opts.Tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Code
	for rows.Next() {
		c, err := scanCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// Tags returns every tag in use, sorted case-insensitively.
func (s *Store) Tags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT value FROM codes, json_each(codes.tags) ORDER BY value COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetSetting returns a setting, or def when it is unset.
func (s *Store) GetSetting(ctx context.Context, key, def string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	return v, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
