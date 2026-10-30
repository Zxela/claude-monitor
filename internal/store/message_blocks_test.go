package store

import (
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/session"
)

// persistMessageBlocks writes one assistant message the way Claude Code logs
// it: a thinking line, then a tool_use line, both carrying the same message id.
func persistMessageBlocks(t *testing.T, db *DB, ts time.Time) {
	t.Helper()
	if err := db.SaveSession(&session.Session{ID: "s1", StartedAt: ts, LastActive: ts}); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	thinking := EventInsert{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", Role: "assistant", MessageID: "msg_1", UUID: "u1",
			ContentText: "[thinking...]", Timestamp: ts,
		},
		Thinking: "check the README first",
	}
	tool := EventInsert{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", Role: "assistant", MessageID: "msg_1", UUID: "u2",
			ContentText: "[tool: Bash]", ToolName: "Bash", ToolDetail: "grep -n Node README.md",
			ToolUseID: "toolu_1", Timestamp: ts.Add(time.Second),
		},
	}
	for _, ei := range []EventInsert{thinking, tool} {
		if err := db.PersistBatch(&EventBatch{Events: []EventInsert{ei}}); err != nil {
			t.Fatalf("PersistBatch: %v", err)
		}
	}
}

func TestPersistBatch_MessageBlocksMerge(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	persistMessageBlocks(t, db, time.Now())

	events, err := db.ListEvents("s1", 10, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want 1", len(events))
	}
	e := events[0]
	if e.ToolName != "Bash" || e.ToolDetail != "grep -n Node README.md" || e.ToolUseID != "toolu_1" {
		t.Errorf("tool fields = %q %q %q, want the tool_use line's", e.ToolName, e.ToolDetail, e.ToolUseID)
	}
	if e.ThinkingContent != "check the README first" {
		t.Errorf("ThinkingContent = %q, want the thinking line's", e.ThinkingContent)
	}

	// Startup replay re-sends both lines; the result must not change and the
	// FTS index must stay consistent with the stored row.
	persistMessageBlocks(t, db, time.Now())
	hits, err := db.SearchFTS("README", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(hits) != 1 || hits[0].ToolName != "Bash" {
		t.Fatalf("SearchFTS after replay = %+v, want the merged row", hits)
	}
	if _, err := db.db.Exec(`INSERT INTO events_fts(events_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		t.Fatalf("fts integrity-check: %v", err)
	}
}

func TestCompaction_Thinking(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	persistMessageBlocks(t, db, time.Now().Add(-48*time.Hour))

	if n, err := db.CompactHotToWarm(0); err != nil || n != 1 {
		t.Fatalf("CompactHotToWarm = %d, %v; want 1", n, err)
	}
	var plain int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM event_content WHERE thinking IS NOT NULL`).Scan(&plain); err != nil {
		t.Fatal(err)
	}
	if plain != 0 {
		t.Errorf("%d warm rows still hold plaintext thinking", plain)
	}
	events, _ := db.ListEvents("s1", 10, 0)
	if len(events) != 1 || events[0].ThinkingContent != "check the README first" {
		t.Fatalf("warm ThinkingContent = %+v", events)
	}

	if _, err := db.CompactWarmToCold(0); err != nil {
		t.Fatalf("CompactWarmToCold: %v", err)
	}
	events, _ = db.ListEvents("s1", 10, 0)
	if events[0].ThinkingContent != "" {
		t.Errorf("cold ThinkingContent = %q, want empty", events[0].ThinkingContent)
	}
}
