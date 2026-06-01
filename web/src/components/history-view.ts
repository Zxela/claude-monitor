// web/src/components/history-view.ts
import type { Session, SessionSkills, ToolUsageEntry } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchSessions, fetchSessionSkills } from '../api';
import { formatDurationSecs, formatTokens, effectiveInputTokens } from '../utils';
import '../styles/views.css';

function sessionIdentifier(s: Session): string {
  if (s.sessionName) return s.sessionName;
  return s.id.slice(0, 8);
}

function projectLabel(s: Session): string {
  if (s.cwd) {
    const parts = s.cwd.replace(/\/+$/, '').split('/');
    return parts[parts.length - 1] || s.cwd;
  }
  return s.repoId || '';
}

let container: HTMLElement | null = null;
let data: Session[] = [];
let sortCol = 'lastActive';
let sortAsc = false;
const collapsedParents = new Set<string>();

// Pagination: HISTORY pages through /api/sessions in PAGE-sized chunks so all
// sessions are reachable (previously a single 200-row fetch hid the rest).
const PAGE = 200;
let offset = 0;
let reachedEnd = false;
let loadingMore = false;
const seenIds = new Set<string>();

// Per-parent tree cost (own + aggregated subagent spend), keyed by parent id.
// Populated by groupRows() and consumed by the Cost column + its sort so the
// most expensive session trees rank correctly and match the same-row badge.
const treeCostMap = new Map<string, number>();

// Sparse map of sessionID → skills invoked, loaded once. Used to badge the rows
// whose sessions invoked skills so they stand out in History.
let sessionSkills: SessionSkills = {};
let skillsLoaded = false;

type Column = { key: string; label: string; cls?: string; fmt: (r: Session) => string };

const COLUMNS: Column[] = [
  {
    key: 'lastActive',
    label: 'Date',
    cls: 'col-dim',
    fmt: (r) => (r.lastActive ? new Date(r.lastActive).toLocaleString() : ''),
  },
  { key: 'session', label: 'Session', cls: 'col-session', fmt: sessionIdentifier },
  { key: 'project', label: 'Project', cls: 'col-dim', fmt: projectLabel },
  { key: 'totalCost', label: 'Cost', cls: 'col-cost', fmt: (r) => `$${r.totalCost.toFixed(2)}` },
  {
    key: 'duration',
    label: 'Duration',
    cls: 'col-dim',
    fmt: (r) => {
      if (!r.startedAt || !r.lastActive) return '';
      const secs = (new Date(r.lastActive).getTime() - new Date(r.startedAt).getTime()) / 1000;
      return formatDurationSecs(secs);
    },
  },
  {
    key: 'tokens',
    label: 'Tokens',
    cls: 'col-tokens',
    fmt: (r) => formatTokens(effectiveInputTokens(r) + r.outputTokens),
  },
  {
    key: 'cache',
    label: 'Cache%',
    cls: 'col-cache',
    fmt: (r) => {
      const total = r.inputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0);
      if (total === 0) return '';
      return `${Math.round((r.cacheReadTokens / total) * 100)}%`;
    },
  },
  { key: 'messageCount', label: 'Msgs', fmt: (r) => String(r.messageCount) },
  {
    key: 'errorCount',
    label: 'Errors',
    cls: 'col-err',
    fmt: (r) => (r.errorCount > 0 ? String(r.errorCount) : ''),
  },
  {
    key: 'model',
    label: 'Model',
    cls: 'col-model',
    fmt: (r) => (r.model || '').replace('claude-', ''),
  },
];

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

let historyRefreshTimer: ReturnType<typeof setTimeout> | null = null;

function showLoading(): void {
  if (!container) return;
  container.innerHTML = '';
  const wrapper = document.createElement('div');
  wrapper.className = 'view-overlay';
  wrapper.innerHTML = '<div style="text-align:center;padding:48px;color:var(--text-dim);font-family:var(--font-mono);font-size:12px;letter-spacing:1px">LOADING HISTORY...</div>';
  container.appendChild(wrapper);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view')) {
    if (state.view === 'history') {
      // Fresh entry: reset pagination so we start from the first page.
      offset = 0;
      reachedEnd = false;
      seenIds.clear();
      data = [];
      showLoading();
      loadData();
    } else {
      // Clear any pending refresh timer when leaving history view
      if (historyRefreshTimer) {
        clearTimeout(historyRefreshTimer);
        historyRefreshTimer = null;
      }
    }
  }
  if (changed.has('historyShowSubagents') && state.view === 'history') {
    show();
  }
  // Refresh history when sessions update while viewing history (debounced)
  if (changed.has('sessions') && state.view === 'history') {
    if (historyRefreshTimer) clearTimeout(historyRefreshTimer);
    historyRefreshTimer = setTimeout(() => loadData(), 5000);
  }
}

async function loadData(append = false): Promise<void> {
  if (state.view !== 'history') return; // Guard against stale timer callbacks
  if (append) {
    if (reachedEnd || loadingMore) return;
    loadingMore = true;
  } else {
    // First load or refresh: start over from page 0 so new sessions appear.
    offset = 0;
    reachedEnd = false;
    seenIds.clear();
    data = [];
  }
  // Load the sparse session→skills map once (fire-and-forget): it's small and
  // all-time, so it badges rows across every page. Re-render when it arrives.
  if (!append && !skillsLoaded) {
    skillsLoaded = true;
    fetchSessionSkills()
      .then((m) => {
        sessionSkills = m;
        if (state.view === 'history') show();
      })
      .catch(() => {
        /* non-fatal: rows simply render without skill badges */
      });
  }
  try {
    const raw = await fetchSessions(PAGE, offset);
    if (state.view !== 'history') return; // Re-check after async
    if (raw.length < PAGE) reachedEnd = true;
    offset += PAGE;
    // Filter out trivial sessions (no cost, no tokens, few messages) and dedupe
    // by id so an overlapping/refetched page never produces duplicate rows.
    for (const s of raw) {
      if (seenIds.has(s.id)) continue;
      if (!(s.totalCost > 0 || s.inputTokens > 0 || s.messageCount > 3)) continue;
      seenIds.add(s.id);
      data.push(s);
    }
    show();
  } catch (err) {
    console.error('Failed to load history:', err);
  } finally {
    loadingMore = false;
  }
}

function exportCsv(): void {
  const headers = COLUMNS.map((c) => c.label);
  const rows = sortData([...data]).map((r) =>
    COLUMNS.map((c) => `"${c.fmt(r).replace(/"/g, '""')}"`).join(','),
  );
  const csv = [headers.join(','), ...rows].join('\n');
  const blob = new Blob([csv], { type: 'text/csv' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `claude-monitor-history-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

/** Group rows: parents first (sorted), children grouped under their parent.
 *  Exported for unit testing of the orphan-promotion / nested-flatten logic. */
export function groupRows(rows: Session[]): { parent: Session; children: Session[] }[] {
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

  // Compute each parent's tree cost (own + aggregated subagent spend) before
  // sorting so the Cost column and its sort can rank trees by total spend.
  treeCostMap.clear();
  for (const parent of parents) {
    const children = childrenByParent.get(parent.id) || [];
    const childCost = children.reduce((sum, c) => sum + c.totalCost, 0);
    treeCostMap.set(parent.id, parent.totalCost + childCost);
  }

  const sorted = sortData(parents);
  return sorted.map((parent) => ({
    parent,
    children: sortData(childrenByParent.get(parent.id) || []),
  }));
}

/** Reduce children into one entry per distinct non-empty workflowId. */
function workflowSummary(children: Session[]): { id: string; count: number; cost: number }[] {
  const byWorkflow = new Map<string, { id: string; count: number; cost: number }>();
  for (const c of children) {
    const id = c.workflowId;
    if (!id) continue;
    const entry = byWorkflow.get(id) || { id, count: 0, cost: 0 };
    entry.count += 1;
    entry.cost += c.totalCost;
    byWorkflow.set(id, entry);
  }
  return [...byWorkflow.values()];
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
  exportBtn.textContent = 'EXPORT CSV';
  exportBtn.style.cssText =
    'padding:4px 12px;background:var(--bg-hover);border:1px solid var(--border);color:var(--cyan);font-family:var(--font-mono);font-size:10px;cursor:pointer;border-radius:3px;letter-spacing:0.5px';
  exportBtn.addEventListener('click', exportCsv);
  toolbar.appendChild(exportBtn);

  // Check if any subagents exist in data
  const hasSubagents = data.some((r) => r.parentId);
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
    toggleLabel.append(' MINIMIZE ALL');
    toolbar.appendChild(toggleLabel);
  }

  // Loaded-count indicator so users know history continues past the first page.
  const countEl = document.createElement('span');
  countEl.style.cssText =
    'margin-left:auto;color:var(--text-dim);font-family:var(--font-mono);font-size:10px;letter-spacing:0.5px';
  countEl.textContent = reachedEnd
    ? `Showing ${data.length} sessions`
    : `Showing ${data.length} sessions (more available)`;
  toolbar.appendChild(countEl);

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
      if (sortCol === col.key) {
        sortAsc = !sortAsc;
      } else {
        sortCol = col.key;
        sortAsc = false;
      }
      show();
    };
    th.addEventListener('click', sortByCol);
    th.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        sortByCol();
      }
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
    const workflows = workflowSummary(children);

    // Parent row
    const tr = createRow(parent, false);
    if (hasChildren) {
      const childCost = children.reduce((sum, c) => sum + c.totalCost, 0);
      // Cost column reflects the whole tree (own + subagents) so the most
      // expensive trees rank correctly and match the same-row badge.
      const treeCost = parent.totalCost + childCost;
      const costCell = tr.children[3] as HTMLElement;
      costCell.textContent = `$${treeCost.toFixed(2)}`;
      costCell.title = `$${parent.totalCost.toFixed(2)} own + $${childCost.toFixed(2)} subagents`;

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
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          triangle.click();
        }
      });
      nameCell.insertBefore(triangle, nameCell.firstChild);

      // Add collapsed summary badge
      if (isCollapsed) {
        const badge = document.createElement('span');
        badge.className = 'history-subagent-badge';
        badge.textContent = `(${children.length} subagent${children.length > 1 ? 's' : ''}, $${childCost.toFixed(2)})`;
        nameCell.appendChild(badge);

        // Per-workflow badges (additive to the subagent badge)
        const titleLines = [`${children.length} subagent${children.length > 1 ? 's' : ''}, $${childCost.toFixed(2)}`];
        for (const wf of workflows) {
          const wfBadge = document.createElement('span');
          wfBadge.className = 'history-subagent-badge';
          const shortId = wf.id.replace(/^wf_/, '').slice(0, 8);
          wfBadge.textContent = `wf ${shortId} (${wf.count} agent${wf.count > 1 ? 's' : ''}, $${wf.cost.toFixed(2)})`;
          nameCell.appendChild(wfBadge);
          titleLines.push(`wf ${shortId}: ${wf.count} agent${wf.count > 1 ? 's' : ''} $${wf.cost.toFixed(2)}`);
        }
        // Recoverable tooltip: reflect the badge breakdown (the badges no longer
        // clip, but hover still surfaces the full per-workflow figures) instead
        // of the prompt that createRow() set on the Session cell.
        nameCell.title = titleLines.join('\n');
      }
    }

    // Skill badge rolls up own + subagent-tree skills (mirrors the Cost column)
    // so a COLLAPSED parent still surfaces skills its children invoked — they'd
    // otherwise vanish with the hidden child rows.
    addSkillBadge(
      tr.children[1] as HTMLElement,
      mergeSkills([sessionSkills[parent.id] ?? [], ...children.map((c) => sessionSkills[c.id] ?? [])]),
    );
    tbody.appendChild(tr);

    // Child rows (if not collapsed)
    if (hasChildren && !isCollapsed) {
      // Workflow summary bands heading the agents that share each workflowId
      for (const wf of workflows) {
        const bandTr = document.createElement('tr');
        bandTr.className = 'history-child-row history-workflow-band';
        for (const col of COLUMNS) {
          const td = document.createElement('td');
          if (col.key === 'session') {
            td.textContent = `Workflow wf_${wf.id.replace(/^wf_/, '')} — ${wf.count} agent${wf.count > 1 ? 's' : ''} · $${wf.cost.toFixed(2)}`;
          }
          bandTr.appendChild(td);
        }
        tbody.appendChild(bandTr);
      }
      for (const child of children) {
        const childTr = createRow(child, true);
        addSkillBadge(childTr.children[1] as HTMLElement, sessionSkills[child.id] ?? []);
        tbody.appendChild(childTr);
      }
    }
  }

  table.appendChild(tbody);
  wrapper.appendChild(table);

  // Incremental loading: a LOAD MORE button plus a scroll sentinel that pages
  // through the remaining sessions so all of them are reachable.
  if (!reachedEnd) {
    const footer = document.createElement('div');
    footer.style.cssText = 'display:flex;justify-content:center;padding:12px';

    const moreBtn = document.createElement('button');
    moreBtn.textContent = loadingMore ? 'LOADING...' : 'LOAD MORE';
    moreBtn.disabled = loadingMore;
    moreBtn.style.cssText =
      'padding:6px 16px;background:var(--bg-hover);border:1px solid var(--border);color:var(--cyan);font-family:var(--font-mono);font-size:10px;cursor:pointer;border-radius:3px;letter-spacing:0.5px';
    moreBtn.addEventListener('click', () => loadData(true));
    footer.appendChild(moreBtn);

    // Sentinel: auto-load when scrolled into view.
    const sentinel = document.createElement('div');
    sentinel.style.cssText = 'height:1px';
    footer.appendChild(sentinel);
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && !loadingMore && !reachedEnd) {
        io.disconnect();
        loadData(true);
      }
    });
    io.observe(sentinel);

    wrapper.appendChild(footer);
  }

  container.appendChild(wrapper);
}

// mergeSkills combines several per-session skill lists into one, summing uses
// and errors per skill name, ordered by uses descending. Used to roll a parent's
// subagent-tree skills into its row badge.
function mergeSkills(lists: ToolUsageEntry[][]): ToolUsageEntry[] {
  const byName = new Map<string, ToolUsageEntry>();
  for (const list of lists) {
    for (const s of list) {
      const cur = byName.get(s.name);
      if (cur) {
        cur.uses += s.uses;
        cur.errors += s.errors;
      } else {
        byName.set(s.name, { name: s.name, uses: s.uses, errors: s.errors });
      }
    }
  }
  return [...byName.values()].sort((a, b) => b.uses - a.uses || a.name.localeCompare(b.name));
}

// addSkillBadge appends a fuchsia "✦ N" badge to a row's Session cell when the
// row (or its subagent tree, for a parent) invoked skills, so skill activity is
// discoverable from History instead of being buried in the feed.
function addSkillBadge(nameCell: HTMLElement, skills: ToolUsageEntry[]): void {
  if (skills.length === 0) return;
  const total = skills.reduce((sum, s) => sum + s.uses, 0);
  const badge = document.createElement('span');
  badge.className = 'history-skill-badge';
  badge.textContent = `✦ ${total}`;
  badge.title =
    'Skills invoked:\n' +
    skills.map((s) => `${s.name} ×${s.uses}${s.errors ? ` (${s.errors} err)` : ''}`).join('\n');
  nameCell.appendChild(badge);
}

function createRow(row: Session, isChild: boolean): HTMLTableRowElement {
  const tr = document.createElement('tr');
  if (isChild) tr.className = 'history-child-row';

  for (const col of COLUMNS) {
    const td = document.createElement('td');
    td.textContent = col.fmt(row);
    if (col.cls) td.className = col.cls;
    if (col.key === 'session') td.title = row.taskDescription || '';
    if (col.key === 'project') td.title = row.repoId || row.cwd || '';
    tr.appendChild(td);
  }
  tr.setAttribute('tabindex', '0');
  tr.setAttribute('role', 'button');
  tr.setAttribute('aria-label', `View session: ${COLUMNS[1].fmt(row)}`);
  const openSession = () => {
    update({ selectedSessionId: row.id, view: 'list' });
  };
  tr.addEventListener('click', openSession);
  tr.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openSession();
    }
  });
  return tr;
}

function sortData(rows: Session[]): Session[] {
  return rows.sort((a, b) => {
    let va: number | string, vb: number | string;
    switch (sortCol) {
      case 'tokens':
        va = a.inputTokens + a.outputTokens + a.cacheReadTokens + (a.cacheCreationTokens || 0);
        vb = b.inputTokens + b.outputTokens + b.cacheReadTokens + (b.cacheCreationTokens || 0);
        break;
      case 'session':
        va = sessionIdentifier(a).toLowerCase();
        vb = sessionIdentifier(b).toLowerCase();
        break;
      case 'project':
        va = projectLabel(a).toLowerCase();
        vb = projectLabel(b).toLowerCase();
        break;
      case 'model':
        va = a.model || '';
        vb = b.model || '';
        break;
      case 'lastActive':
        va = a.lastActive || '';
        vb = b.lastActive || '';
        break;
      default: {
        const numericAccessors: Record<string, (r: Session) => number> = {
          // Parents rank by their tree total (own + subagents); children are
          // absent from treeCostMap and fall back to their own cost.
          totalCost: (r) => treeCostMap.get(r.id) ?? r.totalCost,
          duration: (r) =>
            r.startedAt && r.lastActive
              ? (new Date(r.lastActive).getTime() - new Date(r.startedAt).getTime()) / 1000
              : 0,
          messageCount: (r) => r.messageCount,
          errorCount: (r) => r.errorCount,
          cache: (r) => {
            const t = r.inputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0);
            return t > 0 ? (r.cacheReadTokens / t) * 100 : 0;
          },
        };
        const accessor = numericAccessors[sortCol];
        va = accessor ? accessor(a) : 0;
        vb = accessor ? accessor(b) : 0;
      }
    }
    const cmp =
      typeof va === 'string' ? va.localeCompare(vb as string) : (va as number) - (vb as number);
    return sortAsc ? cmp : -cmp;
  });
}
