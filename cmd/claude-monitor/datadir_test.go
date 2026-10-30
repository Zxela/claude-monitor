package main

import (
	"os"
	"path/filepath"
	"testing"
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

func TestOpenDataDir_FreshInstall(t *testing.T) {
	dbPath, err := openDataDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("history.db mode = %o, want 600", got)
	}
}
