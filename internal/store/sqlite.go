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
		 cache_read_tokens, cache_creation_tokens, message_count, event_count, error_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		 error_count=excluded.error_count`,
		s.ID, s.RepoID, s.ParentID, s.SessionName, s.TaskDescription,
		s.CWD, s.GitBranch, s.Model, startedAt, endedAt,
		s.TotalCost, s.InputTokens, s.OutputTokens,
		s.CacheReadTokens, s.CacheCreationTokens,
		s.MessageCount, s.EventCount, s.ErrorCount,
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
	rows, err := d.db.Query(`SELECT
		id, COALESCE(repo_id,''), COALESCE(parent_id,''), COALESCE(session_name,''),
		COALESCE(task_description,''), COALESCE(cwd,''), COALESCE(branch,''),
		COALESCE(model,''), COALESCE(started_at,''), COALESCE(ended_at,''),
		total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		message_count, event_count, error_count
		FROM sessions ORDER BY ended_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// GetSession returns a single session by ID.
func (d *DB) GetSession(id string) (*SessionRow, error) {
	rows, err := d.db.Query(`SELECT
		id, COALESCE(repo_id,''), COALESCE(parent_id,''), COALESCE(session_name,''),
		COALESCE(task_description,''), COALESCE(cwd,''), COALESCE(branch,''),
		COALESCE(model,''), COALESCE(started_at,''), COALESCE(ended_at,''),
		total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		message_count, event_count, error_count
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
	rows, err := d.db.Query(`SELECT
		id, COALESCE(repo_id,''), COALESCE(parent_id,''), COALESCE(session_name,''),
		COALESCE(task_description,''), COALESCE(cwd,''), COALESCE(branch,''),
		COALESCE(model,''), COALESCE(started_at,''), COALESCE(ended_at,''),
		total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		message_count, event_count, error_count
		FROM sessions WHERE repo_id = ? ORDER BY ended_at DESC LIMIT ? OFFSET ?`, repoID, limit, offset)
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
		); err != nil {
			return result, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
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
		 tool_use_id, for_tool_use_id, is_agent, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, message_id) DO UPDATE SET
		 content_preview=excluded.content_preview,
		 cost_usd=excluded.cost_usd,
		 input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 cache_creation_tokens=excluded.cache_creation_tokens,
		 is_error=excluded.is_error,
		 stop_reason=excluded.stop_reason,
		 timestamp=excluded.timestamp`)
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
	lookupStmt, err := tx.Prepare(`SELECT id FROM events WHERE session_id = ? AND message_id = ?`)
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

		_, err := eventStmt.Exec(
			ei.SessionID, ev.UUID, messageID, ev.Type, ev.Role,
			ev.ContentText, ev.ToolName, ev.ToolDetail,
			ev.CostUSD, ev.InputTokens, ev.OutputTokens,
			ev.CacheReadTokens, ev.CacheCreationTokens,
			ev.Model, ev.IsError, ev.StopReason,
			ev.HookEvent, ev.HookName,
			ev.ToolUseID, ev.ForToolUseID, ev.IsAgent,
			ev.Timestamp.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		// Resolve the actual event ID — for upserts, LastInsertId is unreliable.
		var eventID int64
		if ev.MessageID != "" {
			if err := lookupStmt.QueryRow(ei.SessionID, ev.MessageID).Scan(&eventID); err != nil {
				return fmt.Errorf("lookup event id: %w", err)
			}
		} else {
			// No message_id means a fresh insert — use last_insert_rowid().
			if err := tx.QueryRow(`SELECT last_insert_rowid()`).Scan(&eventID); err != nil {
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
	rows, err := d.db.Query(`SELECT
		e.id, e.session_id, COALESCE(e.uuid,''), COALESCE(e.message_id,''),
		COALESCE(e.type,''), COALESCE(e.role,''), COALESCE(e.content_preview,''),
		COALESCE(e.tool_name,''), COALESCE(e.tool_detail,''),
		e.cost_usd, e.input_tokens, e.output_tokens, e.cache_read_tokens, e.cache_creation_tokens,
		COALESCE(e.model,''), e.is_error, COALESCE(e.stop_reason,''),
		COALESCE(e.hook_event,''), COALESCE(e.hook_name,''),
		COALESCE(e.tool_use_id,''), COALESCE(e.for_tool_use_id,''), e.is_agent,
		e.timestamp,
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

// ListRecentEvents returns the last N events for a session.
func (d *DB) ListRecentEvents(sessionID string, n int) ([]EventRow, error) {
	if n <= 0 {
		n = 50
	}
	rows, err := d.db.Query(`SELECT
		e.id, e.session_id, COALESCE(e.uuid,''), COALESCE(e.message_id,''),
		COALESCE(e.type,''), COALESCE(e.role,''), COALESCE(e.content_preview,''),
		COALESCE(e.tool_name,''), COALESCE(e.tool_detail,''),
		e.cost_usd, e.input_tokens, e.output_tokens, e.cache_read_tokens, e.cache_creation_tokens,
		COALESCE(e.model,''), e.is_error, COALESCE(e.stop_reason,''),
		COALESCE(e.hook_event,''), COALESCE(e.hook_name,''),
		COALESCE(e.tool_use_id,''), COALESCE(e.for_tool_use_id,''), e.is_agent,
		e.timestamp,
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
			&fullContent,
		); err != nil {
			return result, err
		}
		if fullContent != "" {
			r.FullContent = fullContent
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
	rows, err := d.db.Query(`SELECT
		e.id, e.session_id, COALESCE(e.uuid,''), COALESCE(e.message_id,''),
		COALESCE(e.type,''), COALESCE(e.role,''), COALESCE(e.content_preview,''),
		COALESCE(e.tool_name,''), COALESCE(e.tool_detail,''),
		e.cost_usd, e.input_tokens, e.output_tokens, e.cache_read_tokens, e.cache_creation_tokens,
		COALESCE(e.model,''), e.is_error, COALESCE(e.stop_reason,''),
		COALESCE(e.hook_event,''), COALESCE(e.hook_name,''),
		COALESCE(e.tool_use_id,''), COALESCE(e.for_tool_use_id,''), e.is_agent,
		e.timestamp,
		''
		FROM events_fts fts
		JOIN events e ON e.id = fts.rowid
		WHERE events_fts MATCH ?
		ORDER BY fts.rank
		LIMIT ?`, query, limit)
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
	rows, err := d.db.Query(`SELECT
		e.id, e.session_id, COALESCE(e.uuid,''), COALESCE(e.message_id,''),
		COALESCE(e.type,''), COALESCE(e.role,''), COALESCE(e.content_preview,''),
		COALESCE(e.tool_name,''), COALESCE(e.tool_detail,''),
		e.cost_usd, e.input_tokens, e.output_tokens, e.cache_read_tokens, e.cache_creation_tokens,
		COALESCE(e.model,''), e.is_error, COALESCE(e.stop_reason,''),
		COALESCE(e.hook_event,''), COALESCE(e.hook_name,''),
		COALESCE(e.tool_use_id,''), COALESCE(e.for_tool_use_id,''), e.is_agent,
		e.timestamp,
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

// --- Legacy compatibility ---

// SessionSnapshot holds a point-in-time snapshot of session cost/token data.
type SessionSnapshot struct {
	TotalCost           float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// GetSessionSnapshots returns snapshots for the given session IDs.
func (d *DB) GetSessionSnapshots(ids []string) (map[string]SessionSnapshot, error) {
	result := make(map[string]SessionSnapshot)
	if len(ids) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := d.db.Query(`SELECT id, total_cost, input_tokens, output_tokens, cache_read_tokens,
		cache_creation_tokens FROM sessions
		WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var s SessionSnapshot
		if err := rows.Scan(&id, &s.TotalCost, &s.InputTokens, &s.OutputTokens,
			&s.CacheReadTokens, &s.CacheCreationTokens); err != nil {
			return nil, err
		}
		result[id] = s
	}

	return result, rows.Err()
}
