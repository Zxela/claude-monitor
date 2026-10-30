package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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
	resolver := repo.NewResolver(repo.NewFakeGit())

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
			"id":      "msg-1",
			"role":    "assistant",
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
	resolver := repo.NewResolver(repo.NewFakeGit())

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

func TestFlush_PersistsEvents(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver(repo.NewFakeGit())

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
	resolver := repo.NewResolver(repo.NewFakeGit())
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
				"id":      msgID,
				"role":    "assistant",
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
			"id":      "msg-aaa",
			"role":    "assistant",
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
	resolver := repo.NewResolver(repo.NewFakeGit())

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-session-uuid"
	const childID = "child-session-uuid"
	ts := time.Now()

	// Step 1: Process a child event that references the parent via parentUuid.
	// The parent session does NOT exist yet in the store.
	childLine := makeJSONL(t, map[string]interface{}{
		"type":        "assistant",
		"timestamp":   ts.Format(time.RFC3339Nano),
		"sessionId":   childID,
		"parentUuid":  parentID,
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
	resolver := repo.NewResolver(repo.NewFakeGit())

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
	resolver := repo.NewResolver(repo.NewFakeGit())

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
	resolver := repo.NewResolver(repo.NewFakeGit())

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
			resolver := repo.NewResolver(repo.NewFakeGit())

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
			resolver := repo.NewResolver(repo.NewFakeGit())
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
			resolver := repo.NewResolver(repo.NewFakeGit())
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
	resolver := repo.NewResolver(repo.NewFakeGit())

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
	resolver := repo.NewResolver(repo.NewFakeGit())

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
	resolver := repo.NewResolver(repo.NewFakeGit())

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

// TestApplyRepoResolution_RestartReupsertPreservesMetadata ties P1-1 and P1-2 at the
// pipeline layer: after a full git resolution is persisted, a restart-shaped re-upsert
// (a cache-hit Repo carrying no Name or URL) must not blank the stored
// repo's Name/URL.
func TestApplyRepoResolution_RestartReupsertPreservesMetadata(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver(repo.NewFakeGit())

	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const id = "github.com/acme/widget"
	gitRepo := &repo.Repo{ID: id, Name: "widget", URL: "https://github.com/acme/widget", Rank: repo.SourceGitRemote}

	sessions.Upsert("sess-a", func(s *session.Session) {
		p.applyRepoResolution(s, &parser.Event{}, gitRepo, "")
	})

	// Simulate restart: a new session for a cwd whose cache entry only has the ID.
	cacheHit := &repo.Repo{ID: id} // empty Name/URL
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
	resolver := repo.NewResolver(repo.NewFakeGit())

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

// TestApplyRepoResolution_CWDStaysAtStart verifies the displayed cwd reflects
// session START and is never overwritten by a later event's cwd (e.g. after a cd),
// so a card's directory cannot drift away from its pinned project.
func TestApplyRepoResolution_CWDStaysAtStart(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	resolver := repo.NewResolver(repo.NewFakeGit())
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const sid = "cwd-start-session"
	r := &repo.Repo{ID: "alpha", Name: "alpha", Rank: repo.SourceGitToplevel, Toplevel: "/work/alpha"}

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
	resolver := repo.NewResolver(repo.NewFakeGit())
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "parent-session"
	const childID = "agent-deadbeefcafef00d" // worktree-style stem

	// Seed the parent's repo_id directly so resolution is deterministic and does
	// not shell out to git. The pipeline looks this up via the live session store.
	sessions.Upsert(parentID, func(s *session.Session) {
		s.SetRepoPin(repo.Pin{ID: "github.com/acme/widget", Rank: repo.SourceGitRemote})
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
	resolver := repo.NewResolver(repo.NewFakeGit())
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "track-parent-session"
	const childID = "agent-1234567890abcdef" // worktree-style stem

	// Parent starts pinned to a low-authority toplevel-basename id.
	sessions.Upsert(parentID, func(s *session.Session) {
		s.SetRepoPin(repo.Pin{ID: "widget", Rank: repo.SourceGitToplevel})
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
		s.SetRepoPin(repo.Pin{ID: "github.com/acme/widget", Rank: repo.SourceGitRemote})
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
	resolver := repo.NewResolver(repo.NewFakeGit())
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "db-parent-session"
	const childID = "agent-feedface12345678"

	// Persist the parent to the DB ONLY (absent from the live store).
	if err := db.FlushSessions([]*session.Session{{ID: parentID, RepoID: "github.com/acme/gadget"}}); err != nil {
		t.Fatalf("FlushSessions parent: %v", err)
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
	resolver := repo.NewResolver(repo.NewFakeGit())
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	const parentID = "bp-parent-uuid"
	const childID = "agent-00ff00ff00ff00ff"
	ts := time.Now()

	// Pre-seed the child with a phantom repo to simulate its own-cwd resolution
	// having already happened (the parent was unknown at that point). Use the
	// deferred-link path via parentUuid so the child's ParentID is NOT set yet.
	sessions.Upsert(childID, func(s *session.Session) {
		s.SetRepoPin(repo.Pin{ID: "agent-00ff00ff", Rank: repo.SourceGitToplevel}) // phantom worktree repo
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
		s.SetRepoPin(repo.Pin{ID: "github.com/acme/widget", Rank: repo.SourceGitRemote})
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

// A Session starting in a cwd resolved before a restart must stay pinned to
// that cwd's repo when it later runs in an enclosing repo.
func TestProcess_CachedCwdKeepsStartPinAcrossRestart(t *testing.T) {
	db := openTestDB(t)
	git := repo.NewFakeGit().
		AddCheckout("/w/mono", "git@github.com:acme/mono.git").
		AddCheckout("/w/mono/inner", "git@github.com:acme/inner.git")
	line := func(sid, msgID, cwd string) watcher.Event {
		return watcher.Event{SessionID: sid, Line: makeJSONL(t, map[string]interface{}{
			"type":      "assistant",
			"timestamp": time.Now().Format(time.RFC3339Nano),
			"sessionId": sid,
			"cwd":       cwd,
			"message":   map[string]interface{}{"id": msgID, "role": "assistant", "content": "x", "stop_reason": "end_turn"},
		})}
	}

	first := New(session.NewStore(), db, repo.NewResolver(git), nil)
	first.Process(line("before-restart", "m1", "/w/mono/inner"))
	first.Stop()

	cached, err := db.LoadCwdRepos()
	if err != nil {
		t.Fatal(err)
	}
	resolver := repo.NewResolver(git)
	resolver.LoadCache(cached)
	sessions := session.NewStore()
	p := New(sessions, db, resolver, nil)
	defer p.Stop()

	p.Process(line("after-restart", "m2", "/w/mono/inner"))
	p.Process(line("after-restart", "m3", "/w/mono"))

	s, _ := sessions.Get("after-restart")
	if s.RepoID != "github.com/acme/inner" {
		t.Errorf("RepoID = %q, want github.com/acme/inner (start pin)", s.RepoID)
	}
}

// TestProcess_RebuildErrorDedupFromDB verifies that error_count does not
// inflate when the JSONL is replayed after a restart: the "err:" dedup keys
// are reseeded from persisted error events (LoadErrorDedup), so the bootstrap
// replay of an already-counted error line must not re-increment ErrorCount.
func TestProcess_RebuildErrorDedupFromDB(t *testing.T) {
	db := openTestDB(t)
	resolver := repo.NewResolver(repo.NewFakeGit())
	const sid = "err-dedup-rebuild-test"
	ts := time.Now()

	errorLine := func(uuid string) []byte {
		return makeJSONL(t, map[string]interface{}{
			"type":      "user",
			"uuid":      uuid,
			"timestamp": ts.Format(time.RFC3339Nano),
			"sessionId": sid,
			"message": map[string]interface{}{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type":        "tool_result",
						"tool_use_id": "toolu_" + uuid,
						"is_error":    true,
						"content":     "Error: command failed",
					},
				},
			},
		})
	}

	// An assistant message with a message ID, so the restart-rebuild path
	// (which only fires when LoadMessageCosts returns entries) actually runs —
	// that is the real-world shape where the inflation occurred.
	assistantLine := makeJSONL(t, map[string]interface{}{
		"type":      "assistant",
		"uuid":      "uuid-asst-1",
		"timestamp": ts.Format(time.RFC3339Nano),
		"sessionId": sid,
		"message": map[string]interface{}{
			"id":      "msg-err-test",
			"role":    "assistant",
			"content": "working on it",
			"usage": map[string]interface{}{
				"input_tokens":  100,
				"output_tokens": 50,
			},
			"model":       "claude-sonnet-4-6",
			"stop_reason": "end_turn",
		},
	})

	// --- Phase 1: count one error and persist. ---
	sessions1 := session.NewStore()
	p1 := New(sessions1, db, resolver, nil)
	p1.Process(watcher.Event{SessionID: sid, Line: assistantLine, Bootstrap: true})
	p1.Process(watcher.Event{SessionID: sid, Line: errorLine("uuid-err-1"), Bootstrap: true})
	p1.Stop() // flushes to DB

	sess1, ok := sessions1.Get(sid)
	if !ok {
		t.Fatal("session not found after phase 1")
	}
	if sess1.ErrorCount != 1 {
		t.Fatalf("phase 1 ErrorCount = %d, want 1", sess1.ErrorCount)
	}

	// --- Phase 2: restart — fresh store, same DB, full JSONL replay. ---
	sessions2 := session.NewStore()
	p2 := New(sessions2, db, resolver, nil)
	defer p2.Stop()
	p2.Process(watcher.Event{SessionID: sid, Line: assistantLine, Bootstrap: true})
	p2.Process(watcher.Event{SessionID: sid, Line: errorLine("uuid-err-1"), Bootstrap: true})

	sess2, ok := sessions2.Get(sid)
	if !ok {
		t.Fatal("session not found after phase 2")
	}
	if sess2.ErrorCount != 1 {
		t.Errorf("ErrorCount after replay = %d, want 1 (err: dedup keys must survive restart)", sess2.ErrorCount)
	}

	// A genuinely new error after the restart must still count.
	p2.Process(watcher.Event{SessionID: sid, Line: errorLine("uuid-err-2"), Bootstrap: true})
	sess2, _ = sessions2.Get(sid)
	if sess2.ErrorCount != 2 {
		t.Errorf("ErrorCount after new error = %d, want 2", sess2.ErrorCount)
	}
}

// TestProcess_RebuildErrorDedupFromDB_ErrorsOnlySession covers the shape the
// first fix missed: a session whose ONLY persisted events are errors (e.g. an
// agent that crashed before its first costed turn) has no message-id'd rows,
// so the rebuild must be gated on the DB session row, not on the message
// dedup map being non-empty.
func TestProcess_RebuildErrorDedupFromDB_ErrorsOnlySession(t *testing.T) {
	db := openTestDB(t)
	resolver := repo.NewResolver(repo.NewFakeGit())
	const sid = "err-only-rebuild-test"
	ts := time.Now()

	errorLine := makeJSONL(t, map[string]interface{}{
		"type":      "user",
		"uuid":      "uuid-only-err",
		"timestamp": ts.Format(time.RFC3339Nano),
		"sessionId": sid,
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": "toolu_only",
					"is_error":    true,
					"content":     "Error: crashed before first turn",
				},
			},
		},
	})

	sessions1 := session.NewStore()
	p1 := New(sessions1, db, resolver, nil)
	p1.Process(watcher.Event{SessionID: sid, Line: errorLine, Bootstrap: true})
	p1.Stop()

	sess1, _ := sessions1.Get(sid)
	if sess1 == nil || sess1.ErrorCount != 1 {
		t.Fatalf("phase 1 ErrorCount = %v, want 1", sess1)
	}

	// Restart and replay the same line.
	sessions2 := session.NewStore()
	p2 := New(sessions2, db, resolver, nil)
	defer p2.Stop()
	p2.Process(watcher.Event{SessionID: sid, Line: errorLine, Bootstrap: true})

	sess2, ok := sessions2.Get(sid)
	if !ok {
		t.Fatal("session not found after replay")
	}
	if sess2.ErrorCount != 1 {
		t.Errorf("ErrorCount after replay = %d, want 1 (errors-only session must rebuild err: keys)", sess2.ErrorCount)
	}
}

func TestProcess_IgnoresInternalLines(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	var broadcasts int
	p := New(sessions, db, repo.NewResolver(repo.NewFakeGit()), func(*parser.Event, *session.Session, bool, bool) {
		broadcasts++
	})
	defer p.Stop()

	p.Process(watcher.Event{SessionID: "s", Line: makeJSONL(t, map[string]interface{}{
		"type": "assistant", "sessionId": "s", "uuid": "u1",
		"timestamp": time.Now().Format(time.RFC3339Nano),
		"message":   map[string]interface{}{"id": "m1", "role": "assistant", "content": "hi", "stop_reason": "end_turn"},
	})})
	p.Process(watcher.Event{SessionID: "s", Line: makeJSONL(t, map[string]interface{}{
		"type": "api-request-blob", "sessionId": "s", "hash": "h",
		"message": map[string]interface{}{"role": "user", "content": []map[string]interface{}{
			{"type": "tool_result", "tool_use_id": "t1", "is_error": true, "content": "boom"},
		}},
	})})
	p.Process(watcher.Event{SessionID: "s", Line: makeJSONL(t, map[string]interface{}{
		"type": "mode", "sessionId": "s", "mode": "default",
	})})

	sess, _ := sessions.Get("s")
	if sess.EventCount != 1 || sess.MessageCount != 1 || sess.ErrorCount != 0 {
		t.Errorf("counts = events %d, messages %d, errors %d; want 1, 1, 0",
			sess.EventCount, sess.MessageCount, sess.ErrorCount)
	}
	if sess.Status != "waiting" {
		t.Errorf("Status = %q, want waiting (blob must not flip it)", sess.Status)
	}
	if broadcasts != 1 {
		t.Errorf("broadcasts = %d, want 1", broadcasts)
	}
}

func TestProcess_KeylessLinesGetOwnRows(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	defer p.Stop()

	lines := [][]byte{
		[]byte(`{"type":"queue-operation","operation":"enqueue","content":"first","sessionId":"s","timestamp":"2026-10-01T00:00:00Z"}`),
		[]byte(`{"type":"queue-operation","operation":"enqueue","content":"second","sessionId":"s","timestamp":"2026-10-01T00:00:01Z"}`),
	}
	// Process each twice: a re-read must upsert, not duplicate.
	for range 2 {
		for _, l := range lines {
			p.Process(watcher.Event{SessionID: "s", Line: l, Bootstrap: true})
		}
		p.flush()
	}

	events, err := db.ListEvents("s", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d rows, want 2 (one per distinct keyless line)", len(events))
	}
}

func TestProcess_SessionMetadataLines(t *testing.T) {
	db := openTestDB(t)
	sessions := session.NewStore()
	var details, updates int
	p := New(sessions, db, repo.NewResolver(repo.NewFakeGit()), func(_ *parser.Event, _ *session.Session, _, sendDetail bool) {
		if sendDetail {
			details++
		} else {
			updates++
		}
	})
	defer p.Stop()

	lines := []string{
		`{"type":"assistant","sessionId":"s","uuid":"u1","timestamp":"2026-10-07T22:00:00Z","message":{"id":"m1","role":"assistant","content":"hi","stop_reason":"end_turn"}}`,
		`{"type":"ai-title","aiTitle":"Multi-repo support","sessionId":"s"}`,
		`{"type":"pr-link","sessionId":"s","prNumber":7,"prUrl":"https://old.example/7","prRepository":"g/r","timestamp":"2026-10-07T22:07:43Z"}`,
		`{"type":"pr-link","sessionId":"s","prNumber":8,"prUrl":"https://example/8","prRepository":"g/r","timestamp":"2026-10-07T22:08:00Z"}`,
		`{"type":"pr-link","sessionId":"s","prNumber":7,"prUrl":"https://example/7","prRepository":"g/r","timestamp":"2026-10-07T22:09:00Z"}`,
	}
	for _, l := range lines {
		p.Process(watcher.Event{SessionID: "s", Line: []byte(l)})
	}

	sess, _ := sessions.Get("s")
	if sess.AITitle != "Multi-repo support" {
		t.Errorf("AITitle = %q", sess.AITitle)
	}
	if sess.SessionName != "" {
		t.Errorf("SessionName = %q, want empty (AI title is kept apart)", sess.SessionName)
	}
	wantLinks := []session.PRLink{
		{Number: 7, URL: "https://example/7", Repository: "g/r"},
		{Number: 8, URL: "https://example/8", Repository: "g/r"},
	}
	if fmt.Sprint(sess.PRLinks) != fmt.Sprint(wantLinks) {
		t.Errorf("PRLinks = %+v, want %+v", sess.PRLinks, wantLinks)
	}
	if sess.EventCount != 1 || sess.MessageCount != 1 {
		t.Errorf("counts = events %d, messages %d; want 1, 1", sess.EventCount, sess.MessageCount)
	}
	if !sess.LastActive.Equal(time.Date(2026, 10, 7, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("LastActive = %v, metadata lines must not move it", sess.LastActive)
	}
	if details != 1 || updates != 4 {
		t.Errorf("broadcasts = %d detail, %d update-only; want 1, 4", details, updates)
	}

	p.flush()
	events, err := db.ListEvents("s", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("stored events = %d, want 1", len(events))
	}

	// Restart: a fresh store must restore the metadata from the DB on the
	// first line it sees, before any ai-title line is replayed.
	sessions2 := session.NewStore()
	p2 := New(sessions2, db, repo.NewResolver(repo.NewFakeGit()), nil)
	defer p2.Stop()
	p2.Process(watcher.Event{SessionID: "s", Line: []byte(lines[0]), Bootstrap: true})
	sess2, _ := sessions2.Get("s")
	if sess2.AITitle != "Multi-repo support" || fmt.Sprint(sess2.PRLinks) != fmt.Sprint(wantLinks) {
		t.Errorf("after restart: AITitle %q, PRLinks %+v", sess2.AITitle, sess2.PRLinks)
	}
}

func TestProcess_SessionMetadataOnlyIsFlushed(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	defer p.Stop()

	p.Process(watcher.Event{SessionID: "s", Line: []byte(`{"type":"assistant","sessionId":"s","uuid":"u1","message":{"id":"m1","role":"assistant","content":"hi"}}`)})
	p.flush()
	p.Process(watcher.Event{SessionID: "s", Line: []byte(`{"type":"ai-title","aiTitle":"Later title","sessionId":"s"}`)})
	p.flush()

	row, err := db.GetSession("s")
	if err != nil || row == nil {
		t.Fatalf("GetSession: %v %v", row, err)
	}
	if row.AITitle != "Later title" {
		t.Errorf("persisted AITitle = %q, want it flushed without a following event", row.AITitle)
	}
}

func TestProcess_MetadataFirstAfterRestartKeepsSavedRow(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	p.Process(watcher.Event{SessionID: "s", Line: []byte(`{"type":"user","uuid":"u1","sessionId":"s","cwd":"/work/app","timestamp":"2026-10-01T00:00:00Z","message":{"role":"user","content":"hi"}}`)})
	p.Stop()
	before, err := db.GetSession("s")
	if err != nil || before == nil || before.CWD == "" {
		t.Fatalf("setup: %+v %v", before, err)
	}

	// Restart where the first line seen is metadata, as when a Docker source
	// is tailed from EOF.
	p2 := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	p2.Process(watcher.Event{SessionID: "s", Line: []byte(`{"type":"ai-title","aiTitle":"Later title","sessionId":"s"}`)})
	p2.Stop()

	after, err := db.GetSession("s")
	if err != nil || after == nil {
		t.Fatalf("GetSession: %v", err)
	}
	if after.CWD != before.CWD || after.RepoID != before.RepoID || after.StartedAt != before.StartedAt || after.EndedAt != before.EndedAt {
		t.Errorf("saved row changed: before %+v, after %+v", before, after)
	}
	if after.AITitle != "Later title" {
		t.Errorf("AITitle = %q", after.AITitle)
	}
}

func TestFlush_RequeuesSessionsWhenSessionWriteFails(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	p.Stop() // keep the ticker out of the way
	p.Process(watcher.Event{SessionID: "s", Line: []byte(`{"type":"ai-title","aiTitle":"T","sessionId":"s"}`)})
	db.Close()
	p.flush()

	p.bufMu.Lock()
	queued := p.dirty["s"]
	p.bufMu.Unlock()
	if !queued {
		t.Error("session dropped from the dirty set after a failed write, so it is never retried")
	}
}

func TestFlush_RetainsBatchWhenPersistFails(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	defer p.Stop()

	p.Process(watcher.Event{SessionID: "s", Bootstrap: true, Line: []byte(
		`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-10-01T00:00:00Z","message":{"role":"user","content":"hi"}}`)})
	db.Close() // every write now fails
	p.flush()

	p.bufMu.Lock()
	n := len(p.buffer)
	p.bufMu.Unlock()
	if n != 1 {
		t.Errorf("buffer holds %d events after a failed persist, want 1 kept for retry", n)
	}
}

func TestProcess_RetainedEventsDontTriggerFlush(t *testing.T) {
	db := openTestDB(t)
	p := New(session.NewStore(), db, repo.NewResolver(repo.NewFakeGit()), nil)
	defer p.Stop()

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	line := func(i int) []byte {
		return []byte(fmt.Sprintf(`{"type":"user","uuid":"u%d","sessionId":"s","timestamp":"2026-10-01T00:00:00Z","message":{"role":"user","content":"hi"}}`, i))
	}
	for i := 0; i < maxBatchSize-1; i++ {
		p.Process(watcher.Event{SessionID: "s", Bootstrap: true, Line: line(i)})
	}
	db.Close()
	p.Process(watcher.Event{SessionID: "s", Bootstrap: true, Line: line(maxBatchSize)}) // fills the batch; flush fails
	p.Process(watcher.Event{SessionID: "s", Bootstrap: true, Line: line(maxBatchSize + 1)})

	if n := strings.Count(logs.String(), "will retry"); n != 1 {
		t.Errorf("got %d persist attempts, want 1: retained events must not count toward the next size-triggered flush", n)
	}
}

// TestProcess_RestartReplayKeepsCounts replays the same file through a fresh
// pipeline, as every startup does, and expects the counts not to move.
func TestProcess_RestartReplayKeepsCounts(t *testing.T) {
	db := openTestDB(t)
	lines := [][]byte{
		[]byte(`{"type":"user","uuid":"u1","sessionId":"s","timestamp":"2026-10-01T00:00:00Z","message":{"role":"user","content":"go"}}`),
		[]byte(`{"type":"assistant","uuid":"a1","sessionId":"s","timestamp":"2026-10-01T00:00:01Z","message":{"id":"m1","role":"assistant","content":[{"type":"thinking","thinking":"hm"}]}}`),
		[]byte(`{"type":"assistant","uuid":"a2","sessionId":"s","timestamp":"2026-10-01T00:00:02Z","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"done"}]}}`),
		[]byte(`{"type":"summary","uuid":"sum1","sessionId":"s","timestamp":"2026-10-01T00:00:03Z","summary":"compacted"}`),
		[]byte(`{"type":"user","uuid":"u2","sessionId":"s","timestamp":"2026-10-01T00:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","is_error":true,"content":"boom"}]}}`),
	}

	var want [3]int
	for run := range 3 {
		sessions := session.NewStore()
		p := New(sessions, db, repo.NewResolver(repo.NewFakeGit()), nil)
		for _, l := range lines {
			p.Process(watcher.Event{SessionID: "s", Line: l, Bootstrap: run > 0})
		}
		p.Stop()

		sess, _ := sessions.Get("s")
		got := [3]int{sess.EventCount, sess.MessageCount, sess.ErrorCount}
		if run == 0 {
			want = got
			if got != [3]int{4, 3, 1} {
				t.Fatalf("first pass counts = %v, want [4 3 1] (events, messages, errors)", got)
			}
			continue
		}
		if got != want {
			t.Errorf("after restart %d counts = %v, want %v", run, got, want)
		}
	}
}
