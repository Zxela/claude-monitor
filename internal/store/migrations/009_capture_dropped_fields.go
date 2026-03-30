package migrations

import "database/sql"

func init() {
	Register(9, Migration{
		Name: "capture_dropped_fields",
		Up: func(tx *sql.Tx) error {
			// Events table: tool result metadata
			for _, stmt := range []string{
				`ALTER TABLE events ADD COLUMN duration_ms INTEGER`,
				`ALTER TABLE events ADD COLUMN success BOOLEAN`,
				`ALTER TABLE events ADD COLUMN stderr TEXT`,
				`ALTER TABLE events ADD COLUMN interrupted BOOLEAN DEFAULT 0`,
				`ALTER TABLE events ADD COLUMN truncated BOOLEAN DEFAULT 0`,
				// Events table: agent result metadata
				`ALTER TABLE events ADD COLUMN agent_duration_ms INTEGER`,
				`ALTER TABLE events ADD COLUMN agent_tokens INTEGER`,
				`ALTER TABLE events ADD COLUMN agent_tool_use_count INTEGER`,
				`ALTER TABLE events ADD COLUMN agent_type TEXT`,
				// Events table: system message metadata
				`ALTER TABLE events ADD COLUMN subtype TEXT`,
				`ALTER TABLE events ADD COLUMN turn_message_count INTEGER`,
				`ALTER TABLE events ADD COLUMN hook_count INTEGER`,
				`ALTER TABLE events ADD COLUMN hook_infos TEXT`,
				`ALTER TABLE events ADD COLUMN level TEXT`,
				// Events table: session-level metadata per event
				`ALTER TABLE events ADD COLUMN is_meta BOOLEAN DEFAULT 0`,
				`ALTER TABLE events ADD COLUMN version TEXT`,
				`ALTER TABLE events ADD COLUMN entrypoint TEXT`,
				// Sessions table
				`ALTER TABLE sessions ADD COLUMN version TEXT`,
				`ALTER TABLE sessions ADD COLUMN entrypoint TEXT`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			// SQLite 3.35+ supports DROP COLUMN.
			for _, stmt := range []string{
				`ALTER TABLE events DROP COLUMN duration_ms`,
				`ALTER TABLE events DROP COLUMN success`,
				`ALTER TABLE events DROP COLUMN stderr`,
				`ALTER TABLE events DROP COLUMN interrupted`,
				`ALTER TABLE events DROP COLUMN truncated`,
				`ALTER TABLE events DROP COLUMN agent_duration_ms`,
				`ALTER TABLE events DROP COLUMN agent_tokens`,
				`ALTER TABLE events DROP COLUMN agent_tool_use_count`,
				`ALTER TABLE events DROP COLUMN agent_type`,
				`ALTER TABLE events DROP COLUMN subtype`,
				`ALTER TABLE events DROP COLUMN turn_message_count`,
				`ALTER TABLE events DROP COLUMN hook_count`,
				`ALTER TABLE events DROP COLUMN hook_infos`,
				`ALTER TABLE events DROP COLUMN level`,
				`ALTER TABLE events DROP COLUMN is_meta`,
				`ALTER TABLE events DROP COLUMN version`,
				`ALTER TABLE events DROP COLUMN entrypoint`,
				`ALTER TABLE sessions DROP COLUMN version`,
				`ALTER TABLE sessions DROP COLUMN entrypoint`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
