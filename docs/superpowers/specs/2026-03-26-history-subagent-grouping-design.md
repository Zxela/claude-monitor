# History Subagent Grouping — Design Spec

**Date:** 2026-03-26
**Depends on:** DB Migration System (must be implemented first)

## Goal

Group subagent sessions under their parent in the history view, with per-parent collapse, a global toggle, and collapsed summary badges.

## Backend Changes

### Migration 2: Add `parent_id` column

```sql
ALTER TABLE session_history ADD COLUMN parent_id TEXT DEFAULT ''
```

Down migration:

SQLite doesn't support `DROP COLUMN` before 3.35.0. Since we target broad compatibility, the down migration recreates the table without `parent_id`:

```sql
CREATE TABLE session_history_backup AS SELECT <all columns except parent_id> FROM session_history;
DROP TABLE session_history;
ALTER TABLE session_history_backup RENAME TO session_history;
CREATE INDEX IF NOT EXISTS idx_session_history_ended_at ON session_history(ended_at DESC);
```

### SaveSession

Add `parent_id` to the INSERT and ON CONFLICT UPDATE. Persist `s.ParentID`.

### ListHistory

Add `parent_id` to the SELECT statement. Add `ParentID string` field to `HistoryRow` struct with `json:"parentId"`.

### API

No new endpoints. The existing `/api/history` returns the new `parentId` field. Frontend handles grouping.

## Frontend Changes

### Types

`HistoryRow` in `types.ts` gets `parentId?: string`.

### State

In `state.ts`:
- `historyShowSubagents: boolean` — global toggle, default `true`
- History view component maintains a local `Set<string>` for collapsed parent IDs (not global state — only relevant to history view)

### Grouping Logic

After fetching history rows:
1. Separate rows into parents (no `parentId` or `parentId === ''`) and children (have `parentId`)
2. Group children by `parentId`
3. For nested subagents (child whose parent is also a child), flatten — find the top-level ancestor and group under it
4. Orphan children (parent not in current result set) render as top-level rows

### Rendering

**Parent rows:**
- If parent has children: show disclosure triangle (▶/▼) before the session name
- Clicking triangle toggles that parent's children visibility
- When collapsed, show inline badge: "(3 subagents, $0.45)" after session name — sum of children's costs

**Child rows:**
- Indented with `padding-left` (~24px) and slightly muted text color
- No disclosure triangle (flat — one level of nesting only)
- Otherwise identical to parent row layout (same columns)

**Global toggle:**
- Button in the history view header area: "Show subagents" with a checkbox or toggle style
- When off: all children hidden, all parent badges shown regardless of per-parent state
- When toggled back on: per-parent collapse states are restored
- Keyboard accessible (`role="checkbox"`, `aria-checked`)

### Sorting

- Parent rows sort by their own sort column (default: `ended_at` desc)
- Children always appear directly below their parent, sorted by `ended_at` within the group
- Changing sort column re-sorts parents but keeps children grouped under them

### Edge Cases

- Zero subagent sessions: no toggle shown, no change to current behavior
- Parent with 1 child: still shows triangle and grouping
- All children hidden globally: badge counts still visible on parents
- Page/offset: children count toward the limit — if a parent's children span pages, orphans on page 2 render as top-level

## Files

| File | Action | Purpose |
|------|--------|---------|
| `internal/store/migrations/002_add_parent_id.go` | Create | Migration to add parent_id column |
| `internal/store/sqlite.go` | Modify | Add parent_id to SaveSession, ListHistory, HistoryRow |
| `web/src/types.ts` | Modify | Add parentId to HistoryRow |
| `web/src/components/history-view.ts` | Modify | Grouping logic, per-parent collapse, global toggle, badges |
| `web/src/styles/base.css` | Modify | Indented child row styles, badge styles |

## Non-Goals

- Deep nesting (multi-level collapse) — flatten to one level
- Separate API endpoint for grouped history
- Server-side grouping (frontend handles it)
- Filtering by parent/child status
