package migrations

import "database/sql"

func init() {
	Register(4, Migration{
		Name: "add_cache_hit_pct",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE session_history ADD COLUMN cache_hit_pct REAL DEFAULT 0`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			return nil
		},
	})
}
