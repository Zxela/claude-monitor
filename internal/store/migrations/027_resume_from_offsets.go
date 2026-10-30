package migrations

import "database/sql"

func init() {
	Register(27, Migration{
		Name: "resume_from_offsets",
		Up: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`CREATE TABLE file_offsets (
					path        TEXT PRIMARY KEY,
					session_id  TEXT NOT NULL,
					byte_offset INTEGER NOT NULL,
					inode       INTEGER NOT NULL DEFAULT 0
				)`,
				// In-memory session state the restart restores instead of
				// rebuilding it by re-reading every JSONL file.
				`ALTER TABLE sessions ADD COLUMN status TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE sessions ADD COLUMN last_active_exact TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE sessions ADD COLUMN started_at_exact TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE sessions ADD COLUMN repo_source_rank INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE sessions ADD COLUMN repo_toplevel TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE sessions ADD COLUMN repo_inherited INTEGER NOT NULL DEFAULT 0`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`DROP TABLE file_offsets`,
				`ALTER TABLE sessions DROP COLUMN status`,
				`ALTER TABLE sessions DROP COLUMN last_active_exact`,
				`ALTER TABLE sessions DROP COLUMN started_at_exact`,
				`ALTER TABLE sessions DROP COLUMN repo_source_rank`,
				`ALTER TABLE sessions DROP COLUMN repo_toplevel`,
				`ALTER TABLE sessions DROP COLUMN repo_inherited`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
