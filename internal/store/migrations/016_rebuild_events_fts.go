package migrations

import "database/sql"

func init() {
	Register(16, Migration{
		Name: "rebuild_events_fts",
		Up: func(tx *sql.Tx) error {
			// events_fts is an external-content FTS5 table. The previous writer
			// used INSERT OR REPLACE, which never purged a row's old terms on
			// update, leaving stale phantom tokens that produced false-positive
			// search matches (e.g. un-highlighted "[tool: Bash]" hits). The writer
			// now issues the FTS5 'delete' command for old values, but already
			// corrupted rows must be repaired. The 'rebuild' command drops and
			// re-derives the entire index from the content table (events).
			_, err := tx.Exec(`INSERT INTO events_fts(events_fts) VALUES('rebuild')`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// Rebuilding the index from content is non-destructive and has no
			// meaningful inverse; re-running 'rebuild' is always safe.
			_, err := tx.Exec(`INSERT INTO events_fts(events_fts) VALUES('rebuild')`)
			return err
		},
	})
}
