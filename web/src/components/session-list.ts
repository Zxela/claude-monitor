import type { GroupedSessions, Session } from '../types';
import { state, subscribe } from '../state';
import { fetchGroupedSessions } from '../api';
import { renderExpanded, renderCompact } from './session-card';
import '../styles/sessions.css';

let el: HTMLElement | null = null;
const MAX_VISIBLE = 15;
const expandedGroups = new Set<string>();

export function render(container: HTMLElement): void {
  el = document.createElement('div');
  el.className = 'sessions-panel';
  container.appendChild(el);

  refresh();
  setInterval(refresh, 5000);
  subscribe(onStateChange);
}

async function refresh(): Promise<void> {
  try {
    const grouped = await fetchGroupedSessions();
    renderGrouped(grouped);
  } catch (err) {
    console.error('Failed to fetch grouped sessions:', err);
  }
}

function onStateChange(_state: typeof state, changed: Set<string>): void {
  if (changed.has('selectedSessionId') || changed.has('sessions') || changed.has('projectFilter')) {
    refresh();
  }
}

function renderGrouped(grouped: GroupedSessions): void {
  if (!el) return;
  el.innerHTML = '';

  const filter = state.projectFilter;

  const activeSessions = applyFilter(grouped.active, filter);
  if (activeSessions.length > 0) {
    const header = document.createElement('div');
    header.className = 'active-section-header';
    header.textContent = `ACTIVE NOW (${activeSessions.length})`;
    el.appendChild(header);

    const section = document.createElement('div');
    section.className = 'active-section';
    sortByLastActive(activeSessions);
    for (const sess of activeSessions) {
      renderExpanded(sess, section);
    }
    el.appendChild(section);
  }

  const groups: [string, string, Session[]][] = [
    ['lastHour', 'Last hour', grouped.lastHour],
    ['today', 'Today', grouped.today],
    ['yesterday', 'Yesterday', grouped.yesterday],
    ['thisWeek', 'This week', grouped.thisWeek],
    ['older', 'Older', grouped.older],
  ];

  for (const [key, label, sessions] of groups) {
    const filtered = applyFilter(sessions, filter);
    if (filtered.length === 0) continue;

    sortByLastActive(filtered);

    const group = document.createElement('div');
    const isCollapsed = filtered.length > MAX_VISIBLE && !expandedGroups.has(key);

    const header = document.createElement('div');
    header.className = 'time-group-header';
    header.innerHTML = `
      <span>${label}</span>
      <span class="time-group-count">${filtered.length}</span>
    `;
    header.addEventListener('click', () => {
      group.classList.toggle('time-group-collapsed');
    });
    group.appendChild(header);

    const items = document.createElement('div');
    items.className = 'time-group-items';
    const visibleSessions = isCollapsed ? filtered.slice(0, MAX_VISIBLE) : filtered;
    for (const sess of visibleSessions) {
      renderCompact(sess, items);
    }

    if (isCollapsed && filtered.length > MAX_VISIBLE) {
      const showAll = document.createElement('button');
      showAll.className = 'show-all-btn';
      showAll.textContent = `Show all ${filtered.length} sessions`;
      showAll.addEventListener('click', (e) => {
        e.stopPropagation();
        expandedGroups.add(key);
        refresh();
      });
      items.appendChild(showAll);
    }

    group.appendChild(items);
    el.appendChild(group);
  }
}

function applyFilter(sessions: Session[], projectFilter: string | null): Session[] {
  if (!projectFilter) return sessions;
  return sessions.filter(s => s.projectName === projectFilter || s.sessionName === projectFilter);
}

function sortByLastActive(sessions: Session[]): void {
  sessions.sort((a, b) => new Date(b.lastActive).getTime() - new Date(a.lastActive).getTime());
}
