// Package sessionquery answers read queries over Sessions, merging the live store with the history DB.
package sessionquery

import (
	"time"

	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
)

// Queries reads Sessions from the live store first and the history DB second.
type Queries struct {
	live    *session.Store
	history *store.DB
}

// New returns Queries over the live store and the history DB.
func New(live *session.Store, history *store.DB) *Queries {
	return &Queries{live: live, history: history}
}

// Get returns the live Session with id, else the stored one, else nil.
func (q *Queries) Get(id string) (*session.Session, error) {
	if s, ok := q.live.Get(id); ok {
		return s, nil
	}
	row, err := q.history.GetSession(id)
	if err != nil || row == nil {
		return nil, err
	}
	return row.ToSession(), nil
}

// Active returns the live Sessions that are active now.
func (q *Queries) Active() []*session.Session {
	active := []*session.Session{}
	for _, s := range q.live.All() {
		if s.IsActive {
			active = append(active, s)
		}
	}
	return active
}

// Filter narrows List to one Workflow or one repo; Workflow wins when both are set.
type Filter struct {
	Workflow string
	Repo     string
}

// List returns one page of stored Sessions, most recently active first.
func (q *Queries) List(f Filter, limit, offset int) ([]*session.Session, error) {
	var rows []store.SessionRow
	var err error
	switch {
	case f.Workflow != "":
		rows, err = q.history.ListSessionsByWorkflow(f.Workflow, limit, offset)
	case f.Repo != "":
		rows, err = q.history.ListSessionsByRepo(f.Repo, limit, offset)
	default:
		rows, err = q.history.ListSessions(limit, offset)
	}
	if err != nil {
		return nil, err
	}
	return store.SessionRowsToSessions(rows), nil
}

// ActivityGroups holds every Session in exactly one group by when it was last active.
type ActivityGroups struct {
	Active    []*session.Session `json:"active"`
	LastHour  []*session.Session `json:"lastHour"`
	Today     []*session.Session `json:"today"`
	Yesterday []*session.Session `json:"yesterday"`
	ThisWeek  []*session.Session `json:"thisWeek"`
	Older     []*session.Session `json:"older"`
}

const groupPageSize = 1000

// ByActivity groups live and stored Sessions relative to now. A live Session
// shadows its stored row. ThisWeek starts at the ISO-week Monday, so it is
// empty on Mondays and Tuesdays once Today and Yesterday have claimed theirs.
func (q *Queries) ByActivity(now time.Time) ActivityGroups {
	g := ActivityGroups{
		Active: []*session.Session{}, LastHour: []*session.Session{},
		Today: []*session.Session{}, Yesterday: []*session.Session{},
		ThisWeek: []*session.Session{}, Older: []*session.Session{},
	}
	b := newBounds(now)

	seen := make(map[string]bool)
	for _, s := range q.live.All() {
		seen[s.ID] = true
		if s.IsActive {
			g.Active = append(g.Active, s)
			continue
		}
		g.add(b, s)
	}

	// A DB error ends paging and returns what was grouped so far.
	for off := 0; ; off += groupPageSize {
		rows, err := q.history.ListSessions(groupPageSize, off)
		if err != nil {
			break
		}
		for _, row := range rows {
			if !seen[row.ID] {
				g.add(b, row.ToSession())
			}
		}
		if len(rows) < groupPageSize {
			break
		}
	}
	return g
}

type bounds struct {
	hourAgo, today, yesterday, week time.Time
}

func newBounds(now time.Time) bounds {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return bounds{
		hourAgo:   now.Add(-time.Hour),
		today:     today,
		yesterday: today.Add(-24 * time.Hour),
		week:      store.WeekStart(now),
	}
}

func (g *ActivityGroups) add(b bounds, s *session.Session) {
	la := s.LastActive
	switch {
	case la.After(b.hourAgo):
		g.LastHour = append(g.LastHour, s)
	case la.After(b.today):
		g.Today = append(g.Today, s)
	case la.After(b.yesterday):
		g.Yesterday = append(g.Yesterday, s)
	case la.After(b.week):
		g.ThisWeek = append(g.ThisWeek, s)
	default:
		g.Older = append(g.Older, s)
	}
}

// Stats is spend and token totals for Sessions started since a window start,
// plus the live active count and CostRate sum.
type Stats struct {
	TotalCost           float64            `json:"totalCost"`
	InputTokens         int64              `json:"inputTokens"`
	OutputTokens        int64              `json:"outputTokens"`
	CacheReadTokens     int64              `json:"cacheReadTokens"`
	CacheCreationTokens int64              `json:"cacheCreationTokens"`
	SessionCount        int                `json:"sessionCount"`
	AgentCount          int                `json:"agentCount"`
	ActiveSessions      int                `json:"activeSessions"`
	CacheHitPct         float64            `json:"cacheHitPct"`
	CostRate            float64            `json:"costRate"`
	CostByModel         map[string]float64 `json:"costByModel"`
	CostByRepo          map[string]float64 `json:"costByRepo"`
}

// Stats totals stored Sessions started at or after since (zero = lifetime).
func (q *Queries) Stats(since time.Time) (Stats, error) {
	agg, err := q.history.AggregateStats(since)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{
		TotalCost:           agg.TotalCost,
		InputTokens:         agg.InputTokens,
		OutputTokens:        agg.OutputTokens,
		CacheReadTokens:     agg.CacheReadTokens,
		CacheCreationTokens: agg.CacheCreationTokens,
		SessionCount:        agg.SessionCount,
		AgentCount:          agg.AgentCount,
		CostByModel:         agg.CostByModel,
		CostByRepo:          agg.CostByRepo,
	}
	for _, s := range q.live.All() {
		if s.IsActive {
			st.ActiveSessions++
			st.CostRate += s.CostRate
		}
	}
	if st.CostByModel == nil {
		st.CostByModel = make(map[string]float64)
	}
	if st.CostByRepo == nil {
		st.CostByRepo = make(map[string]float64)
	}
	if in := st.InputTokens + st.CacheReadTokens + st.CacheCreationTokens; in > 0 {
		st.CacheHitPct = float64(st.CacheReadTokens) / float64(in) * 100
	}
	return st, nil
}
