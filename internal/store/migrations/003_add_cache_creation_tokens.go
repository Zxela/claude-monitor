package migrations

import "database/sql"

func init() {
	Register(3, Migration{
		Name: "add_cache_creation_tokens",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`ALTER TABLE session_history ADD COLUMN cache_creation_tokens INTEGER DEFAULT 0`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			// SQLite < 3.35.0 doesn't support DROP COLUMN — accept the column remains.
			return nil
		},
	})
}
