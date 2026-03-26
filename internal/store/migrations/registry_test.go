package migrations

import (
	"database/sql"
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
