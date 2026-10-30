package migrations

import "database/sql"

func init() {
	Register(24, Migration{
		Name: "event_content_thinking",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE event_content ADD COLUMN thinking TEXT`); err != nil {
				return err
			}
			if _, err := tx.Exec(`ALTER TABLE event_content ADD COLUMN thinking_compressed BLOB`); err != nil {
				return err
			}
			// Earlier upserts indexed tool_name from the latest line while the row
			// kept the first line's, so later FTS deletes no longer matched and the
			// index drifted from events.
			_, err := tx.Exec(`INSERT INTO events_fts(events_fts) VALUES ('rebuild')`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`ALTER TABLE event_content DROP COLUMN thinking_compressed`); err != nil {
				return err
			}
			_, err := tx.Exec(`ALTER TABLE event_content DROP COLUMN thinking`)
			return err
		},
	})
}
