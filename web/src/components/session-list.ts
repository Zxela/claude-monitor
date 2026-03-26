// web/src/components/session-list.ts
import type { GroupedSessions, Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe } from '../state';
import { fetchGroupedSessions } from '../api';
import { renderExpanded, renderCompact } from './session-card';
import '../styles/sessions.css';

let el: HTMLElement | null = null;
let listEl: HTMLElement | null = null;
const MAX_VISIBLE = 15;
const expandedGroups = new Set<string>();
const collapsedGroups = new Set<string>(['lastHour', 'today', 'yesterday', 'thisWeek', 'older']);
let activeFilter: 'active' | 'recent' | 'all' = 'recent';

export function render(container: HTMLElement): void {
  el = document.createElement('div');
  el.className = 'sessions-panel';

  // Filter bar
  const filterBar = document.createElement('div');
  filterBar.className = 'session-filter-bar';
  filterBar.innerHTML = `
    <button data-filter="active">Active <span id="fc-active"></span></button>
    <button data-filter="recent" class="active">Recent <span id="fc-recent"></span></button>
    <button data-filter="all">All <span id="fc-all"></span></button>
  `;
  filterBar.querySelectorAll('button').forEach(btn => {
    btn.addEventListener('click', () => {
      activeFilter = btn.dataset.filter as typeof activeFilter;
      filterBar.querySelectorAll('button').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      renderFromState();
    });
  });
  el.appendChild(filterBar);

  // Scrollable list area
  listEl = document.createElement('div');
  listEl.style.cssText = 'flex:1; overflow-y:auto;';
  el.appendChild(listEl);

  container.appendChild(el);

  // Initial HTTP fetch + periodic poll
  refresh();
  setInterval(refresh, 5000);
  subscribe(onStateChange);

  // Keyboard shortcuts 1/2/3 for filter
  document.addEventListener('keydown', (e) => {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
    if (e.key === '1') { activeFilter = 'active'; renderFromState(); updateFilterBar(); }
    if (e.key === '2') { activeFilter = 'recent'; renderFromState(); updateFilterBar(); }
    if (e.key === '3') { activeFilter = 'all'; renderFromState(); updateFilterBar(); }
  });
}

function updateFilterBar(): void {
  el?.querySelectorAll('.session-filter-bar button').forEach(btn => {
    (btn as HTMLElement).classList.toggle('active', (btn as HTMLElement).dataset.filter === activeFilter);
  });
}

async function refresh(): Promise<void> {
  try {
    const grouped = await fetchGroupedSessions();
    renderGrouped(grouped);
  } catch (err) {
    console.error('Failed to fetch grouped sessions:', err);
  }
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('selectedSessionId') || changed.has('renderVersion') || changed.has('projectFilter')) {
    renderFromState();
  }
  if (changed.has('focusedSessionId')) {
    // Update focused class on all cards
    listEl?.querySelectorAll<HTMLElement>('.session-card, .session-card-compact').forEach(card => {
      card.classList.toggle('focused', card.dataset.sessionId === _state.focusedSessionId);
    });
    // Scroll focused card into view
    const focused = listEl?.querySelector<HTMLElement>('.focused');
    if (focused) focused.scrollIntoView({ block: 'nearest' });
  }
}

/** Build time groups from state.sessions locally (no HTTP) */
function renderFromState(): void {
  const now = Date.now();
  const todayStart = new Date();
  todayStart.setHours(0, 0, 0, 0);
  const yesterdayStart = new Date(todayStart);
  yesterdayStart.setDate(yesterdayStart.getDate() - 1);
  const weekStart = new Date(todayStart);
  weekStart.setDate(weekStart.getDate() - 7);

  const grouped: GroupedSessions = {
    active: [],
    lastHour: [],
    today: [],
    yesterday: [],
    thisWeek: [],
    older: [],
  };

  for (const sess of state.sessions.values()) {
    if (sess.isActive) {
      grouped.active.push(sess);
      continue;
    }
    const lastActiveMs = new Date(sess.lastActive).getTime();
    if (now - lastActiveMs < 3600_000) {
      grouped.lastHour.push(sess);
    } else if (lastActiveMs >= todayStart.getTime()) {
      grouped.today.push(sess);
    } else if (lastActiveMs >= yesterdayStart.getTime()) {
      grouped.yesterday.push(sess);
    } else if (lastActiveMs >= weekStart.getTime()) {
      grouped.thisWeek.push(sess);
    } else {
      grouped.older.push(sess);
    }
  }

  renderGrouped(grouped);
}

function renderGrouped(grouped: GroupedSessions): void {
  if (!listEl) return;

  // Update filter counts
  const allSessions = [...grouped.active, ...grouped.lastHour, ...grouped.today, ...grouped.yesterday, ...grouped.thisWeek, ...grouped.older];
  const recentCutoff = Date.now() - 4 * 60 * 60 * 1000; // 4 hours
  const recentCount = allSessions.filter(s => s.isActive || new Date(s.lastActive).getTime() > recentCutoff).length;
  const fcActive = el?.querySelector('#fc-active');
  const fcRecent = el?.querySelector('#fc-recent');
  const fcAll = el?.querySelector('#fc-all');
  if (fcActive) fcActive.textContent = String(grouped.active.length);
  if (fcRecent) fcRecent.textContent = String(recentCount);
  if (fcAll) fcAll.textContent = String(allSessions.length);

  // Save scroll position
  const scrollTop = listEl.scrollTop;
  listEl.innerHTML = '';

  const filter = state.projectFilter;

  // Filter out subagents that will be rendered inline by their parents
  const filterTopLevel = (sessions: Session[]): Session[] => {
    return applyFilter(sessions, filter).filter(s => !s.isSubagent);
  };

  // Apply active filter
  const showTimeline = activeFilter !== 'active';
  const recentOnly = activeFilter === 'recent';

  // Active Now section
  const activeSessions = filterTopLevel(grouped.active);
  if (activeSessions.length > 0) {
    const header = document.createElement('div');
    header.className = 'active-section-header';
    header.textContent = `ACTIVE NOW (${activeSessions.length})`;
    listEl.appendChild(header);

    const section = document.createElement('div');
    section.className = 'active-section';
    sortByLastActive(activeSessions);
    for (const sess of activeSessions) {
      renderExpanded(sess, section);
    }
    listEl.appendChild(section);
  }

  // Timeline groups
  const groups: [string, string, Session[]][] = [
    ['lastHour', 'Last hour', grouped.lastHour],
    ['today', 'Today', grouped.today],
    ['yesterday', 'Yesterday', grouped.yesterday],
    ['thisWeek', 'This week', grouped.thisWeek],
    ['older', 'Older', grouped.older],
  ];

  if (!showTimeline) {
    listEl.scrollTop = scrollTop;
    return;
  }

  for (const [key, label, sessions] of groups) {
    // In "recent" mode, only show lastHour and today
    if (recentOnly && key !== 'lastHour' && key !== 'today') continue;

    const filtered = filterTopLevel(sessions);
    if (filtered.length === 0) continue;

    sortByLastActive(filtered);

    const group = document.createElement('div');
    // Restore collapsed state
    if (collapsedGroups.has(key)) {
      group.classList.add('time-group-collapsed');
    }

    const isShowingAll = expandedGroups.has(key);
    const needsTruncation = filtered.length > MAX_VISIBLE && !isShowingAll;

    const header = document.createElement('div');
    header.className = 'time-group-header';
    header.innerHTML = `
      <span>${label}</span>
      <span class="time-group-count">${filtered.length}</span>
    `;
    header.addEventListener('click', () => {
      group.classList.toggle('time-group-collapsed');
      if (group.classList.contains('time-group-collapsed')) {
        collapsedGroups.add(key);
      } else {
        collapsedGroups.delete(key);
      }
    });
    group.appendChild(header);

    const items = document.createElement('div');
    items.className = 'time-group-items';
    const visibleSessions = needsTruncation ? filtered.slice(0, MAX_VISIBLE) : filtered;
    for (const sess of visibleSessions) {
      renderCompact(sess, items);
    }

    if (needsTruncation) {
      const showAll = document.createElement('button');
      showAll.className = 'show-all-btn';
      showAll.textContent = `Show all ${filtered.length} sessions`;
      showAll.addEventListener('click', (e) => {
        e.stopPropagation();
        expandedGroups.add(key);
        renderFromState();
      });
      items.appendChild(showAll);
    }

    group.appendChild(items);
    listEl.appendChild(group);
  }

  // Restore scroll position
  listEl.scrollTop = scrollTop;
}

function applyFilter(sessions: Session[], projectFilter: string | null): Session[] {
  if (!projectFilter) return sessions;
  return sessions.filter(s => s.projectName === projectFilter || s.sessionName === projectFilter);
}

function sortByLastActive(sessions: Session[]): void {
  sessions.sort((a, b) => new Date(b.lastActive).getTime() - new Date(a.lastActive).getTime());
}
