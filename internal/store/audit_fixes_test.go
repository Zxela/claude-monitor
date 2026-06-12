package store

import (
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store/migrations"
)

// TestSearchFTS_ImplicitAnd verifies multi-word queries are matched with
// implicit AND (all terms present anywhere) rather than as an exact-adjacent
// phrase, while still tolerating FTS5 special characters per token.
func TestSearchFTS_ImplicitAnd(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	batch := &EventBatch{Events: []EventInsert{
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "session not found in store",
				Timestamp: time.Now(), UUID: "u1",
			},
		},
		{
			SessionID: "s1",
			Event: &parser.Event{
				Type: "assistant", ContentText: "the session list is empty",
				Timestamp: time.Now().Add(time.Second), UUID: "u2",
			},
		},
	}}
	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}

	// "session not" should match only the doc containing BOTH words (u1),
	// regardless of adjacency. The old phrase behaviour would also have matched
	// u1 here, so use a non-adjacent ordering to make the distinction sharp:
	// "not session" matches u1 under implicit-AND but would match NOTHING under
	// exact-phrase (words are not adjacent in that order).
	results, err := db.SearchFTS("not session", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("implicit-AND: expected 1 result for %q, got %d", "not session", len(results))
	}
	if results[0].UUID != "u1" {
		t.Errorf("expected u1, got %q", results[0].UUID)
	}

	// A query whose two words never co-occur in a single doc returns nothing.
	none, err := db.SearchFTS("found empty", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 results for non-co-occurring terms, got %d", len(none))
	}
}

// TestSearchFTS_SpecialCharsSafe verifies FTS5 operators / unbalanced quotes in
// the query do not cause syntax errors (each token is quoted) and an
// empty/whitespace query returns no results rather than erroring.
func TestSearchFTS_SpecialCharsSafe(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}
	batch := &EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			Type: "assistant", ContentText: "alpha beta gamma",
			Timestamp: time.Now(), UUID: "u1",
		},
	}}}
	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}

	for _, q := range []string{`alpha OR beta`, `"unbalanced`, `NEAR(x y)`, `   `, ``} {
		if _, err := db.SearchFTS(q, 10); err != nil {
			t.Errorf("SearchFTS(%q) returned error: %v", q, err)
		}
	}
}

// TestPersistBatch_FTSNoPhantomTokens verifies that updating an event's
// content_preview/tool_name purges the OLD terms from the external-content FTS5
// index, so a stale term no longer yields a false-positive match.
func TestPersistBatch_FTSNoPhantomTokens(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	ts := time.Now()
	// Initial row indexes the token "bash".
	if err := db.PersistBatch(&EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			MessageID: "msg-1", Type: "assistant",
			ContentText: "[tool: Bash]", ToolName: "Bash", ToolDetail: "ls",
			Timestamp: ts, UUID: "u1",
		},
	}}}); err != nil {
		t.Fatalf("PersistBatch 1 failed: %v", err)
	}

	// Update the same row to a Read tool — the "bash" token must be purged.
	if err := db.PersistBatch(&EventBatch{Events: []EventInsert{{
		SessionID: "s1",
		Event: &parser.Event{
			MessageID: "msg-1", Type: "assistant",
			ContentText: "[tool: Read]", ToolName: "Read", ToolDetail: "/x/y.go",
			Timestamp: ts, UUID: "u1",
		},
	}}}); err != nil {
		t.Fatalf("PersistBatch 2 failed: %v", err)
	}

	// The stale token must not match anymore.
	if got, err := db.SearchFTS("Bash", 10); err != nil {
		t.Fatalf("SearchFTS(Bash) failed: %v", err)
	} else if len(got) != 0 {
		t.Errorf("expected 0 results for purged token 'Bash', got %d (phantom token survived)", len(got))
	}
	// The new token must match.
	if got, err := db.SearchFTS("Read", 10); err != nil {
		t.Fatalf("SearchFTS(Read) failed: %v", err)
	} else if len(got) != 1 {
		t.Errorf("expected 1 result for current token 'Read', got %d", len(got))
	}
}

// TestCountSessionErrors_DedupByUUID verifies error counting dedups on the
// stable identity (message_id, else uuid) so re-emitted error lines are counted
// once and empty-message_id errors are still deduped.
func TestCountSessionErrors_DedupByUUID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{
		ID: "s1", StartedAt: time.Now(), LastActive: time.Now(),
	}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	ts := time.Now()
	// Two distinct errors (one with message_id, one with only uuid) plus a
	// re-emission of the message_id error (same dedup key) and a non-error.
	batch := &EventBatch{Events: []EventInsert{
		{SessionID: "s1", Event: &parser.Event{MessageID: "m1", IsError: true, ContentText: "boom", Timestamp: ts, UUID: "u1"}},
		{SessionID: "s1", Event: &parser.Event{IsError: true, ContentText: "kaboom", Timestamp: ts.Add(time.Second), UUID: "u2"}},
		{SessionID: "s1", Event: &parser.Event{Type: "assistant", ContentText: "ok", Timestamp: ts.Add(2 * time.Second), UUID: "u3"}},
	}}
	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}
	// Re-emit the m1 error (streaming update — same dedup key, upserts in place).
	if err := db.PersistBatch(&EventBatch{Events: []EventInsert{
		{SessionID: "s1", Event: &parser.Event{MessageID: "m1", IsError: true, ContentText: "boom again", Timestamp: ts, UUID: "u1"}},
	}}); err != nil {
		t.Fatalf("PersistBatch re-emit failed: %v", err)
	}

	n, err := db.CountSessionErrors("s1")
	if err != nil {
		t.Fatalf("CountSessionErrors failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 distinct errors, got %d", n)
	}
}

// TestAggregateStatsByRepo_UnknownRepoNoPhantom verifies an unknown repo id
// yields an empty costByRepo map rather than a phantom zero-cost entry.
func TestAggregateStatsByRepo_UnknownRepoNoPhantom(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	agg, err := db.AggregateStatsByRepo("does-not-exist")
	if err != nil {
		t.Fatalf("AggregateStatsByRepo failed: %v", err)
	}
	if len(agg.CostByRepo) != 0 {
		t.Errorf("expected empty costByRepo for unknown repo, got %v", agg.CostByRepo)
	}
	if agg.SessionCount != 0 {
		t.Errorf("expected 0 sessions for unknown repo, got %d", agg.SessionCount)
	}
}

// TestAggregateStatsByRepo_ChildOnlyRepoStillReportsCost verifies a repo whose
// only rows are subagent children (parent_id set) still reports its spend in
// costByRepo: agent rows count as "repo matched rows" even though sessionCount
// (top-level only) is 0.
func TestAggregateStatsByRepo_ChildOnlyRepoStillReportsCost(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if _, err := db.db.Exec(`INSERT INTO repos (id, name, first_seen) VALUES ('child-repo','child-repo','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	now := time.Now()
	if err := db.SaveSession(&session.Session{
		ID: "agent-only", ParentID: "some-parent", RepoID: "child-repo",
		TotalCost: 0.75, StartedAt: now, LastActive: now,
	}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	agg, err := db.AggregateStatsByRepo("child-repo")
	if err != nil {
		t.Fatalf("AggregateStatsByRepo failed: %v", err)
	}
	if agg.SessionCount != 0 || agg.AgentCount != 1 {
		t.Errorf("got sessionCount=%d agentCount=%d, want 0/1", agg.SessionCount, agg.AgentCount)
	}
	if got := agg.CostByRepo["child-repo"]; got != 0.75 {
		t.Errorf("costByRepo[child-repo] = %v, want 0.75 (agent spend must not vanish)", got)
	}
}

// TestRepoExists verifies the existence helper used by the per-repo 404 path.
func TestRepoExists(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if _, err := db.db.Exec(`INSERT INTO repos (id, name, first_seen) VALUES ('r1','repo-one','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed repo failed: %v", err)
	}
	if ok, err := db.RepoExists("r1"); err != nil || !ok {
		t.Errorf("RepoExists(r1) = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := db.RepoExists("nope"); err != nil || ok {
		t.Errorf("RepoExists(nope) = (%v,%v), want (false,nil)", ok, err)
	}
}

// TestCountReplayEvents verifies the replay total counts the session's own
// events plus its direct children's events.
func TestCountReplayEvents(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{ID: "parent", StartedAt: time.Now(), LastActive: time.Now()}); err != nil {
		t.Fatalf("SaveSession(parent) failed: %v", err)
	}
	if err := db.SaveSession(&session.Session{ID: "child", ParentID: "parent", StartedAt: time.Now(), LastActive: time.Now()}); err != nil {
		t.Fatalf("SaveSession(child) failed: %v", err)
	}

	ts := time.Now()
	batch := &EventBatch{Events: []EventInsert{
		{SessionID: "parent", Event: &parser.Event{Type: "assistant", ContentText: "p1", Timestamp: ts, UUID: "p1"}},
		{SessionID: "parent", Event: &parser.Event{Type: "assistant", ContentText: "p2", Timestamp: ts.Add(time.Second), UUID: "p2"}},
		{SessionID: "child", Event: &parser.Event{Type: "assistant", ContentText: "c1", Timestamp: ts.Add(2 * time.Second), UUID: "c1"}},
	}}
	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}

	n, err := db.CountReplayEvents("parent")
	if err != nil {
		t.Fatalf("CountReplayEvents failed: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 replay events (2 parent + 1 child), got %d", n)
	}
}

// TestMigration015_RecomputeAggregates verifies the recompute migration corrects
// a stale sessions.total_cost / token totals / error_count to match the events
// ledger, and is idempotent. It seeds deliberately-wrong session columns, then
// re-runs the migration via Down/Up and asserts the corrected values.
func TestMigration015_RecomputeAggregates(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	if err := db.SaveSession(&session.Session{ID: "s1", StartedAt: time.Now(), LastActive: time.Now()}); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	ts := time.Now()
	// Two costed messages + one error (with uuid) + a re-emission of msg-a.
	batch := &EventBatch{Events: []EventInsert{
		{SessionID: "s1", Event: &parser.Event{MessageID: "msg-a", Type: "assistant", ContentText: "a", CostUSD: 1.25, InputTokens: 100, OutputTokens: 10, Timestamp: ts, UUID: "ua"}},
		{SessionID: "s1", Event: &parser.Event{MessageID: "msg-b", Type: "assistant", ContentText: "b", CostUSD: 2.50, InputTokens: 200, OutputTokens: 20, Timestamp: ts.Add(time.Second), UUID: "ub"}},
		{SessionID: "s1", Event: &parser.Event{IsError: true, ContentText: "err", Timestamp: ts.Add(2 * time.Second), UUID: "uerr"}},
	}}
	if err := db.PersistBatch(batch); err != nil {
		t.Fatalf("PersistBatch failed: %v", err)
	}
	// Re-emit msg-a (same dedup key, upsert) with a higher cost — the deduped
	// recompute must use the LATEST row's value (MAX(id) per message_id).
	if err := db.PersistBatch(&EventBatch{Events: []EventInsert{
		{SessionID: "s1", Event: &parser.Event{MessageID: "msg-a", Type: "assistant", ContentText: "a2", CostUSD: 1.75, InputTokens: 120, OutputTokens: 12, Timestamp: ts, UUID: "ua"}},
	}}); err != nil {
		t.Fatalf("PersistBatch re-emit failed: %v", err)
	}

	// Corrupt the stored aggregates to simulate the historical drift bug.
	if _, err := db.db.Exec(`UPDATE sessions SET total_cost = 99.0, input_tokens = 1, output_tokens = 1, error_count = 759 WHERE id = 's1'`); err != nil {
		t.Fatalf("corrupt session failed: %v", err)
	}

	// Re-run migration 015 by rolling the version back below 15 (rolling back
	// every migration stacked on top of it generically, so this stays valid as
	// later migrations are added) then re-applying to head.
	rollbackBelow15 := func() {
		for {
			v, err := migrations.GetVersion(db.db)
			if err != nil {
				t.Fatalf("GetVersion failed: %v", err)
			}
			if v < 15 {
				return
			}
			if _, err := migrations.RunDown(db.db); err != nil {
				t.Fatalf("RunDown from v%d failed: %v", v, err)
			}
		}
	}
	rollbackBelow15()
	if _, err := migrations.RunUp(db.db); err != nil { // re-applies 15 and everything above
		t.Fatalf("RunUp failed: %v", err)
	}

	row, err := db.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	// Expected deduped cost = latest msg-a (1.75) + msg-b (2.50) = 4.25.
	if got := row.TotalCost; got < 4.2499 || got > 4.2501 {
		t.Errorf("total_cost: got %v, want 4.25", got)
	}
	if got := row.InputTokens; got != 320 { // 120 + 200
		t.Errorf("input_tokens: got %d, want 320", got)
	}
	if got := row.OutputTokens; got != 32 { // 12 + 20
		t.Errorf("output_tokens: got %d, want 32", got)
	}
	if got := row.ErrorCount; got != 1 {
		t.Errorf("error_count: got %d, want 1", got)
	}

	// Idempotency: running the recompute SQL again must not change the values.
	rollbackBelow15()
	if _, err := migrations.RunUp(db.db); err != nil {
		t.Fatalf("RunUp(idempotency) failed: %v", err)
	}
	row2, err := db.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession(idempotency) failed: %v", err)
	}
	if row2.TotalCost != row.TotalCost || row2.ErrorCount != row.ErrorCount {
		t.Errorf("recompute not idempotent: %+v vs %+v", row2, row)
	}
}
