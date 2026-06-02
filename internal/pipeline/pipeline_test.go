package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		// Nested subagents: the walk returns the FIRST (innermost) "subagents"
		// parent going up, so a subagent-of-a-subagent attributes to its immediate
		// agent parent, not the top-level session.
		{"/home/u/.claude/projects/hash/gp/subagents/agent-x/subagents/agent-y.jsonl", "agent-x"},
		// Degenerate root: a "subagents" segment directly under filesystem root.
		// filepath.Dir("/subagents") == "/" so the parent basename is "/" — locked
		// in to document the boundary (never produced by real Claude Code paths).
		{"/subagents/agent-y.jsonl", "/"},
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
	// AgentKind/AgentID are stamped by watcher.emit in production; in unit tests
	// Process receives the Event directly, so populate them with literal strings
	// (the pipeline package must not import the watcher kind constants).
	p.Process(watcher.Event{
		SessionID: childKey,
		FilePath:  "/home/user/.claude/projects/hash/" + parentID + "/subagents/" + childKey + ".jsonl",
		Line:      childLine,
		Bootstrap: true,
		AgentKind: "subagent",
		AgentID:   childKey,
	})

	childSess, ok := sessions.Get(childKey)
	if !ok {
		t.Fatal("child session not found")
	}
	if childSess.ParentID != parentID {
		t.Errorf("child ParentID = %q, want %q (in-content sessionId)", childSess.ParentID, parentID)
	}
	if childSess.AgentKind != "subagent" {
		t.Errorf("child AgentKind = %q, want subagent", childSess.AgentKind)
	}
	if childSess.AgentID != childKey {
		t.Errorf("child AgentID = %q, want %q", childSess.AgentID, childKey)
	}
	if childSess.WorkflowID != "" {
		t.Errorf("child WorkflowID = %q, want empty (shape 2 has no workflow)", childSess.WorkflowID)
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
	// Workflow identity is stamped by watcher.emit in production; populate the
	// Event fields with literal strings here (see shape-2 test note).
	p.Process(watcher.Event{
		SessionID:  childKey,
		FilePath:   "/home/user/.claude/projects/hash/" + parentID + "/subagents/workflows/wf_x/" + childKey + ".jsonl",
		Line:       childLine,
		Bootstrap:  true,
		AgentKind:  "workflow_agent",
		AgentID:    childKey,
		WorkflowID: "wf_x",
	})

	childSess, ok := sessions.Get(childKey)
	if !ok {
		t.Fatal("child session not found")
	}
	if childSess.ParentID != parentID {
		t.Errorf("workflow agent ParentID = %q, want %q", childSess.ParentID, parentID)
	}
	if childSess.AgentKind != "workflow_agent" {
		t.Errorf("workflow agent AgentKind = %q, want workflow_agent", childSess.AgentKind)
	}
	if childSess.AgentID != childKey {
		t.Errorf("workflow agent AgentID = %q, want %q", childSess.AgentID, childKey)
	}
	if childSess.WorkflowID != "wf_x" {
		t.Errorf("workflow agent WorkflowID = %q, want wf_x", childSess.WorkflowID)
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

// TestParentLinking_InContentBeatsParentUuid is the precedence guard for the REAL
// on-disk shape, where a sidechain line carries BOTH an in-content sessionId (the
// true parent session UUID) AND a parentUuid (an intra-file message-chain pointer
// that is NOT a session key). resolveParentID must prefer the in-content sessionId.
// The decoy parent named by parentUuid is pre-seeded into the store so that an
// (incorrect) parentUuid-first ordering WOULD resolve to it — if a future refactor
// moved the parentUuid lookup above the in-content branch, this test fails instead
// of silently re-breaking every real workflow's parent attribution.
func TestParentLinking_InContentBeatsParentUuid(t *testing.T) {
	cases := []struct {
		name     string
		childKey string
		childDir string
	}{
		{"shape2_task_subagent", "agent-prec2", "subagents"},
		{"shape3_workflow_agent", "agent-prec3", "subagents/workflows/wf_p"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)
			sessions := session.NewStore()
			resolver := repo.NewResolver()
			p := New(sessions, db, resolver, nil)
			defer p.Stop()

			const realParent = "real-parent-uuid"
			const decoyParent = "decoy-parent-uuid"
			ts := time.Now()

			// Pre-seed the decoy parent so a parentUuid-first lookup WOULD succeed.
			decoyLine := makeJSONL(t, map[string]interface{}{
				"type":      "assistant",
				"timestamp": ts.Format(time.RFC3339Nano),
				"sessionId": decoyParent,
				"message": map[string]interface{}{
					"id":      "msg-decoy",
					"role":    "assistant",
					"content": "decoy",
					"usage":   map[string]interface{}{"input_tokens": 5, "output_tokens": 5},
					"model":   "claude-sonnet-4-6",
				},
			})
			p.Process(watcher.Event{SessionID: decoyParent, Line: decoyLine, Bootstrap: true})

			// Child carries BOTH in-content sessionId (real parent) and parentUuid (decoy).
			childLine := makeJSONL(t, map[string]interface{}{
				"type":        "assistant",
				"timestamp":   ts.Add(time.Second).Format(time.RFC3339Nano),
				"sessionId":   realParent,
				"parentUuid":  decoyParent,
				"isSidechain": true,
				"message": map[string]interface{}{
					"id":      "msg-" + tc.childKey,
					"role":    "assistant",
					"content": "agent response",
					"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
					"model":   "claude-sonnet-4-6",
				},
			})
			p.Process(watcher.Event{
				SessionID: tc.childKey,
				FilePath:  "/home/user/.claude/projects/hash/" + realParent + "/" + tc.childDir + "/" + tc.childKey + ".jsonl",
				Line:      childLine,
				Bootstrap: true,
			})

			childSess, ok := sessions.Get(tc.childKey)
			if !ok {
				t.Fatal("child session not found")
			}
			if childSess.ParentID != realParent {
				t.Errorf("child ParentID = %q, want %q (in-content sessionId must beat parentUuid %q)", childSess.ParentID, realParent, decoyParent)
			}
		})
	}
}

// TestParentLinking_InContent_ParentBeforeChild covers the common live-tailing
// order: the parent <uuid>.jsonl is read BEFORE a sidechain agent line carrying
// the in-content sessionId. The parent already exists, so the child must link
// immediately, parent.Children must contain it, and no deferred link should
// linger. Mirrors TestParentLinking_DirectUUID but drives the in-content branch
// (ev.SessionID != msg.SessionID, no parentUuid).
func TestParentLinking_InContent_ParentBeforeChild(t *testing.T) {
	cases := []struct {
		name     string
		childKey string
		childDir string
	}{
		{"shape2_task_subagent", "agent-pbc2", "subagents"},
		{"shape3_workflow_agent", "agent-pbc3", "subagents/workflows/wf_q"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)
			sessions := session.NewStore()
			resolver := repo.NewResolver()
			p := New(sessions, db, resolver, nil)
			defer p.Stop()

			const parentID = "parent-pbc-uuid"
			ts := time.Now()

			// Step 1: parent arrives first.
			parentLine := makeJSONL(t, map[string]interface{}{
				"type":      "assistant",
				"timestamp": ts.Format(time.RFC3339Nano),
				"sessionId": parentID,
				"message": map[string]interface{}{
					"id":      "msg-parent-pbc",
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

			// Step 2: child sidechain line, in-content sessionId == parent, no parentUuid.
			childLine := makeJSONL(t, map[string]interface{}{
				"type":        "assistant",
				"timestamp":   ts.Add(time.Second).Format(time.RFC3339Nano),
				"sessionId":   parentID,
				"isSidechain": true,
				"message": map[string]interface{}{
					"id":      "msg-" + tc.childKey,
					"role":    "assistant",
					"content": "agent response",
					"usage":   map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
					"model":   "claude-sonnet-4-6",
				},
			})
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
				t.Errorf("parent.Children does not contain %q; got %v", tc.childKey, parentSess.Children)
			}

			// Parent already existed, so resolvePendingLinks must clear the deferred
			// backlink within the same Process call.
			p.linkMu.Lock()
			remaining := p.pendingParentLinks[tc.childKey]
			p.linkMu.Unlock()
			if remaining != "" {
				t.Errorf("pendingParentLinks[%q] should be cleared (parent already present); got %q", tc.childKey, remaining)
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
	// SAME repository, low confidence then high confidence: the start cwd's git
	// lookup failed (basename fallback), then a later event for the SAME directory
	// resolves the git remote. The fallback has no toplevel; same-repo evidence
	// comes from the session's start cwd living inside the git toplevel.
	const startCWD = "/work/my-project"
	fallback := &repo.Repo{ID: "my-project", Name: "my-project", FromGit: false}
	gitRepo := &repo.Repo{ID: "github.com/acme/my-project", Name: "my-project", URL: "git@github.com:acme/my-project.git", FromGit: true, Toplevel: startCWD}

	// First resolution: non-git fallback (also records the start cwd).
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: startCWD}, fallback, "")
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "my-project" {
		t.Fatalf("after fallback: RepoID = %q, want %q", sess.RepoID, "my-project")
	}
	if sess.RepoFromGit() {
		t.Error("after fallback: RepoFromGit() = true, want false")
	}

	// Second resolution: git-backed for the SAME directory -> upgrade.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: startCWD}, gitRepo, "")
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
		p.applyRepoResolution(s, &parser.Event{}, gitRepo, "")
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" || !sess.RepoFromGit() {
		t.Fatalf("setup: RepoID=%q fromGit=%v", sess.RepoID, sess.RepoFromGit())
	}

	// A non-git fallback must NOT downgrade.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, fallback, "")
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
		p.applyRepoResolution(s, &parser.Event{}, gitRepo, "")
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" {
		t.Errorf("after identical git: RepoID = %q, want %q", sess.RepoID, "github.com/acme/widget")
	}
}

// TestApplyRepoResolution_RemoteUpgradesToplevel verifies the provenance ranking
// (remote > toplevel > fallback): a git toplevel-basename resolution (FromGit, no
// URL) is upgraded when a git remote-origin resolution (FromGit, with URL) arrives
// for the same session, and the reverse never downgrades. Guards the review
// finding that a binary FromGit flag let a toplevel resolution permanently shadow
// a stronger remote one for the same cwd-set.
func TestApplyRepoResolution_RemoteUpgradesToplevel(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "remote-upgrade-session"
	// Same checkout, resolved two ways: the toplevel-basename id and the
	// remote-origin id share a git working-tree root, which is the same-repo
	// evidence that permits the in-place upgrade.
	const top = "/work/widget"
	toplevel := &repo.Repo{ID: "widget", Name: "widget", FromGit: true, Toplevel: top} // git toplevel basename, no remote URL
	remote := &repo.Repo{ID: "github.com/acme/widget", Name: "widget", URL: "git@github.com:acme/widget.git", FromGit: true, Toplevel: top}

	// First: git toplevel basename (rank = SourceGitToplevel).
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, toplevel, "")
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "widget" || sess.RepoSourceRank() != repo.SourceGitToplevel {
		t.Fatalf("after toplevel: RepoID=%q rank=%d, want widget/%d", sess.RepoID, sess.RepoSourceRank(), repo.SourceGitToplevel)
	}

	// Then: git remote origin (rank = SourceGitRemote) -> must upgrade.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, remote, "")
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" {
		t.Errorf("after remote: RepoID = %q, want github.com/acme/widget (remote should upgrade toplevel)", sess.RepoID)
	}
	if sess.RepoSourceRank() != repo.SourceGitRemote {
		t.Errorf("after remote: rank = %d, want %d", sess.RepoSourceRank(), repo.SourceGitRemote)
	}

	// A later toplevel resolution must NOT downgrade the remote.
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, &repo.Repo{ID: "widget", Name: "widget", FromGit: true, Toplevel: top}, "")
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "github.com/acme/widget" {
		t.Errorf("toplevel must not downgrade remote: RepoID = %q, want github.com/acme/widget", sess.RepoID)
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
		p.applyRepoResolution(s, &parser.Event{}, gitRepo, "")
	})

	// Simulate restart: a new session for a cwd whose cache entry only has the ID.
	cacheHit := &repo.Repo{ID: id} // empty Name/URL, FromGit=false
	sessions.Upsert("sess-b", func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, cacheHit, "")
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

// TestApplyRepoResolution_StartPin_NoFlipToDifferentRepo verifies the start-pin
// rule: a session that starts in project A (resolved at a LOW rank) must NOT have
// its repo_id flipped to a DIFFERENT project B even when B resolves at a strictly
// HIGHER source rank (e.g. the run cd's into another checkout whose git remote
// resolves). The project stays pinned to where the session started.
func TestApplyRepoResolution_StartPin_NoFlipToDifferentRepo(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "startpin-session"
	// Start: project A resolved at the LOW git-toplevel rank (remote lookup timed out).
	projectA := &repo.Repo{ID: "alpha", Name: "alpha", FromGit: true, Toplevel: "/work/alpha"}
	// Later: a DIFFERENT project B at a HIGHER rank (git remote, with URL),
	// resolved from a cwd inside B's own (different) working tree.
	projectB := &repo.Repo{ID: "github.com/acme/beta", Name: "beta", URL: "git@github.com:acme/beta.git", FromGit: true, Toplevel: "/work/beta"}

	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: "/work/alpha"}, projectA, "")
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "alpha" {
		t.Fatalf("after start: RepoID = %q, want alpha", sess.RepoID)
	}

	// Higher-rank, DIFFERENT repo -> must be ignored (start-pin).
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: "/work/beta"}, projectB, "")
	})
	sess, _ = sessions.Get(sid)
	if sess.RepoID != "alpha" {
		t.Errorf("start-pin violated: RepoID = %q, want alpha (must not flip to higher-rank different repo beta)", sess.RepoID)
	}
	if sess.RepoSourceRank() != repo.SourceGitToplevel {
		t.Errorf("rank changed: got %d, want %d (no upgrade across repos)", sess.RepoSourceRank(), repo.SourceGitToplevel)
	}
}

// TestApplyRepoResolution_StartPin_UpgradesSameRepoSubdir verifies the legitimate
// upgrade is preserved across a parent/child directory relationship: starting in a
// subdir (toplevel /work/mono/pkg) then a higher-rank remote resolution for the
// repo root (/work/mono) is recognised as the SAME repo and upgrades in place.
func TestApplyRepoResolution_StartPin_UpgradesSameRepoSubdir(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "subdir-upgrade-session"
	// Start: toplevel basename, working-tree root is a nested path.
	start := &repo.Repo{ID: "mono", Name: "mono", FromGit: true, Toplevel: "/work/mono"}
	// Later: remote origin whose toplevel is the same root.
	remote := &repo.Repo{ID: "github.com/acme/mono", Name: "mono", URL: "git@github.com:acme/mono.git", FromGit: true, Toplevel: "/work/mono"}

	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: "/work/mono/pkg"}, start, "")
	})
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: "/work/mono"}, remote, "")
	})
	sess, _ := sessions.Get(sid)
	if sess.RepoID != "github.com/acme/mono" {
		t.Errorf("same-repo upgrade failed: RepoID = %q, want github.com/acme/mono", sess.RepoID)
	}
}

// TestApplyRepoResolution_CWDStaysAtStart verifies the displayed cwd reflects
// session START and is never overwritten by a later event's cwd (e.g. after a cd),
// so a card's directory cannot drift away from its pinned project.
func TestApplyRepoResolution_CWDStaysAtStart(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "cwd-start-session"
	r := &repo.Repo{ID: "alpha", Name: "alpha", FromGit: true, Toplevel: "/work/alpha"}

	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: "/work/alpha"}, r, "")
	})
	sessions.Upsert(sid, func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{CWD: "/work/beta/deep/subdir"}, r, "")
	})
	sess, _ := sessions.Get(sid)
	if sess.CWD != "/work/alpha" {
		t.Errorf("CWD drifted: got %q, want /work/alpha (start cwd, never overwritten)", sess.CWD)
	}
}

// TestProcess_ChildInheritsParentRepo is an end-to-end check that a subagent
// running in a git worktree inherits its PARENT's repo_id instead of minting a
// phantom "agent-<hash>" repo from its own worktree directory basename.
func TestProcess_ChildInheritsParentRepo(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-session"
	const childID = "agent-deadbeefcafef00d" // worktree-style stem

	// Seed the parent's repo_id directly so resolution is deterministic and does
	// not shell out to git. The pipeline looks this up via the live session store.
	sessions.Upsert(parentID, func(s *session.Session) {
		s.RepoID = "github.com/acme/widget"
		s.SetRepoSourceRank(repo.SourceGitRemote)
	})

	// Child event from a worktree whose basename would otherwise resolve to a
	// phantom "agent-deadbeef" repo. isSidechain + in-content sessionId names the
	// parent (resolveParentID shape-2 path).
	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   time.Now().Format(time.RFC3339Nano),
		"sessionId":   parentID, // in-content sessionId names the true parent
		"isSidechain": true,
		"cwd":         "/work/.worktrees/agent-deadbeefcafef00d",
		"message": map[string]interface{}{
			"id":          "cmsg-1",
			"role":        "assistant",
			"content":     "child work",
			"stop_reason": "end_turn",
		},
	})

	p.Process(watcher.Event{SessionID: childID, Line: childLine})

	child, ok := sessions.Get(childID)
	if !ok {
		t.Fatal("child session not found")
	}
	if child.ParentID != parentID {
		t.Fatalf("child ParentID = %q, want %q", child.ParentID, parentID)
	}
	if child.RepoID != "github.com/acme/widget" {
		t.Errorf("child RepoID = %q, want inherited parent repo github.com/acme/widget", child.RepoID)
	}
	if strings.Contains(child.RepoID, "agent-") {
		t.Errorf("child RepoID = %q is a phantom agent-* repo; should inherit parent", child.RepoID)
	}
}

// TestProcess_ChildTracksParentRepoChange guards the SYNCHRONOUS inheritance path
// (parent already in the live store when the child event arrives) and, in
// particular, that Rule 1 is NOT set-once: the child mirrors the parent's CURRENT
// repo_id on every event. A redundant event must not thrash the pin back to the
// child's own worktree cwd, and a later same-repo QUALITY upgrade on the parent
// (toplevel-basename id → git-remote id) must propagate to the child.
func TestProcess_ChildTracksParentRepoChange(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "track-parent-session"
	const childID = "agent-1234567890abcdef" // worktree-style stem

	// Parent starts pinned to a low-authority toplevel-basename id.
	sessions.Upsert(parentID, func(s *session.Session) {
		s.RepoID = "widget"
		s.SetRepoSourceRank(repo.SourceGitToplevel)
	})

	// A child event from the worktree, naming the parent via in-content sessionId
	// + isSidechain (resolveParentID shape-2). Distinct message ids avoid dedup.
	childEvent := func(msgID string) watcher.Event {
		line := makeJSONL(t, map[string]interface{}{
			"type":        "assistant",
			"timestamp":   time.Now().Format(time.RFC3339Nano),
			"sessionId":   parentID,
			"isSidechain": true,
			"cwd":         "/work/.worktrees/agent-1234567890abcdef",
			"message": map[string]interface{}{
				"id":          msgID,
				"role":        "assistant",
				"content":     "child work",
				"stop_reason": "end_turn",
			},
		})
		return watcher.Event{SessionID: childID, Line: line}
	}

	// Event 1: child inherits the parent's current pin synchronously.
	p.Process(childEvent("track-c1"))
	child, ok := sessions.Get(childID)
	if !ok {
		t.Fatal("child session not found")
	}
	if child.RepoID != "widget" {
		t.Fatalf("after first event: child RepoID = %q, want inherited widget", child.RepoID)
	}
	if strings.Contains(child.RepoID, "agent-") {
		t.Fatalf("child RepoID = %q is a phantom worktree repo; should inherit parent", child.RepoID)
	}

	// Event 2: parent unchanged → child stays pinned and does NOT thrash back to
	// its own worktree cwd phantom.
	p.Process(childEvent("track-c2"))
	child, _ = sessions.Get(childID)
	if child.RepoID != "widget" {
		t.Errorf("after redundant event: child RepoID = %q, want still widget (no thrash)", child.RepoID)
	}

	// Parent's own pin is upgraded for the SAME checkout (basename → git-remote).
	sessions.Upsert(parentID, func(s *session.Session) {
		s.RepoID = "github.com/acme/widget"
		s.SetRepoSourceRank(repo.SourceGitRemote)
	})

	// Event 3: child tracks the parent's CURRENT id (Rule 1 is not set-once).
	p.Process(childEvent("track-c3"))
	child, _ = sessions.Get(childID)
	if child.RepoID != "github.com/acme/widget" {
		t.Errorf("after parent upgrade: child RepoID = %q, want tracked github.com/acme/widget", child.RepoID)
	}
}

// TestProcess_ChildInheritsParentRepoFromDB verifies the inheritance falls back to
// the persisted sessions table when the parent is not in the live store (e.g. the
// parent was flushed in an earlier run or replayed out of order).
func TestProcess_ChildInheritsParentRepoFromDB(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "db-parent-session"
	const childID = "agent-feedface12345678"

	// Persist the parent to the DB ONLY (absent from the live store).
	if err := db.SaveSession(&session.Session{ID: parentID, RepoID: "github.com/acme/gadget"}); err != nil {
		t.Fatalf("SaveSession parent: %v", err)
	}

	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   time.Now().Format(time.RFC3339Nano),
		"sessionId":   parentID,
		"isSidechain": true,
		"cwd":         "/work/.worktrees/agent-feedface12345678",
		"message": map[string]interface{}{
			"id":          "cmsg-2",
			"role":        "assistant",
			"content":     "child work",
			"stop_reason": "end_turn",
		},
	})

	p.Process(watcher.Event{SessionID: childID, Line: childLine})

	child, ok := sessions.Get(childID)
	if !ok {
		t.Fatal("child session not found")
	}
	if child.RepoID != "github.com/acme/gadget" {
		t.Errorf("child RepoID = %q, want inherited-from-DB parent repo github.com/acme/gadget", child.RepoID)
	}
}

// TestProcess_ChildBeforeParent_BackfillsRepo verifies the child-before-parent
// ordering: a subagent whose event arrives BEFORE its parent's first resolves its
// own (worktree) cwd to a phantom repo, but once the parent arrives the deferred
// link wiring re-points the child at the parent's project.
func TestProcess_ChildBeforeParent_BackfillsRepo(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "bp-parent-uuid"
	const childID = "agent-00ff00ff00ff00ff"
	ts := time.Now()

	// Pre-seed the child with a phantom repo to simulate its own-cwd resolution
	// having already happened (the parent was unknown at that point). Use the
	// deferred-link path via parentUuid so the child's ParentID is NOT set yet.
	sessions.Upsert(childID, func(s *session.Session) {
		s.RepoID = "agent-00ff00ff" // phantom worktree repo
		s.SetRepoSourceRank(repo.SourceGitToplevel)
	})

	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   ts.Format(time.RFC3339Nano),
		"sessionId":   childID,
		"parentUuid":  parentID,
		"isSidechain": true,
		"message": map[string]interface{}{
			"id":          "bp-cmsg-1",
			"role":        "assistant",
			"content":     "child",
			"stop_reason": "end_turn",
		},
	})
	p.Process(watcher.Event{SessionID: childID, Line: childLine, Bootstrap: true})

	// Parent not present yet -> link deferred, child keeps its phantom repo.
	child, _ := sessions.Get(childID)
	if child.ParentID != "" {
		t.Fatalf("child ParentID should be deferred (empty); got %q", child.ParentID)
	}

	// Parent arrives with a real repo.
	sessions.Upsert(parentID, func(s *session.Session) {
		s.RepoID = "github.com/acme/widget"
		s.SetRepoSourceRank(repo.SourceGitRemote)
	})
	parentLine := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"timestamp": ts.Add(time.Second).Format(time.RFC3339Nano),
		"sessionId": parentID,
		"message": map[string]interface{}{
			"id":          "bp-pmsg-1",
			"role":        "assistant",
			"content":     "parent",
			"stop_reason": "end_turn",
		},
	})
	p.Process(watcher.Event{SessionID: parentID, Line: parentLine, Bootstrap: true})

	child, _ = sessions.Get(childID)
	if child.ParentID != parentID {
		t.Fatalf("child ParentID = %q, want %q after parent arrival", child.ParentID, parentID)
	}
	if child.RepoID != "github.com/acme/widget" {
		t.Errorf("child RepoID = %q, want back-filled parent repo github.com/acme/widget", child.RepoID)
	}
}

// TestSameRepo verifies the same-repo decision used to gate start-pin upgrades.
func TestSameRepo(t *testing.T) {
	mk := func(top string) *repo.Repo { return &repo.Repo{Toplevel: top} }
	cases := []struct {
		name       string
		pinnedTop  string
		startCWD   string
		incoming   *repo.Repo
		newCWD     string
		want       bool
	}{
		{"identical toplevels", "/work/widget", "", mk("/work/widget"), "", true},
		// Direction guard: a narrower incoming root nested inside the pin is the
		// same (or a more-specific) checkout and may upgrade; a wider incoming
		// root that CONTAINS the pin is the enclosing monorepo and must NOT let
		// the inner pin flip outward.
		{"incoming nested within pin (narrower) -> same", "/work/mono", "", mk("/work/mono/pkg"), "", true},
		{"incoming contains pin (outer monorepo) -> different", "/work/mono/pkg", "", mk("/work/mono"), "", false},
		{"different repos", "/work/alpha", "", mk("/work/beta"), "", false},
		{"prefix-but-not-path (/repo vs /repo2)", "/work/repo", "", mk("/work/repo2"), "", false},
		{"incoming has no toplevel", "/work/widget", "", mk(""), "", false},
		{"fallback pin, start cwd inside incoming toplevel", "", "/work/widget/src", mk("/work/widget"), "", true},
		{"fallback pin, start cwd outside incoming toplevel", "", "/elsewhere/foo", mk("/work/widget"), "", false},
		{"fallback pin, no start cwd, new cwd inside toplevel", "", "", mk("/work/widget"), "/work/widget/cmd", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &session.Session{CWD: tc.startCWD}
			s.SetRepoToplevel(tc.pinnedTop)
			if got := sameRepo(s, tc.incoming, tc.newCWD); got != tc.want {
				t.Errorf("sameRepo = %v, want %v", got, tc.want)
			}
		})
	}
}
