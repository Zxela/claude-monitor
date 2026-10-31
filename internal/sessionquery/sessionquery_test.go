package sessionquery

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
)

func setup(t *testing.T) (*Queries, *session.Store, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	live := session.NewStore()
	return New(live, db), live, db
}

func save(t *testing.T, db *store.DB, sessions ...*session.Session) {
	t.Helper()
	if err := db.FlushSessions(sessions); err != nil {
		t.Fatalf("flush: %v", err)
	}
}

func ids(sessions []*session.Session) []string {
	out := make([]string, len(sessions))
	for i, s := range sessions {
		out[i] = s.ID
	}
	return out
}

func assertIDs(t *testing.T, group string, got []*session.Session, want ...string) {
	t.Helper()
	g := ids(got)
	if fmt.Sprint(g) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", group, g, want)
	}
}

func TestByActivity_WeekStartsOnISOMonday(t *testing.T) {
	t.Parallel()
	q, _, db := setup(t)
	loc := time.Local
	now := time.Date(2026, 3, 13, 15, 0, 0, 0, loc) // Friday
	at := func(day, hour int) time.Time { return time.Date(2026, 3, day, hour, 0, 0, 0, loc) }
	save(t, db,
		&session.Session{ID: "hour", LastActive: now.Add(-10 * time.Minute)},
		&session.Session{ID: "today", LastActive: at(13, 9)},
		&session.Session{ID: "yesterday", LastActive: at(12, 9)},
		&session.Session{ID: "monday", LastActive: at(9, 9)},
		&session.Session{ID: "sunday", LastActive: at(8, 9)},
	)

	g := q.ByActivity(now)

	assertIDs(t, "Active", g.Active)
	assertIDs(t, "LastHour", g.LastHour, "hour")
	assertIDs(t, "Today", g.Today, "today")
	assertIDs(t, "Yesterday", g.Yesterday, "yesterday")
	assertIDs(t, "ThisWeek", g.ThisWeek, "monday")
	assertIDs(t, "Older", g.Older, "sunday")
}

func TestByActivity_LiveSessionShadowsStoredRow(t *testing.T) {
	t.Parallel()
	q, live, db := setup(t)
	now := time.Now()
	save(t, db,
		&session.Session{ID: "a", LastActive: now.Add(-30 * 24 * time.Hour)},
		&session.Session{ID: "b", LastActive: now.Add(-30 * 24 * time.Hour)},
	)
	live.Upsert("a", func(s *session.Session) { s.LastActive = now.Add(-10 * time.Minute) })
	live.Upsert("c", func(s *session.Session) { s.LastActive = now })

	g := q.ByActivity(now)

	assertIDs(t, "Active", g.Active, "c")
	assertIDs(t, "LastHour", g.LastHour, "a")
	assertIDs(t, "Older", g.Older, "b")
}

func TestByActivity_GroupsEveryStoredRow(t *testing.T) {
	t.Parallel()
	q, _, db := setup(t)
	now := time.Now()
	rows := make([]*session.Session, groupPageSize+5)
	for i := range rows {
		rows[i] = &session.Session{ID: fmt.Sprintf("s%04d", i), LastActive: now.Add(-time.Duration(30*24+i) * time.Hour)}
	}
	save(t, db, rows...)

	if got := len(q.ByActivity(now).Older); got != len(rows) {
		t.Errorf("Older has %d Sessions, want %d", got, len(rows))
	}
}

func TestByActivity_EmptyGroupsAreNotNil(t *testing.T) {
	t.Parallel()
	q, _, _ := setup(t)
	g := q.ByActivity(time.Now())
	for name, s := range map[string][]*session.Session{
		"Active": g.Active, "LastHour": g.LastHour, "Today": g.Today,
		"Yesterday": g.Yesterday, "ThisWeek": g.ThisWeek, "Older": g.Older,
	} {
		if s == nil {
			t.Errorf("%s is nil, want empty", name)
		}
	}
}

func TestGet_PrefersLiveThenStored(t *testing.T) {
	t.Parallel()
	q, live, db := setup(t)
	save(t, db,
		&session.Session{ID: "both", TotalCost: 1, LastActive: time.Now().Add(-time.Hour)},
		&session.Session{ID: "stored", TotalCost: 2, LastActive: time.Now().Add(-time.Hour)},
	)
	live.Upsert("both", func(s *session.Session) { s.TotalCost = 9 })

	for _, tc := range []struct {
		id   string
		cost float64
	}{{"both", 9}, {"stored", 2}} {
		s, err := q.Get(tc.id)
		if err != nil || s == nil {
			t.Fatalf("Get(%q) = %v, %v", tc.id, s, err)
		}
		if s.TotalCost != tc.cost {
			t.Errorf("Get(%q).TotalCost = %v, want %v", tc.id, s.TotalCost, tc.cost)
		}
	}

	if s, err := q.Get("missing"); s != nil || err != nil {
		t.Errorf("Get(missing) = %v, %v, want nil, nil", s, err)
	}
}

func TestActive_OnlyActiveLiveSessions(t *testing.T) {
	t.Parallel()
	q, live, _ := setup(t)
	if got := q.Active(); got == nil || len(got) != 0 {
		t.Fatalf("Active() on empty store = %v, want []", got)
	}
	live.Upsert("on", func(s *session.Session) { s.LastActive = time.Now() })
	live.Upsert("off", func(s *session.Session) { s.LastActive = time.Now().Add(-time.Hour) })
	assertIDs(t, "Active()", q.Active(), "on")
}

func TestList_FiltersByWorkflowBeforeRepo(t *testing.T) {
	t.Parallel()
	q, _, db := setup(t)
	now := time.Now()
	save(t, db,
		&session.Session{ID: "wf", WorkflowID: "wf_1", RepoID: "r", LastActive: now},
		&session.Session{ID: "repo", RepoID: "r", LastActive: now.Add(-time.Minute)},
	)

	all, err := q.List(Filter{}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, "List(all)", all, "wf", "repo")

	byRepo, err := q.List(Filter{Repo: "r"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, "List(repo)", byRepo, "wf", "repo")

	byWorkflow, err := q.List(Filter{Workflow: "wf_1", Repo: "r"}, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertIDs(t, "List(workflow)", byWorkflow, "wf")

	none, err := q.List(Filter{Repo: "nope"}, 50, 0)
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("List(unknown repo) = %v, %v, want []", none, err)
	}
}

func TestStats_StoredTotalsPlusLiveBurn(t *testing.T) {
	t.Parallel()
	q, live, db := setup(t)
	now := time.Now()
	save(t, db,
		&session.Session{ID: "p", Model: "m", RepoID: "r", TotalCost: 3, InputTokens: 10, CacheReadTokens: 30, StartedAt: now.Add(-time.Hour), LastActive: now},
		&session.Session{ID: "c", ParentID: "p", Model: "m", TotalCost: 1, StartedAt: now.Add(-time.Hour), LastActive: now},
		&session.Session{ID: "old", TotalCost: 100, StartedAt: now.Add(-48 * time.Hour), LastActive: now.Add(-47 * time.Hour)},
	)
	for _, id := range []string{"x", "y"} {
		live.Upsert(id, func(s *session.Session) {
			s.StartedAt = now.Add(-10 * time.Minute)
			s.LastActive = time.Now()
			s.TotalCost = 5
		})
	}
	live.Upsert("quiet", func(s *session.Session) {
		s.StartedAt = now.Add(-time.Hour)
		s.LastActive = now.Add(-time.Hour)
		s.TotalCost = 50
	})

	st, err := q.Stats(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	if st.TotalCost != 4 || st.SessionCount != 1 || st.AgentCount != 1 {
		t.Errorf("totals = cost %v, sessions %d, agents %d; want 4, 1, 1", st.TotalCost, st.SessionCount, st.AgentCount)
	}
	if st.CacheHitPct != 75 {
		t.Errorf("CacheHitPct = %v, want 75", st.CacheHitPct)
	}
	if st.CostByModel["m"] != 4 || st.CostByRepo["r"] != 3 {
		t.Errorf("CostByModel = %v, CostByRepo = %v", st.CostByModel, st.CostByRepo)
	}
	if st.ActiveSessions != 2 {
		t.Errorf("ActiveSessions = %d, want 2", st.ActiveSessions)
	}
	if math.Abs(st.CostRate-1.0) > 0.01 {
		t.Errorf("CostRate = %v, want ~1.0 (two active Sessions at $0.50/min)", st.CostRate)
	}
}

func TestStats_EmptyHistoryHasEmptyMaps(t *testing.T) {
	t.Parallel()
	q, _, _ := setup(t)
	st, err := q.Stats(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if st.CostByModel == nil || st.CostByRepo == nil || st.CacheHitPct != 0 {
		t.Errorf("Stats on empty history = %+v", st)
	}
}
