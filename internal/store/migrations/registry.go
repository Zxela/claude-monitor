// Package migrations manages versioned schema migrations for the SQLite database.
package migrations

import (
	"database/sql"
	"fmt"
	"sort"
)

// Migration defines a schema change with Up (apply) and Down (rollback) functions.
type Migration struct {
	Name string
	Up   func(*sql.Tx) error
	Down func(*sql.Tx) error
}

// registry holds all registered migrations, keyed by version number.
var registry []registeredMigration

type registeredMigration struct {
	Version   int
	Migration Migration
}

// Register adds a migration at the given version. Called from init() in migration files.
func Register(version int, m Migration) {
	registry = append(registry, registeredMigration{Version: version, Migration: m})
}

// sorted returns migrations sorted by version.
func sorted() []registeredMigration {
	s := make([]registeredMigration, len(registry))
	copy(s, registry)
	sort.Slice(s, func(i, j int) bool { return s[i].Version < s[j].Version })
	return s
}

// GetVersion reads the current schema version from PRAGMA user_version.
func GetVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

func setVersionTx(tx *sql.Tx, v int) error {
	_, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v))
	return err
}

// RunUp applies all pending migrations. Returns the number of migrations applied.
func RunUp(db *sql.DB) (int, error) {
	current, err := GetVersion(db)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}

	applied := 0
	for _, rm := range sorted() {
		if rm.Version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return applied, fmt.Errorf("begin transaction for migration %d (%s): %w", rm.Version, rm.Migration.Name, err)
		}

		if err := rm.Migration.Up(tx); err != nil {
			tx.Rollback()
			return applied, fmt.Errorf("migration %d (%s) failed: %w", rm.Version, rm.Migration.Name, err)
		}

		if err := setVersionTx(tx, rm.Version); err != nil {
			tx.Rollback()
			return applied, fmt.Errorf("set version in migration %d: %w", rm.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return applied, fmt.Errorf("commit migration %d (%s): %w", rm.Version, rm.Migration.Name, err)
		}

		applied++
	}

	return applied, nil
}

// RunDown rolls back the last applied migration. Returns the migration name.
func RunDown(db *sql.DB) (string, error) {
	current, err := GetVersion(db)
	if err != nil {
		return "", fmt.Errorf("read schema version: %w", err)
	}
	if current == 0 {
		return "", fmt.Errorf("no migrations to roll back (version 0)")
	}

	// Find the migration at current version.
	for _, rm := range sorted() {
		if rm.Version == current {
			tx, err := db.Begin()
			if err != nil {
				return "", fmt.Errorf("begin rollback transaction: %w", err)
			}

			if err := rm.Migration.Down(tx); err != nil {
				tx.Rollback()
				return "", fmt.Errorf("rollback migration %d (%s) failed: %w", rm.Version, rm.Migration.Name, err)
			}

			if err := setVersionTx(tx, rm.Version-1); err != nil {
				tx.Rollback()
				return "", fmt.Errorf("set version in rollback: %w", err)
			}

			if err := tx.Commit(); err != nil {
				return "", fmt.Errorf("commit rollback %d (%s): %w", rm.Version, rm.Migration.Name, err)
			}

			return rm.Migration.Name, nil
		}
	}

	return "", fmt.Errorf("migration version %d not found in registry", current)
}

// Status returns the current version, latest available version, and pending migration names.
func Status(db *sql.DB) (current, latest int, pending []string, err error) {
	current, err = GetVersion(db)
	if err != nil {
		return 0, 0, nil, err
	}

	s := sorted()
	if len(s) > 0 {
		latest = s[len(s)-1].Version
	}

	for _, rm := range s {
		if rm.Version > current {
			pending = append(pending, rm.Migration.Name)
		}
	}

	return current, latest, pending, nil
}
