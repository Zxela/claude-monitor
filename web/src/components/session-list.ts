// web/src/components/session-list.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe } from '../state';
import { renderCompact } from './session-card';
import { isSessionActive } from '../utils';
import '../styles/sessions.css';

let el: HTMLElement | null = null;
let listEl: HTMLElement | null = null;
let lastRenderTime = 0;
const MAX_VISIBLE = 15;
const showAllGroups = new Set<string>();
const collapsedGroups = new Set<string>(['yesterday', 'thisWeek', 'older']);
let activeFilter: 'active' | 'recent' | 'all' = 'recent';
let refreshInterval: ReturnType<typeof setInterval> | null = null;
let keydownHandler: ((e: KeyboardEvent) => void) | null = null;

export function render(container: HTMLElement): void {
  el = document.createElement('div');
  el.className = 'sessions-panel';

  // Filter bar
  const filterBar = document.createElement('div');
  filterBar.className = 'session-filter-bar';
  filterBar.innerHTML = `
    <button data-filter="active">ACTIVE (<span id="fc-active"></span>)</button>
    <button data-filter="recent" class="active">RECENT (<span id="fc-recent"></span>)</button>
    <button data-filter="all">ALL (<span id="fc-all"></span>)</button>
  `;
  filterBar.querySelectorAll('button').forEach(btn => {
    btn.addEventListener('click', () => {
      activeFilter = btn.dataset.filter as typeof activeFilter;
      filterBar.querySelectorAll('button').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      renderList();
    });
  });
  el.appendChild(filterBar);

  // Scrollable list area
  listEl = document.createElement('div');
  listEl.style.cssText = 'flex:1; overflow-y:auto;';
  el.appendChild(listEl);

  container.appendChild(el);

  renderList();

  // Clear previous interval/listener to prevent accumulation on re-render
  if (refreshInterval !== null) clearInterval(refreshInterval);
  refreshInterval = setInterval(renderList, 5000);

  subscribe(onStateChange);

  if (keydownHandler) document.removeEventListener('keydown', keydownHandler);
  keydownHandler = (e: KeyboardEvent) => {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
    if (e.key === '1') { activeFilter = 'active'; renderList(); updateFilterBar(); }
    if (e.key === '2') { activeFilter = 'recent'; renderList(); updateFilterBar(); }
    if (e.key === '3') { activeFilter = 'all'; renderList(); updateFilterBar(); }
  };
  document.addEventListener('keydown', keydownHandler);
}

function updateFilterBar(): void {
  el?.querySelectorAll('.session-filter-bar button').forEach(btn => {
    (btn as HTMLElement).classList.toggle('active', (btn as HTMLElement).dataset.filter === activeFilter);
  });
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('selectedSessionId') || changed.has('renderVersion') || changed.has('repoFilter')) {
    renderList();
  }
  if (changed.has('sessions')) {
    const now = Date.now();
    if (now - lastRenderTime > 1000) {
      lastRenderTime = now;
      renderList();
    }
  }
  if (changed.has('focusedSessionId')) {
    listEl?.querySelectorAll<HTMLElement>('.session-card, .session-card-compact').forEach(card => {
      card.classList.toggle('focused', card.dataset.sessionId === _state.focusedSessionId);
    });
    const focused = listEl?.querySelector<HTMLElement>('.focused');
    if (focused) focused.scrollIntoView({ block: 'nearest' });
  }
}

function renderList(): void {
  if (!listEl) return;

  const now = Date.now();

  // Locally transition stale sessions to idle so subagents don't linger as "waiting".
  // Exception: parents with active children stay as "waiting" — work is still happening.
  for (const sess of state.sessions.values()) {
    if (sess.isActive && !isSessionActive(sess.lastActive)) {
      const hasActiveChild = (sess.children ?? []).some(id => {
        const child = state.sessions.get(id);
        return child && isSessionActive(child.lastActive);
      });
      if (hasActiveChild) {
        sess.status = 'waiting';
      } else {
        sess.isActive = false;
        sess.status = 'idle';
      }
    }
  }

  const todayStart = new Date(); todayStart.setHours(0, 0, 0, 0);
  const yesterdayStart = new Date(todayStart); yesterdayStart.setDate(yesterdayStart.getDate() - 1);
  const weekStart = new Date(todayStart); weekStart.setDate(weekStart.getDate() - 7);
  const recentCutoff = now - 4 * 3600_000; // 4 hours

  // Group sessions
  const active: Session[] = [];
  const lastHour: Session[] = [];
  const today: Session[] = [];
  const yesterday: Session[] = [];
  const thisWeek: Session[] = [];
  const older: Session[] = [];

  for (const sess of state.sessions.values()) {
    if (isSessionActive(sess.lastActive)) {
      active.push(sess);
      continue;
    }
    const ms = new Date(sess.lastActive).getTime();
    if (now - ms < 3600_000) lastHour.push(sess);
    else if (ms >= todayStart.getTime()) today.push(sess);
    else if (ms >= yesterdayStart.getTime()) yesterday.push(sess);
    else if (ms >= weekStart.getTime()) thisWeek.push(sess);
    else older.push(sess);
  }

  // Auto-reveal: if a session is selected, ensure its group is visible
  if (state.selectedSessionId) {
    const sel = state.sessions.get(state.selectedSessionId);
    // Find the parent if this is a subagent
    const target = sel?.parentId ? state.sessions.get(sel.parentId) ?? sel : sel;
    if (target && !isSessionActive(target.lastActive)) {
      const ms = new Date(target.lastActive).getTime();
      let groupKey: string | null = null;
      if (now - ms < 3600_000) groupKey = 'lastHour';
      else if (ms >= todayStart.getTime()) groupKey = 'today';
      else if (ms >= yesterdayStart.getTime()) groupKey = 'yesterday';
      else if (ms >= weekStart.getTime()) groupKey = 'thisWeek';
      else groupKey = 'older';

      // Switch to 'all' filter if the group isn't visible under current filter
      if (activeFilter === 'active' || (activeFilter === 'recent' && groupKey !== 'lastHour' && groupKey !== 'today')) {
        activeFilter = 'all';
        updateFilterBar();
      }
      // Uncollapse the group
      if (groupKey) collapsedGroups.delete(groupKey);
    }
  }

  // Save scroll + render
  const scrollTop = listEl.scrollTop;
  listEl.innerHTML = '';

  const filter = state.repoFilter;

  // Update filter counts — only count top-level, non-trivial sessions (no subagents) to match group display
  const isTrivialCount = (s: Session) => s.totalCost === 0 && (s.inputTokens + s.outputTokens + s.cacheReadTokens) === 0 && s.messageCount < 4;
  const topLevelFilter = (s: Session) => !s.parentId && !isTrivialCount(s) && (!filter || s.cwd === filter || s.sessionName === filter);
  const recentCount = active.length + [...lastHour, ...today].filter(s => topLevelFilter(s) && new Date(s.lastActive).getTime() > recentCutoff).length;
  const totalCount = Array.from(state.sessions.values()).filter(topLevelFilter).length;
  const fcActive = el?.querySelector('#fc-active');
  const fcRecent = el?.querySelector('#fc-recent');
  const fcAll = el?.querySelector('#fc-all');
  if (fcActive) fcActive.textContent = String(active.filter(s => !s.parentId).length);
  if (fcRecent) fcRecent.textContent = String(recentCount);
  if (fcAll) fcAll.textContent = String(totalCount);
  // Filter: top-level only, repo filter, and skip trivial sessions (no cost, no tokens, few messages)
  const isTrivial = (s: Session) => s.totalCost === 0 && (s.inputTokens + s.outputTokens + s.cacheReadTokens) === 0 && s.messageCount < 4;
  const topLevel = (sessions: Session[], includeTrivial = false) =>
    (filter ? sessions.filter(s => s.cwd === filter || s.sessionName === filter) : sessions)
      .filter(s => !s.parentId)
      .filter(s => includeTrivial || !isTrivial(s));
  const sort = (sessions: Session[]) =>
    sessions.sort((a, b) => new Date(b.lastActive).getTime() - new Date(a.lastActive).getTime());

  // Active Now (always show active sessions, even trivial ones)
  const activeSorted = sort(topLevel(active, true));
  if (activeSorted.length > 0) {
    const header = document.createElement('div');
    header.className = 'active-section-header';
    header.textContent = `ACTIVE NOW (${activeSorted.length})`;
    listEl.appendChild(header);

    const section = document.createElement('div');
    section.className = 'active-section';
    // Build parent→children map for inline subagent display
    const activeChildrenOf = new Map<string, Session[]>();
    for (const s of active) {
      if (s.parentId) {
        const list = activeChildrenOf.get(s.parentId) ?? [];
        list.push(s);
        activeChildrenOf.set(s.parentId, list);
      }
    }
    for (const sess of activeSorted) {
      renderCompact(sess, section);
      const children = activeChildrenOf.get(sess.id);
      if (children && children.length > 0) {
        const familyIds = new Set([sess.id, ...children.map(c => c.id)]);
        if (state.selectedSessionId && familyIds.has(state.selectedSessionId)) {
          // Active parents: only show active children
          const activeOnly = sort([...children]).filter(c => c.isActive);
          for (const child of activeOnly) renderCompact(child, section);
        }
      }
    }
    listEl.appendChild(section);
  }

  // Time groups
  if (activeFilter === 'active') {
    if (activeSorted.length === 0) {
      const empty = document.createElement('div');
      empty.className = 'time-group-empty';
      empty.textContent = 'NO ACTIVE SESSIONS';
      listEl.appendChild(empty);
    }
    listEl.scrollTop = scrollTop;
    return;
  }

  const groups: [string, string, Session[]][] = [
    ['lastHour', 'LAST HOUR', lastHour],
    ['today', 'TODAY', today],
    ['yesterday', 'YESTERDAY', yesterday],
    ['thisWeek', 'THIS WEEK', thisWeek],
    ['older', 'OLDER', older],
  ];

  for (const [key, label, sessions] of groups) {
    if (activeFilter === 'recent' && key !== 'lastHour' && key !== 'today') continue;

    const filtered = sort(topLevel(sessions));
    if (filtered.length === 0) continue;

    const group = document.createElement('div');
    if (collapsedGroups.has(key)) group.classList.add('time-group-collapsed');

    const isShowingAll = showAllGroups.has(key);
    const needsTruncation = filtered.length > MAX_VISIBLE && !isShowingAll;

    const header = document.createElement('div');
    header.className = 'time-group-header';
    header.innerHTML = `<span>${label}</span><span class="time-group-count">${filtered.length}</span>`;
    header.addEventListener('click', () => {
      group.classList.toggle('time-group-collapsed');
      if (group.classList.contains('time-group-collapsed')) collapsedGroups.add(key);
      else collapsedGroups.delete(key);
    });
    group.appendChild(header);

    const items = document.createElement('div');
    items.className = 'time-group-items';
    const visible = needsTruncation ? filtered.slice(0, MAX_VISIBLE) : filtered;
    // Build parent→children map for inline subagent display
    const childrenOf = new Map<string, Session[]>();
    for (const s of sessions) {
      if (s.parentId) {
        const list = childrenOf.get(s.parentId) ?? [];
        list.push(s);
        childrenOf.set(s.parentId, list);
      }
    }
    for (const sess of visible) {
      renderCompact(sess, items);
      // For inactive parents: show children inline when this family is selected
      const children = childrenOf.get(sess.id);
      if (children && children.length > 0) {
        const familyIds = new Set([sess.id, ...children.map(c => c.id)]);
        if (state.selectedSessionId && familyIds.has(state.selectedSessionId)) {
          for (const child of sort([...children])) renderCompact(child, items);
        }
      }
    }

    if (needsTruncation) {
      const btn = document.createElement('button');
      btn.className = 'show-all-btn';
      btn.textContent = `SHOW ALL ${filtered.length} SESSIONS`;
      btn.addEventListener('click', (e) => { e.stopPropagation(); showAllGroups.add(key); renderList(); });
      items.appendChild(btn);
    }

    group.appendChild(items);
    listEl.appendChild(group);
  }

  listEl.scrollTop = scrollTop;

  // Scroll selected session into view
  if (state.selectedSessionId) {
    const selected = listEl.querySelector<HTMLElement>(`.selected`);
    if (selected) selected.scrollIntoView({ block: 'nearest' });
  }
}
