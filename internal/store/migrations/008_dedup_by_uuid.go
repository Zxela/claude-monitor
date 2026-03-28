package migrations

import "database/sql"

func init() {
	Register(8, Migration{
		Name: "dedup_by_uuid",
		Up: func(tx *sql.Tx) error {
			// The previous unique index ON events(session_id, message_id) allows
			// unlimited duplicates when message_id IS NULL (NULLs are always
			// distinct in SQLite unique indexes). Events without a message_id
			// still carry a stable uuid from the JSONL, so we can use
			// COALESCE(message_id, uuid) as the dedup key.

			// First, remove duplicate rows keeping the lowest id for each group.
			if _, err := tx.Exec(`DELETE FROM events WHERE id NOT IN (
				SELECT MIN(id) FROM events
				GROUP BY session_id, COALESCE(message_id, uuid)
			)`); err != nil {
				return err
			}

			// Clean up orphaned event_content rows.
			if _, err := tx.Exec(`DELETE FROM event_content WHERE event_id NOT IN (SELECT id FROM events)`); err != nil {
				return err
			}

			// Replace the index with one that covers NULL message_ids via uuid.
			if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_events_dedup`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, COALESCE(message_id, uuid))`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_events_dedup`); err != nil {
				return err
			}
			_, err := tx.Exec(`CREATE UNIQUE INDEX idx_events_dedup ON events(session_id, message_id)`)
			return err
		},
	})
}
