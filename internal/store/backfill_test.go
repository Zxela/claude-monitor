package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/session"
)

// writeAgentFile writes a single-line JSONL transcript at path with the given
// in-content sessionId, creating parent directories as needed.
func writeAgentFile(t *testing.T, path, inContentSessionID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	line := fmt.Sprintf(`{"type":"assistant","sessionId":%q,"isSidechain":true,"uuid":"u1","timestamp":%q,"message":{"role":"assistant","content":"hi"}}`,
		inContentSessionID, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// saveOrphan inserts a session row with empty identity columns, simulating a row
// ingested before Workstream 2 identity stamping.
func saveOrphan(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.SaveSession(&session.Session{
		ID: id, StartedAt: time.Now(), LastActive: time.Now(),
	}); err != nil {
		t.Fatalf("SaveSession(%s) failed: %v", id, err)
	}
}

func TestClassifyAgentPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		path           string
		wantKind       string
		wantAgentID    string
		wantWorkflowID string
	}{
		{
			name:     "shape1 normal session",
			path:     "/proj/3f2e-uuid.jsonl",
			wantKind: "",
		},
		{
			name:        "shape2 task subagent",
			path:        "/proj/parentUUID/subagents/agent-AAA.jsonl",
			wantKind:    backfillKindSubagent,
			wantAgentID: "agent-AAA",
		},
		{
			name:           "shape3 workflow agent",
			path:           "/proj/parentUUID/subagents/workflows/wf_9/agent-BBB.jsonl",
			wantKind:       backfillKindWorkflowAgent,
			wantAgentID:    "agent-BBB",
			wantWorkflowID: "wf_9",
		},
		{
			name:     "non-agent file under subagents",
			path:     "/proj/parentUUID/subagents/notes.jsonl",
			wantKind: "",
		},
		{
			// Degenerate empty-id "agent-.jsonl": must classify identically to the
			// watcher's HasPrefix(stem, "agent-") path (subagent, full stem as id),
			// not be skipped — the two in-sync code paths must agree.
			name:        "degenerate empty-id agent file",
			path:        "/proj/parentUUID/subagents/agent-.jsonl",
			wantKind:    backfillKindSubagent,
			wantAgentID: "agent-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, agentID, workflowID := classifyAgentPath(tt.path)
			if kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", kind, tt.wantKind)
			}
			if agentID != tt.wantAgentID {
				t.Errorf("agentID = %q, want %q", agentID, tt.wantAgentID)
			}
			if workflowID != tt.wantWorkflowID {
				t.Errorf("workflowID = %q, want %q", workflowID, tt.wantWorkflowID)
			}
		})
	}
}

func TestRunBackfillV013_OrphanRowLinked(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	tmp := t.TempDir()
	parentUUID := "11111111-2222-3333-4444-555555555555"

	// shape-2 subagent and shape-3 workflow-agent transcripts on disk.
	shape2 := filepath.Join(tmp, "proj", parentUUID, "subagents", "agent-AAA.jsonl")
	shape3 := filepath.Join(tmp, "proj", parentUUID, "subagents", "workflows", "wf_123", "agent-BBB.jsonl")
	writeAgentFile(t, shape2, parentUUID)
	writeAgentFile(t, shape3, parentUUID)

	// Pre-insert orphan sessions keyed by the agent-<id> stem.
	saveOrphan(t, db, "agent-AAA")
	saveOrphan(t, db, "agent-BBB")

	res, err := db.RunBackfillV013([]string{tmp}, false)
	if err != nil {
		t.Fatalf("RunBackfillV013 failed: %v", err)
	}
	if res.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", res.Scanned)
	}
	if res.Updated != 2 {
		t.Errorf("Updated = %d, want 2", res.Updated)
	}

	a, err := db.GetSession("agent-AAA")
	if err != nil || a == nil {
		t.Fatalf("GetSession(agent-AAA): %v / %v", a, err)
	}
	if a.ParentID != parentUUID {
		t.Errorf("agent-AAA ParentID = %q, want %q", a.ParentID, parentUUID)
	}
	if a.AgentID != "agent-AAA" {
		t.Errorf("agent-AAA AgentID = %q, want agent-AAA", a.AgentID)
	}
	if a.AgentKind != backfillKindSubagent {
		t.Errorf("agent-AAA AgentKind = %q, want %q", a.AgentKind, backfillKindSubagent)
	}
	if a.WorkflowID != "" {
		t.Errorf("agent-AAA WorkflowID = %q, want empty", a.WorkflowID)
	}

	b, err := db.GetSession("agent-BBB")
	if err != nil || b == nil {
		t.Fatalf("GetSession(agent-BBB): %v / %v", b, err)
	}
	if b.ParentID != parentUUID {
		t.Errorf("agent-BBB ParentID = %q, want %q", b.ParentID, parentUUID)
	}
	if b.AgentKind != backfillKindWorkflowAgent {
		t.Errorf("agent-BBB AgentKind = %q, want %q", b.AgentKind, backfillKindWorkflowAgent)
	}
	if b.WorkflowID != "wf_123" {
		t.Errorf("agent-BBB WorkflowID = %q, want wf_123", b.WorkflowID)
	}
}

// TestRunBackfillV013_SessionIDOnLaterLine covers the realistic transcript shape
// where the first lines carry no in-content sessionId — a leading blank line and a
// summary line — and an unparseable line appears before the real assistant line.
// firstValidEventSessionID must skip the blank/summary/garbage lines and link the
// orphan row using the sessionId from the later line.
func TestRunBackfillV013_SessionIDOnLaterLine(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	tmp := t.TempDir()
	parentUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	path := filepath.Join(tmp, "proj", parentUUID, "subagents", "agent-LATE.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	summary := `{"type":"summary","summary":"prior session","leafUuid":"x"}` // parses, no sessionId
	garbage := `this is not json`                                          // unparseable
	real := fmt.Sprintf(`{"type":"assistant","sessionId":%q,"isSidechain":true,"uuid":"u2","timestamp":%q,"message":{"role":"assistant","content":"hi"}}`,
		parentUUID, time.Now().UTC().Format(time.RFC3339))
	// Leading blank line + no-sessionId summary + garbage, then the real line.
	content := "\n" + summary + "\n" + garbage + "\n" + real + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	saveOrphan(t, db, "agent-LATE")

	res, err := db.RunBackfillV013([]string{tmp}, false)
	if err != nil {
		t.Fatalf("RunBackfillV013 failed: %v", err)
	}
	if res.Updated != 1 {
		t.Errorf("Updated = %d, want 1", res.Updated)
	}

	a, err := db.GetSession("agent-LATE")
	if err != nil || a == nil {
		t.Fatalf("GetSession(agent-LATE): %v / %v", a, err)
	}
	if a.ParentID != parentUUID {
		t.Errorf("ParentID = %q, want %q (sessionId resolved from a later line)", a.ParentID, parentUUID)
	}
	if a.AgentKind != backfillKindSubagent {
		t.Errorf("AgentKind = %q, want %q", a.AgentKind, backfillKindSubagent)
	}
}

func TestRunBackfillV013_MarkerPreventsRerun(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	tmp := t.TempDir()
	parentUUID := "aaaa-parent"
	shape2 := filepath.Join(tmp, "proj", parentUUID, "subagents", "agent-AAA.jsonl")
	writeAgentFile(t, shape2, parentUUID)
	saveOrphan(t, db, "agent-AAA")

	// First run sets the marker.
	if _, err := db.RunBackfillV013([]string{tmp}, false); err != nil {
		t.Fatalf("first RunBackfillV013 failed: %v", err)
	}

	// Mutate the row to a sentinel parent so we can detect a (wrongful) rewrite.
	if _, err := db.db.Exec(`UPDATE sessions SET parent_id = 'SENTINEL' WHERE id = 'agent-AAA'`); err != nil {
		t.Fatalf("sentinel update failed: %v", err)
	}

	res, err := db.RunBackfillV013([]string{tmp}, false)
	if err != nil {
		t.Fatalf("second RunBackfillV013 failed: %v", err)
	}
	if res.Scanned != 0 {
		t.Errorf("Scanned = %d, want 0 (marker should short-circuit the walk)", res.Scanned)
	}

	a, err := db.GetSession("agent-AAA")
	if err != nil || a == nil {
		t.Fatalf("GetSession(agent-AAA): %v / %v", a, err)
	}
	if a.ParentID != "SENTINEL" {
		t.Errorf("ParentID = %q, want SENTINEL unchanged (marker short-circuit)", a.ParentID)
	}
}

func TestRunBackfillV013_Idempotent_NoClobber(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	tmp := t.TempDir()
	parentUUID := "bbbb-parent"
	shape2 := filepath.Join(tmp, "proj", parentUUID, "subagents", "agent-AAA.jsonl")
	writeAgentFile(t, shape2, parentUUID)
	saveOrphan(t, db, "agent-AAA")

	// First run populates the identity columns.
	if _, err := db.RunBackfillV013([]string{tmp}, false); err != nil {
		t.Fatalf("first RunBackfillV013 failed: %v", err)
	}
	before, err := db.GetSession("agent-AAA")
	if err != nil || before == nil {
		t.Fatalf("GetSession before: %v / %v", before, err)
	}

	// Force a second run even though the marker is set. The walk must run, but the
	// CASE guards mean already-populated columns are preserved.
	res, err := db.RunBackfillV013([]string{tmp}, true)
	if err != nil {
		t.Fatalf("forced RunBackfillV013 failed: %v", err)
	}
	if res.Scanned != 1 {
		t.Errorf("Scanned = %d, want 1 (force=true must walk even with marker set)", res.Scanned)
	}
	// The row is already fully populated, so the guarded UPDATE matches no row:
	// nothing changed, so Updated must be 0 (not over-reported via RowsAffected)
	// and the unchanged row is counted as Skipped instead.
	if res.Updated != 0 {
		t.Errorf("Updated = %d, want 0 (forced re-run over a populated row changes nothing)", res.Updated)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (the unchanged row)", res.Skipped)
	}

	after, err := db.GetSession("agent-AAA")
	if err != nil || after == nil {
		t.Fatalf("GetSession after: %v / %v", after, err)
	}
	if after.ParentID != before.ParentID || after.WorkflowID != before.WorkflowID ||
		after.AgentID != before.AgentID || after.AgentKind != before.AgentKind {
		t.Errorf("identity columns changed on re-run: before=%+v after=%+v", before, after)
	}
}

func TestRunBackfillV013_NoMatchingFilesNoError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	tmp := t.TempDir()
	// Only a normal shape-1 transcript; no agent-*.jsonl files.
	normal := filepath.Join(tmp, "proj", "plain-uuid.jsonl")
	writeAgentFile(t, normal, "plain-uuid")

	res, err := db.RunBackfillV013([]string{tmp}, false)
	if err != nil {
		t.Fatalf("RunBackfillV013 failed: %v", err)
	}
	if res.Scanned != 0 {
		t.Errorf("Scanned = %d, want 0", res.Scanned)
	}
	if res.Updated != 0 {
		t.Errorf("Updated = %d, want 0", res.Updated)
	}
	if v, err := db.GetSetting(backfillMarkerKey); err != nil || v != "1" {
		t.Errorf("marker = %q (err %v), want \"1\"", v, err)
	}
}

func TestRunBackfillV013_MissingBasePathSkipped(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	res, err := db.RunBackfillV013([]string{missing}, false)
	if err != nil {
		t.Fatalf("RunBackfillV013 should silently skip missing path, got: %v", err)
	}
	if res.Scanned != 0 || res.Errors != 0 {
		t.Errorf("Scanned=%d Errors=%d, want 0/0", res.Scanned, res.Errors)
	}
}

func TestRunBackfillV013_SelfParentGuard(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	tmp := t.TempDir()
	// in-content sessionId equals the file stem "agent-CCC" -> no self-parenting.
	shape2 := filepath.Join(tmp, "proj", "someparent", "subagents", "agent-CCC.jsonl")
	writeAgentFile(t, shape2, "agent-CCC")
	saveOrphan(t, db, "agent-CCC")

	if _, err := db.RunBackfillV013([]string{tmp}, false); err != nil {
		t.Fatalf("RunBackfillV013 failed: %v", err)
	}
	c, err := db.GetSession("agent-CCC")
	if err != nil || c == nil {
		t.Fatalf("GetSession(agent-CCC): %v / %v", c, err)
	}
	if c.ParentID != "" {
		t.Errorf("ParentID = %q, want empty (self-parent guard)", c.ParentID)
	}
	// Identity columns are still filled even with no parent link.
	if c.AgentKind != backfillKindSubagent || c.AgentID != "agent-CCC" {
		t.Errorf("identity not filled: kind=%q id=%q", c.AgentKind, c.AgentID)
	}
}

func TestDefaultBackfillBasePaths(t *testing.T) {
	t.Parallel()
	paths := DefaultBackfillBasePaths()
	if len(paths) < 2 {
		t.Fatalf("expected at least the two container paths, got %v", paths)
	}
	// The two well-known container locations are always present.
	want := map[string]bool{
		"/home/node/.claude/projects": false,
		"/root/.claude/projects":      false,
	}
	for _, p := range paths {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("missing well-known path %q in %v", p, paths)
		}
	}
}
