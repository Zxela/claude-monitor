package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/watcher"
)

// runIngest wires a watcher and pipeline over dir the way main does, waits
// until wantLines lines have been processed, then shuts down.
func runIngest(t *testing.T, db *store.DB, dir string, resume bool, wantLines int) *session.Store {
	t.Helper()
	sessions := session.NewStore()
	p := New(sessions, db, repo.NewResolver(), nil)
	fw, err := watcher.New([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int64
	fw.SetBootstrapCallback(func(ev watcher.Event) {
		p.Process(ev)
		n.Add(1)
	})
	if resume {
		saved, err := db.LoadFileOffsets()
		if err != nil {
			t.Fatal(err)
		}
		offsets := make(map[string]watcher.FileOffset, len(saved))
		for _, o := range saved {
			offsets[o.Path] = watcher.FileOffset{Offset: o.Offset, Inode: o.Inode}
		}
		fw.Resume(offsets, p.Restore)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := fw.Start(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for n.Load() < int64(wantLines) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	for range events {
	}
	p.Stop()
	if got := n.Load(); got != int64(wantLines) {
		t.Fatalf("processed %d lines, want %d", got, wantLines)
	}
	return sessions
}

// snapshot renders what the API serves from the live store and the DB.
func snapshot(t *testing.T, sessions *session.Store, db *store.DB) (live, rows string) {
	t.Helper()
	all := sessions.All()
	slices.SortFunc(all, func(a, b *session.Session) int {
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	for _, s := range all {
		slices.Sort(s.Children)
	}
	l, _ := json.MarshalIndent(all, "", " ")
	dbRows, err := db.ListSessions(1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(dbRows, func(a, b store.SessionRow) int {
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	r, _ := json.MarshalIndent(dbRows, "", " ")
	return string(l), string(r)
}

func TestResume_MatchesFullReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "proj")
	agentDir := filepath.Join(dir, "parent", "subagents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ts := func(sec int) string { return old.Add(time.Duration(sec) * time.Second).Format(time.RFC3339Nano) }
	recent := func(ms int) string {
		return time.Now().Add(-20*time.Second + time.Duration(ms)*time.Millisecond).UTC().Format(time.RFC3339Nano)
	}
	user := func(sid, uuid, when, text string) string {
		return fmt.Sprintf(`{"type":"user","uuid":%q,"sessionId":%q,"timestamp":%q,"cwd":%q,"message":{"role":"user","content":%q}}`, uuid, sid, when, cwd, text)
	}
	asst := func(sid, uuid, mid, when, stop string, out int) string {
		return fmt.Sprintf(`{"type":"assistant","uuid":%q,"sessionId":%q,"timestamp":%q,"cwd":%q,"message":{"id":%q,"role":"assistant","model":"claude-opus-4-8","stop_reason":%q,"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":%d}}}`, uuid, sid, when, cwd, mid, stop, out)
	}
	side := func(parentID, uuid, when string) string {
		return fmt.Sprintf(`{"type":"user","uuid":%q,"sessionId":%q,"isSidechain":true,"timestamp":%q,"cwd":%q,"message":{"role":"user","content":"sub task"}}`, uuid, parentID, when, cwd)
	}
	errLine := func(sid, uuid, when string) string {
		return fmt.Sprintf(`{"type":"user","uuid":%q,"sessionId":%q,"timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","is_error":true,"content":"boom"}]}}`, uuid, sid, when)
	}
	write := func(path string, lines ...string) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range lines {
			if _, err := f.WriteString(l + "\n"); err != nil {
				t.Fatal(err)
			}
		}
		f.Close()
	}
	parent := filepath.Join(dir, "parent.jsonl")
	agent := filepath.Join(agentDir, "agent-a1.jsonl")
	quietAgent := filepath.Join(agentDir, "agent-b1.jsonl")
	lateAgentDir := filepath.Join(dir, "late", "subagents")
	if err := os.MkdirAll(lateAgentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lateAgent := filepath.Join(lateAgentDir, "agent-c1.jsonl")
	streamed := filepath.Join(dir, "streamed.jsonl")
	active := filepath.Join(dir, "active.jsonl")
	untouched := filepath.Join(dir, "untouched.jsonl")

	write(parent, user("parent", "p1", ts(0), "build it"), asst("parent", "p2", "pm1", ts(1), "tool_use", 5))
	write(agent, side("parent", "a1", ts(2)), asst("parent", "a2", "am1", ts(3), "end_turn", 7))
	write(quietAgent, side("parent", "b1", ts(5)))
	write(lateAgent, side("late", "c1", ts(6)))
	streamedFirst := user("streamed", "s1", ts(10), "go")
	write(streamed,
		streamedFirst,
		errLine("streamed", "s2", ts(11)),
		`{"type":"summary","uuid":"s3","sessionId":"streamed","summary":"compacted"}`,
		asst("streamed", "s4", "sm1", ts(12), "", 3)) // first chunk; the rest arrives after the restart
	write(active, user("active", "x1", recent(0), "hi"), asst("active", "x2", "xm1", recent(1), "tool_use", 4))
	write(untouched, user("untouched", "u1", ts(20), "hello"), `not json`, asst("untouched", "u2", "um1", ts(21), "end_turn", 2))
	firstRun := 2 + 2 + 1 + 1 + 4 + 2 + 3

	db := openTestDB(t)
	runIngest(t, db, dir, false, firstRun)
	saved, err := db.LoadFileOffsets()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range saved {
		info, _ := os.Stat(o.Path)
		if o.Offset != info.Size() {
			t.Errorf("%s saved at %d, file is %d bytes", filepath.Base(o.Path), o.Offset, info.Size())
		}
	}
	if len(saved) != 7 {
		t.Fatalf("saved %d offsets, want 7", len(saved))
	}

	// As after a crash between persisting lines and saving their offset: the
	// resume re-reads three lines that are already stored.
	for _, o := range saved {
		if o.Path == streamed {
			o.Offset = int64(len(streamedFirst) + 1)
			if err := db.PersistBatch(&store.EventBatch{Offsets: []store.FileOffset{o}}); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Written while the daemon was down.
	write(streamed, asst("streamed", "s5", "sm1", ts(13), "end_turn", 9), errLine("streamed", "s2", ts(11)), user("streamed", "s6", ts(14), "again"))
	write(agent, asst("parent", "a3", "am2", ts(4), "end_turn", 1))
	// The parent of an already-persisted subagent shows up only now.
	write(filepath.Join(dir, "late.jsonl"), user("late", "l1", ts(30), "fresh"))
	appended := 3 + 1 + 1
	reread := 3

	sessResumed := runIngest(t, db, dir, true, appended+reread)
	liveResumed, rowsResumed := snapshot(t, sessResumed, db)

	fresh := openTestDB(t)
	sessFull := runIngest(t, fresh, dir, false, firstRun+appended)
	liveFull, rowsFull := snapshot(t, sessFull, fresh)

	if liveResumed != liveFull {
		t.Errorf("live sessions differ after resume\nresumed:\n%s\nfull replay:\n%s", liveResumed, liveFull)
	}
	if rowsResumed != rowsFull {
		t.Errorf("session rows differ after resume\nresumed:\n%s\nfull replay:\n%s", rowsResumed, rowsFull)
	}

	p, _ := sessResumed.Get("parent")
	slices.Sort(p.Children)
	if !slices.Equal(p.Children, []string{"agent-a1", "agent-b1"}) {
		t.Errorf("parent children = %v, want [agent-a1 agent-b1]", p.Children)
	}
	if l, _ := sessResumed.Get("late"); !slices.Equal(l.Children, []string{"agent-c1"}) {
		t.Errorf("late children = %v, want [agent-c1]", l.Children)
	}
	if a, _ := sessResumed.Get("active"); !a.IsActive || a.Status != "tool_use" {
		t.Errorf("active session = active %v status %q, want an active tool_use session", a.IsActive, a.Status)
	}
	if s, _ := sessResumed.Get("streamed"); s.OutputTokens != 9 || s.ErrorCount != 1 || s.EventCount != 4+1 {
		t.Errorf("streamed = out %d errors %d events %d, want 9, 1, 5", s.OutputTokens, s.ErrorCount, s.EventCount)
	}
}

// TestFlush_FailedBatchOffsets: a retried batch keeps its offsets for the
// retry; once a batch is dropped, no later offset for its file may be saved, or
// the restart would skip the dropped lines.
func TestFlush_FailedBatchOffsets(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(), nil)
	defer p.Stop()
	line := func(i int) []byte {
		return []byte(fmt.Sprintf(`{"type":"user","uuid":"u%d","sessionId":"s","timestamp":"2026-10-01T00:00:00Z","message":{"role":"user","content":"hi"}}`, i))
	}
	offsetOf := func(path string) (store.FileOffset, bool, bool) {
		p.bufMu.Lock()
		defer p.bufMu.Unlock()
		o, ok := p.offsets[path]
		return o, ok, p.offsetHold[path]
	}

	p.Process(watcher.Event{SessionID: "s", FilePath: "/p/s.jsonl", Offset: 10, Bootstrap: true, Line: line(0)})
	db.Close()
	p.flush()
	if o, ok, held := offsetOf("/p/s.jsonl"); !ok || o.Offset != 10 || held {
		t.Fatalf("after a retried batch: offset %+v present %v held %v, want 10 kept for the retry", o, ok, held)
	}

	for i := 1; i <= maxRetainedEvents; i++ {
		p.Process(watcher.Event{SessionID: "s", FilePath: "/p/s.jsonl", Offset: int64(10 + i), Bootstrap: true, Line: line(i)})
	}
	p.flush()
	p.Process(watcher.Event{SessionID: "o", FilePath: "/p/o.jsonl", Offset: 5, Bootstrap: true, Line: []byte("not json")})
	p.Process(watcher.Event{SessionID: "s", FilePath: "/p/s.jsonl", Offset: 99999, Bootstrap: true, Line: line(-1)})
	if _, ok, held := offsetOf("/p/s.jsonl"); ok || !held {
		t.Errorf("after a dropped batch: offset present %v held %v, want none recorded", ok, held)
	}
	if o, _, _ := offsetOf("/p/o.jsonl"); o.Offset != 5 {
		t.Errorf("other file's offset = %+v, want 5", o)
	}
}

// TestPendingLinkRepointIsPersisted: a child read before its parent is
// re-pointed to the parent's repo when the parent arrives. That must reach the
// DB even if the child has no further lines, or a restore brings back the
// child's own repo.
func TestPendingLinkRepointIsPersisted(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	p := New(sessions, db, repo.NewResolver(), nil)
	base := t.TempDir()
	childCWD, parentCWD := filepath.Join(base, "alpha"), filepath.Join(base, "beta")

	p.Process(watcher.Event{SessionID: "agent-c", Bootstrap: true, Line: []byte(fmt.Sprintf(
		`{"type":"user","uuid":"c1","sessionId":"par","isSidechain":true,"timestamp":"2026-10-01T00:00:00Z","cwd":%q,"message":{"role":"user","content":"x"}}`, childCWD))})
	p.flush()
	p.Process(watcher.Event{SessionID: "par", Bootstrap: true, Line: []byte(fmt.Sprintf(
		`{"type":"user","uuid":"p1","sessionId":"par","timestamp":"2026-10-01T00:00:01Z","cwd":%q,"message":{"role":"user","content":"y"}}`, parentCWD))})
	p.Stop()

	live, _ := sessions.Get("agent-c")
	row, err := db.GetSession("agent-c")
	if err != nil || row == nil {
		t.Fatalf("child row: %v", err)
	}
	if live.RepoID != "beta" || row.RepoID != live.RepoID {
		t.Errorf("child repo: live %q, stored %q; want both beta", live.RepoID, row.RepoID)
	}
}
