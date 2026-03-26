# DB Migration System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a versioned migration system using SQLite's `PRAGMA user_version` so schema changes across releases don't break existing databases.

**Architecture:** Migrations are Go files in `internal/store/migrations/` that register `Up`/`Down` functions via `init()`. A registry runs pending migrations on `Open()`. The main binary gets a `migrate` subcommand for status, rollback, and manual migration.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), `PRAGMA user_version`

---

### Task 1: Create migration registry

**Files:**
- Create: `internal/store/migrations/registry.go`
- Create: `internal/store/migrations/registry_test.go`

- [ ] **Step 1: Write the test file**

Create `internal/store/migrations/registry_test.go`:

```go
package migrations

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestGetVersion_FreshDB(t *testing.T) {
	db := openTestDB(t)
	v, err := GetVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Errorf("expected version 0, got %d", v)
	}
}

func TestRunUp_AppliesAllMigrations(t *testing.T) {
	// Save and restore global registry.
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	Register(1, Migration{
		Name: "create_test_table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE test`)
			return err
		},
	})
	Register(2, Migration{
		Name: "add_test_column",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`ALTER TABLE test ADD COLUMN name TEXT DEFAULT ''`)
			return err
		},
		Down: func(db *sql.DB) error {
			// SQLite doesn't support DROP COLUMN easily; recreate.
			_, err := db.Exec(`CREATE TABLE test_bak AS SELECT id FROM test;
				DROP TABLE test; ALTER TABLE test_bak RENAME TO test`)
			return err
		},
	})

	db := openTestDB(t)

	applied, err := RunUp(db)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 2 {
		t.Errorf("expected 2 applied, got %d", applied)
	}

	v, err := GetVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Errorf("expected version 2, got %d", v)
	}

	// Verify table and column exist.
	_, err = db.Exec(`INSERT INTO test (id, name) VALUES ('a', 'hello')`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
}

func TestRunUp_SkipsAlreadyApplied(t *testing.T) {
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	Register(1, Migration{
		Name: "create_test_table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE test`)
			return err
		},
	})

	db := openTestDB(t)

	// Run once.
	if _, err := RunUp(db); err != nil {
		t.Fatal(err)
	}

	// Add second migration.
	Register(2, Migration{
		Name: "add_column",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`ALTER TABLE test ADD COLUMN val TEXT DEFAULT ''`)
			return err
		},
		Down: func(db *sql.DB) error {
			return nil
		},
	})

	// Run again — should only apply migration 2.
	applied, err := RunUp(db)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Errorf("expected 1 applied, got %d", applied)
	}

	v, err := GetVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Errorf("expected version 2, got %d", v)
	}
}

func TestRunDown_RollsBackLastMigration(t *testing.T) {
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	Register(1, Migration{
		Name: "create_test_table",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE test`)
			return err
		},
	})

	db := openTestDB(t)
	if _, err := RunUp(db); err != nil {
		t.Fatal(err)
	}

	name, err := RunDown(db)
	if err != nil {
		t.Fatal(err)
	}
	if name != "create_test_table" {
		t.Errorf("expected name 'create_test_table', got %q", name)
	}

	v, err := GetVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Errorf("expected version 0, got %d", v)
	}
}

func TestRunDown_NothingToRollback(t *testing.T) {
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	db := openTestDB(t)
	_, err := RunDown(db)
	if err == nil {
		t.Error("expected error when nothing to rollback")
	}
}

func TestStatus(t *testing.T) {
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	Register(1, Migration{Name: "first", Up: func(db *sql.DB) error { return nil }, Down: func(db *sql.DB) error { return nil }})
	Register(2, Migration{Name: "second", Up: func(db *sql.DB) error { return nil }, Down: func(db *sql.DB) error { return nil }})

	db := openTestDB(t)

	current, latest, pending, err := Status(db)
	if err != nil {
		t.Fatal(err)
	}
	if current != 0 {
		t.Errorf("current: got %d, want 0", current)
	}
	if latest != 2 {
		t.Errorf("latest: got %d, want 2", latest)
	}
	if len(pending) != 2 {
		t.Errorf("pending: got %d, want 2", len(pending))
	}
}

func TestFailedMigration_RollsBack(t *testing.T) {
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	Register(1, Migration{
		Name: "good",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(db *sql.DB) error { return nil },
	})
	Register(2, Migration{
		Name: "bad",
		Up: func(db *sql.DB) error {
			return sql.ErrNoRows // simulate failure
		},
		Down: func(db *sql.DB) error { return nil },
	})

	db := openTestDB(t)
	_, err := RunUp(db)
	if err == nil {
		t.Fatal("expected error from bad migration")
	}

	// Version should be 1 (good migration applied, bad one rolled back).
	v, err := GetVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Errorf("expected version 1 after failed migration 2, got %d", v)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /root/claude-monitor && go test ./internal/store/migrations/ -v -count=1`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Write the registry implementation**

Create `internal/store/migrations/registry.go`:

```go
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
	Up   func(*sql.DB) error
	Down func(*sql.DB) error
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

func setVersion(db *sql.DB, v int) error {
	_, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v))
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

		if err := rm.Migration.Up(db); err != nil {
			tx.Rollback()
			return applied, fmt.Errorf("migration %d (%s) failed: %w", rm.Version, rm.Migration.Name, err)
		}

		if err := tx.Commit(); err != nil {
			return applied, fmt.Errorf("commit migration %d (%s): %w", rm.Version, rm.Migration.Name, err)
		}

		if err := setVersion(db, rm.Version); err != nil {
			return applied, fmt.Errorf("set version after migration %d: %w", rm.Version, err)
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

			if err := rm.Migration.Down(db); err != nil {
				tx.Rollback()
				return "", fmt.Errorf("rollback migration %d (%s) failed: %w", rm.Version, rm.Migration.Name, err)
			}

			if err := tx.Commit(); err != nil {
				return "", fmt.Errorf("commit rollback %d (%s): %w", rm.Version, rm.Migration.Name, err)
			}

			if err := setVersion(db, rm.Version-1); err != nil {
				return "", fmt.Errorf("set version after rollback: %w", err)
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
```

- [ ] **Step 4: Run tests**

Run: `cd /root/claude-monitor && go test ./internal/store/migrations/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/registry.go internal/store/migrations/registry_test.go
git commit -m "feat: add migration registry with RunUp, RunDown, Status

Versioned schema migrations using PRAGMA user_version. Each migration
has Up/Down functions, runs in a transaction, with rollback on failure."
```

---

### Task 2: Create migration 1 (initial schema) and integrate with store.Open

**Files:**
- Create: `internal/store/migrations/001_initial_schema.go`
- Modify: `internal/store/sqlite.go:38-74` (replace CREATE TABLE with migrations.RunUp)

- [ ] **Step 1: Create migration 1**

Create `internal/store/migrations/001_initial_schema.go`:

```go
package migrations

import "database/sql"

func init() {
	Register(1, Migration{
		Name: "initial_schema",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS session_history (
				id TEXT PRIMARY KEY,
				project_name TEXT,
				session_name TEXT,
				total_cost REAL,
				input_tokens INTEGER,
				output_tokens INTEGER,
				cache_read_tokens INTEGER,
				message_count INTEGER,
				error_count INTEGER,
				started_at TEXT,
				ended_at TEXT,
				duration_seconds REAL,
				outcome TEXT,
				model TEXT,
				cwd TEXT,
				git_branch TEXT,
				task_description TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_session_history_ended_at ON session_history(ended_at DESC)`)
			return err
		},
		Down: func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE IF EXISTS session_history`)
			return err
		},
	})
}
```

- [ ] **Step 2: Update store.Open to use migrations**

In `internal/store/sqlite.go`, replace the `createTableSQL` constant and its usage in `Open()`.

Remove lines 38-57 (the `const createTableSQL` block).

Add import for migrations package:
```go
"github.com/zxela-claude/claude-monitor/internal/store/migrations"
```

Replace lines 70-73 in `Open()`:
```go
if _, err := sqlDB.Exec(createTableSQL); err != nil {
    sqlDB.Close()
    return nil, err
}
```

with:
```go
if _, err := migrations.RunUp(sqlDB); err != nil {
    sqlDB.Close()
    return nil, err
}
```

- [ ] **Step 3: Run all store tests**

Run: `cd /root/claude-monitor && go test ./internal/store/ -v -count=1`
Expected: ALL PASS (existing tests should work identically)

- [ ] **Step 4: Run all tests**

Run: `cd /root/claude-monitor && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/migrations/001_initial_schema.go internal/store/sqlite.go
git commit -m "feat: replace CREATE TABLE with migration 1, integrate with store.Open

Existing schema is now migration 001. store.Open() calls migrations.RunUp()
instead of raw SQL. Existing databases get user_version set to 1."
```

---

### Task 3: Add `migrate` subcommand to main binary

**Files:**
- Modify: `cmd/claude-monitor/main.go:111-117` (add subcommand handling before flag.Parse)

- [ ] **Step 1: Add migrate subcommand handling**

In `cmd/claude-monitor/main.go`, after the `--version` check (line 115) and before `flag.Parse()` (line 118), add the migrate subcommand handler:

```go
	// Handle 'migrate' subcommand before flag.Parse().
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		handleMigrate(os.Args[2:])
		return
	}
```

Add the import for migrations:
```go
"github.com/zxela-claude/claude-monitor/internal/store/migrations"
```

Add the `handleMigrate` function before `main()` (after the `repeatable` type, around line 50):

```go
func handleMigrate(args []string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	dbPath := filepath.Join(homeDir, ".claude-monitor", "history.db")

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("cannot create data directory: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Fatalf("cannot set WAL mode: %v", err)
	}

	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "", "up":
		applied, err := migrations.RunUp(db)
		if err != nil {
			log.Fatalf("migration failed: %v", err)
		}
		if applied == 0 {
			current, _, _, _ := migrations.Status(db)
			fmt.Printf("Schema version: %d (up to date)\n", current)
		} else {
			current, _, _, _ := migrations.Status(db)
			fmt.Printf("Applied %d migration(s). Schema version: %d (up to date)\n", applied, current)
		}

	case "status":
		current, latest, pending, err := migrations.Status(db)
		if err != nil {
			log.Fatalf("cannot read status: %v", err)
		}
		fmt.Printf("Database: %s\n", dbPath)
		fmt.Printf("Schema version: %d (latest: %d)\n", current, latest)
		if len(pending) > 0 {
			fmt.Println("Pending migrations:")
			for _, name := range pending {
				fmt.Printf("  - %s\n", name)
			}
		} else {
			fmt.Println("No pending migrations.")
		}

	case "rollback":
		name, err := migrations.RunDown(db)
		if err != nil {
			log.Fatalf("rollback failed: %v", err)
		}
		current, _, _, _ := migrations.Status(db)
		fmt.Printf("Rolled back: %s\nSchema version: %d\n", name, current)

	default:
		fmt.Fprintf(os.Stderr, "Unknown migrate command: %s\nUsage: claude-monitor migrate [status|rollback]\n", sub)
		os.Exit(1)
	}
}
```

Add `"database/sql"` to the import block if not already present.

- [ ] **Step 2: Run all tests**

Run: `cd /root/claude-monitor && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/claude-monitor/main.go
git commit -m "feat: add 'migrate' subcommand (status, rollback, up)

claude-monitor migrate        — run pending migrations
claude-monitor migrate status — show current/latest version
claude-monitor migrate rollback — undo last migration"
```

---

### Task 4: Add Makefile targets and migration template

**Files:**
- Modify: `Makefile`
- Create: `internal/store/migrations/template.go.tmpl`

- [ ] **Step 1: Create migration template**

Create `internal/store/migrations/template.go.tmpl`:

```go
package migrations

import "database/sql"

func init() {
	Register({{.Version}}, Migration{
		Name: "{{.Name}}",
		Up: func(db *sql.DB) error {
			// TODO: implement up migration
			return nil
		},
		Down: func(db *sql.DB) error {
			// TODO: implement down migration
			return nil
		},
	})
}
```

- [ ] **Step 2: Add Makefile targets**

Append to `Makefile`:

```makefile

# --- Migration commands ---
.PHONY: migrate migrate-status migrate-rollback migrate-create

migrate:
	go run ./cmd/claude-monitor migrate

migrate-status:
	go run ./cmd/claude-monitor migrate status

migrate-rollback:
	go run ./cmd/claude-monitor migrate rollback

# Usage: make migrate-create NAME=add_parent_id
migrate-create:
	@if [ -z "$(NAME)" ]; then echo "Usage: make migrate-create NAME=add_parent_id"; exit 1; fi
	@NEXT=$$(ls internal/store/migrations/[0-9]*.go 2>/dev/null | wc -l | tr -d ' '); \
	NEXT=$$((NEXT + 1)); \
	FILE=$$(printf "internal/store/migrations/%03d_%s.go" $$NEXT "$(NAME)"); \
	sed "s/{{.Version}}/$$NEXT/g; s/{{.Name}}/$(NAME)/g" internal/store/migrations/template.go.tmpl > "$$FILE"; \
	echo "Created $$FILE"
```

- [ ] **Step 3: Verify make targets work**

Run: `cd /root/claude-monitor && make migrate-status`
Expected: Shows current schema version

- [ ] **Step 4: Commit**

```bash
git add Makefile internal/store/migrations/template.go.tmpl
git commit -m "feat: add Makefile targets for migration management

make migrate, migrate-status, migrate-rollback, migrate-create NAME=..."
```
