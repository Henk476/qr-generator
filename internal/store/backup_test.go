package store

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/generate"
)

func TestBackupRestore(t *testing.T) {
	ctx := context.Background()
	src := openTest(t)
	c := &Code{Name: "Shop", Type: "url", Fields: map[string]string{"url": "a.com"}, Design: generate.DefaultDesign(), Tags: []string{"x"}}
	if err := src.CreateCode(ctx, c); err != nil {
		t.Fatal(err)
	}
	p := &Preset{Name: "Mine", Design: generate.DefaultDesign()}
	src.CreatePreset(ctx, p)
	logoID, _ := src.SaveAsset(ctx, "logo", "image/png", []byte("logo"))
	src.SetSetting(ctx, "default_size", "777")
	src.SetSetting(ctx, "csrf_token", "source-token")

	var archive bytes.Buffer
	if err := src.Backup(ctx, &archive, "test"); err != nil {
		t.Fatal(err)
	}

	// Restore into a store that already has other data.
	dst := openTest(t)
	dst.CreateCode(ctx, &Code{Name: "Old", Type: "url", Fields: map[string]string{"url": "old.com"}, Design: generate.DefaultDesign()})
	dst.SetSetting(ctx, "csrf_token", "dest-token")
	stats, err := dst.Restore(ctx, bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if stats != (RestoreStats{Codes: 1, Presets: 1, Logos: 1}) {
		t.Errorf("stats = %+v", stats)
	}
	codes, _ := dst.ListCodes(ctx, ListOptions{})
	if len(codes) != 1 || codes[0].ID != c.ID || codes[0].Name != "Shop" {
		t.Errorf("codes after restore: %+v", codes)
	}
	if _, err := dst.GetAsset(ctx, logoID); err != nil {
		t.Errorf("logo not restored: %v", err)
	}
	if v, _ := dst.GetSetting(ctx, "default_size", ""); v != "777" {
		t.Errorf("setting not restored: %q", v)
	}
	if v, _ := dst.GetSetting(ctx, "csrf_token", ""); v != "dest-token" {
		t.Errorf("machine-local token overwritten: %q", v)
	}
	ps, _ := dst.ListPresets(ctx)
	if len(ps) != len(builtinPresets)+1 {
		t.Errorf("%d presets after restore", len(ps))
	}
}

func TestRestoreRejectsBadArchives(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	s.CreateCode(ctx, &Code{Name: "Keep", Type: "url", Fields: map[string]string{"url": "a.com"}, Design: generate.DefaultDesign()})

	zipWith := func(files map[string]string) []byte {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for name, body := range files {
			f, _ := zw.Create(name)
			f.Write([]byte(body))
		}
		zw.Close()
		return b.Bytes()
	}
	newer, _ := json.Marshal(manifest{Format: backupFormat, Schema: 99})
	ok, _ := json.Marshal(manifest{Format: backupFormat, Schema: 1})
	tests := map[string][]byte{
		"not a ZIP":     []byte("hello"),
		"missing":       zipWith(map[string]string{"manifest.json": string(ok)}),
		"unrecognized":  zipWith(map[string]string{"manifest.json": `{"format":"other"}`, "canute.db": "x"}),
		"newer version": zipWith(map[string]string{"manifest.json": string(newer), "canute.db": "x"}),
		"is unreadable": zipWith(map[string]string{"manifest.json": string(ok), "canute.db": "not sqlite"}),
	}
	for want, data := range tests {
		_, err := s.Restore(ctx, bytes.NewReader(data), int64(len(data)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v", want, err)
		}
	}
	// Nothing was lost along the way.
	if codes, _ := s.ListCodes(ctx, ListOptions{}); len(codes) != 1 {
		t.Errorf("%d codes after failed restores", len(codes))
	}
}

func TestRestoreMigratesOlderBackup(t *testing.T) {
	ctx := context.Background()
	// Build a schema-v1 (M2) database and back it up by hand.
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	old.CreateCode(ctx, &Code{Name: "From M2", Type: "url", Fields: map[string]string{"url": "a.com"}, Design: generate.DefaultDesign()})
	old.db.ExecContext(ctx, "DROP TABLE assets; PRAGMA user_version=1")
	var archive bytes.Buffer
	if err := old.Backup(ctx, &archive, "m2"); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s := openTest(t)
	stats, err := s.Restore(ctx, bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil || stats.Codes != 1 {
		t.Fatalf("restore older backup: %+v, %v", stats, err)
	}
}
