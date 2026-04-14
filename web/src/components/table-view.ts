// web/src/components/table-view.ts
import type { Session } from '../types';
import { state, subscribe, update } from '../state';
import { sessionDisplayName, formatDurationSecs } from '../utils';
import '../styles/views.css';

type SortKey = 'name' | 'status' | 'cost' | 'tokens' | 'messages' | 'errors' | 'duration' | 'lastActive';
type SortDir = 'asc' | 'desc';

let container: HTMLElement | null = null;
let tableEl: HTMLElement | null = null;
let sortKey: SortKey = 'lastActive';
let sortDir: SortDir = 'desc';

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe((_s, changed) => {
    if (
      changed.has('view') ||
      changed.has('sessions') ||
      changed.has('grouped') ||
      changed.has('selectedSessionId')
    ) {
      renderTable();
    }
  });
  renderTable();
}

function getAllSessions(): Session[] {
  const g = state.grouped;
  if (!g) return [];
  return [
    ...g.active,
    ...g.lastHour,
    ...g.today,
    ...g.yesterday,
    ...g.thisWeek,
    ...g.older,
  ].filter((s) => !s.parentId); // top-level only
}

function sortSessions(sessions: Session[]): Session[] {
  return [...sessions].sort((a, b) => {
    let cmp = 0;
    switch (sortKey) {
      case 'name':
        cmp = sessionDisplayName(a).localeCompare(sessionDisplayName(b));
        break;
      case 'status':
        cmp = (a.isActive ? 0 : 1) - (b.isActive ? 0 : 1);
        break;
      case 'cost':
        cmp = a.totalCost - b.totalCost;
        break;
      case 'tokens':
        cmp = (a.inputTokens + a.outputTokens) - (b.inputTokens + b.outputTokens);
        break;
      case 'messages':
        cmp = a.messageCount - b.messageCount;
        break;
      case 'errors':
        cmp = a.errorCount - b.errorCount;
        break;
      case 'duration': {
        const dA = a.startedAt ? Date.now() - new Date(a.startedAt).getTime() : 0;
        const dB = b.startedAt ? Date.now() - new Date(b.startedAt).getTime() : 0;
        cmp = dA - dB;
        break;
      }
      case 'lastActive':
        cmp = new Date(a.lastActive).getTime() - new Date(b.lastActive).getTime();
        break;
    }
    return sortDir === 'asc' ? cmp : -cmp;
  });
}

function handleSort(key: SortKey): void {
  if (sortKey === key) {
    sortDir = sortDir === 'asc' ? 'desc' : 'asc';
  } else {
    sortKey = key;
    sortDir = 'desc';
  }
  renderTable();
}

function renderTable(): void {
  if (!container) return;

  if (state.view !== 'table') {
    if (tableEl) {
      tableEl.style.display = 'none';
    }
    return;
  }

  if (!tableEl) {
    tableEl = document.createElement('div');
    tableEl.className = 'table-view';
    container.appendChild(tableEl);
  }

  tableEl.style.display = 'flex';
  tableEl.style.flexDirection = 'column';

  const sessions = sortSessions(getAllSessions());

  const cols: { key: SortKey; label: string; align?: string }[] = [
    { key: 'name', label: 'SESSION' },
    { key: 'status', label: 'STATUS' },
    { key: 'cost', label: 'COST', align: 'right' },
    { key: 'tokens', label: 'TOKENS', align: 'right' },
    { key: 'messages', label: 'MSGS', align: 'right' },
    { key: 'errors', label: 'ERRORS', align: 'right' },
    { key: 'duration', label: 'DURATION', align: 'right' },
    { key: 'lastActive', label: 'LAST ACTIVE', align: 'right' },
  ];

  const arrowFor = (key: SortKey): string => {
    if (sortKey !== key) return '<span style="color:var(--text-dim);opacity:0.4">⇅</span>';
    return sortDir === 'asc'
      ? '<span style="color:var(--cyan)">↑</span>'
      : '<span style="color:var(--cyan)">↓</span>';
  };

  const headerCells = cols
    .map(
      (c) =>
        `<th data-sort="${c.key}" style="text-align:${c.align ?? 'left'};cursor:pointer;user-select:none;padding:6px 10px;border-bottom:1px solid var(--border);color:var(--text-dim);font-size:10px;letter-spacing:0.5px;white-space:nowrap">
          ${c.label} ${arrowFor(c.key)}
        </th>`
    )
    .join('');

  const rowsHtml = sessions
    .map((s) => {
      const name = sessionDisplayName(s);
      const totalTokens = s.inputTokens + s.outputTokens;
      const durationMs = s.startedAt ? Date.now() - new Date(s.startedAt).getTime() : 0;
      const duration = formatDurationSecs(durationMs / 1000);
      const lastActive = new Date(s.lastActive).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
      });
      const statusColor = s.isActive
        ? s.status === 'thinking'
          ? 'var(--cyan)'
          : s.status === 'tool_use'
            ? 'var(--yellow)'
            : 'var(--green)'
        : 'var(--text-dim)';
      const statusLabel = s.isActive ? s.status.toUpperCase().replace('_', ' ') : 'IDLE';
      const isSelected = state.selectedSessionId === s.id;

      return `<tr data-session-id="${s.id}" style="cursor:pointer;background:${isSelected ? 'var(--bg-hover)' : 'transparent'};border-left:${isSelected ? '2px solid var(--cyan)' : '2px solid transparent'}">
        <td style="padding:5px 10px;font-size:11px;max-width:200px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${name}">${name}</td>
        <td style="padding:5px 10px;font-size:10px;color:${statusColor}">${statusLabel}</td>
        <td style="padding:5px 10px;font-size:11px;text-align:right;color:${s.totalCost > 0.1 ? 'var(--yellow)' : 'var(--text)'}">$${s.totalCost.toFixed(4)}</td>
        <td style="padding:5px 10px;font-size:11px;text-align:right;color:var(--text-dim)">${totalTokens.toLocaleString()}</td>
        <td style="padding:5px 10px;font-size:11px;text-align:right">${s.messageCount}</td>
        <td style="padding:5px 10px;font-size:11px;text-align:right;color:${s.errorCount > 0 ? 'var(--red)' : 'var(--text-dim)'}">${s.errorCount || '—'}</td>
        <td style="padding:5px 10px;font-size:11px;text-align:right;color:var(--text-dim)">${duration}</td>
        <td style="padding:5px 10px;font-size:11px;text-align:right;color:var(--text-dim)">${lastActive}</td>
      </tr>`;
    })
    .join('');

  tableEl.innerHTML = `
    <div style="display:flex;align-items:center;justify-content:space-between;padding:8px 10px;border-bottom:1px solid var(--border)">
      <span style="color:var(--cyan);font-size:11px;letter-spacing:1px">TABLE VIEW</span>
      <span style="color:var(--text-dim);font-size:10px">${sessions.length} session${sessions.length !== 1 ? 's' : ''}</span>
    </div>
    <div style="overflow:auto;flex:1">
      <table style="width:100%;border-collapse:collapse;font-family:var(--font-mono,monospace)">
        <thead>
          <tr>${headerCells}</tr>
        </thead>
        <tbody>${rowsHtml}</tbody>
      </table>
    </div>
  `;

  // Sort header click handlers
  tableEl.querySelectorAll<HTMLElement>('th[data-sort]').forEach((th) => {
    th.addEventListener('click', () => handleSort(th.dataset.sort as SortKey));
    th.addEventListener('mouseenter', () => { th.style.color = 'var(--text)'; });
    th.addEventListener('mouseleave', () => { th.style.color = sortKey === th.dataset.sort ? 'var(--cyan)' : 'var(--text-dim)'; });
  });

  // Row click: select session
  tableEl.querySelectorAll<HTMLElement>('tr[data-session-id]').forEach((row) => {
    row.addEventListener('mouseenter', () => {
      if (state.selectedSessionId !== row.dataset.sessionId) {
        row.style.background = 'var(--bg-hover)';
      }
    });
    row.addEventListener('mouseleave', () => {
      if (state.selectedSessionId !== row.dataset.sessionId) {
        row.style.background = 'transparent';
      }
    });
    row.addEventListener('click', () => {
      const sid = row.dataset.sessionId!;
      update({
        selectedSessionId: state.selectedSessionId === sid ? null : sid,
        view: 'list',
      });
    });
  });
}
