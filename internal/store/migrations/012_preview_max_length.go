package migrations

import "database/sql"

func init() {
	Register(12, Migration{
		Name: "preview_max_length",
		Up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT OR IGNORE INTO settings VALUES ('preview_max_length', '200')`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`DELETE FROM settings WHERE key = 'preview_max_length'`)
			return err
		},
	})
}
