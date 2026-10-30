package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zxela/claude-monitor/internal/store"
)

func TestOpenDataDir_TightensExistingInstall(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-monitor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"history.db", "history.db-wal"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dbPath, err := openDataDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if dbPath != filepath.Join(dir, "history.db") {
		t.Errorf("dbPath = %q", dbPath)
	}
	for path, want := range map[string]os.FileMode{
		dir:             0o700,
		dbPath:          0o600,
		dbPath + "-wal": 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(path), got, want)
		}
	}
}

func TestOpenDataDir_FreshInstallThroughSQLite(t *testing.T) {
	home := t.TempDir()
	dbPath, err := openDataDir(home)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for path, want := range map[string]os.FileMode{
		filepath.Dir(dbPath): 0o700,
		dbPath:               0o600,
		dbPath + "-wal":      0o600,
		dbPath + "-shm":      0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(path), got, want)
		}
	}
}
