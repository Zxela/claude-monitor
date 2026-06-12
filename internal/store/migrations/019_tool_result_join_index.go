package migrations

import "database/sql"

func init() {
	Register(19, Migration{
		Name: "tool_result_join_index",
		Up: func(tx *sql.Tx) error {
			// ToolUsage and SessionSkills attribute errors by self-joining each
			// tool_use event to its tool_result via
			//   r.for_tool_use_id = u.tool_use_id AND r.session_id = u.session_id
			// Without an index on for_tool_use_id the planner resolves the join
			// through idx_events_dedup (session_id only), scanning every event in
			// the session per tool_use row — multi-second /api/stats/tools and
			// /api/skills/sessions on grown databases. Including is_error makes
			// the index covering, so the join never touches the table.
			//
			// Deliberately not a partial index (WHERE for_tool_use_id != ''):
			// the planner cannot prove the join's u.tool_use_id != '' guard
			// implies r.for_tool_use_id != '' through the equality, so it would
			// ignore the index entirely.
			// IF NOT EXISTS: tolerate databases where the index was applied
			// out-of-band (e.g. as a manual hotfix) so startup doesn't wedge.
			_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_events_result_lookup
				ON events(for_tool_use_id, session_id, is_error)`)
			return err
		},
		Down: func(tx *sql.Tx) error {
			_, err := tx.Exec(`DROP INDEX IF EXISTS idx_events_result_lookup`)
			return err
		},
	})
}
