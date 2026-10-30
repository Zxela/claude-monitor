package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/session"

	_ "modernc.org/sqlite"
)

const contentSelectCols = `COALESCE(ec.content,''), ec.compressed, COALESCE(ec.thinking,''), ec.thinking_compressed`

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
	ThinkingContent     string  `json:"thinkingContent,omitempty"`
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
	DurationMs        *int64 `json:"durationMs,omitempty"`
	Success           *bool  `json:"success,omitempty"`
	Stderr            string `json:"stderr,omitempty"`
	Interrupted       bool   `json:"interrupted,omitempty"`
	Truncated         bool   `json:"truncated,omitempty"`
	AgentDurationMs   *int64 `json:"agentDurationMs,omitempty"`
	AgentTokens       *int64 `json:"agentTokens,omitempty"`
	AgentToolUseCount *int   `json:"agentToolUseCount,omitempty"`
	AgentType         string `json:"agentType,omitempty"`
	Subtype           string `json:"subtype,omitempty"`
	TurnMessageCount  *int   `json:"turnMessageCount,omitempty"`
	HookCount         *int   `json:"hookCount,omitempty"`
	HookInfos         string `json:"hookInfos,omitempty"`
	Level             string `json:"level,omitempty"`
	IsMeta            bool   `json:"isMeta,omitempty"`
	Version           string `json:"version,omitempty"`
	Entrypoint        string `json:"entrypoint,omitempty"`
	// Per-event metadata (migration 010)
	ToolUseIDs  string `json:"toolUseIds,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	GitBranch   string `json:"gitBranch,omitempty"`
	IsSidechain bool   `json:"isSidechain,omitempty"`
	AgentName   string `json:"agentName,omitempty"`
	TeamName    string `json:"teamName,omitempty"`
}

// EventBatch holds events to be persisted in a single transaction, with the
// sessions they touched and the file offsets they were read up to. Writing the
// offsets in the same transaction means an offset is never saved ahead of the
// events and session totals it covers.
type EventBatch struct {
	Events   []EventInsert
	Sessions []*session.Session
	Offsets  []FileOffset
}

// EventInsert holds data for a single event to insert.
type EventInsert struct {
	SessionID   string
	Event       *parser.Event
	FullContent string // goes to event_content table
	Thinking    string
}

// PersistBatch inserts a batch of events, upserts the batch's sessions and
// saves its file offsets in a single transaction.
func (d *DB) PersistBatch(batch *EventBatch) error {
	if len(batch.Events) == 0 && len(batch.Sessions) == 0 && len(batch.Offsets) == 0 {
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
		 tool_name=COALESCE(NULLIF(excluded.tool_name,''), events.tool_name),
		 tool_detail=COALESCE(NULLIF(excluded.tool_detail,''), events.tool_detail),
		 tool_use_id=COALESCE(NULLIF(excluded.tool_use_id,''), events.tool_use_id),
		 is_agent=(events.is_agent OR excluded.is_agent),
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
		 team_name=excluded.team_name
		RETURNING id, COALESCE(content_preview,''), COALESCE(tool_name,''), COALESCE(tool_detail,'')`)
	if err != nil {
		return fmt.Errorf("prepare event stmt: %w", err)
	}
	defer eventStmt.Close()

	// Startup re-ingest replays every line, so only a still-hot row may take new
	// content; warm and cold rows keep what retention left them. A message's
	// blocks arrive as separate lines sharing one row, so an empty value from a
	// later line must not erase an earlier line's text or thinking.
	contentStmt, err := tx.Prepare(`INSERT INTO event_content (event_id, tier, content, thinking)
		VALUES (?, 'hot', NULLIF(?,''), NULLIF(?,''))
		ON CONFLICT(event_id) DO UPDATE SET
		 content=COALESCE(excluded.content, event_content.content),
		 thinking=COALESCE(excluded.thinking, event_content.thinking)
		WHERE event_content.tier = 'hot'`)
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
			oldRowExists                           bool
			oldRowID                               int64
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

		// RETURNING yields the row's id whether it was inserted or updated;
		// LastInsertId is stale after an ON CONFLICT update.
		var eventID int64
		var newPreview, newToolName, newToolDetail string
		err := eventStmt.QueryRow(
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
		).Scan(&eventID, &newPreview, &newToolName, &newToolDetail)
		if err != nil {
			return fmt.Errorf("insert event: %w", err)
		}

		if ei.FullContent != "" || ei.Thinking != "" {
			if _, err := contentStmt.Exec(eventID, ei.FullContent, ei.Thinking); err != nil {
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
		// Index the merged row, not this line: the next purge reads the row back.
		if _, err := ftsInsertStmt.Exec(eventID, newPreview, newToolName, newToolDetail); err != nil {
			return fmt.Errorf("insert fts: %w", err)
		}
	}

	if err := upsertSessions(tx, batch.Sessions); err != nil {
		return err
	}
	if err := upsertOffsets(tx, batch.Offsets); err != nil {
		return err
	}
	return tx.Commit()
}

// ListEvents returns events for a session, ordered by timestamp.
func (d *DB) ListEvents(sessionID string, limit, offset int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		`+contentSelectCols+`
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
		`+contentSelectCols+`
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
		`+contentSelectCols+`
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
		`+contentSelectCols+`
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
		var fullContent, thinking string
		var compressed, thinkingCompressed []byte
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
			&fullContent, &compressed, &thinking, &thinkingCompressed,
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
		if thinking != "" {
			r.ThinkingContent = thinking
		} else if len(thinkingCompressed) > 0 {
			if s, err := decompressContent(thinkingCompressed); err == nil {
				r.ThinkingContent = s
			}
		}
		if durationMs.Valid {
			v := durationMs.Int64
			r.DurationMs = &v
		}
		if success.Valid {
			v := success.Bool
			r.Success = &v
		}
		if agentDurationMs.Valid {
			v := agentDurationMs.Int64
			r.AgentDurationMs = &v
		}
		if agentTokens.Valid {
			v := agentTokens.Int64
			r.AgentTokens = &v
		}
		if agentToolUseCount.Valid {
			v := int(agentToolUseCount.Int64)
			r.AgentToolUseCount = &v
		}
		if turnMessageCount.Valid {
			v := int(turnMessageCount.Int64)
			r.TurnMessageCount = &v
		}
		if hookCount.Valid {
			v := int(hookCount.Int64)
			r.HookCount = &v
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
