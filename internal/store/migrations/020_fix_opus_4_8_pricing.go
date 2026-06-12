package migrations

import "database/sql"

func init() {
	Register(20, Migration{
		Name: "fix_opus_4_8_pricing",
		Up: func(tx *sql.Tx) error {
			// A claude-opus-4-8 pricing row at the old Opus 4.0/4.1 rates ($15/$75,
			// 3x the real $5/$25) predated migration 014 on some databases; 014's
			// INSERT OR IGNORE preserved it, and every opus-4-8 event ingested while
			// it was active carries a 3x-inflated cost_usd. Correct the row and
			// recompute the affected events from their stored token counts.
			//
			// Value-gated: only a row at exactly the known-bad rates is touched, so
			// deliberately customized pricing survives — and the event recompute
			// runs only when the row fix fired, since events priced under custom
			// rates are consistent with their table and must not be clobbered.
			res, err := tx.Exec(`
				UPDATE model_pricing
				SET input_per_mtok = 5.0, output_per_mtok = 25.0,
				    cache_read_per_mtok = 0.50, cache_create_per_mtok = 6.25
				WHERE model_prefix = 'claude-opus-4-8'
				  AND input_per_mtok = 15.0 AND output_per_mtok = 75.0
				  AND cache_read_per_mtok = 1.5 AND cache_create_per_mtok = 18.75
			`)
			if err != nil {
				return err
			}
			fixed, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if fixed > 0 {
				if _, err := tx.Exec(`
					UPDATE events SET cost_usd =
						input_tokens*5.0/1e6 + output_tokens*25.0/1e6 +
						cache_read_tokens*0.50/1e6 + cache_creation_tokens*6.25/1e6
					WHERE model LIKE 'claude-opus-4-8%'
				`); err != nil {
					return err
				}
			}

			// Re-sync session aggregates with the events ledger (same recompute as
			// migration 015). Runs unconditionally: it repairs both the cost change
			// above and the error_count inflation caused by bootstrap replay
			// re-incrementing errors after a restart (fixed in the pipeline in this
			// release; this heals the historical damage). Idempotent.
			if _, err := tx.Exec(`
				UPDATE sessions
				SET total_cost = 0, input_tokens = 0, output_tokens = 0,
				    cache_read_tokens = 0, cache_creation_tokens = 0
				WHERE EXISTS (SELECT 1 FROM events e WHERE e.session_id = sessions.id)
			`); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE sessions SET
					total_cost            = COALESCE(agg.c, 0),
					input_tokens          = COALESCE(agg.it, 0),
					output_tokens         = COALESCE(agg.ot, 0),
					cache_read_tokens     = COALESCE(agg.cr, 0),
					cache_creation_tokens = COALESCE(agg.cc, 0)
				FROM (
					SELECT e.session_id AS sid,
					       SUM(e.cost_usd)              AS c,
					       SUM(e.input_tokens)          AS it,
					       SUM(e.output_tokens)         AS ot,
					       SUM(e.cache_read_tokens)     AS cr,
					       SUM(e.cache_creation_tokens) AS cc
					FROM events e
					JOIN (
						SELECT MAX(id) AS mid FROM events
						WHERE message_id IS NOT NULL AND message_id != ''
						GROUP BY session_id, message_id
					) m ON m.mid = e.id
					GROUP BY e.session_id
				) AS agg
				WHERE sessions.id = agg.sid
			`); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE sessions SET error_count = 0
				WHERE EXISTS (SELECT 1 FROM events e WHERE e.session_id = sessions.id)
			`); err != nil {
				return err
			}
			if _, err := tx.Exec(`
				UPDATE sessions SET error_count = COALESCE(ec.n, 0)
				FROM (
					SELECT session_id AS sid,
					       COUNT(DISTINCT COALESCE(NULLIF(message_id,''), uuid)) AS n
					FROM events WHERE is_error = 1
					GROUP BY session_id
				) AS ec
				WHERE sessions.id = ec.sid
			`); err != nil {
				return err
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			// Corrects bad data to match the events ledger; the prior values were
			// wrong, so there is no meaningful inverse. No-op, like migration 015.
			return nil
		},
	})
}
