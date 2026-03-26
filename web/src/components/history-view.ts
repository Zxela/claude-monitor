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
let loaded = false;

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
    if (!loaded) {
      loadData();
    } else {
      show();
    }
  }
}

async function loadData(): Promise<void> {
  try {
    data = await fetchHistory(200, 0);
    loaded = true;
    show();
  } catch (err) {
    console.error('Failed to load history:', err);
  }
}

function show(): void {
  if (!container) return;
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
  const sorted = sortData([...data]);

  for (const row of sorted) {
    const tr = document.createElement('tr');
    for (const col of COLUMNS) {
      const td = document.createElement('td');
      td.textContent = col.fmt(row);
      if (col.cls) td.className = col.cls;
      if (col.key === 'projectName') td.title = row.taskDescription || '';
      tr.appendChild(td);
    }
    tr.addEventListener('click', () => {
      // Open replay for this session
      update({ replaySessionId: row.id, view: 'list' });
    });
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrapper.appendChild(table);
  container.appendChild(wrapper);
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

