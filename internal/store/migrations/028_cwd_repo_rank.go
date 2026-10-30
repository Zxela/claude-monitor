package migrations

import "database/sql"

func init() {
	Register(28, Migration{
		Name: "cwd_repo_rank",
		Up: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE cwd_repos ADD COLUMN source_rank INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE cwd_repos ADD COLUMN repo_toplevel TEXT NOT NULL DEFAULT ''`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE cwd_repos DROP COLUMN source_rank`,
				`ALTER TABLE cwd_repos DROP COLUMN repo_toplevel`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
