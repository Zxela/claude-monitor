# History Subagent Grouping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group subagent sessions under their parent in the history view with per-parent collapse, global toggle, and collapsed summary badges.

**Architecture:** Migration 002 adds `parent_id` to `session_history`. Backend persists and returns it. Frontend groups rows by `parentId`, renders children indented under parents with disclosure triangles and a global "Show subagents" toggle.

**Tech Stack:** Go (SQLite migrations), TypeScript (vanilla DOM)

---

### Task 1: Migration 002 — add parent_id column

**Files:**
- Create: `internal/store/migrations/002_add_parent_id.go`

- [ ] **Step 1: Create migration file**

Create `internal/store/migrations/002_add_parent_id.go`:

```go
package migrations

import "database/sql"

func init() {
	Register(2, Migration{
		Name: "add_parent_id",
		Up: func(db *sql.DB) error {
			_, err := db.Exec(`ALTER TABLE session_history ADD COLUMN parent_id TEXT DEFAULT ''`)
			return err
		},
		Down: func(db *sql.DB) error {
			// SQLite < 3.35.0 doesn't support DROP COLUMN. Recreate table without parent_id.
			_, err := db.Exec(`
				CREATE TABLE session_history_backup AS SELECT
					id, project_name, session_name, total_cost, input_tokens, output_tokens,
					cache_read_tokens, message_count, error_count, started_at, ended_at,
					duration_seconds, outcome, model, cwd, git_branch, task_description
				FROM session_history;
				DROP TABLE session_history;
				ALTER TABLE session_history_backup RENAME TO session_history;
				CREATE INDEX IF NOT EXISTS idx_session_history_ended_at ON session_history(ended_at DESC)`)
			return err
		},
	})
}
```

- [ ] **Step 2: Run migration tests**

Run: `cd /root/claude-monitor && go test ./internal/store/migrations/ -v -count=1`
Expected: PASS

- [ ] **Step 3: Verify migration applies**

Run: `cd /root/claude-monitor && make migrate-status`
Expected: Shows pending migration "add_parent_id"

- [ ] **Step 4: Commit**

```bash
git add internal/store/migrations/002_add_parent_id.go
git commit -m "feat: add migration 002 — parent_id column for subagent grouping"
```

---

### Task 2: Update SaveSession and ListHistory for parent_id

**Files:**
- Modify: `internal/store/sqlite.go:15-31` (HistoryRow struct)
- Modify: `internal/store/sqlite.go:77-105` (SaveSession)
- Modify: `internal/store/sqlite.go:116-145` (ListHistory)
- Modify: `internal/store/sqlite_test.go` (add parent_id test)

- [ ] **Step 1: Add ParentID to HistoryRow struct**

In `internal/store/sqlite.go`, add after `TaskDescription` (line 31):

```go
	ParentID        string  `json:"parentId"`
```

- [ ] **Step 2: Update SaveSession to persist parent_id**

In the `SaveSession` method, update the INSERT statement to include `parent_id`.

Replace the column list in the INSERT (line 78-80):
```go
	_, err := d.db.Exec(`INSERT INTO session_history
		(id, project_name, session_name, total_cost, input_tokens, output_tokens,
		 cache_read_tokens, message_count, error_count, started_at, ended_at,
		 duration_seconds, outcome, model, cwd, git_branch, task_description, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		 project_name=excluded.project_name,
		 session_name=excluded.session_name,
		 total_cost=excluded.total_cost,
		 input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 message_count=excluded.message_count,
		 error_count=excluded.error_count,
		 started_at=excluded.started_at,
		 ended_at=excluded.ended_at,
		 duration_seconds=excluded.duration_seconds,
		 outcome=excluded.outcome,
		 model=excluded.model,
		 cwd=excluded.cwd,
		 git_branch=excluded.git_branch,
		 task_description=excluded.task_description,
		 parent_id=excluded.parent_id`,
		s.ID, s.ProjectName, s.SessionName, s.TotalCost,
		s.InputTokens, s.OutputTokens, s.CacheReadTokens,
		s.MessageCount, s.ErrorCount,
		startedAt, endedAt, duration,
		"", s.Model, s.CWD, s.GitBranch, s.TaskDescription, s.ParentID,
	)
```

- [ ] **Step 3: Update ListHistory to return parent_id**

In the `ListHistory` method, add `parent_id` to the SELECT and Scan.

Update the SELECT (line 116-119):
```go
	rows, err := d.db.Query(`SELECT
		id, project_name, session_name, total_cost, input_tokens, output_tokens,
		cache_read_tokens, message_count, error_count, started_at, ended_at,
		duration_seconds, outcome, model, cwd, git_branch, task_description, parent_id
		FROM session_history
		ORDER BY ended_at DESC
		LIMIT ? OFFSET ?`, limit, offset)
```

Update the Scan (line 132-138) to add `&r.ParentID` at the end:
```go
		if err := rows.Scan(
			&r.ID, &r.ProjectName, &r.SessionName, &r.TotalCost,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens,
			&r.MessageCount, &r.ErrorCount,
			&r.StartedAt, &r.EndedAt,
			&r.DurationSeconds, &outcome, &r.Model, &r.CWD, &r.GitBranch,
			&r.TaskDescription, &r.ParentID,
		); err != nil {
```

- [ ] **Step 4: Add test for parent_id persistence**

In `internal/store/sqlite_test.go`, add a test after the existing tests:

```go
func TestSaveSession_ParentID(t *testing.T) {
	t.Parallel()
	db, err := Open(tempDBPath(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	now := time.Now()
	parent := &session.Session{
		ID:         "parent-1",
		StartedAt:  now.Add(-10 * time.Minute),
		LastActive: now,
	}
	child := &session.Session{
		ID:         "child-1",
		ParentID:   "parent-1",
		StartedAt:  now.Add(-5 * time.Minute),
		LastActive: now,
	}

	if err := db.SaveSession(parent); err != nil {
		t.Fatalf("SaveSession parent failed: %v", err)
	}
	if err := db.SaveSession(child); err != nil {
		t.Fatalf("SaveSession child failed: %v", err)
	}

	rows, err := db.ListHistory(10, 0)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Find the child row.
	var childRow *HistoryRow
	for i := range rows {
		if rows[i].ID == "child-1" {
			childRow = &rows[i]
			break
		}
	}
	if childRow == nil {
		t.Fatal("child row not found")
	}
	if childRow.ParentID != "parent-1" {
		t.Errorf("ParentID: got %q, want 'parent-1'", childRow.ParentID)
	}

	// Parent should have empty ParentID.
	var parentRow *HistoryRow
	for i := range rows {
		if rows[i].ID == "parent-1" {
			parentRow = &rows[i]
			break
		}
	}
	if parentRow == nil {
		t.Fatal("parent row not found")
	}
	if parentRow.ParentID != "" {
		t.Errorf("Parent's ParentID should be empty, got %q", parentRow.ParentID)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd /root/claude-monitor && go test ./internal/store/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: persist and return parent_id in session history

SaveSession stores ParentID, ListHistory returns it in HistoryRow.
Enables frontend grouping of subagent sessions under parents."
```

---

### Task 3: Frontend — add parentId to types and state

**Files:**
- Modify: `web/src/types.ts:57-74` (HistoryRow interface)
- Modify: `web/src/state.ts` (add historyShowSubagents)

- [ ] **Step 1: Add parentId to HistoryRow type**

In `web/src/types.ts`, add after `taskDescription: string;` (line 73):

```typescript
  parentId?: string;
```

- [ ] **Step 2: Add historyShowSubagents to state**

In `web/src/state.ts`, add to the `AppState` interface (after `focusedSessionId: string | null;`):

```typescript
  // History grouping
  historyShowSubagents: boolean;
```

Add to the `state` object defaults (after `focusedSessionId: null,`):

```typescript
  historyShowSubagents: true,
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /root/claude-monitor/web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/types.ts web/src/state.ts
git commit -m "feat: add parentId to HistoryRow and historyShowSubagents state"
```

---

### Task 4: Frontend — history view grouping, collapse, and global toggle

**Files:**
- Modify: `web/src/components/history-view.ts`
- Modify: `web/src/styles/base.css`

- [ ] **Step 1: Update history-view.ts with grouping logic**

Replace the entire contents of `web/src/components/history-view.ts` with:

```typescript
// web/src/components/history-view.ts
import type { HistoryRow } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchHistory } from '../api';
import { formatDurationSecs, formatTokens } from '../utils';
import '../styles/views.css';

let container: HTMLElement | null = null;
let data: HistoryRow[] = [];
let sortCol = 'endedAt';
let sortAsc = false;
const collapsedParents = new Set<string>();

type Column = { key: string; label: string; cls?: string; fmt: (r: HistoryRow) => string };

const COLUMNS: Column[] = [
  { key: 'endedAt', label: 'Date', cls: 'col-dim', fmt: r => r.endedAt ? new Date(r.endedAt).toLocaleString() : '' },
  { key: 'projectName', label: 'Name', fmt: r => r.sessionName || r.projectName || r.id },
  { key: 'totalCost', label: 'Cost', cls: 'col-cost', fmt: r => `$${r.totalCost.toFixed(2)}` },
  { key: 'durationSeconds', label: 'Duration', cls: 'col-dim', fmt: r => formatDurationSecs(r.durationSeconds) },
  { key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: r => formatTokens(r.inputTokens + r.outputTokens + r.cacheReadTokens) },
  { key: 'messageCount', label: 'Msgs', fmt: r => String(r.messageCount) },
  { key: 'errorCount', label: 'Errors', cls: 'col-err', fmt: r => r.errorCount > 0 ? String(r.errorCount) : '' },
  { key: 'model', label: 'Model', cls: 'col-model', fmt: r => (r.model || '').replace('claude-', '') },
];

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view') && state.view === 'history') {
    loadData();
  }
  if (changed.has('historyShowSubagents') && state.view === 'history') {
    show();
  }
}

async function loadData(): Promise<void> {
  try {
    data = await fetchHistory(200, 0);
    show();
  } catch (err) {
    console.error('Failed to load history:', err);
  }
}

function exportCsv(): void {
  const headers = COLUMNS.map(c => c.label);
  const rows = sortData([...data]).map(r => COLUMNS.map(c => `"${c.fmt(r).replace(/"/g, '""')}"`).join(','));
  const csv = [headers.join(','), ...rows].join('\n');
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `claude-monitor-history-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

/** Group rows: parents first (sorted), children grouped under their parent */
function groupRows(rows: HistoryRow[]): { parent: HistoryRow; children: HistoryRow[] }[] {
  const childrenByParent = new Map<string, HistoryRow[]>();
  const parents: HistoryRow[] = [];
  const childParentIds = new Set<string>();

  // First pass: identify children
  for (const row of rows) {
    if (row.parentId) {
      childParentIds.add(row.parentId);
      const list = childrenByParent.get(row.parentId) || [];
      list.push(row);
      childrenByParent.set(row.parentId, list);
    }
  }

  // Second pass: identify parents and orphan children
  for (const row of rows) {
    if (!row.parentId) {
      parents.push(row);
    } else if (!rows.some(r => r.id === row.parentId)) {
      // Orphan child — parent not in result set, render as top-level
      parents.push(row);
    }
  }

  // Flatten nested subagents: if a child's parent is itself a child, move under the top-level ancestor
  // (we only support one level of nesting)
  for (const [parentId, children] of childrenByParent) {
    // Check if this parent is itself a child
    const parentRow = rows.find(r => r.id === parentId);
    if (parentRow && parentRow.parentId) {
      // Move children under the grandparent
      const gpId = parentRow.parentId;
      const gpChildren = childrenByParent.get(gpId) || [];
      gpChildren.push(...children);
      childrenByParent.set(gpId, gpChildren);
      childrenByParent.delete(parentId);
    }
  }

  const sorted = sortData(parents);
  return sorted.map(parent => ({
    parent,
    children: sortData(childrenByParent.get(parent.id) || []),
  }));
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'view-overlay';

  // Toolbar: export + subagent toggle
  const toolbar = document.createElement('div');
  toolbar.className = 'history-toolbar';

  const exportBtn = document.createElement('button');
  exportBtn.textContent = 'Export CSV';
  exportBtn.style.cssText = 'padding:4px 12px;background:var(--bg-hover);border:1px solid var(--border);color:var(--cyan);font-family:var(--font-mono);font-size:10px;cursor:pointer;border-radius:3px;letter-spacing:0.5px';
  exportBtn.addEventListener('click', exportCsv);
  toolbar.appendChild(exportBtn);

  // Check if any subagents exist in data
  const hasSubagents = data.some(r => r.parentId);
  if (hasSubagents) {
    const toggleLabel = document.createElement('label');
    toggleLabel.className = 'history-subagent-toggle';
    const checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    checkbox.checked = state.historyShowSubagents;
    checkbox.setAttribute('aria-label', 'Show subagent sessions');
    checkbox.addEventListener('change', () => {
      update({ historyShowSubagents: checkbox.checked });
    });
    toggleLabel.appendChild(checkbox);
    toggleLabel.append(' Show subagents');
    toolbar.appendChild(toggleLabel);
  }

  wrapper.appendChild(toolbar);

  const table = document.createElement('table');
  const thead = document.createElement('thead');
  const headerRow = document.createElement('tr');

  for (const col of COLUMNS) {
    const th = document.createElement('th');
    th.setAttribute('role', 'columnheader');
    th.setAttribute('tabindex', '0');
    if (sortCol === col.key) {
      th.setAttribute('aria-sort', sortAsc ? 'ascending' : 'descending');
    } else {
      th.setAttribute('aria-sort', 'none');
    }
    th.innerHTML = `${col.label}${sortCol === col.key ? `<span class="sort-arrow">${sortAsc ? '▲' : '▼'}</span>` : ''}`;
    const sortByCol = () => {
      if (sortCol === col.key) { sortAsc = !sortAsc; } else { sortCol = col.key; sortAsc = false; }
      show();
    };
    th.addEventListener('click', sortByCol);
    th.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); sortByCol(); }
    });
    headerRow.appendChild(th);
  }
  thead.appendChild(headerRow);
  table.appendChild(thead);

  const tbody = document.createElement('tbody');
  const grouped = groupRows([...data]);

  for (const { parent, children } of grouped) {
    const hasChildren = children.length > 0;
    const isCollapsed = collapsedParents.has(parent.id) || !state.historyShowSubagents;

    // Parent row
    const tr = createRow(parent, false);
    if (hasChildren) {
      // Add disclosure triangle to the name cell
      const nameCell = tr.children[1] as HTMLElement;
      const triangle = document.createElement('span');
      triangle.className = 'history-disclosure';
      triangle.textContent = isCollapsed ? '▶' : '▼';
      triangle.setAttribute('role', 'button');
      triangle.setAttribute('tabindex', '0');
      triangle.setAttribute('aria-label', isCollapsed ? 'Expand subagents' : 'Collapse subagents');
      triangle.addEventListener('click', (e) => {
        e.stopPropagation();
        if (collapsedParents.has(parent.id)) {
          collapsedParents.delete(parent.id);
        } else {
          collapsedParents.add(parent.id);
        }
        show();
      });
      triangle.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); triangle.click(); }
      });
      nameCell.insertBefore(triangle, nameCell.firstChild);

      // Add collapsed summary badge
      if (isCollapsed) {
        const badge = document.createElement('span');
        badge.className = 'history-subagent-badge';
        const childCost = children.reduce((sum, c) => sum + c.totalCost, 0);
        badge.textContent = `(${children.length} subagent${children.length > 1 ? 's' : ''}, $${childCost.toFixed(2)})`;
        nameCell.appendChild(badge);
      }
    }
    tbody.appendChild(tr);

    // Child rows (if not collapsed)
    if (hasChildren && !isCollapsed) {
      for (const child of children) {
        const childTr = createRow(child, true);
        tbody.appendChild(childTr);
      }
    }
  }

  table.appendChild(tbody);
  wrapper.appendChild(table);
  container.appendChild(wrapper);
}

function createRow(row: HistoryRow, isChild: boolean): HTMLTableRowElement {
  const tr = document.createElement('tr');
  if (isChild) tr.className = 'history-child-row';

  for (const col of COLUMNS) {
    const td = document.createElement('td');
    td.textContent = col.fmt(row);
    if (col.cls) td.className = col.cls;
    if (col.key === 'projectName') td.title = row.taskDescription || '';
    tr.appendChild(td);
  }
  tr.setAttribute('tabindex', '0');
  tr.setAttribute('role', 'button');
  tr.setAttribute('aria-label', `View session: ${COLUMNS[1].fmt(row)}`);
  const openSession = () => { update({ selectedSessionId: row.id, view: 'list' }); };
  tr.addEventListener('click', openSession);
  tr.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openSession(); }
  });
  return tr;
}

function sortData(rows: HistoryRow[]): HistoryRow[] {
  return rows.sort((a, b) => {
    let va: number | string, vb: number | string;
    switch (sortCol) {
      case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens; vb = b.inputTokens + b.outputTokens + b.cacheReadTokens; break;
      case 'projectName': va = (a.sessionName || a.projectName || '').toLowerCase(); vb = (b.sessionName || b.projectName || '').toLowerCase(); break;
      case 'model': va = a.model || ''; vb = b.model || ''; break;
      case 'endedAt': va = a.endedAt || ''; vb = b.endedAt || ''; break;
      default: va = (a as any)[sortCol] ?? 0; vb = (b as any)[sortCol] ?? 0;
    }
    const cmp = typeof va === 'string' ? va.localeCompare(vb as string) : (va as number) - (vb as number);
    return sortAsc ? cmp : -cmp;
  });
}
```

- [ ] **Step 2: Add CSS for child rows, badges, and toggle**

Append to `web/src/styles/base.css`:

```css
/* History subagent grouping */
.history-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 8px 10px;
}

.history-subagent-toggle {
  font-size: 11px;
  color: var(--fg-muted, #8b949e);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
}

.history-subagent-toggle input {
  cursor: pointer;
}

.history-child-row {
  opacity: 0.8;
}

.history-child-row td:nth-child(2) {
  padding-left: 28px;
}

.history-disclosure {
  cursor: pointer;
  font-size: 10px;
  margin-right: 6px;
  color: var(--fg-muted, #8b949e);
  user-select: none;
}

.history-disclosure:hover {
  color: var(--fg, #c9d1d9);
}

.history-subagent-badge {
  font-size: 10px;
  color: var(--fg-muted, #8b949e);
  margin-left: 8px;
  font-style: italic;
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /root/claude-monitor/web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/history-view.ts web/src/styles/base.css
git commit -m "feat: group subagent sessions under parents in history view

Per-parent collapse with disclosure triangles, global Show subagents
toggle, collapsed summary badges showing count and cost. Children
render indented with muted styling."
```
