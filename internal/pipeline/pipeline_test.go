package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/repo"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/store"
	"github.com/zxela-claude/claude-monitor/internal/watcher"
)

func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeJSONL(t *testing.T, fields map[string]interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestProcess_BasicEvent(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	var mu sync.Mutex
	var broadcasts []struct{ isNew, sendDetail bool }

	p := New(sessions, db, resolver, func(event *parser.Event, sess *session.Session, isNew, sendDetail bool) {
		mu.Lock()
		broadcasts = append(broadcasts, struct{ isNew, sendDetail bool }{isNew, sendDetail})
		mu.Unlock()
	})
	defer p.Stop()

	line := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"sessionId": "test-session",
		"message": map[string]interface{}{
			"id":   "msg-1",
			"role": "assistant",
			"content": "Hello!",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 50,
			},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "end_turn",
		},
	})

	p.Process(watcher.Event{
		SessionID: "test-session",
		Line:      line,
	})

	// Check session was created
	sess, ok := sessions.Get("test-session")
	if !ok {
		t.Fatal("session not found")
	}
	if sess.MessageCount != 1 {
		t.Errorf("MessageCount: got %d, want 1", sess.MessageCount)
	}
	if sess.EventCount != 1 {
		t.Errorf("EventCount: got %d, want 1", sess.EventCount)
	}
	if sess.Status != "waiting" {
		t.Errorf("Status: got %q, want waiting", sess.Status)
	}

	// Check broadcast was called
	mu.Lock()
	if len(broadcasts) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(broadcasts))
	}
	if len(broadcasts) > 0 && !broadcasts[0].isNew {
		t.Error("expected isNew=true for first event")
	}
	if len(broadcasts) > 0 && !broadcasts[0].sendDetail {
		t.Error("expected sendDetail=true for assistant message")
	}
	mu.Unlock()
}

func TestProcess_BootstrapNoBroadcast(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	broadcastCalled := false
	p := New(sessions, db, resolver, func(event *parser.Event, sess *session.Session, isNew, sendDetail bool) {
		broadcastCalled = true
	})
	defer p.Stop()

	line := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"message":   map[string]interface{}{"role": "assistant", "content": "hi"},
	})

	p.Process(watcher.Event{
		SessionID: "boot-session",
		Line:      line,
		Bootstrap: true,
	})

	if broadcastCalled {
		t.Error("broadcast should not be called during bootstrap")
	}
}

func TestSkipDetail(t *testing.T) {
	tests := []struct {
		event *parser.Event
		want  bool
	}{
		{&parser.Event{Type: "assistant"}, false},
		{&parser.Event{Type: "user"}, false},
		{&parser.Event{Type: "progress", HookEvent: "PreToolUse"}, false},
		{&parser.Event{Type: "progress"}, false},
		{&parser.Event{Type: "system"}, true},
		{&parser.Event{Type: "system", Subtype: "turn_duration"}, false},
		{&parser.Event{Type: "file-history-snapshot"}, true},
		{&parser.Event{Type: "custom-title"}, false},
		{&parser.Event{Type: "agent-name"}, false},
		{&parser.Event{Type: "queue-operation"}, true},
		{&parser.Event{Type: "unknown-future-type"}, false},
	}

	for _, tt := range tests {
		got := skipDetail(tt.event)
		if got != tt.want {
			t.Errorf("skipDetail(%q, hook=%q): got %v, want %v",
				tt.event.Type, tt.event.HookEvent, got, tt.want)
		}
	}
}

func TestParentSessionIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/.claude/projects/hash/abc-123/subagents/agent-xyz.jsonl", "abc-123"},
		{"/home/user/.claude/projects/hash/session.jsonl", ""},
		{"/tmp/test.jsonl", ""},
	}
	for _, tt := range tests {
		got := parentSessionIDFromPath(tt.path)
		if got != tt.want {
			t.Errorf("parentSessionIDFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestFlush_PersistsEvents(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	ts := time.Now()
	line := makeJSONL(t, map[string]interface{}{
		"type":      "user",
		"timestamp": ts.Format(time.RFC3339Nano),
		"message": map[string]interface{}{
			"role":    "user",
			"content": "Hello world",
		},
	})

	p.Process(watcher.Event{
		SessionID: "flush-test",
		Line:      line,
		Bootstrap: true, // skip broadcast
	})

	// Manually flush
	p.flush()

	// Verify events were persisted
	events, err := db.ListEvents("flush-test", 100, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "user" {
		t.Errorf("event type: got %q, want user", events[0].Type)
	}
}

func TestProcess_RebuildDedupFromDB(t *testing.T) {
	db := openTestDB(t)
	resolver := repo.NewResolver()
	const sid = "dedup-rebuild-test"
	ts := time.Now()

	// --- Phase 1: Process events and persist to DB ---
	sessions1 := session.NewStore()
	p1 := New(sessions1, db, resolver, nil)

	// Process two distinct messages with costs.
	for i, msgID := range []string{"msg-aaa", "msg-bbb"} {
		line := makeJSONL(t, map[string]interface{}{
			"type":      "assistant",
			"timestamp": ts.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			"sessionId": sid,
			"message": map[string]interface{}{
				"id":   msgID,
				"role": "assistant",
				"content": "response " + msgID,
				"usage": map[string]interface{}{
					"input_tokens":  100,
					"output_tokens": 50,
				},
				"model":       "claude-sonnet-4-6",
				"stop_reason": "end_turn",
			},
			"costUSD": 0.01,
		})
		p1.Process(watcher.Event{SessionID: sid, Line: line, Bootstrap: true})
	}
	p1.Stop() // flushes to DB

	sess1, ok := sessions1.Get(sid)
	if !ok {
		t.Fatal("session not found after phase 1")
	}
	origCost := sess1.TotalCost
	origMsgCount := sess1.MessageCount
	if origCost == 0 {
		t.Fatal("expected non-zero cost after phase 1")
	}
	if origMsgCount != 2 {
		t.Fatalf("expected 2 messages, got %d", origMsgCount)
	}

	// --- Phase 2: Simulate restart — fresh session store, same DB ---
	sessions2 := session.NewStore()
	p2 := New(sessions2, db, resolver, nil)
	defer p2.Stop()

	// Re-process the same event (msg-aaa) — simulates re-reading the JSONL after restart.
	dupLine := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Format(time.RFC3339Nano),
		"sessionId": sid,
		"message": map[string]interface{}{
			"id":   "msg-aaa",
			"role": "assistant",
			"content": "response msg-aaa",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 50,
			},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "end_turn",
		},
		"costUSD": 0.01,
	})
	p2.Process(watcher.Event{SessionID: sid, Line: dupLine, Bootstrap: true})

	sess2, ok := sessions2.Get(sid)
	if !ok {
		t.Fatal("session not found after phase 2")
	}

	// Cost and message count must NOT have doubled.
	if sess2.TotalCost != origCost {
		t.Errorf("TotalCost: got %f, want %f (dedup should prevent double-counting)", sess2.TotalCost, origCost)
	}
	if sess2.MessageCount != origMsgCount {
		t.Errorf("MessageCount: got %d, want %d (dedup should prevent double-counting)", sess2.MessageCount, origMsgCount)
	}
}

// populateMetaCacheForTest fills the meta cache with n synthetic entries for testing.
func (p *Pipeline) populateMetaCacheForTest(n int) {
	p.metaMu.Lock()
	defer p.metaMu.Unlock()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("sess-%d", i)
		p.metaCache[id] = &agentMeta{Name: id}
		p.metaOrder = append(p.metaOrder, id)
	}
}

func TestLoadMeta_EvictsOldestHalf(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	// Pre-populate cache with 501 entries (exceeds maxMetaCache=500).
	p.populateMetaCacheForTest(501)

	if len(p.metaCache) != 501 {
		t.Fatalf("setup: expected 501 entries, got %d", len(p.metaCache))
	}

	// Create a temp .meta.json for the new session so loadMeta can read it.
	tmpDir := t.TempDir()
	newSessionID := "sess-new"
	metaPath := filepath.Join(tmpDir, newSessionID+".meta.json")
	metaJSON, _ := json.Marshal(agentMeta{Name: "new-agent", AgentType: "task"})
	if err := os.WriteFile(metaPath, metaJSON, 0644); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}

	// Call loadMeta — the new entry pushes cache to 502, triggering eviction.
	p.loadMeta(watcher.Event{
		SessionID: newSessionID,
		FilePath:  filepath.Join(tmpDir, newSessionID+".jsonl"),
	})

	// After eviction: oldest half (251 entries) removed from 502 total → 251 remain.
	if len(p.metaCache) != 251 {
		t.Errorf("cache size after eviction: got %d, want 251", len(p.metaCache))
	}

	// Oldest entries should be evicted.
	for i := 0; i < 251; i++ {
		id := fmt.Sprintf("sess-%d", i)
		if _, ok := p.metaCache[id]; ok {
			t.Errorf("expected %s to be evicted", id)
			break
		}
	}

	// Newest original entries should survive.
	for i := 251; i < 501; i++ {
		id := fmt.Sprintf("sess-%d", i)
		if _, ok := p.metaCache[id]; !ok {
			t.Errorf("expected %s to survive eviction", id)
			break
		}
	}

	// The newly loaded session should be present.
	if _, ok := p.metaCache[newSessionID]; !ok {
		t.Error("expected new session to be in cache")
	}

	// metaOrder should match cache size.
	if len(p.metaOrder) != 251 {
		t.Errorf("metaOrder length: got %d, want 251", len(p.metaOrder))
	}
}
