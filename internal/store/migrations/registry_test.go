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
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`DROP TABLE test`)
			return err
		},
	})
	Register(2, Migration{
		Name: "add_test_column",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE test ADD COLUMN name TEXT DEFAULT ''`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE test_bak AS SELECT id FROM test;
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
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`DROP TABLE test`)
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
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE test ADD COLUMN val TEXT DEFAULT ''`)
			return err
		},
		Down: func(tx *sql.Tx) error {
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
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`DROP TABLE test`)
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

	Register(1, Migration{Name: "first", Up: func(tx *sql.Tx) error { return nil }, Down: func(tx *sql.Tx) error { return nil }})
	Register(2, Migration{Name: "second", Up: func(tx *sql.Tx) error { return nil }, Down: func(tx *sql.Tx) error { return nil }})

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

// TestOpus48Pricing_SeededAndReversible runs the REAL migration registry to head
// and asserts migration 014 seeds the claude-opus-4-8 model_pricing row at $5/$25
// (matching Opus 4.5/4.6/4.7), then rolls back 014 and confirms only that row is
// removed (011's rows survive).
func TestOpus48Pricing_SeededAndReversible(t *testing.T) {
	// Uses the real package registry (no swap) so all numbered migrations apply.
	db := openTestDB(t)
	if _, err := RunUp(db); err != nil {
		t.Fatalf("RunUp: %v", err)
	}

	var input, output, cacheRead, cacheCreate float64
	err := db.QueryRow(
		`SELECT input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok FROM model_pricing WHERE model_prefix = 'claude-opus-4-8'`,
	).Scan(&input, &output, &cacheRead, &cacheCreate)
	if err != nil {
		t.Fatalf("claude-opus-4-8 row not found after RunUp: %v", err)
	}
	if input != 5.0 {
		t.Errorf("input_per_mtok = %v, want 5", input)
	}
	if output != 25.0 {
		t.Errorf("output_per_mtok = %v, want 25", output)
	}
	// Cache rates follow the Opus convention from migration 011: cache read is
	// 10% of input and 5m cache create is 1.25x input.
	if cacheRead != 0.50 {
		t.Errorf("cache_read_per_mtok = %v, want 0.50", cacheRead)
	}
	if cacheCreate != 6.25 {
		t.Errorf("cache_create_per_mtok = %v, want 6.25", cacheCreate)
	}

	// Roll back the migrations above 014 (newest first) so 014 becomes the head,
	// then roll back 014 itself.
	for _, want := range []string{"reattribute_child_repos", "rebuild_events_fts", "recompute_session_aggregates"} {
		name, err := RunDown(db)
		if err != nil {
			t.Fatalf("RunDown: %v", err)
		}
		if name != want {
			t.Fatalf("RunDown rolled back %q, want %q", name, want)
		}
	}

	// Roll back migration 014.
	name, err := RunDown(db)
	if err != nil {
		t.Fatalf("RunDown: %v", err)
	}
	if name != "opus_4_8_pricing" {
		t.Fatalf("RunDown rolled back %q, want opus_4_8_pricing", name)
	}

	// The opus-4-8 row must be gone.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM model_pricing WHERE model_prefix = 'claude-opus-4-8'`).Scan(&n); err != nil {
		t.Fatalf("count after down: %v", err)
	}
	if n != 0 {
		t.Errorf("claude-opus-4-8 row should be removed after RunDown, found %d", n)
	}

	// But the rows seeded by migration 011 must remain intact.
	if err := db.QueryRow(`SELECT COUNT(*) FROM model_pricing WHERE model_prefix = 'claude-opus-4-6'`).Scan(&n); err != nil {
		t.Fatalf("count opus-4-6 after down: %v", err)
	}
	if n != 1 {
		t.Errorf("claude-opus-4-6 (from migration 011) should survive 014 rollback, found %d", n)
	}
}

// TestOpus48Pricing_DownPreservesUserEditedRow verifies migration 014's Down is
// value-gated: if the user has edited the claude-opus-4-8 price (e.g. via the
// pricing API) away from the seeded values, a rollback must NOT delete it, since
// Up used INSERT OR IGNORE and never created/owned that edited row.
func TestOpus48Pricing_DownPreservesUserEditedRow(t *testing.T) {
	db := openTestDB(t)
	if _, err := RunUp(db); err != nil {
		t.Fatalf("RunUp: %v", err)
	}

	// Simulate a user edit of the price after the migration seeded it.
	if _, err := db.Exec(
		`UPDATE model_pricing SET input_per_mtok = 9.0, output_per_mtok = 45.0 WHERE model_prefix = 'claude-opus-4-8'`,
	); err != nil {
		t.Fatalf("simulate user edit: %v", err)
	}

	if _, err := RunDown(db); err != nil {
		t.Fatalf("RunDown: %v", err)
	}

	// The user-edited row must survive the rollback, untouched.
	var in, out float64
	err := db.QueryRow(
		`SELECT input_per_mtok, output_per_mtok FROM model_pricing WHERE model_prefix = 'claude-opus-4-8'`,
	).Scan(&in, &out)
	if err != nil {
		t.Fatalf("user-edited claude-opus-4-8 row should survive 014 rollback, but: %v", err)
	}
	if in != 9.0 || out != 45.0 {
		t.Errorf("user edit not preserved: input=%v output=%v, want 9/45", in, out)
	}
}

func TestFailedMigration_RollsBack(t *testing.T) {
	saved := registry
	defer func() { registry = saved }()
	registry = nil

	Register(1, Migration{
		Name: "good",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE test (id TEXT)`)
			return err
		},
		Down: func(tx *sql.Tx) error { return nil },
	})
	Register(2, Migration{
		Name: "bad",
		Up: func(tx *sql.Tx) error {
			return sql.ErrNoRows // simulate failure
		},
		Down: func(tx *sql.Tx) error { return nil },
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

// TestMigration017_ReattributesChildRepos runs the REAL registry to head, then
// re-applies migration 017 against populated data (by rolling its version back to
// 16 and running up again) to verify children — including a multi-level chain —
// are re-pointed at their parent's repo_id, root sessions are left alone, and a
// second application is a no-op (idempotent).
func TestMigration017_ReattributesChildRepos(t *testing.T) {
	db := openTestDB(t)
	if _, err := RunUp(db); err != nil {
		t.Fatalf("RunUp: %v", err)
	}

	// Seed sessions AFTER the initial RunUp (so 017's first pass saw an empty
	// table). A root in a real repo, a child with a phantom worktree repo, and a
	// grandchild with its own phantom repo. parent_repo is a sibling root in a
	// different project that must be untouched.
	seed := func(id, parentID, repoID string) {
		t.Helper()
		if _, err := db.Exec(
			`INSERT INTO sessions (id, parent_id, repo_id) VALUES (?, ?, ?)`,
			id, parentID, repoID,
		); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("root", "", "github.com/acme/widget")
	seed("child", "root", "agent-deadbeef")       // phantom worktree repo
	seed("grandchild", "child", "agent-cafef00d") // phantom, one level deeper
	seed("other-root", "", "github.com/acme/other")

	// Re-apply 017 against the now-populated table: roll its version back to 16
	// (Down is a no-op, leaving rows intact) then RunUp re-applies just 017.
	if _, err := RunDown(db); err != nil {
		t.Fatalf("RunDown 017: %v", err)
	}
	if v, _ := GetVersion(db); v != 16 {
		t.Fatalf("after RunDown: version = %d, want 16", v)
	}
	if _, err := RunUp(db); err != nil {
		t.Fatalf("re-RunUp (applies 017): %v", err)
	}

	repoOf := func(id string) string {
		t.Helper()
		var r string
		if err := db.QueryRow(`SELECT COALESCE(repo_id,'') FROM sessions WHERE id = ?`, id).Scan(&r); err != nil {
			t.Fatalf("read repo of %s: %v", id, err)
		}
		return r
	}

	if got := repoOf("child"); got != "github.com/acme/widget" {
		t.Errorf("child repo = %q, want inherited github.com/acme/widget", got)
	}
	// Multi-level: grandchild should propagate up to the root project via the fixpoint loop.
	if got := repoOf("grandchild"); got != "github.com/acme/widget" {
		t.Errorf("grandchild repo = %q, want propagated github.com/acme/widget", got)
	}
	if got := repoOf("root"); got != "github.com/acme/widget" {
		t.Errorf("root repo = %q, want unchanged github.com/acme/widget", got)
	}
	if got := repoOf("other-root"); got != "github.com/acme/other" {
		t.Errorf("other-root repo = %q, want unchanged github.com/acme/other", got)
	}

	// Idempotent: rolling back and re-applying again must yield identical results.
	if _, err := RunDown(db); err != nil {
		t.Fatalf("RunDown 017 (2nd): %v", err)
	}
	if _, err := RunUp(db); err != nil {
		t.Fatalf("re-RunUp 017 (2nd): %v", err)
	}
	if got := repoOf("grandchild"); got != "github.com/acme/widget" {
		t.Errorf("after 2nd apply: grandchild repo = %q, want github.com/acme/widget", got)
	}
}

// columnExists reports whether a column is present on a table.
func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if name == col {
			return true
		}
	}
	return false
}

// indexExists reports whether a named index exists.
func indexExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n); err != nil {
		t.Fatalf("index lookup: %v", err)
	}
	return n > 0
}

// TestMigration013_DownRoundTrip runs the REAL registry to head, rolls back past
// migration 013 (workflow columns), asserts the columns + index are dropped, then
// re-applies to head — verifying 013's Down is correct and reversible. It rolls
// back to version 12 generically so it stays valid as later migrations stack on 013.
func TestMigration013_DownRoundTrip(t *testing.T) {
	db := openTestDB(t)
	if _, err := RunUp(db); err != nil {
		t.Fatalf("RunUp: %v", err)
	}
	for _, col := range []string{"workflow_id", "agent_id", "agent_kind"} {
		if !columnExists(t, db, "sessions", col) {
			t.Fatalf("after RunUp: sessions.%s missing", col)
		}
	}
	if !indexExists(t, db, "idx_sessions_workflow") {
		t.Fatal("after RunUp: idx_sessions_workflow missing")
	}

	// Roll back everything above migration 12 (013 and anything stacked on it).
	for {
		v, err := GetVersion(db)
		if err != nil {
			t.Fatalf("GetVersion: %v", err)
		}
		if v <= 12 {
			break
		}
		if _, err := RunDown(db); err != nil {
			t.Fatalf("RunDown from v%d: %v", v, err)
		}
	}

	// 013's columns and index must be gone.
	for _, col := range []string{"workflow_id", "agent_id", "agent_kind"} {
		if columnExists(t, db, "sessions", col) {
			t.Errorf("after rollback past 013: sessions.%s should be dropped", col)
		}
	}
	if indexExists(t, db, "idx_sessions_workflow") {
		t.Error("after rollback past 013: idx_sessions_workflow should be dropped")
	}

	// Re-apply to head — 013 (and anything above) must cleanly re-create the columns.
	if _, err := RunUp(db); err != nil {
		t.Fatalf("re-RunUp after rollback: %v", err)
	}
	if !columnExists(t, db, "sessions", "workflow_id") {
		t.Error("after re-RunUp: workflow_id not restored")
	}
	if !indexExists(t, db, "idx_sessions_workflow") {
		t.Error("after re-RunUp: idx_sessions_workflow not restored")
	}
}
