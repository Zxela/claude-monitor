package migrations

import "database/sql"

func init() {
	Register(5, Migration{
		Name: "backfill_cache_hit_pct",
		Up: func(tx *sql.Tx) error {
			// Backfill cache_hit_pct for rows that have token data but were
			// saved before migration 004 added the column.
			_, err := tx.Exec(`UPDATE session_history
				SET cache_hit_pct = cache_read_tokens * 100.0 /
					(input_tokens + cache_read_tokens + COALESCE(cache_creation_tokens, 0))
				WHERE cache_hit_pct = 0
				  AND (input_tokens + cache_read_tokens + COALESCE(cache_creation_tokens, 0)) > 0
				  AND cache_read_tokens > 0`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`UPDATE session_history SET cache_hit_pct = 0`)
			return err
		},
	})
}
