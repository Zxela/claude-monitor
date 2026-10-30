package migrations

import "database/sql"

var haiku55 = seedPrice{"claude-haiku-5-5", 0.10, 0.50, 0.01, 0.125}

const haiku55LongThreshold = 100_000

var haiku55Long = seedPrice{"claude-haiku-5-5", 0.50, 2.50, 0.05, 0.625}

func init() {
	Register(26, Migration{
		Name: "haiku_5_5_long_context_pricing",
		Up: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE model_pricing ADD COLUMN long_context_threshold INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE model_pricing ADD COLUMN long_input_per_mtok REAL NOT NULL DEFAULT 0`,
				`ALTER TABLE model_pricing ADD COLUMN long_output_per_mtok REAL NOT NULL DEFAULT 0`,
				`ALTER TABLE model_pricing ADD COLUMN long_cache_read_per_mtok REAL NOT NULL DEFAULT 0`,
				`ALTER TABLE model_pricing ADD COLUMN long_cache_create_per_mtok REAL NOT NULL DEFAULT 0`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			// Only the row 021 seeded gets the tier; a user-edited row is left alone.
			res, err := tx.Exec(`UPDATE model_pricing SET
					long_context_threshold = ?, long_input_per_mtok = ?, long_output_per_mtok = ?,
					long_cache_read_per_mtok = ?, long_cache_create_per_mtok = ?
				WHERE model_prefix = ?
				  AND input_per_mtok = ? AND output_per_mtok = ?
				  AND cache_read_per_mtok = ? AND cache_create_per_mtok = ?`,
				haiku55LongThreshold, haiku55Long.in, haiku55Long.out, haiku55Long.cacheRead, haiku55Long.cacheCreate,
				haiku55.prefix, haiku55.in, haiku55.out, haiku55.cacheRead, haiku55.cacheCreate)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return nil
			}
			if _, err := tx.Exec(`
				UPDATE events SET cost_usd = CASE
					WHEN input_tokens + cache_read_tokens + cache_creation_tokens > ? THEN
						input_tokens*?/1e6 + output_tokens*?/1e6 +
						cache_read_tokens*?/1e6 + cache_creation_tokens*?/1e6
					ELSE
						input_tokens*?/1e6 + output_tokens*?/1e6 +
						cache_read_tokens*?/1e6 + cache_creation_tokens*?/1e6
					END
				WHERE model LIKE ? || '%'
				  AND NOT EXISTS (
					SELECT 1 FROM model_pricing mp
					WHERE length(mp.model_prefix) > length(?)
					  AND events.model LIKE mp.model_prefix || '%')
			`, haiku55LongThreshold,
				haiku55Long.in, haiku55Long.out, haiku55Long.cacheRead, haiku55Long.cacheCreate,
				haiku55.in, haiku55.out, haiku55.cacheRead, haiku55.cacheCreate,
				haiku55.prefix, haiku55.prefix); err != nil {
				return err
			}
			return resyncSessionCosts(tx)
		},
		Down: func(tx *sql.Tx) error {
			// Event costs stay as recomputed, like 021.
			for _, col := range []string{
				"long_context_threshold", "long_input_per_mtok", "long_output_per_mtok",
				"long_cache_read_per_mtok", "long_cache_create_per_mtok",
			} {
				if _, err := tx.Exec(`ALTER TABLE model_pricing DROP COLUMN ` + col); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
