package migrations

import "database/sql"

func init() {
	Register(23, Migration{
		Name: "recount_session_events",
		Up: func(tx *sql.Tx) error {
			// Restart replay inflated event_count and message_count. Live sessions
			// are recounted from events on their next replay, but sessions whose
			// JSONL is gone never are. Recount all of them the way the pipeline
			// now counts: one per distinct event row.
			_, err := tx.Exec(`
				UPDATE sessions SET
					event_count   = c.events,
					message_count = c.turns
				FROM (
					SELECT session_id AS sid, COUNT(*) AS events, SUM(is_turn) AS turns
					FROM (
						SELECT session_id, MAX(type IN ('assistant','user','human')) AS is_turn
						FROM events
						GROUP BY session_id, COALESCE(NULLIF(message_id,''), uuid, '')
					)
					GROUP BY session_id
				) AS c
				WHERE sessions.id = c.sid`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// The previous values were inflated; there is nothing to restore.
			return nil
		},
	})
}
