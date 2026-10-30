// Package store provides persistent storage using SQLite for the v2 data model.
package store

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/zxela/claude-monitor/internal/store/migrations"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB connection to the SQLite database.
type DB struct {
	db  *sql.DB // writes: single connection so concurrent writers queue instead of erroring
	rdb *sql.DB // reads: concurrent read-only pool so queries don't queue behind writes
}

// readPoolSize bounds concurrent read connections. The history page fires a
// handful of API queries per load; a small pool covers that without holding
// many file handles open.
const readPoolSize = 4

// dsn builds a file: URI for path with the given pragma options. SQLite's URI
// parser treats ? and # as delimiters and decodes %XX sequences, so those
// three characters must be escaped for the path to round-trip; everything
// else (including spaces) passes through literally.
func dsn(path, pragmas string) string {
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
	return "file:" + escaped + "?" + pragmas
}

// Open opens a SQLite database at the given path and runs pending migrations.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", dsn(path, "_pragma=busy_timeout(5000)"))
	if err != nil {
		return nil, err
	}
	// SQLite only supports one writer at a time. Limit this pool to one
	// connection to avoid "database is locked" errors from concurrent writes
	// across goroutines (pipeline flush, retention compaction).
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := migrations.RunUp(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	// WAL mode supports any number of readers concurrent with the single
	// writer. Reads go through a separate read-only pool so API queries are
	// not serialized behind long-running ingest or retention transactions.
	rdb, err := sql.Open("sqlite", dsn(path, "_pragma=busy_timeout(5000)&_pragma=query_only(1)"))
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	rdb.SetMaxOpenConns(readPoolSize)
	rdb.SetMaxIdleConns(readPoolSize)
	return &DB{db: sqlDB, rdb: rdb}, nil
}

// Close closes the underlying database connections.
func (d *DB) Close() error {
	rErr := d.rdb.Close()
	if err := d.db.Close(); err != nil {
		return err
	}
	return rErr
}

// Ping verifies both database pools are alive.
func (d *DB) Ping() error {
	return errors.Join(d.db.Ping(), d.rdb.Ping())
}
