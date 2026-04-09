// Package store provides persistent storage using SQLite for the v2 data model.
package store

import (
	"compress/gzip"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/repo"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/store/migrations"

	_ "modernc.org/sqlite"
)

// eventSelectCols is the column list used by all event SELECT queries.
// Must match the scan order in scanEventRows().
const eventSelectCols = `e.id, e.session_id, COALESCE(e.uuid,''), COALESCE(e.message_id,''),
	COALESCE(e.type,''), COALESCE(e.role,''), COALESCE(e.content_preview,''),
	COALESCE(e.tool_name,''), COALESCE(e.tool_detail,''),
	e.cost_usd, e.input_tokens, e.output_tokens, e.cache_read_tokens, e.cache_creation_tokens,
	COALESCE(e.model,''), e.is_error, COALESCE(e.stop_reason,''),
	COALESCE(e.hook_event,''), COALESCE(e.hook_name,''),
	COALESCE(e.tool_use_id,''), COALESCE(e.for_tool_use_id,''), e.is_agent,
	e.timestamp,
	e.duration_ms, e.success, COALESCE(e.stderr,''), e.interrupted, e.truncated,
	e.agent_duration_ms, e.agent_tokens, e.agent_tool_use_count, COALESCE(e.agent_type,''),
	COALESCE(e.subtype,''), e.turn_message_count, e.hook_count, COALESCE(e.hook_infos,''), COALESCE(e.level,''),
	e.is_meta, COALESCE(e.version,''), COALESCE(e.entrypoint,'')`

// sessionSelectCols is the column list used by all session SELECT queries.
// Must match the scan order in scanSessionRows().
const sessionSelectCols = `id, COALESCE(repo_id,''), COALESCE(parent_id,''), COALESCE(session_name,''),
	COALESCE(task_description,''), COALESCE(cwd,''), COALESCE(branch,''),
	COALESCE(model,''), COALESCE(started_at,''), COALESCE(ended_at,''),
	total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
	message_count, event_count, error_count,
	COALESCE(version,''), COALESCE(entrypoint,'')`

// SessionRow represents a session as stored in the database.
type SessionRow struct {
	ID                  string  `json:"id"`
	RepoID              string  `json:"repoId"`
	ParentID            string  `json:"parentId"`
	SessionName         string  `json:"sessionName"`
	TaskDescription     string  `json:"taskDescription"`
	CWD                 string  `json:"cwd"`
	Branch              string  `json:"branch"`
	Model               string  `json:"model"`
	StartedAt           string  `json:"startedAt"`
	EndedAt             string  `json:"endedAt"`
	TotalCost           float64 `json:"totalCost"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	MessageCount        int     `json:"messageCount"`
	EventCount          int     `json:"eventCount"`
	ErrorCount          int     `json:"errorCount"`
	Version             string  `json:"version,omitempty"`
	Entrypoint          string  `json:"entrypoint,omitempty"`
}

// ToSession converts a SessionRow to a session.Session with default runtime fields.
func (r *SessionRow) ToSession() *session.Session {
	s := &session.Session{
		ID:                  r.ID,
		RepoID:              r.RepoID,
		SessionName:         r.SessionName,
		TotalCost:           r.TotalCost,
		InputTokens:         r.InputTokens,
		OutputTokens:        r.OutputTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		MessageCount:        r.MessageCount,
		EventCount:          r.EventCount,
		ErrorCount:          r.ErrorCount,
		ParentID:            r.ParentID,
		CWD:                 r.CWD,
		GitBranch:           r.Branch,
		Model:               r.Model,
		TaskDescription:     r.TaskDescription,
		Version:             r.Version,
		Entrypoint:          r.Entrypoint,
		IsActive:            false,
		Status:              "idle",
		CostRate:            0,
	}
	if r.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, r.StartedAt); err == nil {
			s.StartedAt = t
		}
	}
	if r.EndedAt != "" {
		if t, err := time.Parse(time.RFC3339, r.EndedAt); err == nil {
			s.LastActive = t
		}
	}
	return s
}

// SessionRowsToSessions converts a slice of SessionRow to session.Session.
func SessionRowsToSessions(rows []SessionRow) []*session.Session {
	result := make([]*session.Session, len(rows))
	for i := range rows {
		result[i] = rows[i].ToSession()
	}
	return result
}

// EventRow represents a single event as stored in the database.
type EventRow struct {
	ID                  int64   `json:"id"`
	SessionID           string  `json:"sessionId"`
	UUID                string  `json:"uuid,omitempty"`
	MessageID           string  `json:"messageId,omitempty"`
	Type                string  `json:"type"`
	Role                string  `json:"role"`
	ContentPreview      string  `json:"contentPreview"`
	FullContent         string  `json:"fullContent,omitempty"`
	ToolName            string  `json:"toolName,omitempty"`
	ToolDetail          string  `json:"toolDetail,omitempty"`
	CostUSD             float64 `json:"costUSD"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	Model               string  `json:"model,omitempty"`
	IsError             bool    `json:"isError"`
	StopReason          string  `json:"stopReason,omitempty"`
	HookEvent           string  `json:"hookEvent,omitempty"`
	HookName            string  `json:"hookName,omitempty"`
	ToolUseID           string  `json:"toolUseId,omitempty"`
	ForToolUseID        string  `json:"forToolUseId,omitempty"`
	IsAgent             bool    `json:"isAgent,omitempty"`
	Timestamp           string  `json:"timestamp"`
	// New fields from JSONL capture.
	DurationMs        *int64  `json:"durationMs,omitempty"`
	Success           *bool   `json:"success,omitempty"`
	Stderr            string  `json:"stderr,omitempty"`
	Interrupted       bool    `json:"interrupted,omitempty"`
	Truncated         bool    `json:"truncated,omitempty"`
	AgentDurationMs   *int64  `json:"agentDurationMs,omitempty"`
	AgentTokens       *int64  `json:"agentTokens,omitempty"`
	AgentToolUseCount *int    `json:"agentToolUseCount,omitempty"`
	AgentType         string  `json:"agentType,omitempty"`
	Subtype           string  `json:"subtype,omitempty"`
	TurnMessageCount  *int    `json:"turnMessageCount,omitempty"`
	HookCount         *int    `json:"hookCount,omitempty"`
	HookInfos         string  `json:"hookInfos,omitempty"`
	Level             string  `json:"level,omitempty"`
	IsMeta            bool    `json:"isMeta,omitempty"`
	Version           string  `json:"version,omitempty"`
	Entrypoint        string  `json:"entrypoint,omitempty"`
}

// AggregateResult holds aggregate statistics across sessions.
type AggregateResult struct {
	TotalCost           float64            `json:"totalCost"`
	InputTokens         int64              `json:"inputTokens"`
	OutputTokens        int64              `json:"outputTokens"`
	CacheReadTokens     int64              `json:"cacheReadTokens"`
	CacheCreationTokens int64              `json:"cacheCreationTokens"`
	SessionCount        int                `json:"sessionCount"`
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

// TrendSummary holds totals across all buckets.
type TrendSummary struct {
	TotalCost       float64 `json:"totalCost"`
	EffectiveTokens int64   `json:"effectiveTokens"`
	CacheHitPct     float64 `json:"cacheHitPct"`
	SessionCount    int     `json:"sessionCount"`
}

// TrendResult holds the complete trend analysis response.
type TrendResult struct {
	Window  string        `json:"window"`
	Buckets []TrendBucket `json:"buckets"`
	ByRepo  []RepoTrend   `json:"byRepo"`
	ByModel []ModelTrend  `json:"byModel"`
	Summary TrendSummary  `json:"summary"`
}

// StorageInfo holds database storage statistics.
type StorageInfo struct {
	TotalSizeBytes  int64 `json:"totalSizeBytes"`
	HotContentBytes int64 `json:"hotContentBytes"`
	WarmContentBytes int64 `json:"warmContentBytes"`
	EventCount       int64 `json:"eventCount"`
}

// DB wraps a sql.DB connection to the SQLite database.
type DB struct {
	db *sql.DB
}

// Open opens a SQLite database at the given path and runs pending migrations.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// SQLite only supports one writer at a time. Limit the pool to one
	// connection to avoid "database is locked" errors from concurrent writes
	// across goroutines (pipeline flush, retention compaction, HTTP handlers).
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := migrations.RunUp(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &DB{db: sqlDB}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// Ping verifies the database connection is alive.
func (d *DB) Ping() error {
	return d.db.Ping()
}

// --- Repos ---

// UpsertRepo inserts or updates a repo record.
func (d *DB) UpsertRepo(r *repo.Repo) error {
	_, err := d.db.Exec(`INSERT INTO repos (id, name, url, first_seen) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, url=excluded.url`,
		r.ID, r.Name, r.URL, time.Now().Format(time.RFC3339))
	return err
}

// UpsertCwdRepo persists a cwd → repo_id mapping.
func (d *DB) UpsertCwdRepo(cwd, repoID string) error {
	_, err := d.db.Exec(`INSERT INTO cwd_repos (cwd, repo_id) VALUES (?, ?)
		ON CONFLICT(cwd) DO UPDATE SET repo_id=excluded.repo_id`, cwd, repoID)
	return err
}

// LoadCwdRepos returns all persisted cwd → repo_id mappings.
func (d *DB) LoadCwdRepos() (map[string]string, error) {
	rows, err := d.db.Query(`SELECT cwd, repo_id FROM cwd_repos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var cwd, repoID string
		if err := rows.Scan(&cwd, &repoID); err != nil {
			return nil, err
		}
		result[cwd] = repoID
	}
	return result, rows.Err()
}

// ClearCwdRepos deletes all cwd → repo_id mappings.
func (d *DB) ClearCwdRepos() error {
	_, err := d.db.Exec(`DELETE FROM cwd_repos`)
	return err
}

// --- Sessions ---

// SaveSession upserts a session into the sessions table from live session state.
func (d *DB) SaveSession(s *session.Session) error {
	var endedAt string
	var startedAt string
	if !s.LastActive.IsZero() {
		endedAt = s.LastActive.Format(time.RFC3339)
	}
	if !s.StartedAt.IsZero() {
		startedAt = s.StartedAt.Format(time.RFC3339)
	}

	_, err := d.db.Exec(`INSERT INTO sessions
		(id, repo_id, parent_id, session_name, task_description, cwd, branch, model,
		 started_at, ended_at, total_cost, input_tokens, output_tokens,
		 cache_read_tokens, cache_creation_tokens, message_count, event_count, error_count,
		 version, entrypoint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		 repo_id=excluded.repo_id,
		 parent_id=excluded.parent_id,
		 session_name=excluded.session_name,
		 task_description=excluded.task_description,
		 cwd=excluded.cwd,
		 branch=excluded.branch,
		 model=excluded.model,
		 started_at=excluded.started_at,
		 ended_at=excluded.ended_at,
		 total_cost=excluded.total_cost,
		 input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 cache_creation_tokens=excluded.cache_creation_tokens,
		 message_count=excluded.message_count,
		 event_count=excluded.event_count,
		 error_count=excluded.error_count,
		 version=excluded.version,
		 entrypoint=excluded.entrypoint`,
		s.ID, s.RepoID, s.ParentID, s.SessionName, s.TaskDescription,
		s.CWD, s.GitBranch, s.Model, startedAt, endedAt,
		s.TotalCost, s.InputTokens, s.OutputTokens,
		s.CacheReadTokens, s.CacheCreationTokens,
		s.MessageCount, s.EventCount, s.ErrorCount,
		s.Version, s.Entrypoint,
	)
	return err
}

// ListSessions returns sessions ordered by ended_at descending.
func (d *DB) ListSessions(limit, offset int) ([]SessionRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.db.Query(`SELECT `+sessionSelectCols+`
		FROM sessions ORDER BY ended_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// GetSession returns a single session by ID.
func (d *DB) GetSession(id string) (*SessionRow, error) {
	rows, err := d.db.Query(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result, err := scanSessionRows(rows)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	return &result[0], nil
}

// ListSessionsByRepo returns sessions for a given repo, ordered by ended_at descending.
func (d *DB) ListSessionsByRepo(repoID string, limit, offset int) ([]SessionRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE repo_id = ? ORDER BY ended_at DESC LIMIT ? OFFSET ?`, repoID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// ListChildSessions returns all sessions whose parent_id matches the given session ID.
func (d *DB) ListChildSessions(parentID string) ([]SessionRow, error) {
	rows, err := d.db.Query(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE parent_id = ? ORDER BY started_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// AggregateStatsByRepo returns aggregate statistics for a single repo.
func (d *DB) AggregateStatsByRepo(repoID string) (*AggregateResult, error) {
	r := &AggregateResult{
		CostByModel: make(map[string]float64),
		CostByRepo:  make(map[string]float64),
	}
	err := d.db.QueryRow(`SELECT COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		COALESCE(SUM(cache_creation_tokens),0), COUNT(*)
		FROM sessions WHERE repo_id = ?`, repoID).Scan(
		&r.TotalCost, &r.InputTokens, &r.OutputTokens,
		&r.CacheReadTokens, &r.CacheCreationTokens, &r.SessionCount,
	)
	if err != nil {
		return nil, err
	}
	modelRows, err := d.db.Query(`SELECT COALESCE(model,''), SUM(total_cost)
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
	r.CostByRepo[repoID] = r.TotalCost
	return r, nil
}

// ListRepos returns all repos with their names.
type RepoRow struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	URL       string  `json:"url,omitempty"`
	TotalCost float64 `json:"totalCost"`
}

func (d *DB) ListRepos() ([]RepoRow, error) {
	rows, err := d.db.Query(`SELECT r.id, r.name, COALESCE(r.url,''),
		COALESCE(SUM(s.total_cost), 0)
		FROM repos r LEFT JOIN sessions s ON s.repo_id = r.id
		GROUP BY r.id ORDER BY COALESCE(SUM(s.total_cost),0) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RepoRow
	for rows.Next() {
		var r RepoRow
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.TotalCost); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func scanSessionRows(rows *sql.Rows) ([]SessionRow, error) {
	var result []SessionRow
	for rows.Next() {
		var r SessionRow
		if err := rows.Scan(
			&r.ID, &r.RepoID, &r.ParentID, &r.SessionName,
			&r.TaskDescription, &r.CWD, &r.Branch, &r.Model,
			&r.StartedAt, &r.EndedAt,
			&r.TotalCost, &r.InputTokens, &r.OutputTokens,
			&r.CacheReadTokens, &r.CacheCreationTokens,
			&r.MessageCount, &r.EventCount, &r.ErrorCount,
			&r.Version, &r.Entrypoint,
		); err != nil {
			return result, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ListMostExpensiveSessions returns sessions sorted by cost descending.
// If since is non-zero, only sessions started on or after that time are included.
func (d *DB) ListMostExpensiveSessions(since time.Time, limit int) ([]SessionRow, error) {
	if limit <= 0 {
		limit = 5
	}
	query := `SELECT ` + sessionSelectCols + ` FROM sessions`
	var args []interface{}
	if !since.IsZero() {
		query += ` WHERE started_at >= ?`
		args = append(args, since.Format(time.RFC3339))
	}
	query += ` ORDER BY total_cost DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// AggregateStats returns aggregate statistics. Includes ALL sessions (no parent filter).
func (d *DB) AggregateStats(since time.Time) (*AggregateResult, error) {
	r := &AggregateResult{
		CostByModel: make(map[string]float64),
		CostByRepo:  make(map[string]float64),
	}

	var where string
	var args []interface{}
	if !since.IsZero() {
		where = ` WHERE started_at >= ?`
		args = append(args, since.Format(time.RFC3339))
	}

	err := d.db.QueryRow(`SELECT COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0),
		COALESCE(SUM(output_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		COALESCE(SUM(cache_creation_tokens),0), COUNT(*)
		FROM sessions`+where, args...).Scan(
		&r.TotalCost, &r.InputTokens, &r.OutputTokens,
		&r.CacheReadTokens, &r.CacheCreationTokens, &r.SessionCount,
	)
	if err != nil {
		return nil, err
	}

	// Cost by model
	modelRows, err := d.db.Query(`SELECT COALESCE(model,''), SUM(total_cost)
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
	repoRows, err := d.db.Query(`SELECT COALESCE(repo_id,''), SUM(total_cost)
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

// TrendData returns time-bucketed analytics for the given window and optional repo filter.
// window must be "24h", "7d", or "30d". repoID may be empty for all repos.
func (d *DB) TrendData(window string, repoID string) (*TrendResult, error) {
	var dateFmt string
	var interval string
	switch window {
	case "24h":
		dateFmt = "%Y-%m-%d %H:00"
		interval = "-1 days"
	case "7d":
		dateFmt = "%Y-%m-%d"
		interval = "-7 days"
	case "30d":
		dateFmt = "%Y-%m-%d"
		interval = "-30 days"
	default:
		return nil, fmt.Errorf("invalid window: %s", window)
	}

	where := fmt.Sprintf("started_at >= datetime('now', '%s') AND (parent_id IS NULL OR parent_id = '')", interval)
	var args []interface{}
	if repoID != "" {
		where += " AND repo_id = ?"
		args = append(args, repoID)
	}

	// Query 1: Bucket aggregation
	bucketQuery := fmt.Sprintf(`SELECT strftime('%s', started_at) AS bucket,
		COALESCE(SUM(total_cost),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_creation_tokens),0),
		COUNT(*), COALESCE(AVG(total_cost),0)
		FROM sessions WHERE %s
		GROUP BY bucket ORDER BY bucket`, dateFmt, where)

	rows, err := d.db.Query(bucketQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend buckets: %w", err)
	}
	defer rows.Close()

	var buckets []TrendBucket
	for rows.Next() {
		var b TrendBucket
		if err := rows.Scan(&b.Date, &b.Cost, &b.InputTokens, &b.OutputTokens,
			&b.CacheReadTokens, &b.CacheCreationTokens, &b.SessionCount, &b.AvgSessionCost); err != nil {
			return nil, fmt.Errorf("scan bucket: %w", err)
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
		return nil, err
	}
	if buckets == nil {
		buckets = []TrendBucket{}
	}

	// Query 2: Percentiles — fetch all session costs per bucket
	percQuery := fmt.Sprintf(`SELECT strftime('%s', started_at) AS bucket, total_cost
		FROM sessions WHERE %s
		ORDER BY bucket, total_cost`, dateFmt, where)

	percRows, err := d.db.Query(percQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend percentiles: %w", err)
	}
	defer percRows.Close()

	// Collect costs grouped by bucket
	bucketCosts := make(map[string][]float64)
	for percRows.Next() {
		var bucket string
		var cost float64
		if err := percRows.Scan(&bucket, &cost); err != nil {
			return nil, fmt.Errorf("scan percentile: %w", err)
		}
		bucketCosts[bucket] = append(bucketCosts[bucket], cost)
	}
	if err := percRows.Err(); err != nil {
		return nil, err
	}

	// Apply percentiles to buckets
	for i := range buckets {
		costs := bucketCosts[buckets[i].Date]
		if len(costs) > 0 {
			buckets[i].MedianSessionCost = percentile(costs, 0.5)
			buckets[i].P95SessionCost = percentile(costs, 0.95)
		}
	}

	// Query 3: By repo — build WHERE with table-prefixed columns for JOIN
	repoWhere := fmt.Sprintf("s.started_at >= datetime('now', '%s') AND (s.parent_id IS NULL OR s.parent_id = '')", interval)
	if repoID != "" {
		repoWhere += " AND s.repo_id = ?"
	}
	repoQuery := fmt.Sprintf(`SELECT s.repo_id, COALESCE(r.name,''), COALESCE(SUM(s.total_cost),0),
		COALESCE(SUM(s.input_tokens + s.output_tokens + s.cache_read_tokens + s.cache_creation_tokens),0),
		COUNT(*)
		FROM sessions s JOIN repos r ON r.id = s.repo_id
		WHERE %s AND s.repo_id != ''
		GROUP BY r.id ORDER BY SUM(s.total_cost) DESC`, repoWhere)

	repoRows, err := d.db.Query(repoQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend by repo: %w", err)
	}
	defer repoRows.Close()

	var byRepo []RepoTrend
	for repoRows.Next() {
		var rt RepoTrend
		if err := repoRows.Scan(&rt.RepoID, &rt.RepoName, &rt.Cost, &rt.Tokens, &rt.Sessions); err != nil {
			return nil, fmt.Errorf("scan repo trend: %w", err)
		}
		byRepo = append(byRepo, rt)
	}
	if err := repoRows.Err(); err != nil {
		return nil, err
	}
	if byRepo == nil {
		byRepo = []RepoTrend{}
	}

	// Query 4: By model
	modelQuery := fmt.Sprintf(`SELECT COALESCE(model,'unknown'), COALESCE(SUM(total_cost),0),
		COALESCE(SUM(input_tokens + output_tokens + cache_read_tokens + cache_creation_tokens),0),
		COUNT(*)
		FROM sessions WHERE %s AND model != ''
		GROUP BY model ORDER BY SUM(total_cost) DESC`, where)

	modelRows, err := d.db.Query(modelQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend by model: %w", err)
	}
	defer modelRows.Close()

	var byModel []ModelTrend
	for modelRows.Next() {
		var mt ModelTrend
		if err := modelRows.Scan(&mt.Model, &mt.Cost, &mt.Tokens, &mt.Sessions); err != nil {
			return nil, fmt.Errorf("scan model trend: %w", err)
		}
		byModel = append(byModel, mt)
	}
	if err := modelRows.Err(); err != nil {
		return nil, err
	}
	if byModel == nil {
		byModel = []ModelTrend{}
	}

	// Build summary from buckets
	var summary TrendSummary
	var totalEffInput, totalCacheRead int64
	for _, b := range buckets {
		summary.TotalCost += b.Cost
		summary.EffectiveTokens += b.InputTokens + b.OutputTokens + b.CacheReadTokens + b.CacheCreationTokens
		summary.SessionCount += b.SessionCount
		totalEffInput += b.InputTokens + b.CacheReadTokens + b.CacheCreationTokens
		totalCacheRead += b.CacheReadTokens
	}
	if totalEffInput > 0 {
		summary.CacheHitPct = float64(totalCacheRead) / float64(totalEffInput) * 100
	}

	return &TrendResult{
		Window:  window,
		Buckets: buckets,
		ByRepo:  byRepo,
		ByModel: byModel,
		Summary: summary,
	}, nil
}

// percentile returns the value at the given percentile (0-1) from a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// --- Events ---

// EventBatch holds events to be persisted in a single transaction.
type EventBatch struct {
	Events []EventInsert
}

// EventInsert holds data for a single event to insert.
type EventInsert struct {
	SessionID   string
	Event       *parser.Event
	FullContent string // goes to event_content table
}

// PersistBatch inserts a batch of events and updates session aggregates in a single transaction.
func (d *DB) PersistBatch(batch *EventBatch) error {
	if len(batch.Events) == 0 {
		return nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin batch: %w", err)
	}
	defer tx.Rollback()

	eventStmt, err := tx.Prepare(`INSERT INTO events
		(session_id, uuid, message_id, type, role, content_preview, tool_name, tool_detail,
		 cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		 model, is_error, stop_reason, hook_event, hook_name,
		 tool_use_id, for_tool_use_id, is_agent, timestamp,
		 duration_ms, success, stderr, interrupted, truncated,
		 agent_duration_ms, agent_tokens, agent_tool_use_count, agent_type,
		 subtype, turn_message_count, hook_count, hook_infos, level,
		 is_meta, version, entrypoint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, COALESCE(message_id, uuid)) DO UPDATE SET
		 content_preview=excluded.content_preview,
		 cost_usd=excluded.cost_usd,
		 input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 cache_creation_tokens=excluded.cache_creation_tokens,
		 is_error=excluded.is_error,
		 stop_reason=excluded.stop_reason,
		 timestamp=excluded.timestamp,
		 duration_ms=excluded.duration_ms,
		 success=excluded.success,
		 stderr=excluded.stderr,
		 interrupted=excluded.interrupted,
		 truncated=excluded.truncated,
		 agent_duration_ms=excluded.agent_duration_ms,
		 agent_tokens=excluded.agent_tokens,
		 agent_tool_use_count=excluded.agent_tool_use_count,
		 agent_type=excluded.agent_type,
		 subtype=excluded.subtype,
		 turn_message_count=excluded.turn_message_count,
		 hook_count=excluded.hook_count,
		 hook_infos=excluded.hook_infos,
		 level=excluded.level,
		 is_meta=excluded.is_meta,
		 version=excluded.version,
		 entrypoint=excluded.entrypoint`)
	if err != nil {
		return fmt.Errorf("prepare event stmt: %w", err)
	}
	defer eventStmt.Close()

	contentStmt, err := tx.Prepare(`INSERT INTO event_content (event_id, tier, content) VALUES (?, 'hot', ?)
		ON CONFLICT(event_id) DO UPDATE SET content=excluded.content`)
	if err != nil {
		return fmt.Errorf("prepare content stmt: %w", err)
	}
	defer contentStmt.Close()

	// FTS5 uses INSERT OR REPLACE to handle both new inserts and updates.
	ftsStmt, err := tx.Prepare(`INSERT OR REPLACE INTO events_fts(rowid, content_preview, tool_name, tool_detail)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare fts stmt: %w", err)
	}
	defer ftsStmt.Close()

	// For upserts, LastInsertId is unreliable — look up the actual ID.
	lookupStmt, err := tx.Prepare(`SELECT id FROM events WHERE session_id = ? AND COALESCE(message_id, uuid) = ?`)
	if err != nil {
		return fmt.Errorf("prepare lookup stmt: %w", err)
	}
	defer lookupStmt.Close()

	for _, ei := range batch.Events {
		ev := ei.Event
		var messageID *string
		if ev.MessageID != "" {
			messageID = &ev.MessageID
		}

		result, err := eventStmt.Exec(
			ei.SessionID, ev.UUID, messageID, ev.Type, ev.Role,
			ev.ContentText, ev.ToolName, ev.ToolDetail,
			ev.CostUSD, ev.InputTokens, ev.OutputTokens,
			ev.CacheReadTokens, ev.CacheCreationTokens,
			ev.Model, ev.IsError, ev.StopReason,
			ev.HookEvent, ev.HookName,
			ev.ToolUseID, ev.ForToolUseID, ev.IsAgent,
			ev.Timestamp.Format(time.RFC3339Nano),
			ev.DurationMs, ev.Success, ev.Stderr, ev.Interrupted, ev.Truncated,
			ev.AgentDurationMs, ev.AgentTokens, ev.AgentToolUseCount, ev.AgentType,
			ev.Subtype, ev.TurnMessageCount, ev.HookCount, ev.HookInfos, ev.Level,
			ev.IsMeta, ev.Version, ev.Entrypoint,
		)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		// Resolve the actual event ID for content and FTS association.
		// ON CONFLICT may fire for any upsert, so LastInsertId is unreliable.
		// Look up by the dedup key: message_id if present, else uuid.
		var eventID int64
		dedupKey := ev.UUID
		if ev.MessageID != "" {
			dedupKey = ev.MessageID
		}
		if dedupKey != "" {
			if err := lookupStmt.QueryRow(ei.SessionID, dedupKey).Scan(&eventID); err != nil {
				return fmt.Errorf("lookup event id: %w", err)
			}
		} else {
			eventID, err = result.LastInsertId()
			if err != nil {
				return fmt.Errorf("get last insert id: %w", err)
			}
		}

		// Insert/update full content if present
		if ei.FullContent != "" {
			if _, err := contentStmt.Exec(eventID, ei.FullContent); err != nil {
				return fmt.Errorf("insert content: %w", err)
			}
		}

		// Update FTS5 index
		if _, err := ftsStmt.Exec(eventID, ev.ContentText, ev.ToolName, ev.ToolDetail); err != nil {
			return fmt.Errorf("insert fts: %w", err)
		}
	}

	return tx.Commit()
}

// ListEvents returns events for a session, ordered by timestamp.
func (d *DB) ListEvents(sessionID string, limit, offset int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.db.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,'')
		FROM events e LEFT JOIN event_content ec ON ec.event_id = e.id
		WHERE e.session_id = ?
		ORDER BY e.timestamp ASC
		LIMIT ? OFFSET ?`, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// ListPinnedEvents returns all error and agent events for a session, ordered chronologically.
// These are "pinned" because they should always be visible regardless of the recent-events window.
func (d *DB) ListPinnedEvents(sessionID string) ([]EventRow, error) {
	rows, err := d.db.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,'')
		FROM events e LEFT JOIN event_content ec ON ec.event_id = e.id
		WHERE e.session_id = ? AND (e.is_error = 1 OR e.is_agent = 1 OR e.content_preview LIKE '[agent%')
		ORDER BY e.timestamp ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// ListRecentEvents returns the last N events for a session.
func (d *DB) ListRecentEvents(sessionID string, n int) ([]EventRow, error) {
	if n <= 0 {
		n = 50
	}
	rows, err := d.db.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,'')
		FROM events e LEFT JOIN event_content ec ON ec.event_id = e.id
		WHERE e.session_id = ?
		ORDER BY e.timestamp DESC
		LIMIT ?`, sessionID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result, err := scanEventRows(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to chronological order
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

func scanEventRows(rows *sql.Rows) ([]EventRow, error) {
	var result []EventRow
	for rows.Next() {
		var r EventRow
		var fullContent string
		var durationMs, agentDurationMs, agentTokens sql.NullInt64
		var agentToolUseCount, turnMessageCount, hookCount sql.NullInt64
		var success sql.NullBool
		if err := rows.Scan(
			&r.ID, &r.SessionID, &r.UUID, &r.MessageID,
			&r.Type, &r.Role, &r.ContentPreview,
			&r.ToolName, &r.ToolDetail,
			&r.CostUSD, &r.InputTokens, &r.OutputTokens,
			&r.CacheReadTokens, &r.CacheCreationTokens,
			&r.Model, &r.IsError, &r.StopReason,
			&r.HookEvent, &r.HookName,
			&r.ToolUseID, &r.ForToolUseID, &r.IsAgent,
			&r.Timestamp,
			&durationMs, &success, &r.Stderr, &r.Interrupted, &r.Truncated,
			&agentDurationMs, &agentTokens, &agentToolUseCount, &r.AgentType,
			&r.Subtype, &turnMessageCount, &hookCount, &r.HookInfos, &r.Level,
			&r.IsMeta, &r.Version, &r.Entrypoint,
			&fullContent,
		); err != nil {
			return result, err
		}
		if fullContent != "" {
			r.FullContent = fullContent
		}
		if durationMs.Valid {
			v := durationMs.Int64; r.DurationMs = &v
		}
		if success.Valid {
			v := success.Bool; r.Success = &v
		}
		if agentDurationMs.Valid {
			v := agentDurationMs.Int64; r.AgentDurationMs = &v
		}
		if agentTokens.Valid {
			v := agentTokens.Int64; r.AgentTokens = &v
		}
		if agentToolUseCount.Valid {
			v := int(agentToolUseCount.Int64); r.AgentToolUseCount = &v
		}
		if turnMessageCount.Valid {
			v := int(turnMessageCount.Int64); r.TurnMessageCount = &v
		}
		if hookCount.Valid {
			v := int(hookCount.Int64); r.HookCount = &v
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// --- Search ---

// SearchFTS searches the FTS5 index for matching events.
func (d *DB) SearchFTS(query string, limit int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 50
	}
	// Wrap in double quotes to treat as a literal phrase, escaping any
	// internal double quotes. This prevents FTS5 syntax errors from
	// user-supplied queries (e.g. unbalanced quotes, bare operators).
	safe := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	rows, err := d.db.Query(`SELECT `+eventSelectCols+`,
		''
		FROM events_fts fts
		JOIN events e ON e.id = fts.rowid
		WHERE events_fts MATCH ?
		ORDER BY fts.rank * (1.0 + 10.0 / (1.0 + (julianday('now') - julianday(e.timestamp)) * 24.0))
		LIMIT ?`, safe, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// SearchFullContent scans event_content for a substring match (slower, for key leak detection etc).
func (d *DB) SearchFullContent(query string, limit int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,'')
		FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE ec.content LIKE ?
		ORDER BY e.timestamp DESC
		LIMIT ?`, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// --- Settings ---

// GetSetting returns a setting value by key.
func (d *DB) GetSetting(key string) (string, error) {
	var value string
	err := d.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, err
}

// SetSetting upserts a setting.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// AllSettings returns all settings as a map.
func (d *DB) AllSettings() (map[string]string, error) {
	rows, err := d.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}

// --- Retention ---

// CompactHotToWarm compresses event_content older than hotDays into gzip BLOBs.
func (d *DB) CompactHotToWarm(hotDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -hotDays).Format(time.RFC3339)

	rows, err := d.db.Query(`SELECT ec.event_id, ec.content FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE ec.tier = 'hot' AND ec.content IS NOT NULL AND e.timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type compactEntry struct {
		eventID int64
		data    []byte
	}
	var entries []compactEntry

	for rows.Next() {
		var eventID int64
		var content string
		if err := rows.Scan(&eventID, &content); err != nil {
			return 0, err
		}
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := io.WriteString(w, content); err != nil {
			w.Close()
			continue
		}
		w.Close()
		entries = append(entries, compactEntry{eventID: eventID, data: buf.Bytes()})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(entries) == 0 {
		return 0, nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE event_content SET tier='warm', content=NULL, compressed=? WHERE event_id=?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.Exec(e.data, e.eventID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(entries)), nil
}

// CompactWarmToCold deletes event_content older than warmDays.
func (d *DB) CompactWarmToCold(warmDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -warmDays).Format(time.RFC3339)
	result, err := d.db.Exec(`DELETE FROM event_content WHERE event_id IN (
		SELECT ec.event_id FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE e.timestamp < ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// StorageInfo returns storage statistics.
func (d *DB) StorageInfo() (*StorageInfo, error) {
	info := &StorageInfo{}

	// Total DB size via page_count * page_size
	if err := d.db.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&info.TotalSizeBytes); err != nil {
		// Fallback: just count events
		info.TotalSizeBytes = 0
	}

	d.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&info.EventCount)
	d.db.QueryRow(`SELECT COALESCE(SUM(LENGTH(content)),0) FROM event_content WHERE tier='hot'`).Scan(&info.HotContentBytes)
	d.db.QueryRow(`SELECT COALESCE(SUM(LENGTH(compressed)),0) FROM event_content WHERE tier='warm'`).Scan(&info.WarmContentBytes)

	return info, nil
}

