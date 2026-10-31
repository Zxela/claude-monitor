package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"

	_ "modernc.org/sqlite"
)

// sessionSelectCols is the column list used by all session SELECT queries.
// Must match the scan order in scanSessionRows().
const sessionSelectCols = `id, COALESCE(repo_id,''), COALESCE(parent_id,''), COALESCE(session_name,''),
	COALESCE(task_description,''), COALESCE(cwd,''), COALESCE(branch,''),
	COALESCE(model,''), COALESCE(started_at,''), COALESCE(ended_at,''),
	total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
	message_count, event_count, error_count,
	COALESCE(version,''), COALESCE(entrypoint,''),
	COALESCE(workflow_id,''), COALESCE(agent_id,''), COALESCE(agent_kind,''),
	COALESCE(ai_title,''), COALESCE(pr_links,'')`

// SessionRow represents a session as stored in the database.
type SessionRow struct {
	ID                  string           `json:"id"`
	RepoID              string           `json:"repoId"`
	ParentID            string           `json:"parentId"`
	SessionName         string           `json:"sessionName"`
	TaskDescription     string           `json:"taskDescription"`
	CWD                 string           `json:"cwd"`
	Branch              string           `json:"branch"`
	Model               string           `json:"model"`
	StartedAt           string           `json:"startedAt"`
	EndedAt             string           `json:"endedAt"`
	TotalCost           float64          `json:"totalCost"`
	InputTokens         int64            `json:"inputTokens"`
	OutputTokens        int64            `json:"outputTokens"`
	CacheReadTokens     int64            `json:"cacheReadTokens"`
	CacheCreationTokens int64            `json:"cacheCreationTokens"`
	MessageCount        int              `json:"messageCount"`
	EventCount          int              `json:"eventCount"`
	ErrorCount          int              `json:"errorCount"`
	Version             string           `json:"version,omitempty"`
	Entrypoint          string           `json:"entrypoint,omitempty"`
	WorkflowID          string           `json:"workflowId,omitempty"`
	AgentID             string           `json:"agentId,omitempty"`
	AgentKind           string           `json:"agentKind,omitempty"`
	AITitle             string           `json:"aiTitle,omitempty"`
	PRLinks             []session.PRLink `json:"prLinks,omitempty"`
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
		AITitle:             r.AITitle,
		PRLinks:             r.PRLinks,
		IsActive:            false,
		Status:              session.StatusIdle,
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

// sessionUpsertSQL writes a session's aggregates plus the runtime state a
// restart restores (status, exact timestamps, repo resolution provenance).
const sessionUpsertSQL = `INSERT INTO sessions
	(id, repo_id, parent_id, session_name, task_description, cwd, branch, model,
	 started_at, ended_at, total_cost, input_tokens, output_tokens,
	 cache_read_tokens, cache_creation_tokens, message_count, event_count, error_count,
	 version, entrypoint, workflow_id, agent_id, agent_kind, ai_title, pr_links,
	 status, last_active_exact, started_at_exact, repo_source_rank, repo_toplevel, repo_inherited)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
	 agent_kind=excluded.agent_kind,
	 ai_title=excluded.ai_title,
	 pr_links=excluded.pr_links,
	 status=excluded.status,
	 last_active_exact=excluded.last_active_exact,
	 started_at_exact=excluded.started_at_exact,
	 repo_source_rank=excluded.repo_source_rank,
	 repo_toplevel=excluded.repo_toplevel,
	 repo_inherited=excluded.repo_inherited`

func sessionUpsertArgs(s *session.Session) []any {
	// started_at/ended_at stay canonical UTC RFC3339 ("…Z") so window-boundary
	// and bucketing comparisons (lexical TEXT compares in SQLite) are valid.
	// The *_exact columns keep full precision for restoring the live session.
	var endedAt, startedAt, lastActiveExact, startedAtExact string
	if !s.LastActive.IsZero() {
		endedAt = s.LastActive.UTC().Format(time.RFC3339)
		lastActiveExact = s.LastActive.Format(time.RFC3339Nano)
	}
	if !s.StartedAt.IsZero() {
		startedAt = s.StartedAt.UTC().Format(time.RFC3339)
		startedAtExact = s.StartedAt.Format(time.RFC3339Nano)
	}
	pin := s.RepoPin()
	return []any{
		s.ID, s.RepoID, s.ParentID, s.SessionName, s.TaskDescription,
		s.CWD, s.GitBranch, s.Model, startedAt, endedAt,
		s.TotalCost, s.InputTokens, s.OutputTokens,
		s.CacheReadTokens, s.CacheCreationTokens,
		s.MessageCount, s.EventCount, s.ErrorCount,
		s.Version, s.Entrypoint,
		s.WorkflowID, s.AgentID, s.AgentKind,
		s.AITitle, encodePRLinks(s.PRLinks),
		s.LastTurn(), lastActiveExact, startedAtExact,
		pin.Rank, pin.Toplevel, pin.Inherited,
	}
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

	if err := upsertSessions(tx, sessions); err != nil {
		return fmt.Errorf("FlushSessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("FlushSessions commit: %w", err)
	}
	return nil
}

func upsertSessions(tx *sql.Tx, sessions []*session.Session) error {
	if len(sessions) == 0 {
		return nil
	}
	sessStmt, err := tx.Prepare(sessionUpsertSQL)
	if err != nil {
		return fmt.Errorf("prepare session: %w", err)
	}
	defer sessStmt.Close()

	cwdStmt, err := tx.Prepare(cwdRepoUpsertSQL)
	if err != nil {
		return fmt.Errorf("prepare cwd: %w", err)
	}
	defer cwdStmt.Close()

	for _, s := range sessions {
		if _, err := sessStmt.Exec(sessionUpsertArgs(s)...); err != nil {
			return fmt.Errorf("save %s: %w", s.ID, err)
		}

		if e, ok := s.RepoPin().CacheEntry(s.CWD); ok {
			if _, err := cwdStmt.Exec(cwdRepoUpsertArgs(e)...); err != nil {
				return fmt.Errorf("cwdRepo %s: %w", s.ID, err)
			}
		}
	}
	return nil
}

// RestoreSessions loads the given sessions as the live store holds them,
// including the runtime state that SessionRow does not carry. Dedup state is
// not loaded; the sessions are marked DedupPending.
func (d *DB) RestoreSessions(ids []string) ([]*session.Session, error) {
	const chunk = 500
	var out []*session.Session
	for len(ids) > 0 {
		n := min(chunk, len(ids))
		batch := ids[:n]
		ids = ids[n:]

		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		rows, err := d.rdb.Query(`SELECT `+sessionSelectCols+`,
			status, last_active_exact, started_at_exact, repo_source_rank, repo_toplevel, repo_inherited
			FROM sessions WHERE id IN (?`+strings.Repeat(",?", len(batch)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var status, lastActive, startedAt, toplevel string
			var rank int
			var inherited bool
			r, err := scanSessionRow(rows, &status, &lastActive, &startedAt, &rank, &toplevel, &inherited)
			if err != nil {
				rows.Close()
				return nil, err
			}
			s := r.ToSession()
			s.SetLastTurn(status)
			if t, err := time.Parse(time.RFC3339Nano, lastActive); err == nil {
				s.LastActive = t
			}
			if t, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
				s.StartedAt = t
			}
			s.SetRepoPin(repo.Pin{ID: s.RepoID, Rank: rank, Toplevel: toplevel, Inherited: inherited})
			s.SetDedupPending(true)
			out = append(out, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// lastMessageRowsSQL selects each Message's last persisted row in one Session
// (two session_id args). Rows with no cost, input or output are left out, as
// applyCostDedup never records them; SessionTotals and LoadMessageCosts must
// agree, or a restored Session counts a Message's cost twice.
const lastMessageRowsSQL = `FROM events
	WHERE session_id = ?
	AND id IN (SELECT MAX(id) FROM events WHERE session_id = ? AND message_id IS NOT NULL AND message_id != '' GROUP BY message_id)
	AND (cost_usd > 0 OR input_tokens > 0 OR output_tokens > 0)`

// SessionTotals is a Session's cost and token totals.
type SessionTotals struct {
	CostUSD             float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// SessionTotals sums the last persisted row of each Message in a Session.
func (d *DB) SessionTotals(sessionID string) (SessionTotals, error) {
	var t SessionTotals
	err := d.rdb.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0), COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_creation_tokens), 0)
		`+lastMessageRowsSQL, sessionID, sessionID).
		Scan(&t.CostUSD, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheCreationTokens)
	return t, err
}

// LoadMessageCosts returns the last persisted cost/tokens of each Message in a
// Session, the baseline the live per-Message deltas continue from after a restart.
func (d *DB) LoadMessageCosts(sessionID string) (map[string]session.MessageCosts, error) {
	rows, err := d.rdb.Query(`SELECT message_id, cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens
		`+lastMessageRowsSQL, sessionID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	costs := make(map[string]session.MessageCosts)
	for rows.Next() {
		var mid string
		var mc session.MessageCosts
		if err := rows.Scan(&mid, &mc.CostUSD, &mc.InputTokens, &mc.OutputTokens, &mc.CacheReadTokens, &mc.CacheCreationTokens); err != nil {
			return nil, err
		}
		costs[mid] = mc
	}
	return costs, rows.Err()
}

// LoadErrorDedup returns the distinct stable identities (message_id, else uuid)
// of a session's persisted error events. Used to rebuild the in-memory "err:"
// dedup keys after a restart so bootstrap replay does not re-increment
// error_count for errors already counted, matching CountSessionErrors' identity
// definition.
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

// LoadRowKeys returns the dedup key of every persisted event in a session,
// plus how many distinct event rows and conversation turns those keys cover.
// These are the counts the live pipeline produces, since it counts a row once.
func (d *DB) LoadRowKeys(sessionID string) (keys map[string]struct{}, events, turns int, err error) {
	rows, err := d.rdb.Query(`SELECT COALESCE(NULLIF(message_id,''), uuid, '') AS k,
		MAX(type IN ('assistant','user','human'))
		FROM events WHERE session_id = ? GROUP BY k`, sessionID)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	keys = make(map[string]struct{})
	for rows.Next() {
		var k string
		var isTurn bool
		if err := rows.Scan(&k, &isTurn); err != nil {
			return nil, 0, 0, err
		}
		events++
		if isTurn {
			turns++
		}
		if k != "" {
			keys[k] = struct{}{}
		}
	}
	return keys, events, turns, rows.Err()
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
// workflow_id is nullable (migration 013 adds it as plain TEXT with no default),
// so the non-empty filter coalesces NULL to the empty string before comparing:
//
//	COALESCE(workflow_id, '') <> ''
//
// The SQL above is an indented doc-comment code block on purpose. In ordinary
// prose gofmt rewrites the two-apostrophe quoting convention into a Unicode
// right double quote, which would misdocument the query.
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

func encodePRLinks(links []session.PRLink) string {
	if len(links) == 0 {
		return ""
	}
	b, err := json.Marshal(links)
	if err != nil {
		return ""
	}
	return string(b)
}

// scanSessionRow scans one row of sessionSelectCols, then any extra columns.
func scanSessionRow(rows *sql.Rows, extra ...any) (SessionRow, error) {
	var r SessionRow
	var prLinks string
	dest := append([]any{
		&r.ID, &r.RepoID, &r.ParentID, &r.SessionName,
		&r.TaskDescription, &r.CWD, &r.Branch, &r.Model,
		&r.StartedAt, &r.EndedAt,
		&r.TotalCost, &r.InputTokens, &r.OutputTokens,
		&r.CacheReadTokens, &r.CacheCreationTokens,
		&r.MessageCount, &r.EventCount, &r.ErrorCount,
		&r.Version, &r.Entrypoint,
		&r.WorkflowID, &r.AgentID, &r.AgentKind,
		&r.AITitle, &prLinks,
	}, extra...)
	if err := rows.Scan(dest...); err != nil {
		return r, err
	}
	if prLinks != "" {
		if err := json.Unmarshal([]byte(prLinks), &r.PRLinks); err != nil {
			log.Printf("session %s: bad pr_links: %v", r.ID, err)
		}
	}
	return r, nil
}

func scanSessionRows(rows *sql.Rows) ([]SessionRow, error) {
	var result []SessionRow
	for rows.Next() {
		r, err := scanSessionRow(rows)
		if err != nil {
			return result, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
