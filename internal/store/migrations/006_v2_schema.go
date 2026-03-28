package migrations

import "database/sql"

func init() {
	Register(6, Migration{
		Name: "v2_schema",
		Up: func(tx *sql.Tx) error {
			// Drop the v1 table — data will be re-ingested from JSONL files.
			if _, err := tx.Exec(`DROP TABLE IF EXISTS session_history`); err != nil {
				return err
			}

			// Repo identity (stable across worktrees/machines).
			if _, err := tx.Exec(`CREATE TABLE repos (
				id         TEXT PRIMARY KEY,
				name       TEXT NOT NULL,
				url        TEXT,
				first_seen TEXT NOT NULL
			)`); err != nil {
				return err
			}

			// Persistent cwd → repo cache.
			if _, err := tx.Exec(`CREATE TABLE cwd_repos (
				cwd     TEXT PRIMARY KEY,
				repo_id TEXT REFERENCES repos(id)
			)`); err != nil {
				return err
			}

			// Session = one Claude Code conversation.
			if _, err := tx.Exec(`CREATE TABLE sessions (
				id                    TEXT PRIMARY KEY,
				repo_id               TEXT REFERENCES repos(id),
				parent_id             TEXT,
				session_name          TEXT,
				task_description      TEXT,
				cwd                   TEXT,
				branch                TEXT,
				model                 TEXT,
				started_at            TEXT,
				ended_at              TEXT,
				total_cost            REAL DEFAULT 0,
				input_tokens          INTEGER DEFAULT 0,
				output_tokens         INTEGER DEFAULT 0,
				cache_read_tokens     INTEGER DEFAULT 0,
				cache_creation_tokens INTEGER DEFAULT 0,
				message_count         INTEGER DEFAULT 0,
				event_count           INTEGER DEFAULT 0,
				error_count           INTEGER DEFAULT 0
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX idx_sessions_repo ON sessions(repo_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX idx_sessions_parent ON sessions(parent_id)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX idx_sessions_ended ON sessions(ended_at DESC)`); err != nil {
				return err
			}

			// Every JSONL line, deduped to final state per message_id.
			if _, err := tx.Exec(`CREATE TABLE events (
				id                    INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id            TEXT NOT NULL REFERENCES sessions(id),
				uuid                  TEXT,
				message_id            TEXT,
				type                  TEXT,
				role                  TEXT,
				content_preview       TEXT,
				tool_name             TEXT,
				tool_detail           TEXT,
				cost_usd              REAL DEFAULT 0,
				input_tokens          INTEGER DEFAULT 0,
				output_tokens         INTEGER DEFAULT 0,
				cache_read_tokens     INTEGER DEFAULT 0,
				cache_creation_tokens INTEGER DEFAULT 0,
				model                 TEXT,
				is_error              BOOLEAN DEFAULT 0,
				stop_reason           TEXT,
				hook_event            TEXT,
				hook_name             TEXT,
				tool_use_id           TEXT,
				for_tool_use_id       TEXT,
				is_agent              BOOLEAN DEFAULT 0,
				timestamp             TEXT NOT NULL
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX idx_events_session ON events(session_id, timestamp)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX idx_events_tool ON events(tool_name)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE INDEX idx_events_timestamp ON events(timestamp)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, message_id)`); err != nil {
				return err
			}

			// Full content, separate table for tiered retention.
			if _, err := tx.Exec(`CREATE TABLE event_content (
				event_id   INTEGER PRIMARY KEY REFERENCES events(id),
				tier       TEXT DEFAULT 'hot',
				content    TEXT,
				compressed BLOB
			)`); err != nil {
				return err
			}

			// FTS5 on always-available fields.
			if _, err := tx.Exec(`CREATE VIRTUAL TABLE events_fts USING fts5(
				content_preview, tool_name, tool_detail,
				content=events,
				content_rowid=id
			)`); err != nil {
				return err
			}

			// Settings with defaults.
			if _, err := tx.Exec(`CREATE TABLE settings (
				key   TEXT PRIMARY KEY,
				value TEXT NOT NULL
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO settings VALUES ('retention_hot_days', '30')`); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO settings VALUES ('retention_warm_days', '90')`); err != nil {
				return err
			}

			return nil
		},
		Down: func(tx *sql.Tx) error {
			for _, table := range []string{
				"events_fts", "event_content", "events",
				"sessions", "cwd_repos", "repos", "settings",
			} {
				if _, err := tx.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
