package migrations

import "database/sql"

func init() {
	Register(15, Migration{
		Name: "recompute_session_aggregates",
		Up: func(tx *sql.Tx) error {
			// Historical sessions.total_cost / token totals / error_count drifted
			// from the events ledger because restart-restore re-seeded the stale
			// stored columns instead of re-deriving them from the per-message
			// events. Recompute them from events so the canonical aggregates match
			// the ground truth. Idempotent: running again yields the same values.
			//
			// Efficiency: a single deduped pass (MAX(id) per session,message_id —
			// matching LoadMessageDedup) aggregated per session and applied with
			// UPDATE ... FROM. On ~3300 sessions / ~130k events this completes in
			// ~1s, versus minutes for the naive per-column correlated form.

			// 1) Zero the cost/token columns for every session that has events, so
			//    a session whose only messages carry no message_id (no entry in the
			//    dedup set) settles to 0 — matching the in-memory cost-map total.
			if _, err := tx.Exec(`
				UPDATE sessions
				SET total_cost = 0, input_tokens = 0, output_tokens = 0,
				    cache_read_tokens = 0, cache_creation_tokens = 0
				WHERE EXISTS (SELECT 1 FROM events e WHERE e.session_id = sessions.id)
			`); err != nil {
				return err
			}

			// 2) Install the deduped per-message sums for sessions that have them.
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

			// 3) error_count: distinct error events deduped by the stable identity
			//    (message_id, else uuid), matching CountSessionErrors. Zero first so
			//    sessions whose stale count accumulated across re-bootstraps but now
			//    have no error rows settle to 0, then install the real counts.
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
			// This migration only corrects derived aggregate columns to match the
			// events ledger; there is no meaningful inverse (the prior values were
			// stale/incorrect). No-op so rollback does not re-introduce drift.
			return nil
		},
	})
}
