package migrations

import "database/sql"

type seedPrice struct {
	prefix                          string
	in, out, cacheRead, cacheCreate float64
}

var claude5xPricing = []seedPrice{
	{"claude-opus-5", 5.0, 25.0, 0.50, 6.25},
	{"claude-sonnet-5", 2.0, 10.0, 0.20, 2.50},
	{"claude-mythos-5", 10.0, 50.0, 1.0, 12.50},
	{"claude-fable-5-1", 10.0, 50.0, 0.25, 12.50},
	{"claude-mythos-5-1", 10.0, 50.0, 0.25, 12.50},
	{"claude-opus-5-5", 4.0, 20.0, 0.20, 5.0},
	{"claude-sonnet-5-5", 2.0, 10.0, 0.20, 2.50},
	{"claude-haiku-5-5", 0.10, 0.50, 0.01, 0.125},
}

func init() {
	Register(21, Migration{
		Name: "claude_5x_pricing",
		Up: func(tx *sql.Tx) error {
			// Events for these models were costed at the Sonnet 4.6 fallback rate
			// (or, for fable-5-1, at fable-5's 4x cache-read rate). Recompute only
			// for rows this migration seeded: a pre-existing row is user pricing
			// and its events are already consistent with it.
			var seeded []seedPrice
			for _, p := range claude5xPricing {
				res, err := tx.Exec(
					`INSERT OR IGNORE INTO model_pricing (model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok) VALUES (?, ?, ?, ?, ?)`,
					p.prefix, p.in, p.out, p.cacheRead, p.cacheCreate,
				)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil {
					return err
				}
				if n > 0 {
					seeded = append(seeded, p)
				}
			}
			if len(seeded) == 0 {
				return nil
			}
			// All rows are in place before recomputing, so the longest-prefix
			// guard keeps claude-opus-5 from repricing claude-opus-5-5 events.
			for _, p := range seeded {
				if _, err := tx.Exec(`
					UPDATE events SET cost_usd =
						input_tokens*?/1e6 + output_tokens*?/1e6 +
						cache_read_tokens*?/1e6 + cache_creation_tokens*?/1e6
					WHERE model LIKE ? || '%'
					  AND NOT EXISTS (
						SELECT 1 FROM model_pricing mp
						WHERE length(mp.model_prefix) > length(?)
						  AND events.model LIKE mp.model_prefix || '%')
				`, p.in, p.out, p.cacheRead, p.cacheCreate, p.prefix, p.prefix); err != nil {
					return err
				}
			}
			return resyncSessionCosts(tx)
		},
		Down: func(tx *sql.Tx) error {
			// Value-gated like 018: user-edited rows survive. Event costs are left
			// as recomputed; the fallback-rate values they replaced were wrong.
			for _, p := range claude5xPricing {
				if _, err := tx.Exec(`DELETE FROM model_pricing
					WHERE model_prefix = ?
					  AND input_per_mtok = ? AND output_per_mtok = ?
					  AND cache_read_per_mtok = ? AND cache_create_per_mtok = ?`,
					p.prefix, p.in, p.out, p.cacheRead, p.cacheCreate); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// resyncSessionCosts is the cost/token half of migration 015's recompute.
func resyncSessionCosts(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		UPDATE sessions
		SET total_cost = 0, input_tokens = 0, output_tokens = 0,
		    cache_read_tokens = 0, cache_creation_tokens = 0
		WHERE EXISTS (SELECT 1 FROM events e WHERE e.session_id = sessions.id)
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
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
	`)
	return err
}
