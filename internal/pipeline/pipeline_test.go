package pipeline

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/repo"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/store"
	"github.com/zxela-claude/claude-monitor/internal/watcher"
	"github.com/zxela-claude/claude-monitor/internal/parser"
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
