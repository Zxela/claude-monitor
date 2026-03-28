// web/src/components/history-view.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchSessions } from '../api';
import { formatDurationSecs, formatTokens } from '../utils';
import '../styles/views.css';

let container: HTMLElement | null = null;
let data: Session[] = [];
let sortCol = 'lastActive';
let sortAsc = false;
const collapsedParents = new Set<string>();

type Column = { key: string; label: string; cls?: string; fmt: (r: Session) => string };

const COLUMNS: Column[] = [
  { key: 'lastActive', label: 'Date', cls: 'col-dim', fmt: r => r.lastActive ? new Date(r.lastActive).toLocaleString() : '' },
  { key: 'projectName', label: 'Name', fmt: r => r.sessionName || r.cwd || r.id },
  { key: 'totalCost', label: 'Cost', cls: 'col-cost', fmt: r => `$${r.totalCost.toFixed(2)}` },
  { key: 'duration', label: 'Duration', cls: 'col-dim', fmt: r => {
    if (!r.startedAt || !r.lastActive) return '';
    const secs = (new Date(r.lastActive).getTime() - new Date(r.startedAt).getTime()) / 1000;
    return formatDurationSecs(secs);
  }},
  { key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: r => formatTokens(r.inputTokens + r.outputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0)) },
  { key: 'cache', label: 'Cache%', cls: 'col-cache', fmt: r => {
    const total = r.inputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0);
    if (total === 0) return '';
    return `${Math.round(r.cacheReadTokens / total * 100)}%`;
  }},
  { key: 'messageCount', label: 'Msgs', fmt: r => String(r.messageCount) },
  { key: 'errorCount', label: 'Errors', cls: 'col-err', fmt: r => r.errorCount > 0 ? String(r.errorCount) : '' },
  { key: 'model', label: 'Model', cls: 'col-model', fmt: r => (r.model || '').replace('claude-', '') },
];

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

let historyRefreshTimer: ReturnType<typeof setTimeout> | null = null;

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view') && state.view === 'history') {
    loadData();
  }
  if (changed.has('historyShowSubagents') && state.view === 'history') {
    show();
  }
  // Refresh history when sessions update while viewing history (debounced)
  if (changed.has('sessions') && state.view === 'history') {
    if (historyRefreshTimer) clearTimeout(historyRefreshTimer);
    historyRefreshTimer = setTimeout(loadData, 5000);
  }
}

async function loadData(): Promise<void> {
  try {
    data = await fetchSessions(200, 0);
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
function groupRows(rows: Session[]): { parent: Session; children: Session[] }[] {
  const childrenByParent = new Map<string, Session[]>();
  const parents: Session[] = [];
  const rowById = new Map<string, Session>();

  for (const row of rows) {
    rowById.set(row.id, row);
  }

  // First pass: identify children
  for (const row of rows) {
    if (row.parentId) {
      const list = childrenByParent.get(row.parentId) || [];
      list.push(row);
      childrenByParent.set(row.parentId, list);
    }
  }

  // Flatten nested subagents: if a child's parent is itself a child, move under the top-level ancestor
  for (const [parentId, children] of childrenByParent) {
    const parentRow = rowById.get(parentId);
    if (parentRow && parentRow.parentId) {
      // Find top-level ancestor
      let ancestor = parentRow;
      let depth = 0;
      while (ancestor.parentId && rowById.has(ancestor.parentId) && depth < 10) {
        ancestor = rowById.get(ancestor.parentId)!;
        depth++;
      }
      const ancestorChildren = childrenByParent.get(ancestor.id) || [];
      ancestorChildren.push(...children);
      childrenByParent.set(ancestor.id, ancestorChildren);
      childrenByParent.delete(parentId);
    }
  }

  // Second pass: identify top-level rows (parents and orphan children)
  for (const row of rows) {
    if (!row.parentId) {
      parents.push(row);
    } else if (!rowById.has(row.parentId)) {
      // Orphan child — parent not in result set, render as top-level
      parents.push(row);
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
    checkbox.checked = !state.historyShowSubagents;
    checkbox.setAttribute('aria-label', 'Minimize all subagent groups');
    checkbox.addEventListener('change', () => {
      update({ historyShowSubagents: !checkbox.checked });
    });
    toggleLabel.appendChild(checkbox);
    toggleLabel.append(' Minimize all');
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
        if (isCollapsed) {
          // Expanding this parent: if global minimize is on, switch to
          // per-parent mode by collapsing all others individually instead.
          if (!state.historyShowSubagents) {
            for (const { parent: p, children: c } of grouped) {
              if (c.length > 0 && p.id !== parent.id) {
                collapsedParents.add(p.id);
              }
            }
            update({ historyShowSubagents: true });
          }
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

function createRow(row: Session, isChild: boolean): HTMLTableRowElement {
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

function sortData(rows: Session[]): Session[] {
  return rows.sort((a, b) => {
    let va: number | string, vb: number | string;
    switch (sortCol) {
      case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens + (a.cacheCreationTokens || 0); vb = b.inputTokens + b.outputTokens + b.cacheReadTokens + (b.cacheCreationTokens || 0); break;
      case 'projectName': va = (a.sessionName || a.cwd || '').toLowerCase(); vb = (b.sessionName || b.cwd || '').toLowerCase(); break;
      case 'model': va = a.model || ''; vb = b.model || ''; break;
      case 'lastActive': va = a.lastActive || ''; vb = b.lastActive || ''; break;
      default: {
        const numericAccessors: Record<string, (r: Session) => number> = {
          totalCost: r => r.totalCost,
          duration: r => r.startedAt && r.lastActive ? (new Date(r.lastActive).getTime() - new Date(r.startedAt).getTime()) / 1000 : 0,
          messageCount: r => r.messageCount,
          errorCount: r => r.errorCount,
          cache: r => { const t = r.inputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0); return t > 0 ? r.cacheReadTokens / t * 100 : 0; },
        };
        const accessor = numericAccessors[sortCol];
        va = accessor ? accessor(a) : 0;
        vb = accessor ? accessor(b) : 0;
      }
    }
    const cmp = typeof va === 'string' ? va.localeCompare(vb as string) : (va as number) - (vb as number);
    return sortAsc ? cmp : -cmp;
  });
}
