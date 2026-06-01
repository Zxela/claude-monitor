package migrations

import "database/sql"

func init() {
	Register(17, Migration{
		Name: "reattribute_child_repos",
		Up: func(tx *sql.Tx) error {
			// Mirror the new write-path rule (pipeline.applyRepoResolution rule 1):
			// a child session inherits its PARENT's repo_id. Historically, subagents
			// running in git worktrees resolved their own worktree directory and were
			// attributed to phantom "agent-<hash>" repos, fragmenting a parent run
			// across fake projects. Re-point each child at its parent's project.
			//
			// Iterate to a fixpoint so multi-level chains (a subagent that spawns its
			// own subagent) propagate the root project down every level, for any
			// physical row order.
			//
			// Within a SINGLE UPDATE, SQLite walks rows in rowid order and a
			// correlated subquery reads values written EARLIER in that same statement
			// — it is NOT a pre-statement snapshot. So when a parent's row precedes
			// its child's in rowid order, one pass already cascades the entire chain
			// (the child reads the parent's freshly-written id, the grandchild reads
			// the child's, and so on). But when a child precedes its parent (rows
			// inserted or replayed out of depth order), each pass advances a chain by
			// only one level. The loop runs until a pass changes nothing, which
			// converges either way. Bounded by maxPasses as a safety net against a
			// pathological parent cycle (which the write path cannot create, but
			// defensive code is cheap here).
			//
			// Idempotent: once every child equals its parent's repo_id, the first pass
			// affects zero rows and the loop exits. Re-running the migration is a no-op.
			const maxPasses = 64
			for i := 0; i < maxPasses; i++ {
				res, err := tx.Exec(`
					UPDATE sessions
					SET repo_id = (
						SELECT p.repo_id FROM sessions p
						WHERE p.id = sessions.parent_id
					)
					WHERE parent_id IS NOT NULL
					  AND parent_id != ''
					  AND EXISTS (
						SELECT 1 FROM sessions p
						WHERE p.id = sessions.parent_id
						  AND COALESCE(p.repo_id,'') != ''
						  AND COALESCE(p.repo_id,'') != COALESCE(sessions.repo_id,'')
					)
				`)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil {
					return err
				}
				if n == 0 {
					break
				}
			}
			return nil
		},
		Down: func(tx *sql.Tx) error {
			// This migration only re-points historical child sessions to match the
			// canonical write-path attribution. The prior phantom "agent-*" repo_ids
			// were incorrect, so there is no meaningful inverse to restore. No-op so
			// rollback does not re-introduce the fragmentation.
			return nil
		},
	})
}
