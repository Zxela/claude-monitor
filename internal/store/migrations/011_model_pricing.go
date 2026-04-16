package migrations

import "database/sql"

func init() {
	Register(11, Migration{
		Name: "model_pricing",
		Up: func(tx *sql.Tx) error {
			if _, err := tx.Exec(`CREATE TABLE model_pricing (
				model_prefix          TEXT PRIMARY KEY,
				input_per_mtok        REAL NOT NULL,
				output_per_mtok       REAL NOT NULL,
				cache_read_per_mtok   REAL NOT NULL,
				cache_create_per_mtok REAL NOT NULL
			)`); err != nil {
				return err
			}
			// Seed with the three known models.
			for _, row := range []struct {
				prefix                                          string
				input, output, cacheRead, cacheCreate float64
			}{
				{"claude-opus-4-6", 5.0, 25.0, 0.50, 6.25},
				{"claude-sonnet-4-6", 3.0, 15.0, 0.30, 3.75},
				{"claude-haiku-4-5", 1.0, 5.0, 0.10, 1.25},
			} {
				if _, err := tx.Exec(
					`INSERT INTO model_pricing (model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok) VALUES (?, ?, ?, ?, ?)`,
					row.prefix, row.input, row.output, row.cacheRead, row.cacheCreate,
				); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`DROP TABLE IF EXISTS model_pricing`)
			return err
		},
	})
}
