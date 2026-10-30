package store

import (
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
)

func offsetBatch() *EventBatch {
	return &EventBatch{
		Events: []EventInsert{{SessionID: "s", Event: &parser.Event{
			UUID: "u1", Type: "user", Role: "user", Timestamp: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		}}},
		Sessions: []*session.Session{{ID: "s", EventCount: 1}},
		Offsets:  []FileOffset{{Path: "/p/s.jsonl", SessionID: "s", Offset: 42, Inode: 1<<63 + 7}},
	}
}

func TestPersistBatch_SavesOffsetsWithEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if err := db.PersistBatch(offsetBatch()); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadFileOffsets()
	if err != nil {
		t.Fatal(err)
	}
	want := offsetBatch().Offsets
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("offsets = %+v, want %+v", got, want)
	}
	if row, err := db.GetSession("s"); err != nil || row == nil || row.EventCount != 1 {
		t.Fatalf("session row = %+v (err %v), want it written in the same batch", row, err)
	}

	// A later batch moves the offset forward.
	b := offsetBatch()
	b.Events = nil
	b.Offsets[0].Offset = 99
	if err := db.PersistBatch(b); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.LoadFileOffsets(); len(got) != 1 || got[0].Offset != 99 {
		t.Fatalf("offsets after update = %+v, want offset 99", got)
	}
}

// TestPersistBatch_OffsetsShareTheEventsTransaction: if the offset write fails,
// the events must not be committed either, and vice versa.
func TestPersistBatch_OffsetsShareTheEventsTransaction(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	if _, err := db.db.Exec(`CREATE TRIGGER no_offsets BEFORE INSERT ON file_offsets
		BEGIN SELECT RAISE(ABORT, 'offsets rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.PersistBatch(offsetBatch()); err == nil {
		t.Fatal("PersistBatch succeeded, want the trigger's error")
	}
	if evs, _ := db.ListEvents("s", 10, 0); len(evs) != 0 {
		t.Errorf("%d events committed without their offset", len(evs))
	}
	if row, _ := db.GetSession("s"); row != nil {
		t.Errorf("session committed without its offset")
	}

	if _, err := db.db.Exec(`DROP TRIGGER no_offsets; CREATE TRIGGER no_events BEFORE INSERT ON events
		BEGIN SELECT RAISE(ABORT, 'events rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.PersistBatch(offsetBatch()); err == nil {
		t.Fatal("PersistBatch succeeded, want the trigger's error")
	}
	if got, _ := db.LoadFileOffsets(); len(got) != 0 {
		t.Errorf("offset %+v committed without its events", got)
	}
}

func TestRestoreSessions_RoundTripsRuntimeState(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	last := time.Date(2026, 10, 1, 12, 0, 1, 123456789, time.UTC)
	s := &session.Session{
		ID: "child", ParentID: "parent",
		StartedAt: last.Add(-time.Minute), LastActive: last, TotalCost: 1.5,
		EventCount: 3, MessageCount: 2, ErrorCount: 1, CWD: "/w", GitBranch: "main",
		AgentKind: "subagent", AgentID: "child",
	}
	s.SetLastTurn("tool_use")
	pin := repo.Pin{ID: "github.com/x/y", Rank: 3, Toplevel: "/w", Inherited: true}
	s.SetRepoPin(pin)
	if err := db.FlushSessions([]*session.Session{s, {ID: "other"}}); err != nil {
		t.Fatal(err)
	}

	got, err := db.RestoreSessions([]string{"child", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("restored %d sessions, want 1", len(got))
	}
	r := got[0]
	if !r.LastActive.Equal(last) || !r.StartedAt.Equal(s.StartedAt) {
		t.Errorf("times = %v / %v, want %v / %v at full precision", r.StartedAt, r.LastActive, s.StartedAt, last)
	}
	if r.LastTurn() != "tool_use" || r.ParentID != "parent" || r.TotalCost != 1.5 ||
		r.EventCount != 3 || r.MessageCount != 2 || r.ErrorCount != 1 || r.AgentKind != "subagent" {
		t.Errorf("restored %+v", r)
	}
	if got := r.RepoPin(); got != pin {
		t.Errorf("RepoPin = %+v, want %+v", got, pin)
	}
	if !r.DedupPending() {
		t.Error("restored session should be marked DedupPending")
	}
}
