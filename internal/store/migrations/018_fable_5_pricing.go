package migrations

import "database/sql"

func init() {
	Register(18, Migration{
		Name: "fable_5_pricing",
		Up: func(tx *sql.Tx) error {
			// Seed claude-fable-5 pricing: $10 input / $50 output per 1M tokens
			// (double the Opus 4.x rate — Fable is a new tier above Opus).
			// cache_read = 0.1x input, 5m cache_create = 1.25x input, per the
			// convention in 011. Sessions may report the model as
			// "claude-fable-5[1m]"; the parser's longest-prefix match covers it.
			_, err := tx.Exec(
				`INSERT OR IGNORE INTO model_pricing (model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok) VALUES (?, ?, ?, ?, ?)`,
				"claude-fable-5", 10.0, 50.0, 1.0, 12.50,
			)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// Only remove the row if it still holds exactly the values we seeded.
			// Up used INSERT OR IGNORE (it never overwrote an existing row), so a
			// price the user edited via the pricing API must survive rollback —
			// Down must not delete data this migration did not create.
			_, err := tx.Exec(`DELETE FROM model_pricing
				WHERE model_prefix = 'claude-fable-5'
				  AND input_per_mtok = 10.0 AND output_per_mtok = 50.0
				  AND cache_read_per_mtok = 1.0 AND cache_create_per_mtok = 12.50`)
			return err
		},
	})
}
