package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/watcher"
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
		{"/home/user/.claude/projects/hash/abc-123/subagents/workflows/wf_def/agent-xyz.jsonl", "abc-123"},
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

// TestParentLinking_DeferredUUID verifies that when a child session arrives
// before its parent, the parentUuid is stored as a deferred link and resolved
// once the parent session is processed.
func TestParentLinking_DeferredUUID(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-session-uuid"
	const childID = "child-session-uuid"
	ts := time.Now()

	// Step 1: Process a child event that references the parent via parentUuid.
	// The parent session does NOT exist yet in the store.
	childLine := makeJSONL(t, map[string]interface{}{
		"type":       "assistant",
		"timestamp":  ts.Format(time.RFC3339Nano),
		"sessionId":  childID,
		"parentUuid": parentID,
		"isSidechain": true,
		"message": map[string]interface{}{
			"id":      "msg-child-1",
			"role":    "assistant",
			"content": "child response",
			"usage": map[string]interface{}{
				"input_tokens":  10,
				"output_tokens": 5,
			},
			"model": "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: childID,
		Line:      childLine,
		Bootstrap: true,
	})

	// After processing child only, ParentID should not be set yet (parent unknown).
	childSess, ok := sessions.Get(childID)
	if !ok {
		t.Fatal("child session not found")
	}
	if childSess.ParentID != "" {
		t.Errorf("child ParentID should be empty before parent arrives; got %q", childSess.ParentID)
	}

	// Verify the deferred link was stored.
	p.linkMu.Lock()
	pending := p.pendingParentLinks[childID]
	p.linkMu.Unlock()
	if pending != parentID {
		t.Errorf("pendingParentLinks[%q] = %q, want %q", childID, pending, parentID)
	}

	// Step 2: Process an event for the parent session.
	parentLine := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Add(time.Second).Format(time.RFC3339Nano),
		"sessionId": parentID,
		"message": map[string]interface{}{
			"id":      "msg-parent-1",
			"role":    "assistant",
			"content": "parent response",
			"usage": map[string]interface{}{
				"input_tokens":  20,
				"output_tokens": 10,
			},
			"model": "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: parentID,
		Line:      parentLine,
		Bootstrap: true,
	})

	// After processing the parent, resolvePendingLinks should have fired and
	// set the child's ParentID.
	childSess, ok = sessions.Get(childID)
	if !ok {
		t.Fatal("child session not found after parent arrival")
	}
	if childSess.ParentID != parentID {
		t.Errorf("child ParentID = %q, want %q", childSess.ParentID, parentID)
	}

	// The parent should have the child recorded in Children.
	parentSess, ok := sessions.Get(parentID)
	if !ok {
		t.Fatal("parent session not found")
	}
	found := false
	for _, c := range parentSess.Children {
		if c == childID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("parent.Children does not contain childID %q; got %v", childID, parentSess.Children)
	}

	// Pending link should be cleared.
	p.linkMu.Lock()
	remaining := p.pendingParentLinks[childID]
	p.linkMu.Unlock()
	if remaining != "" {
		t.Errorf("pendingParentLinks[%q] should be cleared after resolution; got %q", childID, remaining)
	}
}

// TestParentLinking_DirectUUID verifies that when parentUuid directly matches
// an already-present session, the link is set immediately without deferral.
func TestParentLinking_DirectUUID(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-direct-uuid"
	const childID = "child-direct-uuid"
	ts := time.Now()

	// Step 1: Process the parent session first.
	parentLine := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Format(time.RFC3339Nano),
		"sessionId": parentID,
		"message": map[string]interface{}{
			"id":      "msg-parent-direct",
			"role":    "assistant",
			"content": "parent response",
			"usage": map[string]interface{}{
				"input_tokens":  20,
				"output_tokens": 10,
			},
			"model": "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: parentID,
		Line:      parentLine,
		Bootstrap: true,
	})

	// Step 2: Process child — parent is already in store, should link immediately.
	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   ts.Add(time.Second).Format(time.RFC3339Nano),
		"sessionId":   childID,
		"parentUuid":  parentID,
		"isSidechain": true,
		"message": map[string]interface{}{
			"id":      "msg-child-direct",
			"role":    "assistant",
			"content": "child response",
			"usage": map[string]interface{}{
				"input_tokens":  10,
				"output_tokens": 5,
			},
			"model": "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: childID,
		Line:      childLine,
		Bootstrap: true,
	})

	childSess, ok := sessions.Get(childID)
	if !ok {
		t.Fatal("child session not found")
	}
	if childSess.ParentID != parentID {
		t.Errorf("child ParentID = %q, want %q", childSess.ParentID, parentID)
	}

	// No deferred link should exist.
	p.linkMu.Lock()
	pending := p.pendingParentLinks[childID]
	p.linkMu.Unlock()
	if pending != "" {
		t.Errorf("no pending link expected; got %q", pending)
	}
}

// TestParentLinking_Shape2_InContentSessionID verifies a Task subagent (shape 2)
// is linked to its parent via the in-content sessionId, which on disk equals the
// PARENT session UUID while the watcher keys the row on the agent-<id> filename
// stem. parentUuid is absent (mirrors real disk), the parent is not yet in the
// store, and the link must still be set immediately (no deferral).
func TestParentLinking_Shape2_InContentSessionID(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-uuid-shape2"
	const childKey = "agent-aaa"
	ts := time.Now()

	// In-content sessionId IS the parent UUID; no parentUuid; isSidechain=true.
	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   ts.Format(time.RFC3339Nano),
		"sessionId":   parentID,
		"isSidechain": true,
		"message": map[string]interface{}{
			"id":      "msg-child-shape2",
			"role":    "assistant",
			"content": "child response",
			"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"model":   "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: childKey,
		FilePath:  "/home/user/.claude/projects/hash/" + parentID + "/subagents/" + childKey + ".jsonl",
		Line:      childLine,
		Bootstrap: true,
	})

	childSess, ok := sessions.Get(childKey)
	if !ok {
		t.Fatal("child session not found")
	}
	if childSess.ParentID != parentID {
		t.Errorf("child ParentID = %q, want %q (in-content sessionId)", childSess.ParentID, parentID)
	}

	// The in-content sessionId resolves the child's ParentID immediately even
	// before the parent arrives, but a deferred parent→child backlink must also
	// be recorded so parent.Children is backfilled once the parent shows up.
	// (parent.Children is only ever populated by LinkChild.)
	p.linkMu.Lock()
	pending := p.pendingParentLinks[childKey]
	p.linkMu.Unlock()
	if pending != parentID {
		t.Errorf("pendingParentLinks[%q] = %q, want %q (deferred backlink)", childKey, pending, parentID)
	}

	// Now process a parent line. resolvePendingLinks should fire and record the
	// child in parent.Children WITHOUT re-processing the child line.
	parentLine := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Add(time.Second).Format(time.RFC3339Nano),
		"sessionId": parentID,
		"message": map[string]interface{}{
			"id":      "msg-parent-shape2",
			"role":    "assistant",
			"content": "parent response",
			"usage":   map[string]interface{}{"input_tokens": 20, "output_tokens": 10},
			"model":   "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: parentID,
		FilePath:  "/home/user/.claude/projects/hash/" + parentID + ".jsonl",
		Line:      parentLine,
		Bootstrap: true,
	})

	parentSess, ok := sessions.Get(parentID)
	if !ok {
		t.Fatal("parent session not found")
	}
	found := false
	for _, c := range parentSess.Children {
		if c == childKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("parent.Children does not contain %q; got %v", childKey, parentSess.Children)
	}

	// The deferred link should be cleared after resolution.
	p.linkMu.Lock()
	remaining := p.pendingParentLinks[childKey]
	p.linkMu.Unlock()
	if remaining != "" {
		t.Errorf("pendingParentLinks[%q] should be cleared after resolution; got %q", childKey, remaining)
	}
}

// TestParentLinking_Shape3_Workflow verifies a workflow agent (shape 3) is linked
// to its parent via the in-content sessionId. Previously this regressed to "" both
// because parentUuid is absent on disk and because the path inference did not walk
// up past the wf_<id> directory. With P0-2 the in-content sessionId resolves it.
func TestParentLinking_Shape3_Workflow(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-uuid-shape3"
	const childKey = "agent-w1"
	ts := time.Now()

	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   ts.Format(time.RFC3339Nano),
		"sessionId":   parentID,
		"isSidechain": true,
		"message": map[string]interface{}{
			"id":      "msg-child-shape3",
			"role":    "assistant",
			"content": "workflow agent response",
			"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"model":   "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: childKey,
		FilePath:  "/home/user/.claude/projects/hash/" + parentID + "/subagents/workflows/wf_x/" + childKey + ".jsonl",
		Line:      childLine,
		Bootstrap: true,
	})

	childSess, ok := sessions.Get(childKey)
	if !ok {
		t.Fatal("child session not found")
	}
	if childSess.ParentID != parentID {
		t.Errorf("workflow agent ParentID = %q, want %q", childSess.ParentID, parentID)
	}
}

// TestParentLinking_InContent_ChildBeforeParent_BackfillsChildren is the
// regression guard for the out-of-order (child-before-parent) ordering that
// happens during bootstrap/replay: a finished subagent's lines may all be read
// BEFORE the parent's top-level <uuid>.jsonl line. The in-content sessionId sets
// the child's ParentID immediately, but parent.Children must ALSO be backfilled
// once the parent arrives — WITHOUT re-processing the child line (mirroring the
// TestParentLinking_DeferredUUID contract for the parentUuid path). Covers both
// shape 2 (task subagent) and shape 3 (workflow agent) file layouts.
func TestParentLinking_InContent_ChildBeforeParent_BackfillsChildren(t *testing.T) {
	cases := []struct {
		name     string
		childKey string
		childDir string // directory holding the agent JSONL, relative to the project hash dir
	}{
		{
			name:     "shape2_task_subagent",
			childKey: "agent-ooo2",
			childDir: "subagents",
		},
		{
			name:     "shape3_workflow_agent",
			childKey: "agent-ooo3",
			childDir: "subagents/workflows/wf_z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)
			sessions := session.NewStore()
			resolver := repo.NewResolver()

			p := New(sessions, db, resolver, nil)
			defer p.Stop()

			const parentID = "parent-uuid-ooo"
			ts := time.Now()

			childLine := makeJSONL(t, map[string]interface{}{
				"type":        "assistant",
				"timestamp":   ts.Format(time.RFC3339Nano),
				"sessionId":   parentID, // in-content sessionId == parent UUID on disk
				"isSidechain": true,
				"message": map[string]interface{}{
					"id":      "msg-" + tc.childKey,
					"role":    "assistant",
					"content": "agent response",
					"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
					"model":   "claude-sonnet-4-6",
				},
			})

			// Step 1: process the CHILD first; the parent session does not exist yet.
			p.Process(watcher.Event{
				SessionID: tc.childKey,
				FilePath:  "/home/user/.claude/projects/hash/" + parentID + "/" + tc.childDir + "/" + tc.childKey + ".jsonl",
				Line:      childLine,
				Bootstrap: true,
			})

			childSess, ok := sessions.Get(tc.childKey)
			if !ok {
				t.Fatal("child session not found")
			}
			if childSess.ParentID != parentID {
				t.Errorf("child ParentID = %q, want %q", childSess.ParentID, parentID)
			}

			// Step 2: process the PARENT line. Do NOT re-process the child.
			parentLine := makeJSONL(t, map[string]interface{}{
				"type":      "assistant",
				"timestamp": ts.Add(time.Second).Format(time.RFC3339Nano),
				"sessionId": parentID,
				"message": map[string]interface{}{
					"id":      "msg-parent-ooo",
					"role":    "assistant",
					"content": "parent response",
					"usage":   map[string]interface{}{"input_tokens": 20, "output_tokens": 10},
					"model":   "claude-sonnet-4-6",
				},
			})
			p.Process(watcher.Event{
				SessionID: parentID,
				FilePath:  "/home/user/.claude/projects/hash/" + parentID + ".jsonl",
				Line:      parentLine,
				Bootstrap: true,
			})

			// The parent's Children must now contain the child, backfilled by the
			// deferred link via resolvePendingLinks — no child re-processing.
			parentSess, ok := sessions.Get(parentID)
			if !ok {
				t.Fatal("parent session not found")
			}
			found := false
			for _, c := range parentSess.Children {
				if c == tc.childKey {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("parent.Children does not contain %q after parent arrival (no child re-process); got %v", tc.childKey, parentSess.Children)
			}

			// The deferred link should be cleared after resolution.
			p.linkMu.Lock()
			remaining := p.pendingParentLinks[tc.childKey]
			p.linkMu.Unlock()
			if remaining != "" {
				t.Errorf("pendingParentLinks[%q] should be cleared after resolution; got %q", tc.childKey, remaining)
			}
		})
	}
}

// TestParentLinking_TwoAgentsShareParent verifies that two distinct agents (one a
// task subagent, one a workflow agent) carrying the same in-content sessionId both
// link to the same parent session.
func TestParentLinking_TwoAgentsShareParent(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "shared-parent-uuid"
	ts := time.Now()

	mkChild := func(key string) []byte {
		return makeJSONL(t, map[string]interface{}{
			"type":        "assistant",
			"timestamp":   ts.Format(time.RFC3339Nano),
			"sessionId":   parentID,
			"isSidechain": true,
			"message": map[string]interface{}{
				"id":      "msg-" + key,
				"role":    "assistant",
				"content": "agent response",
				"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				"model":   "claude-sonnet-4-6",
			},
		})
	}

	p.Process(watcher.Event{
		SessionID: "agent-1",
		FilePath:  "/home/user/.claude/projects/hash/" + parentID + "/subagents/agent-1.jsonl",
		Line:      mkChild("agent-1"),
		Bootstrap: true,
	})
	p.Process(watcher.Event{
		SessionID: "agent-2",
		FilePath:  "/home/user/.claude/projects/hash/" + parentID + "/subagents/workflows/wf_y/agent-2.jsonl",
		Line:      mkChild("agent-2"),
		Bootstrap: true,
	})

	for _, key := range []string{"agent-1", "agent-2"} {
		sess, ok := sessions.Get(key)
		if !ok {
			t.Fatalf("session %q not found", key)
		}
		if sess.ParentID != parentID {
			t.Errorf("%q ParentID = %q, want %q", key, sess.ParentID, parentID)
		}
	}
}

// TestParentLinking_NoSelfParent verifies a sidechain line never self-parents: when
// the in-content sessionId equals ev.SessionID, ParentID stays empty and no deferred
// self-link is recorded — including when path inference would also yield ev.SessionID.
func TestParentLinking_NoSelfParent(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sameID = "same-id"
	ts := time.Now()

	line := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   ts.Format(time.RFC3339Nano),
		"sessionId":   sameID,
		"isSidechain": true,
		"message": map[string]interface{}{
			"id":      "msg-self",
			"role":    "assistant",
			"content": "response",
			"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"model":   "claude-sonnet-4-6",
		},
	})
	// FilePath path inference would also yield "same-id" (dir above subagents),
	// confirming the sid != ev.SessionID guard.
	p.Process(watcher.Event{
		SessionID: sameID,
		FilePath:  "/home/user/.claude/projects/hash/" + sameID + "/subagents/" + sameID + ".jsonl",
		Line:      line,
		Bootstrap: true,
	})

	sess, ok := sessions.Get(sameID)
	if !ok {
		t.Fatal("session not found")
	}
	if sess.ParentID != "" {
		t.Errorf("ParentID = %q, want empty (must never self-parent)", sess.ParentID)
	}
	p.linkMu.Lock()
	pending := p.pendingParentLinks[sameID]
	p.linkMu.Unlock()
	if pending != "" {
		t.Errorf("pendingParentLinks[%q] = %q, want empty (no self deferral)", sameID, pending)
	}
}

// TestProcess_NormalSessionUnaffected locks in that a normal (non-sidechain) session
// whose in-content sessionId equals ev.SessionID is completely unaffected by P0-2:
// no ParentID is set and no deferred link is recorded.
func TestProcess_NormalSessionUnaffected(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "normal-session"
	line := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"sessionId": sid,
		"message": map[string]interface{}{
			"id":      "msg-normal",
			"role":    "assistant",
			"content": "hello",
			"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
			"model":   "claude-sonnet-4-6",
		},
	})
	p.Process(watcher.Event{
		SessionID: sid,
		FilePath:  "/home/user/.claude/projects/hash/" + sid + ".jsonl",
		Line:      line,
		Bootstrap: true,
	})

	sess, ok := sessions.Get(sid)
	if !ok {
		t.Fatal("session not found")
	}
	if sess.ParentID != "" {
		t.Errorf("normal session ParentID = %q, want empty", sess.ParentID)
	}
	p.linkMu.Lock()
	pending := p.pendingParentLinks[sid]
	p.linkMu.Unlock()
	if pending != "" {
		t.Errorf("pendingParentLinks[%q] = %q, want empty", sid, pending)
	}
}

// TestApplyRepoResolution_UpgradesFallbackToGit verifies that a session whose first
// repo resolution was a non-git fallback is upgraded to an authoritative git repo
// when a later resolution arrives with FromGit=true and a different ID.
func TestApplyRepoResolution_UpgradesFallbackToGit(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "upgrade-session"
	fallback := &repo.Repo{ID: "my-project", Name: "my-project", FromGit: false}
	gitRepo := &repo.Repo{ID: "github.com/acme/my-project", Name: "my-project", URL: "git@github.com:acme/my-project.git", FromGit: true}

	// First resolution: non-git fallback.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, fallback)
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "my-project" {
		t.Fatalf("after fallback: RepoID = %q, want %q", sess.RepoID, "my-project")
	}
	if sess.RepoFromGit() {
		t.Error("after fallback: RepoFromGit() = true, want false")
	}

	// Second resolution: git-backed, different ID -> upgrade.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, gitRepo)
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "github.com/acme/my-project" {
		t.Errorf("after upgrade: RepoID = %q, want %q", sess.RepoID, "github.com/acme/my-project")
	}
	if !sess.RepoFromGit() {
		t.Error("after upgrade: RepoFromGit() = false, want true")
	}
}

// TestApplyRepoResolution_NoDowngradeOrThrash verifies that once a git-backed repo
// is set, a subsequent non-git fallback does NOT replace it, and an identical git
// resolution causes no spurious change.
func TestApplyRepoResolution_NoDowngradeOrThrash(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "stable-git-session"
	gitRepo := &repo.Repo{ID: "github.com/acme/widget", Name: "widget", URL: "https://github.com/acme/widget", FromGit: true}
	fallback := &repo.Repo{ID: "widget", Name: "widget", FromGit: false}

	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, gitRepo)
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" || !sess.RepoFromGit() {
		t.Fatalf("setup: RepoID=%q fromGit=%v", sess.RepoID, sess.RepoFromGit())
	}

	// A non-git fallback must NOT downgrade.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, fallback)
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" {
		t.Errorf("after fallback attempt: RepoID = %q, want unchanged %q", sess.RepoID, "github.com/acme/widget")
	}
	if !sess.RepoFromGit() {
		t.Error("after fallback attempt: RepoFromGit() flipped to false")
	}

	// An identical git resolution must cause no change (r.ID == s.RepoID guard).
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, gitRepo)
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" {
		t.Errorf("after identical git: RepoID = %q, want %q", sess.RepoID, "github.com/acme/widget")
	}
}

// TestApplyRepoResolution_RestartReupsertPreservesMetadata ties P1-1 and P1-2 at the
// pipeline layer: after a full git resolution is persisted, a restart-shaped re-upsert
// (a cache-hit Repo carrying only the ID, FromGit=false) must not blank the stored
// repo's Name/URL.
func TestApplyRepoResolution_RestartReupsertPreservesMetadata(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const id = "github.com/acme/widget"
	gitRepo := &repo.Repo{ID: id, Name: "widget", URL: "https://github.com/acme/widget", FromGit: true}

	sessions.Upsert("sess-a", func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, gitRepo)
	})

	// Simulate restart: a new session for a cwd whose cache entry only has the ID.
	cacheHit := &repo.Repo{ID: id} // empty Name/URL, FromGit=false
	sessions.Upsert("sess-b", func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, cacheHit)
	})

	rows, err := db.ListRepos()
	if err != nil {
		t.Fatalf("ListRepos failed: %v", err)
	}
	var found bool
	for _, r := range rows {
		if r.ID == id {
			found = true
			if r.Name != "widget" {
				t.Errorf("Name = %q, want %q after restart re-upsert", r.Name, "widget")
			}
			if r.URL != "https://github.com/acme/widget" {
				t.Errorf("URL = %q, want %q after restart re-upsert", r.URL, "https://github.com/acme/widget")
			}
		}
	}
	if !found {
		t.Fatalf("repo %q not found", id)
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
