package migrations

import "database/sql"

func init() {
	Register(22, Migration{
		Name: "drop_collapsed_keyless_events",
		Up: func(tx *sql.Tx) error {
			// Lines with neither uuid nor message_id all shared the empty dedup
			// key, so each session kept one row overwritten by every such line.
			// The writer also attached those lines' content and FTS terms to
			// whatever row LastInsertId named, an unrelated event. Drop the
			// collapsed rows (the next ingest re-creates them under per-line
			// keys) and rebuild the FTS index to purge the misattributed terms.
			if _, err := tx.Exec(`DELETE FROM event_content WHERE event_id IN
				(SELECT id FROM events WHERE COALESCE(message_id, uuid) = '')`); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM events WHERE COALESCE(message_id, uuid) = ''`); err != nil {
				return err
			}
			_, err := tx.Exec(`INSERT INTO events_fts(events_fts) VALUES('rebuild')`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// The dropped rows were corrupt; there is nothing to restore.
			return nil
		},
	})
}
