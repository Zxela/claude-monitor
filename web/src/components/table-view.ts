// web/src/components/table-view.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { formatDurationSecs, formatTokens } from '../utils';
import '../styles/views.css';

let container: HTMLElement | null = null;
let sortCol = 'totalCostUSD';
let sortAsc = false;
let lastRenderTime = 0;

type Column = { key: string; label: string; cls?: string; fmt: (s: Session) => string };

const COLUMNS: Column[] = [
  { key: 'projectName', label: 'Name', fmt: s => (s.sessionName || s.projectName || s.id).substring(0, 25) },
  { key: 'status', label: 'Status', cls: 'col-dim', fmt: s => s.status },
  { key: 'totalCostUSD', label: 'Cost', cls: 'col-cost', fmt: s => `$${s.totalCostUSD.toFixed(2)}` },
  { key: 'costRate', label: '$/min', cls: 'col-rate', fmt: s => s.costRate > 0 ? `$${s.costRate.toFixed(3)}` : '' },
  { key: 'duration', label: 'Duration', cls: 'col-dim', fmt: s => formatDurationSecs(durationSecs(s)) },
  { key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: s => formatTokens(s.inputTokens + s.outputTokens + s.cacheReadTokens) },
  { key: 'cacheHitPct', label: 'Cache%', cls: 'col-cache', fmt: s => `${s.cacheHitPct.toFixed(0)}%` },
  { key: 'messageCount', label: 'Msgs', fmt: s => String(s.messageCount) },
  { key: 'errorCount', label: 'Errors', cls: 'col-err', fmt: s => s.errorCount > 0 ? String(s.errorCount) : '' },
  { key: 'model', label: 'Model', cls: 'col-model', fmt: s => (s.model || '').replace('claude-', '') },
];

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view') && state.view === 'table') show();
  if (changed.has('sessions') && state.view === 'table') {
    const now = Date.now();
    if (now - lastRenderTime < 500) return;
    show();
  }
}

function show(): void {
  if (!container) return;
  lastRenderTime = Date.now();
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'view-overlay';

  const table = document.createElement('table');
  const thead = document.createElement('thead');
  const headerRow = document.createElement('tr');

  for (const col of COLUMNS) {
    const th = document.createElement('th');
    th.innerHTML = `${col.label}${sortCol === col.key ? `<span class="sort-arrow">${sortAsc ? '▲' : '▼'}</span>` : ''}`;
    th.addEventListener('click', () => {
      if (sortCol === col.key) { sortAsc = !sortAsc; } else { sortCol = col.key; sortAsc = false; }
      show();
    });
    headerRow.appendChild(th);
  }
  thead.appendChild(headerRow);
  table.appendChild(thead);

  const tbody = document.createElement('tbody');
  const sessions = sortSessions(Array.from(state.sessions.values()));

  for (const sess of sessions) {
    const tr = document.createElement('tr');
    if (sess.id === state.selectedSessionId) tr.className = 'selected-row';
    for (const col of COLUMNS) {
      const td = document.createElement('td');
      td.textContent = col.fmt(sess);
      if (col.cls) td.className = col.cls;
      tr.appendChild(td);
    }
    tr.addEventListener('click', () => update({ selectedSessionId: sess.id, view: 'list' }));
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrapper.appendChild(table);
  container.appendChild(wrapper);
}

function sortSessions(sessions: Session[]): Session[] {
  return sessions.sort((a, b) => {
    let va: number | string, vb: number | string;
    switch (sortCol) {
      case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens; vb = b.inputTokens + b.outputTokens + b.cacheReadTokens; break;
      case 'duration': va = durationSecs(a); vb = durationSecs(b); break;
      case 'projectName': va = (a.sessionName || a.projectName || '').toLowerCase(); vb = (b.sessionName || b.projectName || '').toLowerCase(); break;
      case 'status': va = a.status; vb = b.status; break;
      case 'model': va = a.model || ''; vb = b.model || ''; break;
      default: va = (a as any)[sortCol] ?? 0; vb = (b as any)[sortCol] ?? 0;
    }
    const cmp = typeof va === 'string' ? va.localeCompare(vb as string) : (va as number) - (vb as number);
    return sortAsc ? cmp : -cmp;
  });
}

function durationSecs(s: Session): number {
  if (!s.startedAt) return 0;
  const start = new Date(s.startedAt).getTime();
  const end = s.lastActive ? new Date(s.lastActive).getTime() : Date.now();
  return (end - start) / 1000;
}

