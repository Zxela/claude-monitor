// Package store provides persistent storage for session history using SQLite.
package store

import (
	"database/sql"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/store/migrations"

	_ "modernc.org/sqlite"
)

// HistoryRow represents a single row in the session_history table.
type HistoryRow struct {
	ID              string  `json:"id"`
	ProjectName     string  `json:"projectName"`
	SessionName     string  `json:"sessionName"`
	TotalCost       float64 `json:"totalCost"`
	InputTokens     int64   `json:"inputTokens"`
	OutputTokens    int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	MessageCount        int     `json:"messageCount"`
	ErrorCount      int     `json:"errorCount"`
	StartedAt       string  `json:"startedAt"`
	EndedAt         string  `json:"endedAt"`
	DurationSeconds float64 `json:"durationSeconds"`
	Model           string  `json:"model"`
	CWD             string  `json:"cwd"`
	GitBranch       string  `json:"gitBranch"`
	TaskDescription string  `json:"taskDescription"`
	ParentID        string  `json:"parentId"`
}

// DB wraps a sql.DB connection to the history SQLite database.
type DB struct {
	db *sql.DB
}

// Open opens a SQLite database at the given path and creates the schema if needed.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Enable WAL mode for better concurrency.
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

// SaveSession upserts a session into the history table.
func (d *DB) SaveSession(s *session.Session) error {
	var endedAt string
	var duration float64
	if !s.LastActive.IsZero() {
		endedAt = s.LastActive.Format(time.RFC3339)
	}
	if !s.StartedAt.IsZero() && !s.LastActive.IsZero() {
		duration = s.LastActive.Sub(s.StartedAt).Seconds()
	}
	var startedAt string
	if !s.StartedAt.IsZero() {
		startedAt = s.StartedAt.Format(time.RFC3339)
	}

	_, err := d.db.Exec(`INSERT INTO session_history
		(id, project_name, session_name, total_cost, input_tokens, output_tokens,
		 cache_read_tokens, cache_creation_tokens, message_count, error_count, started_at, ended_at,
		 duration_seconds, model, cwd, git_branch, task_description, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		 project_name=excluded.project_name,
		 session_name=excluded.session_name,
		 total_cost=excluded.total_cost,
		 input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 cache_creation_tokens=excluded.cache_creation_tokens,
		 message_count=excluded.message_count,
		 error_count=excluded.error_count,
		 started_at=excluded.started_at,
		 ended_at=excluded.ended_at,
		 duration_seconds=excluded.duration_seconds,
		 model=excluded.model,
		 cwd=excluded.cwd,
		 git_branch=excluded.git_branch,
		 task_description=excluded.task_description,
		 parent_id=excluded.parent_id`,
		s.ID, s.ProjectName, s.SessionName, s.TotalCost,
		s.InputTokens, s.OutputTokens, s.CacheReadTokens, s.CacheCreationTokens,
		s.MessageCount, s.ErrorCount,
		startedAt, endedAt, duration,
		s.Model, s.CWD, s.GitBranch, s.TaskDescription, s.ParentID,
	)
	return err
}

// ListHistory returns historical session rows ordered by ended_at descending.
func (d *DB) ListHistory(limit, offset int) ([]HistoryRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.db.Query(`SELECT
		id, project_name, session_name, total_cost, input_tokens, output_tokens,
		cache_read_tokens, COALESCE(cache_creation_tokens, 0), message_count, error_count,
		started_at, ended_at, duration_seconds, model, cwd, git_branch,
		task_description, parent_id
		FROM session_history
		ORDER BY ended_at DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []HistoryRow
	for rows.Next() {
		var r HistoryRow
		if err := rows.Scan(
			&r.ID, &r.ProjectName, &r.SessionName, &r.TotalCost,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens,
			&r.CacheCreationTokens, &r.MessageCount, &r.ErrorCount,
			&r.StartedAt, &r.EndedAt,
			&r.DurationSeconds, &r.Model, &r.CWD, &r.GitBranch,
			&r.TaskDescription, &r.ParentID,
		); err != nil {
			return result, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
