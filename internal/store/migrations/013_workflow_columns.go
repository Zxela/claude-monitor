package migrations

import "database/sql"

func init() {
	Register(13, Migration{
		Name: "workflow_columns",
		Up: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`ALTER TABLE sessions ADD COLUMN workflow_id TEXT`,
				`ALTER TABLE sessions ADD COLUMN agent_id TEXT`,
				`ALTER TABLE sessions ADD COLUMN agent_kind TEXT`,
				`CREATE INDEX idx_sessions_workflow ON sessions(workflow_id)`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			for _, stmt := range []string{
				`DROP INDEX IF EXISTS idx_sessions_workflow`,
				`ALTER TABLE sessions DROP COLUMN workflow_id`,
				`ALTER TABLE sessions DROP COLUMN agent_id`,
				`ALTER TABLE sessions DROP COLUMN agent_kind`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
