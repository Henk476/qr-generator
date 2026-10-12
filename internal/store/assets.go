package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
)

// Asset is an uploaded file, such as a normalized logo PNG.
type Asset struct {
	ID   string
	Kind string
	Mime string
	Data []byte
}

// SaveAsset stores data and returns its ID. Identical content is stored
// once: saving it again returns the existing ID.
func (s *Store) SaveAsset(ctx context.Context, kind, mime string, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM assets WHERE sha256 = ?`, hash).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = newID()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO assets (id, kind, mime, sha256, data, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, kind, mime, hash, data, now())
	return id, err
}

// GetAsset loads an asset.
func (s *Store) GetAsset(ctx context.Context, id string) (*Asset, error) {
	a := Asset{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT kind, mime, data FROM assets WHERE id = ?`, id).Scan(&a.Kind, &a.Mime, &a.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}
