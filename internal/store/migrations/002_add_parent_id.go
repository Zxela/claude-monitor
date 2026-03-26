package migrations

import "database/sql"

func init() {
	Register(2, Migration{
		Name: "add_parent_id",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`ALTER TABLE session_history ADD COLUMN parent_id TEXT DEFAULT ''`)
			return err
		},
		Down: func(db *sql.DB) error {
			// SQLite < 3.35.0 doesn't support DROP COLUMN. Recreate table without parent_id.
			_, err := db.Exec(`
				CREATE TABLE session_history_backup AS SELECT
					id, project_name, session_name, total_cost, input_tokens, output_tokens,
					cache_read_tokens, message_count, error_count, started_at, ended_at,
					duration_seconds, outcome, model, cwd, git_branch, task_description
				FROM session_history;
				DROP TABLE session_history;
				ALTER TABLE session_history_backup RENAME TO session_history;
				CREATE INDEX IF NOT EXISTS idx_session_history_ended_at ON session_history(ended_at DESC)`)
			return err
		},
	})
}
