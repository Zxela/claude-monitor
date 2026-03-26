# DB Migration System — Design Spec

**Date:** 2026-03-26

## Goal

Add a versioned migration system to SQLite so schema changes across releases don't break existing user databases. Provide Active Record-style CLI commands for managing migrations.

## Architecture

### Migration Files

Each migration lives in its own file in `internal/store/migrations/`, named with a zero-padded sequence number:

```
internal/store/migrations/
├── registry.go          # Migration registry and runner
├── 001_initial_schema.go
├── 002_add_parent_id.go
└── ...
```

Each migration file exports an `init()` that registers itself:

```go
func init() {
    Register(1, Migration{
        Name: "initial_schema",
        Up: func(db *sql.DB) error {
            _, err := db.Exec(`CREATE TABLE IF NOT EXISTS session_history (...)`)
            return err
        },
        Down: func(db *sql.DB) error {
            _, err := db.Exec(`DROP TABLE IF EXISTS session_history`)
            return err
        },
    })
}
```

### Registry (`registry.go`)

```go
type Migration struct {
    Name string
    Up   func(*sql.DB) error
    Down func(*sql.DB) error
}

func Register(version int, m Migration)
func RunUp(db *sql.DB) error        // Run all pending migrations
func RunDown(db *sql.DB) error       // Rollback last migration
func Status(db *sql.DB) (current int, latest int, err error)
```

- Uses `PRAGMA user_version` to track current schema version
- Each migration runs in a transaction
- `RunUp` runs all migrations from `current+1` to `latest`
- `RunDown` runs the `Down` function of the current version, decrements `user_version`

### Integration with `store.Open()`

`Open()` calls `migrations.RunUp(db)` automatically — existing behavior formalized. If a migration fails, `Open()` returns an error and the daemon won't start.

### CLI Subcommands

Added to the main binary via subcommand detection before `flag.Parse()`:

```
claude-monitor migrate          # Run pending migrations (same as startup)
claude-monitor migrate status   # Show current version, latest, pending count
claude-monitor migrate rollback # Undo last migration
```

Output examples:

```
$ claude-monitor migrate status
Database: ~/.claude-monitor/history.db
Schema version: 1 (latest: 2)
Pending migrations:
  2: add_parent_id

$ claude-monitor migrate
Applied migration 2: add_parent_id
Schema version: 2 (up to date)

$ claude-monitor migrate rollback
Rolled back migration 2: add_parent_id
Schema version: 1
```

### Make Commands (dev tooling)

```makefile
migrate-create:  # Scaffold new migration file from template
    # Usage: make migrate-create NAME=add_parent_id
    # Creates: internal/store/migrations/NNN_add_parent_id.go

migrate:         # Run: go run ./cmd/claude-monitor migrate
migrate-status:  # Run: go run ./cmd/claude-monitor migrate status
migrate-rollback: # Run: go run ./cmd/claude-monitor migrate rollback
```

`make migrate-create` generates a file from a template with the next sequence number, pre-filled with the `init()` registration boilerplate and empty `Up`/`Down` functions.

### Migration 1: Initial Schema

The existing `CREATE TABLE IF NOT EXISTS session_history` + index becomes migration 1. This is a no-op for existing databases (table already exists) but formalizes it in the migration system. Sets `user_version` to 1.

### Error Handling

- Failed `Up`: transaction rolls back, `user_version` unchanged, daemon exits with clear error message
- Failed `Down` (rollback): transaction rolls back, `user_version` unchanged, error message with manual recovery instructions
- `user_version` higher than latest migration: warn but don't fail (supports downgrade scenarios where user rolls back binary version)

### Testing

- Fresh DB: runs all migrations from 0 → latest
- Existing DB at version 1: runs only 2+
- Rollback: version goes from N → N-1, `Down` function executes
- Failed migration: rolls back transaction, version unchanged
- Status: reports correct current/latest/pending

## Files

| File | Action | Purpose |
|------|--------|---------|
| `internal/store/migrations/registry.go` | Create | Migration type, Register, RunUp, RunDown, Status |
| `internal/store/migrations/001_initial_schema.go` | Create | Current schema as migration 1 |
| `internal/store/migrations/template.go.tmpl` | Create | Template for `make migrate-create` |
| `internal/store/sqlite.go` | Modify | Replace `CREATE TABLE` with `migrations.RunUp()` |
| `cmd/claude-monitor/main.go` | Modify | Add `migrate` subcommand handling |
| `Makefile` | Modify | Add migrate-create, migrate, migrate-status, migrate-rollback targets |

## Non-Goals

- Multiple database support (only SQLite)
- Concurrent migration runners (single binary)
- Migration file embedding (Go files, compiled in)
- SQL file migrations (Go functions give us full flexibility)
