package migrations

import "database/sql"

func init() {
	Register(7, Migration{
		Name: "fix_dedup_index",
		Up: func(tx *sql.Tx) error {
			// Migration 006 originally created a partial unique index:
			//   CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, message_id) WHERE message_id IS NOT NULL
			// SQLite's ON CONFLICT clause doesn't work with partial indexes.
			// Recreate as a regular unique index (NULLs are distinct in SQLite unique indexes).
			if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_events_dedup`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, message_id)`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_events_dedup`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, message_id) WHERE message_id IS NOT NULL`)
			return err
		},
	})
}
