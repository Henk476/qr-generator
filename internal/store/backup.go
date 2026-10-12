package store

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Backup archive layout.
const (
	backupFormat   = "canute-qr-backup"
	backupManifest = "manifest.json"
	backupDB       = "canute.db"
	// maxBackupDB caps the database inside an archive being restored.
	maxBackupDB = 1 << 30
)

// keptSettings are machine-local secrets that a restore never overwrites,
// so open browser tabs and API clients keep working.
var keptSettings = []string{"csrf_token", "api_token"}

type manifest struct {
	Format  string    `json:"format"`
	Schema  int       `json:"schema"`
	Created time.Time `json:"created"`
	App     string    `json:"app"`
}

// Backup writes a ZIP archive with a consistent snapshot of the database.
func (s *Store) Backup(ctx context.Context, w io.Writer, appVersion string) error {
	dir, err := os.MkdirTemp("", "canute-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	snap := filepath.Join(dir, backupDB)
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", snap); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	zw := zip.NewWriter(w)
	m, err := zw.Create(backupManifest)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(m).Encode(manifest{Format: backupFormat, Schema: len(migrations), Created: time.Now().UTC(), App: appVersion}); err != nil {
		return err
	}
	f, err := os.Open(snap)
	if err != nil {
		return err
	}
	defer f.Close()
	dbw, err := zw.Create(backupDB)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dbw, f); err != nil {
		return err
	}
	return zw.Close()
}

// RestoreStats counts what a restore brought in.
type RestoreStats struct {
	Codes, Presets, Logos int
}

// Restore replaces all codes, user presets, logos and settings with those
// in a backup archive. Built-in presets and machine-local secrets are kept.
// Nothing changes if the archive is invalid.
func (s *Store) Restore(ctx context.Context, r io.ReaderAt, size int64) (RestoreStats, error) {
	var stats RestoreStats
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return stats, errors.New("not a backup file: it is not a ZIP archive")
	}
	var mf, dbf *zip.File
	for _, f := range zr.File {
		switch f.Name {
		case backupManifest:
			mf = f
		case backupDB:
			dbf = f
		}
	}
	if mf == nil || dbf == nil {
		return stats, errors.New("not a backup file: manifest.json or canute.db is missing")
	}
	var m manifest
	rc, err := mf.Open()
	if err != nil {
		return stats, err
	}
	err = json.NewDecoder(io.LimitReader(rc, 1<<16)).Decode(&m)
	rc.Close()
	if err != nil || m.Format != backupFormat {
		return stats, errors.New("not a backup file: unrecognized manifest")
	}
	if m.Schema > len(migrations) {
		return stats, fmt.Errorf("this backup comes from a newer version (schema v%d); update Canute's QR Generator first", m.Schema)
	}
	if dbf.UncompressedSize64 > maxBackupDB {
		return stats, errors.New("the database in this backup is too large")
	}

	dir, err := os.MkdirTemp("", "canute-restore-")
	if err != nil {
		return stats, err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, backupDB)
	if err := extract(dbf, tmp); err != nil {
		return stats, err
	}
	// Opening migrates an older backup to the current schema and proves the
	// file is a readable database.
	bak, err := Open(tmp)
	if err != nil {
		return stats, fmt.Errorf("the backup database is unreadable: %w", err)
	}
	bak.Close()

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return stats, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "ATTACH DATABASE ? AS bak", tmp); err != nil {
		return stats, err
	}
	defer conn.ExecContext(context.Background(), "DETACH DATABASE bak")

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return stats, err
	}
	defer tx.Rollback()
	kept := []any{keptSettings[0], keptSettings[1]}
	steps := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM codes`, nil},
		{`INSERT INTO codes (id, name, type, content, style, tags, created_at, updated_at)
		  SELECT id, name, type, content, style, tags, created_at, updated_at FROM bak.codes`, nil},
		{`DELETE FROM presets WHERE builtin = 0`, nil},
		{`INSERT INTO presets (id, name, style, builtin) SELECT id, name, style, 0 FROM bak.presets WHERE builtin = 0`, nil},
		{`DELETE FROM assets`, nil},
		{`INSERT INTO assets (id, kind, mime, sha256, data, created_at)
		  SELECT id, kind, mime, sha256, data, created_at FROM bak.assets`, nil},
		{`DELETE FROM settings WHERE key NOT IN (?, ?)`, kept},
		{`INSERT INTO settings (key, value) SELECT key, value FROM bak.settings WHERE key NOT IN (?, ?)`, kept},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return stats, fmt.Errorf("restore: %w", err)
		}
	}
	for _, c := range []struct {
		q   string
		dst *int
	}{
		{`SELECT count(*) FROM codes`, &stats.Codes},
		{`SELECT count(*) FROM presets WHERE builtin = 0`, &stats.Presets},
		{`SELECT count(*) FROM assets`, &stats.Logos},
	} {
		if err := tx.QueryRowContext(ctx, c.q).Scan(c.dst); err != nil {
			return stats, err
		}
	}
	return stats, tx.Commit()
}

func extract(f *zip.File, path string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, maxBackupDB+1)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
