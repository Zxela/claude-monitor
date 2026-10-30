package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// openDataDir creates ~/.claude-monitor owner-only and returns the history.db
// path. The database holds transcript content that Claude Code keeps 0700, so
// it must not be readable by other local users. Existing installs created the
// directory 0755 and the files 0644; tighten them in place.
func openDataDir(home string) (string, error) {
	dir := filepath.Join(home, ".claude-monitor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	dbPath := filepath.Join(dir, "history.db")
	// SQLite gives -wal and -shm the main file's mode, so pre-creating it 0600
	// covers those too.
	f, err := os.OpenFile(dbPath, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return "", err
	}
	f.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(dbPath+suffix, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return dbPath, nil
}
