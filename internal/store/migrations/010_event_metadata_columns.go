package migrations

import "database/sql"

func init() {
	Register(10, Migration{
		Name: "event_metadata_columns",
		Up: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE events ADD COLUMN tool_use_ids TEXT`,
				`ALTER TABLE events ADD COLUMN cwd TEXT`,
				`ALTER TABLE events ADD COLUMN git_branch TEXT`,
				`ALTER TABLE events ADD COLUMN is_sidechain BOOLEAN DEFAULT 0`,
				`ALTER TABLE events ADD COLUMN agent_name TEXT`,
				`ALTER TABLE events ADD COLUMN team_name TEXT`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE events DROP COLUMN tool_use_ids`,
				`ALTER TABLE events DROP COLUMN cwd`,
				`ALTER TABLE events DROP COLUMN git_branch`,
				`ALTER TABLE events DROP COLUMN is_sidechain`,
				`ALTER TABLE events DROP COLUMN agent_name`,
				`ALTER TABLE events DROP COLUMN team_name`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
