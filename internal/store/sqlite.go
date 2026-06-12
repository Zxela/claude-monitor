// Package store provides persistent storage using SQLite for the v2 data model.
package store

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store/migrations"

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
	e.is_meta, COALESCE(e.version,''), COALESCE(e.entrypoint,''),
	COALESCE(e.tool_use_ids,''), COALESCE(e.cwd,''), COALESCE(e.git_branch,''),
	e.is_sidechain, COALESCE(e.agent_name,''), COALESCE(e.team_name,'')`

// sessionSelectCols is the column list used by all session SELECT queries.
// Must match the scan order in scanSessionRows().
const sessionSelectCols = `id, COALESCE(repo_id,''), COALESCE(parent_id,''), COALESCE(session_name,''),
	COALESCE(task_description,''), COALESCE(cwd,''), COALESCE(branch,''),
	COALESCE(model,''), COALESCE(started_at,''), COALESCE(ended_at,''),
	total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
	message_count, event_count, error_count,
	COALESCE(version,''), COALESCE(entrypoint,''),
	COALESCE(workflow_id,''), COALESCE(agent_id,''), COALESCE(agent_kind,'')`

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
	WorkflowID          string  `json:"workflowId,omitempty"`
	AgentID             string  `json:"agentId,omitempty"`
	AgentKind           string  `json:"agentKind,omitempty"`
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
		WorkflowID:          r.WorkflowID,
		AgentID:             r.AgentID,
		AgentKind:           r.AgentKind,
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
	// Per-event metadata (migration 010)
	ToolUseIDs  string `json:"toolUseIds,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	GitBranch   string `json:"gitBranch,omitempty"`
	IsSidechain bool   `json:"isSidechain,omitempty"`
	AgentName   string `json:"agentName,omitempty"`
	TeamName    string `json:"teamName,omitempty"`
}

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
	Window  string        `json:"window"`
	Buckets []TrendBucket `json:"buckets"`
	ByRepo    []RepoTrend    `json:"byRepo"`
	ByModel   []ModelTrend   `json:"byModel"`
	BySession []SessionTrend `json:"bySession"`
	Summary   TrendSummary   `json:"summary"`
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
	db  *sql.DB // writes: single connection so concurrent writers queue instead of erroring
	rdb *sql.DB // reads: concurrent read-only pool so queries don't queue behind writes
}

// readPoolSize bounds concurrent read connections. The history page fires a
// handful of API queries per load; a small pool covers that without holding
// many file handles open.
const readPoolSize = 4

// dsn builds a file: URI for path with the given pragma options. SQLite's URI
// parser treats ? and # as delimiters and decodes %XX sequences, so those
// three characters must be escaped for the path to round-trip; everything
// else (including spaces) passes through literally.
func dsn(path, pragmas string) string {
	escaped := strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(path)
	return "file:" + escaped + "?" + pragmas
}

// Open opens a SQLite database at the given path and runs pending migrations.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", dsn(path, "_pragma=busy_timeout(5000)"))
	if err != nil {
		return nil, err
	}
	// SQLite only supports one writer at a time. Limit this pool to one
	// connection to avoid "database is locked" errors from concurrent writes
	// across goroutines (pipeline flush, retention compaction).
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	if _, err := migrations.RunUp(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}
	// WAL mode supports any number of readers concurrent with the single
	// writer. Reads go through a separate read-only pool so API queries are
	// not serialized behind long-running ingest or retention transactions.
	rdb, err := sql.Open("sqlite", dsn(path, "_pragma=busy_timeout(5000)&_pragma=query_only(1)"))
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	rdb.SetMaxOpenConns(readPoolSize)
	rdb.SetMaxIdleConns(readPoolSize)
	return &DB{db: sqlDB, rdb: rdb}, nil
}

// Close closes the underlying database connections.
func (d *DB) Close() error {
	rErr := d.rdb.Close()
	if err := d.db.Close(); err != nil {
		return err
	}
	return rErr
}

// Ping verifies both database pools are alive.
func (d *DB) Ping() error {
	return errors.Join(d.db.Ping(), d.rdb.Ping())
}

// --- Repos ---

// UpsertRepo inserts or updates a repo record.
func (d *DB) UpsertRepo(r *repo.Repo) error {
	_, err := d.db.Exec(`INSERT INTO repos (id, name, url, first_seen) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE repos.name END,
			url  = CASE WHEN excluded.url  <> '' THEN excluded.url  ELSE repos.url  END`,
		r.ID, r.Name, r.URL, time.Now().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("UpsertRepo: %w", err)
	}
	return nil
}

// UpsertCwdRepo persists a cwd → repo_id mapping.
func (d *DB) UpsertCwdRepo(cwd, repoID string) error {
	_, err := d.db.Exec(`INSERT INTO cwd_repos (cwd, repo_id) VALUES (?, ?)
		ON CONFLICT(cwd) DO UPDATE SET repo_id=excluded.repo_id`, cwd, repoID)
	if err != nil {
		return fmt.Errorf("UpsertCwdRepo: %w", err)
	}
	return nil
}

// LoadCwdRepos returns all persisted cwd → repo_id mappings.
func (d *DB) LoadCwdRepos() (map[string]string, error) {
	rows, err := d.rdb.Query(`SELECT cwd, repo_id FROM cwd_repos`)
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
	if err != nil {
		return fmt.Errorf("ClearCwdRepos: %w", err)
	}
	return nil
}

// --- Sessions ---

// SaveSession upserts a session into the sessions table from live session state.
func (d *DB) SaveSession(s *session.Session) error {
	var endedAt string
	var startedAt string
	// Store timestamps in canonical UTC RFC3339 ("…Z") so window-boundary and
	// bucketing comparisons (which are lexical TEXT compares in SQLite) are valid
	// regardless of the source time's zone. Production times are already UTC.
	if !s.LastActive.IsZero() {
		endedAt = s.LastActive.UTC().Format(time.RFC3339)
	}
	if !s.StartedAt.IsZero() {
		startedAt = s.StartedAt.UTC().Format(time.RFC3339)
	}

	_, err := d.db.Exec(`INSERT INTO sessions
		(id, repo_id, parent_id, session_name, task_description, cwd, branch, model,
		 started_at, ended_at, total_cost, input_tokens, output_tokens,
		 cache_read_tokens, cache_creation_tokens, message_count, event_count, error_count,
		 version, entrypoint, workflow_id, agent_id, agent_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		 entrypoint=excluded.entrypoint,
		 workflow_id=excluded.workflow_id,
		 agent_id=excluded.agent_id,
		 agent_kind=excluded.agent_kind`,
		s.ID, s.RepoID, s.ParentID, s.SessionName, s.TaskDescription,
		s.CWD, s.GitBranch, s.Model, startedAt, endedAt,
		s.TotalCost, s.InputTokens, s.OutputTokens,
		s.CacheReadTokens, s.CacheCreationTokens,
		s.MessageCount, s.EventCount, s.ErrorCount,
		s.Version, s.Entrypoint,
		s.WorkflowID, s.AgentID, s.AgentKind,
	)
	if err != nil {
		return fmt.Errorf("SaveSession: %w", err)
	}
	return nil
}

// FlushSessions upserts multiple sessions and their cwd->repo mappings in a single transaction.
func (d *DB) FlushSessions(sessions []*session.Session) error {
	if len(sessions) == 0 {
		return nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("FlushSessions begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			log.Printf("FlushSessions: rollback error: %v", rbErr)
		}
	}()

	sessStmt, err := tx.Prepare(`INSERT INTO sessions
		(id, repo_id, parent_id, session_name, task_description, cwd, branch, model,
		 started_at, ended_at, total_cost, input_tokens, output_tokens,
		 cache_read_tokens, cache_creation_tokens, message_count, event_count, error_count,
		 version, entrypoint, workflow_id, agent_id, agent_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		 entrypoint=excluded.entrypoint,
		 workflow_id=excluded.workflow_id,
		 agent_id=excluded.agent_id,
		 agent_kind=excluded.agent_kind`)
	if err != nil {
		return fmt.Errorf("FlushSessions prepare session: %w", err)
	}
	defer sessStmt.Close()

	cwdStmt, err := tx.Prepare(`INSERT INTO cwd_repos (cwd, repo_id) VALUES (?, ?)
		ON CONFLICT(cwd) DO UPDATE SET repo_id=excluded.repo_id`)
	if err != nil {
		return fmt.Errorf("FlushSessions prepare cwd: %w", err)
	}
	defer cwdStmt.Close()

	for _, s := range sessions {
		var endedAt string
		var startedAt string
		// Canonical UTC storage — see SaveSession. Keeps lexical window/bucket
		// comparisons valid; production times are already UTC.
		if !s.LastActive.IsZero() {
			endedAt = s.LastActive.UTC().Format(time.RFC3339)
		}
		if !s.StartedAt.IsZero() {
			startedAt = s.StartedAt.UTC().Format(time.RFC3339)
		}

		if _, err := sessStmt.Exec(
			s.ID, s.RepoID, s.ParentID, s.SessionName, s.TaskDescription,
			s.CWD, s.GitBranch, s.Model, startedAt, endedAt,
			s.TotalCost, s.InputTokens, s.OutputTokens,
			s.CacheReadTokens, s.CacheCreationTokens,
			s.MessageCount, s.EventCount, s.ErrorCount,
			s.Version, s.Entrypoint,
			s.WorkflowID, s.AgentID, s.AgentKind,
		); err != nil {
			return fmt.Errorf("FlushSessions save %s: %w", s.ID, err)
		}

		// Skip inherited child pins: their cwd is the child's own (worktree)
		// directory, not the parent's project, so recording cwd → inherited
		// repo_id would mis-attribute future unrelated sessions resolving the
		// same directory after a restart seeds the resolver cache from this table.
		if s.CWD != "" && s.RepoID != "" && !s.RepoInherited() {
			if _, err := cwdStmt.Exec(s.CWD, s.RepoID); err != nil {
				return fmt.Errorf("FlushSessions cwdRepo %s: %w", s.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("FlushSessions commit: %w", err)
	}
	return nil
}


// LoadMessageDedup returns the message-ID dedup maps for a session from persisted events.
// This is used to rebuild in-memory dedup state after a restart, preventing double-counting.
func (d *DB) LoadMessageDedup(sessionID string) (map[string]bool, map[string]session.MessageCosts, error) {
	rows, err := d.rdb.Query(`
		SELECT message_id, cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens
		FROM events
		WHERE session_id = ? AND message_id IS NOT NULL AND message_id != ''
		AND id IN (SELECT MAX(id) FROM events WHERE session_id = ? AND message_id IS NOT NULL AND message_id != '' GROUP BY message_id)`,
		sessionID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	ids := make(map[string]bool)
	costs := make(map[string]session.MessageCosts)
	for rows.Next() {
		var mid string
		var mc session.MessageCosts
		if err := rows.Scan(&mid, &mc.CostUSD, &mc.InputTokens, &mc.OutputTokens, &mc.CacheReadTokens, &mc.CacheCreationTokens); err != nil {
			return nil, nil, err
		}
		ids[mid] = true
		if mc.CostUSD > 0 || mc.InputTokens > 0 || mc.OutputTokens > 0 {
			costs[mid] = mc
		}
	}
	return ids, costs, rows.Err()
}

// LoadErrorDedup returns the distinct stable identities (message_id, else uuid)
// of a session's persisted error events. Used to rebuild the in-memory "err:"
// dedup keys after a restart so bootstrap replay does not re-increment
// error_count for errors already counted — the counterpart of LoadMessageDedup
// for the error path, matching CountSessionErrors' identity definition.
func (d *DB) LoadErrorDedup(sessionID string) ([]string, error) {
	rows, err := d.rdb.Query(`SELECT DISTINCT COALESCE(NULLIF(message_id,''), uuid)
		FROM events WHERE session_id = ? AND is_error = 1`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// CountSessionErrors returns the number of distinct error events for a session,
// deduped by the stable event identity (message_id, else uuid) so re-emitted
// error lines are counted once. This is the deterministic source of truth for
// sessions.error_count, matching what the events feed shows.
func (d *DB) CountSessionErrors(sessionID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`SELECT COUNT(DISTINCT COALESCE(NULLIF(message_id,''), uuid))
		FROM events WHERE session_id = ? AND is_error = 1`, sessionID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListSessions returns sessions ordered by ended_at descending.
func (d *DB) ListSessions(limit, offset int) ([]SessionRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.rdb.Query(`SELECT `+sessionSelectCols+`
		FROM sessions ORDER BY ended_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// GetSession returns a single session by ID.
func (d *DB) GetSession(id string) (*SessionRow, error) {
	rows, err := d.rdb.Query(`SELECT `+sessionSelectCols+`
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
	rows, err := d.rdb.Query(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE repo_id = ? ORDER BY ended_at DESC LIMIT ? OFFSET ?`, repoID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// ListSessionsByWorkflow returns sessions belonging to a workflow, ordered by ended_at descending.
func (d *DB) ListSessionsByWorkflow(workflowID string, limit, offset int) ([]SessionRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.rdb.Query(`SELECT `+sessionSelectCols+`
		FROM sessions WHERE workflow_id = ? ORDER BY ended_at DESC LIMIT ? OFFSET ?`, workflowID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// WorkflowRow is a per-workflow aggregate: one row per distinct workflow_id.
type WorkflowRow struct {
	WorkflowID string  `json:"workflowId"`
	AgentCount int     `json:"agentCount"`
	TotalCost  float64 `json:"totalCost"`
	LastActive string  `json:"lastActive"`
}

// ListWorkflows returns one aggregate row per distinct workflow_id, ordered by
// most-recent activity. Cost is summed across every agent row in the workflow,
// so a workflow's spend is counted exactly once across its agents.
//
// workflow_id is nullable (migration 013 adds it as plain TEXT, no DEFAULT ''),
// so the non-empty filter uses COALESCE(workflow_id,'') <> ''.
func (d *DB) ListWorkflows() ([]WorkflowRow, error) {
	rows, err := d.rdb.Query(`SELECT workflow_id, COUNT(*), COALESCE(SUM(total_cost),0), COALESCE(MAX(ended_at),'')
		FROM sessions WHERE COALESCE(workflow_id,'') <> '' GROUP BY workflow_id ORDER BY MAX(ended_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []WorkflowRow
	for rows.Next() {
		var wr WorkflowRow
		if err := rows.Scan(&wr.WorkflowID, &wr.AgentCount, &wr.TotalCost, &wr.LastActive); err != nil {
			return nil, err
		}
		result = append(result, wr)
	}
	return result, rows.Err()
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

// ListRepos returns all repos with their names.
type RepoRow struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	URL       string  `json:"url,omitempty"`
	TotalCost float64 `json:"totalCost"`
}

func (d *DB) ListRepos() ([]RepoRow, error) {
	rows, err := d.rdb.Query(`SELECT r.id, r.name, COALESCE(r.url,''),
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

// RepoExists reports whether a repo row with the given id exists. Used by the
// per-repo endpoints to return 404 for unknown ids (matching the session-by-id
// endpoint), rather than serving a zeroed 200.
func (d *DB) RepoExists(id string) (bool, error) {
	var one int
	err := d.rdb.QueryRow(`SELECT 1 FROM repos WHERE id = ? LIMIT 1`, id).Scan(&one)
	switch err {
	case nil:
		return true, nil
	case sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
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
			&r.WorkflowID, &r.AgentID, &r.AgentKind,
		); err != nil {
			return result, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
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
		       COALESCE(rt.session_name, ''),
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
		// Prefer an explicit session name, then the task/prompt; the frontend
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
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			log.Printf("PersistBatch: rollback error: %v", rbErr)
		}
	}()

	eventStmt, err := tx.Prepare(`INSERT INTO events
		(session_id, uuid, message_id, type, role, content_preview, tool_name, tool_detail,
		 cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		 model, is_error, stop_reason, hook_event, hook_name,
		 tool_use_id, for_tool_use_id, is_agent, timestamp,
		 duration_ms, success, stderr, interrupted, truncated,
		 agent_duration_ms, agent_tokens, agent_tool_use_count, agent_type,
		 subtype, turn_message_count, hook_count, hook_infos, level,
		 is_meta, version, entrypoint,
		 tool_use_ids, cwd, git_branch, is_sidechain, agent_name, team_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		        ?, ?, ?, ?, ?, ?)
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
		 entrypoint=excluded.entrypoint,
		 tool_use_ids=excluded.tool_use_ids,
		 cwd=excluded.cwd,
		 git_branch=excluded.git_branch,
		 is_sidechain=excluded.is_sidechain,
		 agent_name=excluded.agent_name,
		 team_name=excluded.team_name`)
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

	// events_fts is an external-content FTS5 table (content=events), so the
	// index must be kept in sync explicitly. INSERT OR REPLACE does NOT purge an
	// existing row's old terms (it has no stored content to diff against), which
	// leaves stale phantom tokens that produce false-positive search matches.
	// The correct sequence on update is: emit the FTS5 'delete' command with the
	// row's OLD indexed values, then INSERT the new values.
	ftsDeleteStmt, err := tx.Prepare(`INSERT INTO events_fts(events_fts, rowid, content_preview, tool_name, tool_detail)
		VALUES ('delete', ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare fts delete stmt: %w", err)
	}
	defer ftsDeleteStmt.Close()

	ftsInsertStmt, err := tx.Prepare(`INSERT INTO events_fts(rowid, content_preview, tool_name, tool_detail)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare fts insert stmt: %w", err)
	}
	defer ftsInsertStmt.Close()

	// For upserts, LastInsertId is unreliable — look up the actual ID.
	lookupStmt, err := tx.Prepare(`SELECT id FROM events WHERE session_id = ? AND COALESCE(message_id, uuid) = ?`)
	if err != nil {
		return fmt.Errorf("prepare lookup stmt: %w", err)
	}
	defer lookupStmt.Close()

	// Fetch the OLD indexed FTS values for an existing row (by dedup key) so we
	// can purge its stale terms before re-indexing the upserted row.
	oldFTSStmt, err := tx.Prepare(`SELECT id, COALESCE(content_preview,''), COALESCE(tool_name,''), COALESCE(tool_detail,'')
		FROM events WHERE session_id = ? AND COALESCE(message_id, uuid) = ?`)
	if err != nil {
		return fmt.Errorf("prepare old fts lookup stmt: %w", err)
	}
	defer oldFTSStmt.Close()

	for _, ei := range batch.Events {
		ev := ei.Event
		var messageID *string
		if ev.MessageID != "" {
			messageID = &ev.MessageID
		}

		var toolUseIDsJSON string
		if len(ev.ToolUseIDs) > 0 {
			if b, err := json.Marshal(ev.ToolUseIDs); err == nil {
				toolUseIDsJSON = string(b)
			}
		}

		// Capture the OLD indexed FTS values BEFORE the upsert overwrites them,
		// so an existing row's stale terms can be purged from events_fts.
		dedupKey := ev.UUID
		if ev.MessageID != "" {
			dedupKey = ev.MessageID
		}
		var (
			oldRowExists                          bool
			oldRowID                              int64
			oldPreview, oldToolName, oldToolDetail string
		)
		if dedupKey != "" {
			switch err := oldFTSStmt.QueryRow(ei.SessionID, dedupKey).Scan(&oldRowID, &oldPreview, &oldToolName, &oldToolDetail); err {
			case nil:
				oldRowExists = true
			case sql.ErrNoRows:
				// new row — nothing to purge
			default:
				return fmt.Errorf("lookup old fts values: %w", err)
			}
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
			toolUseIDsJSON, ev.CWD, ev.GitBranch, ev.IsSidechain, ev.AgentName, ev.TeamName,
		)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		// Resolve the actual event ID for content and FTS association.
		// ON CONFLICT may fire for any upsert, so LastInsertId is unreliable.
		// Look up by the dedup key: message_id if present, else uuid.
		var eventID int64
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

		// Update FTS5 index. For an existing row, purge its OLD terms first (the
		// FTS5 'delete' command requires the previously-indexed values and the
		// matching rowid), then index the NEW values. For a brand-new row, just
		// insert. This keeps the external-content index free of stale phantom
		// tokens that would otherwise yield false-positive search matches.
		if oldRowExists {
			if _, err := ftsDeleteStmt.Exec(oldRowID, oldPreview, oldToolName, oldToolDetail); err != nil {
				return fmt.Errorf("delete stale fts: %w", err)
			}
		}
		if _, err := ftsInsertStmt.Exec(eventID, ev.ContentText, ev.ToolName, ev.ToolDetail); err != nil {
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
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,''), ec.compressed
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

// ListReplayEvents returns events for a session plus all events belonging to its
// direct children (subagents / workflow agents linked via parent_id), merged in
// chronological order. Used by the replay endpoint so a parent (or workflow root)
// plays back as a single timeline.
//
// parent_id is the Phase-1 in-content parentage link, so selecting children by
// parent_id covers both shape-2 subagents and shape-3 workflow agents without a
// separate workflow join. A leaf session with no children returns exactly the
// same rows as ListEvents (the subquery matches nothing), so this is backward
// compatible. The secondary `e.id ASC` sort keeps the timeline deterministic
// when parent and child events share a timestamp.
func (d *DB) ListReplayEvents(sessionID string, limit, offset int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,''), ec.compressed
		FROM events e LEFT JOIN event_content ec ON ec.event_id = e.id
		WHERE e.session_id = ?
		   OR e.session_id IN (SELECT id FROM sessions WHERE parent_id = ?)
		ORDER BY e.timestamp ASC, e.id ASC
		LIMIT ? OFFSET ?`, sessionID, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// CountReplayEvents returns the total number of events a replay covers for a
// session (the session's own events plus its direct children), mirroring the
// WHERE clause of ListReplayEvents. Used to emit total/hasMore so the UI can
// detect truncation.
func (d *DB) CountReplayEvents(sessionID string) (int, error) {
	var n int
	err := d.rdb.QueryRow(`SELECT COUNT(*) FROM events e
		WHERE e.session_id = ?
		   OR e.session_id IN (SELECT id FROM sessions WHERE parent_id = ?)`,
		sessionID, sessionID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListPinnedEvents returns all error and agent events for a session, ordered chronologically.
// These are "pinned" because they should always be visible regardless of the recent-events window.
func (d *DB) ListPinnedEvents(sessionID string) ([]EventRow, error) {
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,''), ec.compressed
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
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,''), ec.compressed
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
		var compressed []byte
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
			&r.ToolUseIDs, &r.CWD, &r.GitBranch, &r.IsSidechain, &r.AgentName, &r.TeamName,
			&fullContent, &compressed,
		); err != nil {
			return result, err
		}
		if fullContent != "" {
			r.FullContent = fullContent
		} else if len(compressed) > 0 {
			if s, err := decompressContent(compressed); err == nil {
				r.FullContent = s
			}
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

// buildFTSMatch turns a free-text query into a safe FTS5 MATCH expression by
// quoting each whitespace-separated term and joining them with spaces (implicit
// AND). Each term is a double-quoted literal with internal quotes doubled, so
// FTS5 operators and unbalanced quotes are neutralized and cannot trigger
// syntax errors. Returns "" when the query has no usable terms.
func buildFTSMatch(query string) string {
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}

// SearchFTS searches the FTS5 index for matching events.
func (d *DB) SearchFTS(query string, limit int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 50
	}
	// Tokenize on whitespace and quote each term individually, then join the
	// quoted terms with a space. FTS5 implicitly ANDs space-separated phrases,
	// so `session not` becomes `"session" "not"` (matches docs containing both
	// words anywhere) rather than `"session not"` (adjacent-only). Per-token
	// quoting preserves the safety property: every term is a quoted literal, so
	// bare operators and unbalanced quotes cannot cause FTS5 syntax errors.
	safe := buildFTSMatch(query)
	if safe == "" {
		// No usable terms (empty/whitespace-only query) — return no results
		// rather than issuing an invalid empty MATCH.
		return []EventRow{}, nil
	}
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		'', NULL
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
	// Escape LIKE wildcards to prevent injection of % and _ characters.
	escaped := strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		COALESCE(ec.content,''), ec.compressed
		FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE ec.content LIKE ? ESCAPE '\'
		ORDER BY e.timestamp DESC
		LIMIT ?`, "%"+escaped+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// --- Model Pricing ---

// ModelPricing holds per-million-token pricing for a model prefix.
type ModelPricing struct {
	InputPerMTok       float64 `json:"input_per_mtok"`
	OutputPerMTok      float64 `json:"output_per_mtok"`
	CacheReadPerMTok   float64 `json:"cache_read_per_mtok"`
	CacheCreatePerMTok float64 `json:"cache_create_per_mtok"`
}

// AllModelPricing returns all rows from the model_pricing table as a map keyed by model_prefix.
func (d *DB) AllModelPricing() (map[string]ModelPricing, error) {
	rows, err := d.rdb.Query(`SELECT model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok FROM model_pricing`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]ModelPricing)
	for rows.Next() {
		var prefix string
		var p ModelPricing
		if err := rows.Scan(&prefix, &p.InputPerMTok, &p.OutputPerMTok, &p.CacheReadPerMTok, &p.CacheCreatePerMTok); err != nil {
			return nil, err
		}
		result[prefix] = p
	}
	return result, rows.Err()
}

// UpsertModelPricing inserts or replaces a pricing row for the given model prefix.
func (d *DB) UpsertModelPricing(prefix string, p ModelPricing) error {
	_, err := d.db.Exec(
		`INSERT INTO model_pricing (model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(model_prefix) DO UPDATE SET
		 input_per_mtok=excluded.input_per_mtok,
		 output_per_mtok=excluded.output_per_mtok,
		 cache_read_per_mtok=excluded.cache_read_per_mtok,
		 cache_create_per_mtok=excluded.cache_create_per_mtok`,
		prefix, p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheCreatePerMTok,
	)
	if err != nil {
		return fmt.Errorf("UpsertModelPricing: %w", err)
	}
	return nil
}

// --- Settings ---

// GetSetting returns a setting value by key.
func (d *DB) GetSetting(key string) (string, error) {
	var value string
	err := d.rdb.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return value, fmt.Errorf("GetSetting(%s): %w", key, err)
	}
	return value, nil
}

// SetSetting upserts a setting.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("SetSetting: %w", err)
	}
	return nil
}

// AllSettings returns all settings as a map.
func (d *DB) AllSettings() (map[string]string, error) {
	rows, err := d.rdb.Query(`SELECT key, value FROM settings`)
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

// decompressContent inflates gzip-compressed event content from a warm-tier BLOB.
func decompressContent(data []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decompressContent: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("decompressContent: %w", err)
	}
	return string(out), nil
}

// CompactHotToWarm compresses event_content older than hotDays into gzip BLOBs.
func (d *DB) CompactHotToWarm(hotDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -hotDays).Format(time.RFC3339)

	rows, err := d.rdb.Query(`SELECT ec.event_id, ec.content FROM event_content ec
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
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			log.Printf("CompactHotToWarm: rollback error: %v", rbErr)
		}
	}()

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
	if err := d.rdb.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&info.TotalSizeBytes); err != nil {
		// Fallback: just count events
		info.TotalSizeBytes = 0
	}

	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&info.EventCount); err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}
	if err := d.rdb.QueryRow(`SELECT COALESCE(SUM(LENGTH(content)),0) FROM event_content WHERE tier='hot'`).Scan(&info.HotContentBytes); err != nil {
		return nil, fmt.Errorf("hot content size: %w", err)
	}
	if err := d.rdb.QueryRow(`SELECT COALESCE(SUM(LENGTH(compressed)),0) FROM event_content WHERE tier='warm'`).Scan(&info.WarmContentBytes); err != nil {
		return nil, fmt.Errorf("warm content size: %w", err)
	}

	return info, nil
}

