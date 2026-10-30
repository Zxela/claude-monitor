package store

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// AggregateResult holds aggregate statistics across sessions.
//
// SessionCount counts top-level sessions only (parent_id empty); AgentCount
// counts workflow/subagent child rows. The split keeps the displayed
// "Sessions" number consistent with TrendData while still surfacing agent
// activity. Cost and tokens remain summed across ALL rows (each dollar
// counted once where it was incurred).
type AggregateResult struct {
	TotalCost           float64            `json:"totalCost"`
	InputTokens         int64              `json:"inputTokens"`
	OutputTokens        int64              `json:"outputTokens"`
	CacheReadTokens     int64              `json:"cacheReadTokens"`
	CacheCreationTokens int64              `json:"cacheCreationTokens"`
	SessionCount        int                `json:"sessionCount"`
	AgentCount          int                `json:"agentCount"`
	CostByModel         map[string]float64 `json:"costByModel"`
	CostByRepo          map[string]float64 `json:"costByRepo"`
}

// TrendBucket holds aggregated stats for a single time bucket (hour or day).
type TrendBucket struct {
	Date                string  `json:"date"`
	Cost                float64 `json:"cost"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	SessionCount        int     `json:"sessionCount"`
	AgentCount          int     `json:"agentCount"`
	CacheHitPct         float64 `json:"cacheHitPct"`
	AvgSessionCost      float64 `json:"avgSessionCost"`
	MedianSessionCost   float64 `json:"medianSessionCost"`
	P95SessionCost      float64 `json:"p95SessionCost"`
	AvgSessionTokens    float64 `json:"avgSessionTokens"`
	OutputInputRatio    float64 `json:"outputInputRatio"`
}

// RepoTrend holds cost/token breakdown for a single repo.
type RepoTrend struct {
	RepoID   string  `json:"repoId"`
	RepoName string  `json:"repoName"`
	Cost     float64 `json:"cost"`
	Tokens   int64   `json:"tokens"`
	Sessions int     `json:"sessions"`
}

// ModelTrend holds cost/token breakdown for a single model.
type ModelTrend struct {
	Model    string  `json:"model"`
	Cost     float64 `json:"cost"`
	Tokens   int64   `json:"tokens"`
	Sessions int     `json:"sessions"`
}

// SessionTrend holds the cost/token breakdown for one top-level session, with
// subagent/workflow child rows rolled up into their root ancestor. This is the
// honest default unit for cost: cost-by-repo over-attributes a session that
// spans multiple projects to a single project, whereas cost-by-session keeps a
// run's spend intact regardless of how many directories it touched.
type SessionTrend struct {
	SessionID   string  `json:"sessionId"`
	SessionName string  `json:"sessionName"`
	Cost        float64 `json:"cost"`
	Tokens      int64   `json:"tokens"`
	Agents      int     `json:"agents"` // rows rolled up (root + descendants)
}

// TrendSummary holds totals across all buckets.
type TrendSummary struct {
	TotalCost       float64 `json:"totalCost"`
	EffectiveTokens int64   `json:"effectiveTokens"`
	CacheHitPct     float64 `json:"cacheHitPct"`
	SessionCount    int     `json:"sessionCount"`
	AgentCount      int     `json:"agentCount"`
}

// TrendResult holds the complete trend analysis response.
type TrendResult struct {
	Window    string         `json:"window"`
	Buckets   []TrendBucket  `json:"buckets"`
	ByRepo    []RepoTrend    `json:"byRepo"`
	ByModel   []ModelTrend   `json:"byModel"`
	BySession []SessionTrend `json:"bySession"`
	Summary   TrendSummary   `json:"summary"`
}

// AggregateStatsByRepo returns aggregate statistics for a single repo.
func (d *DB) AggregateStatsByRepo(repoID string) (*AggregateResult, error) {
	r := &AggregateResult{
		CostByModel: make(map[string]float64),
		CostByRepo:  make(map[string]float64),
	}
	err := d.rdb.QueryRow(`SELECT COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		COALESCE(SUM(cache_creation_tokens),0),
		COALESCE(SUM(CASE WHEN parent_id IS NULL OR parent_id = '' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN parent_id IS NOT NULL AND parent_id != '' THEN 1 ELSE 0 END),0)
		FROM sessions WHERE repo_id = ?`, repoID).Scan(
		&r.TotalCost, &r.InputTokens, &r.OutputTokens,
		&r.CacheReadTokens, &r.CacheCreationTokens, &r.SessionCount, &r.AgentCount,
	)
	if err != nil {
		return nil, err
	}
	modelRows, err := d.rdb.Query(`SELECT COALESCE(model,''), SUM(total_cost)
		FROM sessions WHERE repo_id = ? GROUP BY model`, repoID)
	if err != nil {
		return nil, err
	}
	defer modelRows.Close()
	for modelRows.Next() {
		var model string
		var cost float64
		if err := modelRows.Scan(&model, &cost); err != nil {
			return nil, err
		}
		if model != "" {
			r.CostByModel[model] = cost
		}
	}
	// Only reflect a repo that actually matched rows, mirroring the model guard
	// above. An unknown/whitespace id thus returns costByRepo:{} instead of a
	// phantom zero-cost entry echoing the queried id. Agent rows count as a
	// match: a repo whose only rows are subagent children still has real spend.
	if r.SessionCount+r.AgentCount > 0 {
		r.CostByRepo[repoID] = r.TotalCost
	}
	return r, nil
}

// AggregateStats returns aggregate statistics across ALL session rows (parents
// and their workflow/subagent children). Cost and tokens are summed exactly
// once because each row carries only its own spend. SessionCount counts
// top-level sessions only and AgentCount the child rows — the same split
// TrendData uses, so /api/stats and /api/stats/trends report the same
// "Sessions" number for the same window.
func (d *DB) AggregateStats(since time.Time) (*AggregateResult, error) {
	r := &AggregateResult{
		CostByModel: make(map[string]float64),
		CostByRepo:  make(map[string]float64),
	}

	var where string
	var args []interface{}
	if !since.IsZero() {
		where = ` WHERE started_at >= ?`
		// started_at is stored as UTC RFC3339 (…Z). Format the boundary in UTC so
		// the lexical TEXT comparison is correct: a local-offset boundary (…-07:00)
		// sorts before "…Z" and wrongly pulls late-yesterday-UTC rows into the
		// window, inflating today/week/month totals. See TestWindowBoundaryUTC.
		args = append(args, since.UTC().Format(time.RFC3339))
	}

	err := d.rdb.QueryRow(`SELECT COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		COALESCE(SUM(cache_creation_tokens),0),
		COALESCE(SUM(CASE WHEN parent_id IS NULL OR parent_id = '' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN parent_id IS NOT NULL AND parent_id != '' THEN 1 ELSE 0 END),0)
		FROM sessions`+where, args...).Scan(
		&r.TotalCost, &r.InputTokens, &r.OutputTokens,
		&r.CacheReadTokens, &r.CacheCreationTokens, &r.SessionCount, &r.AgentCount,
	)
	if err != nil {
		return nil, err
	}

	// Cost by model
	modelRows, err := d.rdb.Query(`SELECT COALESCE(model,''), SUM(total_cost)
		FROM sessions`+where+` GROUP BY model`, args...)
	if err != nil {
		return nil, err
	}
	defer modelRows.Close()
	for modelRows.Next() {
		var model string
		var cost float64
		if err := modelRows.Scan(&model, &cost); err != nil {
			return nil, err
		}
		if model != "" {
			r.CostByModel[model] = cost
		}
	}

	// Cost by repo
	repoRows, err := d.rdb.Query(`SELECT COALESCE(repo_id,''), SUM(total_cost)
		FROM sessions`+where+` GROUP BY repo_id`, args...)
	if err != nil {
		return nil, err
	}
	defer repoRows.Close()
	for repoRows.Next() {
		var repoID string
		var cost float64
		if err := repoRows.Scan(&repoID, &cost); err != nil {
			return nil, err
		}
		if repoID != "" {
			r.CostByRepo[repoID] = cost
		}
	}

	return r, nil
}

// ToolUsageEntry is one tool or skill with its invocation and error counts.
type ToolUsageEntry struct {
	Name   string `json:"name"`   // tool name (e.g. "Bash") or skill name (e.g. "commit")
	Uses   int    `json:"uses"`   // number of tool_use invocations
	Errors int    `json:"errors"` // invocations whose tool_result reported an error
}

// ToolUsageResult is the tool/skill usage breakdown for a window/repo scope.
// Tools are grouped by tool_name (excluding "Skill"); Skills are the "Skill"
// tool's invocations grouped by the invoked skill name (tool_detail). Both
// lists are ordered by Uses descending.
type ToolUsageResult struct {
	Tools  []ToolUsageEntry `json:"tools"`
	Skills []ToolUsageEntry `json:"skills"`
}

// toolUsageLimit caps each list so a pathological history can't return thousands
// of distinct tool/skill rows; the long tail is not useful in the breakdown.
const toolUsageLimit = 50

// ToolUsage returns the tool- and skill-invocation breakdown for sessions whose
// start falls within the window (since; zero = lifetime) and optional repo.
//
// Invocations are counted from tool_use events. Errors are attributed by joining
// each tool_use to its tool_result via tool_use_id/for_tool_use_id and counting
// the ones flagged is_error — is_error lives on the result row, never on the
// tool_use row, so the self-join is required. Scoping is by the OWNING session's
// started_at/repo_id, matching AggregateStats/TrendData semantics.
func (d *DB) ToolUsage(since time.Time, repoID string) (*ToolUsageResult, error) {
	tools, err := d.toolUsageGrouped("u.tool_name", "u.tool_name != '' AND u.tool_name != 'Skill'", since, repoID)
	if err != nil {
		return nil, fmt.Errorf("ToolUsage tools: %w", err)
	}
	skills, err := d.toolUsageGrouped("u.tool_detail", "u.tool_name = 'Skill' AND COALESCE(u.tool_detail,'') != ''", since, repoID)
	if err != nil {
		return nil, fmt.Errorf("ToolUsage skills: %w", err)
	}
	return &ToolUsageResult{Tools: tools, Skills: skills}, nil
}

// toolUsageGrouped runs the shared tool/skill aggregation. keyCol is the GROUP BY
// key (tool_name for tools, tool_detail for skills); rowFilter selects which
// tool_use rows to include. since/repoID scope by the owning session.
func (d *DB) toolUsageGrouped(keyCol, rowFilter string, since time.Time, repoID string) ([]ToolUsageEntry, error) {
	conds := []string{rowFilter}
	var args []interface{}
	if !since.IsZero() {
		// started_at is UTC RFC3339 (…Z); format the boundary in UTC so the
		// lexical TEXT comparison is correct (see AggregateStats).
		conds = append(conds, "s.started_at >= ?")
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	if repoID != "" {
		conds = append(conds, "s.repo_id = ?")
		args = append(args, repoID)
	}
	// COUNT(DISTINCT u.id): the LEFT JOIN can fan out a tool_use into several
	// result rows, so dedupe by tool_use event id for both uses and errors.
	// The `u.tool_use_id != ''` guard is required: ids are stored as "" (never
	// NULL) when absent, and without it an id-less tool_use would join to every
	// id-less tool_result and attribute phantom errors.
	// The join deliberately ignores session_id: a workflow agent's tool_result
	// can land in a child session while the tool_use sits in the parent, and a
	// same-session constraint silently dropped those errors. toolu_ ids are
	// globally unique, so the id alone matches exactly one invocation.
	q := `SELECT ` + keyCol + ` AS k,
		COUNT(DISTINCT u.id) AS uses,
		COUNT(DISTINCT CASE WHEN r.is_error = 1 THEN u.id END) AS errs
		FROM events u
		JOIN sessions s ON s.id = u.session_id
		LEFT JOIN events r ON u.tool_use_id != '' AND r.for_tool_use_id = u.tool_use_id
		WHERE ` + strings.Join(conds, " AND ") + `
		GROUP BY k
		ORDER BY uses DESC, k
		LIMIT ?`
	args = append(args, toolUsageLimit)

	rows, err := d.rdb.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ToolUsageEntry, 0)
	for rows.Next() {
		var e ToolUsageEntry
		if err := rows.Scan(&e.Name, &e.Uses, &e.Errors); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SessionSkills returns, for every session that invoked at least one skill, the
// skills it invoked with per-skill use/error counts. Skill invocations are
// sparse, so the whole map is loaded in one query (no per-session round-trips);
// History uses it to badge rows. Errors are attributed via the tool_use→
// tool_result self-join (see ToolUsage).
func (d *DB) SessionSkills() (map[string][]ToolUsageEntry, error) {
	rows, err := d.rdb.Query(`SELECT u.session_id, u.tool_detail,
		COUNT(DISTINCT u.id) AS uses,
		COUNT(DISTINCT CASE WHEN r.is_error = 1 THEN u.id END) AS errs
		FROM events u
		LEFT JOIN events r ON u.tool_use_id != '' AND r.for_tool_use_id = u.tool_use_id
		WHERE u.tool_name = 'Skill' AND COALESCE(u.tool_detail,'') != ''
		GROUP BY u.session_id, u.tool_detail
		ORDER BY u.session_id, uses DESC, u.tool_detail`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]ToolUsageEntry)
	for rows.Next() {
		var sid string
		var e ToolUsageEntry
		if err := rows.Scan(&sid, &e.Name, &e.Uses, &e.Errors); err != nil {
			return nil, err
		}
		out[sid] = append(out[sid], e)
	}
	return out, rows.Err()
}

// trendParams holds the parsed query parameters shared across TrendData helpers.
//
// windowWhere = all rows in window (used for cost/token SUMs, so each dollar is
// counted exactly once where it was incurred — including workflow/subagent child rows).
// where = top-level sessions only (windowWhere + parent_id empty); used for the
// COUNT/AVG/percentile distributions, which describe top-level sessions.
// sinceArg is the single window boundary (bound as args[0]) shared by every
// subquery, so all of them see exactly the same cutoff instant.
type trendParams struct {
	dateFmt     string
	sinceArg    string
	windowWhere string
	where       string
	args        []interface{}
	repoID      string
}

// TrendData returns time-bucketed analytics for the given window and optional
// repo filter. window accepts both vocabularies with the definitions shared by
// every stats endpoint (see WindowStart): rolling "24h"/"7d"/"30d" and calendar
// "today"/"week"/"month". "24h" and "today" bucket hourly, the rest daily.
// repoID may be empty for all repos.
func (d *DB) TrendData(window string, repoID string) (*TrendResult, error) {
	var dateFmt string
	switch window {
	case "24h", "today":
		dateFmt = "%Y-%m-%d %H:00"
	case "7d", "30d", "week", "month":
		dateFmt = "%Y-%m-%d"
	default:
		// Includes "all": an unbounded window has no sensible bucket count.
		return nil, fmt.Errorf("invalid window: %s", window)
	}
	since, _ := WindowStart(window, time.Now())

	// windowWhere matches ALL rows in the window (for cost/token SUMs, counted
	// once). where additionally restricts to top-level sessions (parent_id empty)
	// for COUNT/AVG/percentile distributions.
	// The boundary is computed once in Go and bound as a parameter in every
	// subquery: started_at is TEXT "YYYY-MM-DDTHH:MM:SSZ", so the bound value
	// must be UTC RFC3339 for the lexical compare to be correct (an earlier
	// inline datetime('now') rendered without T/Z and turned the rolling window
	// into a calendar-day cutoff — see TestTrendData_24hExcludesOlderSameCalendarDay).
	sinceArg := since.UTC().Format(time.RFC3339)
	windowWhere := "started_at >= ?"
	args := []interface{}{sinceArg}
	if repoID != "" {
		windowWhere += " AND repo_id = ?"
		args = append(args, repoID)
	}
	where := windowWhere + " AND (parent_id IS NULL OR parent_id = '')"

	p := &trendParams{dateFmt: dateFmt, sinceArg: sinceArg, windowWhere: windowWhere, where: where, args: args, repoID: repoID}

	buckets, err := d.trendBuckets(p)
	if err != nil {
		return nil, err
	}

	if err := d.trendPercentiles(p, buckets); err != nil {
		return nil, err
	}

	byRepo, err := d.trendByRepo(p)
	if err != nil {
		return nil, err
	}

	byModel, err := d.trendByModel(p)
	if err != nil {
		return nil, err
	}

	bySession, err := d.trendBySession(p)
	if err != nil {
		return nil, err
	}

	// Build summary from buckets
	var summary TrendSummary
	var totalEffInput, totalCacheRead int64
	for _, b := range buckets {
		summary.TotalCost += b.Cost
		summary.EffectiveTokens += b.InputTokens + b.OutputTokens + b.CacheReadTokens + b.CacheCreationTokens
		summary.SessionCount += b.SessionCount
		summary.AgentCount += b.AgentCount
		totalEffInput += b.InputTokens + b.CacheReadTokens + b.CacheCreationTokens
		totalCacheRead += b.CacheReadTokens
	}
	if totalEffInput > 0 {
		summary.CacheHitPct = float64(totalCacheRead) / float64(totalEffInput) * 100
	}

	return &TrendResult{
		Window:    window,
		Buckets:   buckets,
		ByRepo:    byRepo,
		ByModel:   byModel,
		BySession: bySession,
		Summary:   summary,
	}, nil
}

// trendBuckets queries time-bucketed session aggregates.
//
// Cost and tokens are summed over ALL rows in the window (p.windowWhere) so each
// dollar is counted exactly once, including workflow/subagent child rows. The
// SessionCount and AvgSessionCost are computed via CASE over top-level sessions
// only (parent_id empty), keeping those distributions about top-level sessions;
// AgentCount is the complementary child-row count.
func (d *DB) trendBuckets(p *trendParams) ([]TrendBucket, error) {
	bucketQuery := fmt.Sprintf(`SELECT strftime('%s', started_at) AS bucket,
		COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_creation_tokens),0),
		COALESCE(SUM(CASE WHEN parent_id IS NULL OR parent_id = '' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN parent_id IS NOT NULL AND parent_id != '' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN parent_id IS NULL OR parent_id = '' THEN total_cost ELSE 0 END)
			/ NULLIF(SUM(CASE WHEN parent_id IS NULL OR parent_id = '' THEN 1 ELSE 0 END),0),0)
		FROM sessions WHERE %s
		GROUP BY bucket ORDER BY bucket`, p.dateFmt, p.windowWhere)

	rows, err := d.rdb.Query(bucketQuery, p.args...)
	if err != nil {
		return nil, fmt.Errorf("trendBuckets: %w", err)
	}
	defer rows.Close()

	var buckets []TrendBucket
	for rows.Next() {
		var b TrendBucket
		if err := rows.Scan(&b.Date, &b.Cost, &b.InputTokens, &b.OutputTokens,
			&b.CacheReadTokens, &b.CacheCreationTokens, &b.SessionCount, &b.AgentCount, &b.AvgSessionCost); err != nil {
			return nil, fmt.Errorf("trendBuckets: scan: %w", err)
		}
		effInput := b.InputTokens + b.CacheReadTokens + b.CacheCreationTokens
		if effInput > 0 {
			b.CacheHitPct = float64(b.CacheReadTokens) / float64(effInput) * 100
			b.OutputInputRatio = float64(b.OutputTokens) / float64(effInput)
		}
		if b.SessionCount > 0 {
			b.AvgSessionTokens = float64(effInput+b.OutputTokens) / float64(b.SessionCount)
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trendBuckets: rows: %w", err)
	}
	if buckets == nil {
		buckets = []TrendBucket{}
	}
	return buckets, nil
}

// trendPercentiles computes median and p95 session costs per bucket and applies them in-place.
func (d *DB) trendPercentiles(p *trendParams, buckets []TrendBucket) error {
	percQuery := fmt.Sprintf(`SELECT strftime('%s', started_at) AS bucket, total_cost
		FROM sessions WHERE %s
		ORDER BY bucket, total_cost`, p.dateFmt, p.where)

	percRows, err := d.rdb.Query(percQuery, p.args...)
	if err != nil {
		return fmt.Errorf("trendPercentiles: %w", err)
	}
	defer percRows.Close()

	bucketCosts := make(map[string][]float64)
	for percRows.Next() {
		var bucket string
		var cost float64
		if err := percRows.Scan(&bucket, &cost); err != nil {
			return fmt.Errorf("trendPercentiles: scan: %w", err)
		}
		bucketCosts[bucket] = append(bucketCosts[bucket], cost)
	}
	if err := percRows.Err(); err != nil {
		return fmt.Errorf("trendPercentiles: rows: %w", err)
	}

	for i := range buckets {
		costs := bucketCosts[buckets[i].Date]
		if len(costs) > 0 {
			sort.Float64s(costs)
			buckets[i].MedianSessionCost = percentile(costs, 0.5)
			buckets[i].P95SessionCost = percentile(costs, 0.95)
		}
	}
	return nil
}

// trendByRepo queries cost/token breakdown grouped by repository.
//
// Cost and tokens are summed over ALL rows in the window (counted once,
// including child rows). The session count is over top-level sessions only
// (parent_id empty) via CASE. For shapes 2/3 children share the parent's
// repo_id (set in pipeline), so repo attribution stays stable.
func (d *DB) trendByRepo(p *trendParams) ([]RepoTrend, error) {
	// Same boundary parameter as every other trend subquery (p.args[0]).
	repoWindowWhere := "s.started_at >= ?"
	if p.repoID != "" {
		repoWindowWhere += " AND s.repo_id = ?"
	}
	repoQuery := fmt.Sprintf(`SELECT s.repo_id, COALESCE(r.name,''), COALESCE(SUM(s.total_cost),0),
		COALESCE(SUM(s.input_tokens + s.output_tokens + s.cache_read_tokens + s.cache_creation_tokens),0),
		COALESCE(SUM(CASE WHEN s.parent_id IS NULL OR s.parent_id = '' THEN 1 ELSE 0 END),0)
		FROM sessions s JOIN repos r ON r.id = s.repo_id
		WHERE %s AND s.repo_id != ''
		GROUP BY r.id ORDER BY SUM(s.total_cost) DESC`, repoWindowWhere)

	repoRows, err := d.rdb.Query(repoQuery, p.args...)
	if err != nil {
		return nil, fmt.Errorf("trendByRepo: %w", err)
	}
	defer repoRows.Close()

	var byRepo []RepoTrend
	for repoRows.Next() {
		var rt RepoTrend
		if err := repoRows.Scan(&rt.RepoID, &rt.RepoName, &rt.Cost, &rt.Tokens, &rt.Sessions); err != nil {
			return nil, fmt.Errorf("trendByRepo: scan: %w", err)
		}
		byRepo = append(byRepo, rt)
	}
	if err := repoRows.Err(); err != nil {
		return nil, fmt.Errorf("trendByRepo: rows: %w", err)
	}
	if byRepo == nil {
		byRepo = []RepoTrend{}
	}
	return byRepo, nil
}

// trendByModel queries cost/token breakdown grouped by model.
//
// Cost and tokens are summed over ALL rows in the window (p.windowWhere),
// counted once. This now includes child (e.g. subagent on a different model)
// spend, which is desired since a workflow can mix models. The session count is
// over top-level sessions only (parent_id empty) via CASE.
func (d *DB) trendByModel(p *trendParams) ([]ModelTrend, error) {
	modelQuery := fmt.Sprintf(`SELECT COALESCE(model,'unknown'), COALESCE(SUM(total_cost),0),
		COALESCE(SUM(input_tokens + output_tokens + cache_read_tokens + cache_creation_tokens),0),
		COALESCE(SUM(CASE WHEN parent_id IS NULL OR parent_id = '' THEN 1 ELSE 0 END),0)
		FROM sessions WHERE %s AND model != '' AND model != '<synthetic>'
		GROUP BY model ORDER BY SUM(total_cost) DESC`, p.windowWhere)

	modelRows, err := d.rdb.Query(modelQuery, p.args...)
	if err != nil {
		return nil, fmt.Errorf("trendByModel: %w", err)
	}
	defer modelRows.Close()

	var byModel []ModelTrend
	for modelRows.Next() {
		var mt ModelTrend
		if err := modelRows.Scan(&mt.Model, &mt.Cost, &mt.Tokens, &mt.Sessions); err != nil {
			return nil, fmt.Errorf("trendByModel: scan: %w", err)
		}
		byModel = append(byModel, mt)
	}
	if err := modelRows.Err(); err != nil {
		return nil, fmt.Errorf("trendByModel: rows: %w", err)
	}
	if byModel == nil {
		byModel = []ModelTrend{}
	}
	return byModel, nil
}

// trendBySessionLimit caps how many top sessions the cost-by-session breakdown
// returns (a chart of every session would be unreadable; the long tail is tiny).
const trendBySessionLimit = 20

// trendBySession rolls cost/tokens up to each top-level (root) session, summing
// subagent/workflow child rows into their root ancestor via a recursive walk of
// parent_id. This is the default analytics breakdown: unlike cost-by-repo it does
// not over-attribute a multi-project run to a single project, since a session's
// whole tree stays together. Returns the top sessions by cost.
func (d *DB) trendBySession(p *trendParams) ([]SessionTrend, error) {
	// Same boundary parameter as every other trend subquery (p.args[0]).
	sessWindowWhere := "s.started_at >= ?"
	if p.repoID != "" {
		sessWindowWhere += " AND s.repo_id = ?"
	}
	// anc maps every session to its root ancestor (the top-level parent_id-empty
	// row). Rows in the window are then attributed to their root and summed, so a
	// parent and all its subagents collapse into one bar. Orphans (parent not in
	// the table) fall back to being their own root via COALESCE.
	query := fmt.Sprintf(`
		WITH RECURSIVE anc(id, root) AS (
			SELECT id, id FROM sessions WHERE parent_id IS NULL OR parent_id = ''
			UNION ALL
			SELECT s.id, a.root FROM sessions s JOIN anc a ON s.parent_id = a.id
		)
		SELECT COALESCE(a.root, s.id) AS root_id,
		       COALESCE(NULLIF(rt.session_name, ''), rt.ai_title, ''),
		       COALESCE(rt.task_description, ''),
		       COALESCE(SUM(s.total_cost), 0),
		       COALESCE(SUM(s.input_tokens + s.output_tokens + s.cache_read_tokens + s.cache_creation_tokens), 0),
		       COUNT(*)
		FROM sessions s
		LEFT JOIN anc a ON a.id = s.id
		LEFT JOIN sessions rt ON rt.id = COALESCE(a.root, s.id)
		WHERE %s
		GROUP BY root_id
		ORDER BY SUM(s.total_cost) DESC
		LIMIT %d`, sessWindowWhere, trendBySessionLimit)

	rows, err := d.rdb.Query(query, p.args...)
	if err != nil {
		return nil, fmt.Errorf("trendBySession: %w", err)
	}
	defer rows.Close()

	bySession := []SessionTrend{}
	for rows.Next() {
		var st SessionTrend
		var name, task string
		if err := rows.Scan(&st.SessionID, &name, &task, &st.Cost, &st.Tokens, &st.Agents); err != nil {
			return nil, fmt.Errorf("trendBySession: scan: %w", err)
		}
		// Prefer an explicit session name, then the AI title, then the task/prompt; the frontend
		// falls back to the id when both are empty.
		st.SessionName = name
		if st.SessionName == "" {
			st.SessionName = task
		}
		bySession = append(bySession, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("trendBySession: rows: %w", err)
	}
	return bySession, nil
}

// percentile returns the value at the given percentile (0-1) from a sorted
// slice, using the nearest-rank definition: ceil(p*n)-1 (0-indexed). The
// previous floor(p*n) index was biased one rank high — in a 2-session bucket
// it reported the maximum as the "median", which 24h trend views hit often.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
