package migrations

import "database/sql"

func init() {
	Register(25, Migration{
		Name: "session_ai_title_pr_link",
		Up: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE sessions ADD COLUMN ai_title TEXT`,
				`ALTER TABLE sessions ADD COLUMN pr_links TEXT`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE sessions DROP COLUMN ai_title`,
				`ALTER TABLE sessions DROP COLUMN pr_links`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
