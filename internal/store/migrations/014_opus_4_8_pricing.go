package migrations

import "database/sql"

func init() {
	Register(14, Migration{
		Name: "opus_4_8_pricing",
		Up: func(tx *sql.Tx) error {
			// Seed claude-opus-4-8 pricing: $5 input / $25 output per 1M tokens,
			// matching Opus 4.5/4.6/4.7 (the post-Opus-4.5 Opus rate; only the
			// deprecated Opus 4/4.1 were $15/$75). cache_read = 0.1x input,
			// 5m cache_create = 1.25x input, per the Opus convention in 011.
			_, err := tx.Exec(
				`INSERT OR IGNORE INTO model_pricing (model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok) VALUES (?, ?, ?, ?, ?)`,
				"claude-opus-4-8", 5.0, 25.0, 0.50, 6.25,
			)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// Only remove the row if it still holds exactly the values we seeded.
			// Up used INSERT OR IGNORE (it never overwrote an existing row), so a
			// price the user edited via the pricing API must survive rollback —
			// Down must not delete data this migration did not create.
			_, err := tx.Exec(`DELETE FROM model_pricing
				WHERE model_prefix = 'claude-opus-4-8'
				  AND input_per_mtok = 5.0 AND output_per_mtok = 25.0
				  AND cache_read_per_mtok = 0.50 AND cache_create_per_mtok = 6.25`)
			return err
		},
	})
}
