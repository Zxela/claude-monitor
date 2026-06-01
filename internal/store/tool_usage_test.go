package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/repo"
)

// TestToolUsageAndSessionSkills exercises the tool/skill aggregation: invocation
// counts, error attribution via the tool_use→tool_result self-join, the
// tools-vs-skills split, and window/repo scoping, plus the sparse per-session
// skills map used by History.
func TestToolUsageAndSessionSkills(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	// Repos (sessions.repo_id references repos).
	for _, id := range []string{"repoA", "repoB"} {
		if err := db.UpsertRepo(&repo.Repo{ID: id, Name: id}); err != nil {
			t.Fatalf("UpsertRepo %s: %v", id, err)
		}
	}
	// s1: recent, repoA. s2: older, repoB.
	mkSession := func(id, repoID, started string) {
		t.Helper()
		if _, err := db.db.Exec(`INSERT INTO sessions (id, repo_id, started_at) VALUES (?,?,?)`, id, repoID, started); err != nil {
			t.Fatalf("seed session %s: %v", id, err)
		}
	}
	mkSession("s1", "repoA", "2026-05-31T12:00:00Z")
	mkSession("s2", "repoB", "2026-05-20T12:00:00Z")

	var seq int
	use := func(session, tool, detail, tuid string) {
		t.Helper()
		seq++
		if _, err := db.db.Exec(
			`INSERT INTO events (session_id, message_id, role, tool_name, tool_detail, tool_use_id, timestamp)
			 VALUES (?,?,?,?,?,?,?)`,
			session, fmt.Sprintf("m%d", seq), "assistant", tool, detail, tuid, "2026-05-31T12:00:01Z",
		); err != nil {
			t.Fatalf("seed tool_use: %v", err)
		}
	}
	result := func(session, forTUID string, isErr int) {
		t.Helper()
		seq++
		if _, err := db.db.Exec(
			`INSERT INTO events (session_id, message_id, role, for_tool_use_id, is_error, timestamp)
			 VALUES (?,?,?,?,?,?)`,
			session, fmt.Sprintf("m%d", seq), "user", forTUID, isErr, "2026-05-31T12:00:02Z",
		); err != nil {
			t.Fatalf("seed tool_result: %v", err)
		}
	}

	// s1: Bash ok, Bash error, Skill commit ok, Skill commit error, Skill review-pr (no result row).
	use("s1", "Bash", "ls", "tu1")
	result("s1", "tu1", 0)
	use("s1", "Bash", "bad", "tu2")
	result("s1", "tu2", 1)
	use("s1", "Skill", "commit", "tu3")
	result("s1", "tu3", 0)
	use("s1", "Skill", "commit", "tu4")
	result("s1", "tu4", 1)
	use("s1", "Skill", "review-pr", "tu5") // no result → uses=1, errors=0
	// s2 (older, repoB): Read ok, Skill deploy ok.
	use("s2", "Read", "f.go", "tu6")
	result("s2", "tu6", 0)
	use("s2", "Skill", "deploy", "tu7")
	result("s2", "tu7", 0)

	find := func(list []ToolUsageEntry, name string) ToolUsageEntry {
		for _, e := range list {
			if e.Name == name {
				return e
			}
		}
		return ToolUsageEntry{Name: "<missing:" + name + ">"}
	}

	// --- Lifetime, all repos ---
	all, err := db.ToolUsage(time.Time{}, "")
	if err != nil {
		t.Fatalf("ToolUsage all: %v", err)
	}
	if got := find(all.Tools, "Bash"); got.Uses != 2 || got.Errors != 1 {
		t.Errorf("Bash = %+v, want uses=2 errors=1", got)
	}
	if got := find(all.Tools, "Read"); got.Uses != 1 || got.Errors != 0 {
		t.Errorf("Read = %+v, want uses=1 errors=0", got)
	}
	for _, e := range all.Tools {
		if e.Name == "Skill" {
			t.Errorf("Tools must exclude the Skill tool, got %+v", e)
		}
	}
	if got := find(all.Skills, "commit"); got.Uses != 2 || got.Errors != 1 {
		t.Errorf("commit skill = %+v, want uses=2 errors=1", got)
	}
	if got := find(all.Skills, "review-pr"); got.Uses != 1 || got.Errors != 0 {
		t.Errorf("review-pr skill = %+v, want uses=1 errors=0", got)
	}
	if got := find(all.Skills, "deploy"); got.Uses != 1 {
		t.Errorf("deploy skill = %+v, want uses=1", got)
	}
	// Ordering: most-used first.
	if len(all.Tools) >= 2 && all.Tools[0].Name != "Bash" {
		t.Errorf("Tools[0] = %q, want Bash (most-used first)", all.Tools[0].Name)
	}
	if len(all.Skills) >= 1 && all.Skills[0].Name != "commit" {
		t.Errorf("Skills[0] = %q, want commit (most-used first)", all.Skills[0].Name)
	}

	// --- Repo scope: repoA only ---
	a, err := db.ToolUsage(time.Time{}, "repoA")
	if err != nil {
		t.Fatalf("ToolUsage repoA: %v", err)
	}
	if got := find(a.Tools, "Read"); got.Uses != 0 {
		t.Errorf("repoA must exclude Read (it's in repoB), got %+v", got)
	}
	if got := find(a.Skills, "deploy"); got.Uses != 0 {
		t.Errorf("repoA must exclude deploy skill (repoB), got %+v", got)
	}
	if got := find(a.Skills, "commit"); got.Uses != 2 {
		t.Errorf("repoA commit = %+v, want uses=2", got)
	}

	// --- Window scope: since 2026-05-25 excludes the older s2 ---
	since, _ := time.Parse(time.RFC3339, "2026-05-25T00:00:00Z")
	win, err := db.ToolUsage(since, "")
	if err != nil {
		t.Fatalf("ToolUsage window: %v", err)
	}
	if got := find(win.Tools, "Read"); got.Uses != 0 {
		t.Errorf("window must exclude Read from older s2, got %+v", got)
	}
	if got := find(win.Skills, "deploy"); got.Uses != 0 {
		t.Errorf("window must exclude deploy from older s2, got %+v", got)
	}
	if got := find(win.Tools, "Bash"); got.Uses != 2 {
		t.Errorf("window Bash = %+v, want uses=2 (s1 in window)", got)
	}

	// --- Per-session skills map (sparse) ---
	m, err := db.SessionSkills()
	if err != nil {
		t.Fatalf("SessionSkills: %v", err)
	}
	if len(m["s1"]) != 2 {
		t.Errorf("s1 skills = %+v, want 2 (commit, review-pr)", m["s1"])
	}
	if got := find(m["s1"], "commit"); got.Uses != 2 || got.Errors != 1 {
		t.Errorf("s1 commit = %+v, want uses=2 errors=1", got)
	}
	if len(m["s2"]) != 1 || m["s2"][0].Name != "deploy" {
		t.Errorf("s2 skills = %+v, want [deploy]", m["s2"])
	}
	if _, ok := m["no-such-session"]; ok {
		t.Errorf("map should be sparse — only sessions with skills present")
	}
}
