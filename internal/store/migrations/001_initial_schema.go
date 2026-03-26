package migrations

import "database/sql"

func init() {
	Register(1, Migration{
		Name: "initial_schema",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`CREATE TABLE IF NOT EXISTS session_history (
				id TEXT PRIMARY KEY,
				project_name TEXT,
				session_name TEXT,
				total_cost REAL,
				input_tokens INTEGER,
				output_tokens INTEGER,
				cache_read_tokens INTEGER,
				message_count INTEGER,
				error_count INTEGER,
				started_at TEXT,
				ended_at TEXT,
				duration_seconds REAL,
				outcome TEXT,
				model TEXT,
				cwd TEXT,
				git_branch TEXT,
				task_description TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_session_history_ended_at ON session_history(ended_at DESC)`)
			return err
		},
		Down: func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE IF EXISTS session_history`)
			return err
		},
	})
}
