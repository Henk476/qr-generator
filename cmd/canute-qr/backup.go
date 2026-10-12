package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Henk476/qr-generator/internal/store"
)

const dataDirUsage = "where the library and settings are stored (default: <user config dir>/canute-qr)"

// openStore opens the database in dataDir, or the default data directory.
func openStore(dataDir string) (*store.Store, string, error) {
	dir := dataDir
	if dir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return nil, "", fmt.Errorf("cannot find a config directory, use --data-dir: %w", err)
		}
		dir = filepath.Join(cfg, "canute-qr")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	st, err := store.Open(filepath.Join(dir, "canute.db"))
	return st, dir, err
}

func runBackup(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("o", "", "backup file to write, e.g. canute-backup.zip (required)")
	dataDir := fs.String("data-dir", "", dataDirUsage)
	if code := parseFlags(fs, args, stderr); code >= 0 {
		return code
	}
	fail := failer(stderr)
	if *out == "" {
		return fail(errors.New("-o is required"))
	}
	st, _, err := openStore(*dataDir)
	if err != nil {
		return fail(err)
	}
	defer st.Close()
	var buf bytes.Buffer
	if err := st.Backup(context.Background(), &buf, version); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o600); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stderr, "wrote backup %s (%d KB)\n", *out, buf.Len()/1024)
	return 0
}

func runRestore(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := fs.String("data-dir", "", dataDirUsage)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "Usage: canute-qr restore [--data-dir DIR] BACKUP.zip\n\n"+
			"Replaces the library, your presets, uploaded logos and settings with those in the backup.\n"+
			"Stop a running \"canute-qr serve\" first.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := failer(stderr)
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	st, dir, err := openStore(*dataDir)
	if err != nil {
		return fail(err)
	}
	defer st.Close()
	stats, err := st.Restore(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(stderr, "restored %d codes, %d presets and %d logos into %s\n", stats.Codes, stats.Presets, stats.Logos, dir)
	return 0
}
