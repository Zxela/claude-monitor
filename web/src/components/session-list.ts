// web/src/components/session-list.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { renderExpanded, renderCompact, renderDot } from './session-card';
import { isSessionActive } from '../utils';
import '../styles/sessions.css';

let el: HTMLElement | null = null;
let listEl: HTMLElement | null = null;
let lastRenderTime = 0;
const MAX_VISIBLE = 15;
const showAllGroups = new Set<string>();
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
      renderList();
    });
  });
  el.appendChild(filterBar);

  // Scrollable list area
  listEl = document.createElement('div');
  listEl.style.cssText = 'flex:1; overflow-y:auto;';
  el.appendChild(listEl);

  // Toggle button
  const toggleBtn = document.createElement('button');
  toggleBtn.className = 'sidebar-toggle';
  toggleBtn.setAttribute('aria-label', 'Toggle sidebar');
  toggleBtn.textContent = '\u00AB';
  toggleBtn.addEventListener('click', () => {
    update({ sidebarCollapsed: !state.sidebarCollapsed });
  });
  el.appendChild(toggleBtn);

  container.appendChild(el);

  renderList();
  setInterval(renderList, 5000);
  subscribe(onStateChange);

  document.addEventListener('keydown', (e) => {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
    if (e.key === '1') { activeFilter = 'active'; renderList(); updateFilterBar(); }
    if (e.key === '2') { activeFilter = 'recent'; renderList(); updateFilterBar(); }
    if (e.key === '3') { activeFilter = 'all'; renderList(); updateFilterBar(); }
  });
}

function updateFilterBar(): void {
  el?.querySelectorAll('.session-filter-bar button').forEach(btn => {
    (btn as HTMLElement).classList.toggle('active', (btn as HTMLElement).dataset.filter === activeFilter);
  });
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('sidebarCollapsed') && el) {
    el.classList.toggle('collapsed', _state.sidebarCollapsed);
    const toggleBtn = el.querySelector('.sidebar-toggle');
    if (toggleBtn) toggleBtn.textContent = _state.sidebarCollapsed ? '\u00BB' : '\u00AB';
    renderList();
  }
  if (changed.has('selectedSessionId') || changed.has('renderVersion') || changed.has('projectFilter')) {
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

  // Collapsed mode: render dots only
  if (state.sidebarCollapsed) {
    listEl.innerHTML = '';
    const allSessions = [...state.sessions.values()]
      .filter(s => !s.isSubagent)
      .sort((a, b) => new Date(b.lastActive).getTime() - new Date(a.lastActive).getTime());
    for (const sess of allSessions) {
      listEl.appendChild(renderDot(sess));
    }
    return;
  }

  const now = Date.now();
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

  // Update filter counts
  const recentCount = active.length + [...lastHour, ...today].filter(s => new Date(s.lastActive).getTime() > recentCutoff).length;
  const totalCount = state.sessions.size;
  const fcActive = el?.querySelector('#fc-active');
  const fcRecent = el?.querySelector('#fc-recent');
  const fcAll = el?.querySelector('#fc-all');
  if (fcActive) fcActive.textContent = String(active.length);
  if (fcRecent) fcRecent.textContent = String(recentCount);
  if (fcAll) fcAll.textContent = String(totalCount);

  // Save scroll + render
  const scrollTop = listEl.scrollTop;
  listEl.innerHTML = '';

  const filter = state.projectFilter;
  const topLevel = (sessions: Session[]) =>
    (filter ? sessions.filter(s => s.projectName === filter || s.sessionName === filter) : sessions)
      .filter(s => !s.isSubagent);
  const sort = (sessions: Session[]) =>
    sessions.sort((a, b) => new Date(b.lastActive).getTime() - new Date(a.lastActive).getTime());

  // Active Now
  const activeSorted = sort(topLevel(active));
  if (activeSorted.length > 0) {
    const header = document.createElement('div');
    header.className = 'active-section-header';
    header.textContent = `ACTIVE NOW (${activeSorted.length})`;
    listEl.appendChild(header);

    const section = document.createElement('div');
    section.className = 'active-section';
    for (const sess of activeSorted) renderExpanded(sess, section);
    listEl.appendChild(section);
  }

  // Time groups
  if (activeFilter === 'active') {
    listEl.scrollTop = scrollTop;
    return;
  }

  const groups: [string, string, Session[]][] = [
    ['lastHour', 'Last hour', lastHour],
    ['today', 'Today', today],
    ['yesterday', 'Yesterday', yesterday],
    ['thisWeek', 'This week', thisWeek],
    ['older', 'Older', older],
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
    for (const sess of visible) renderCompact(sess, items);

    if (needsTruncation) {
      const btn = document.createElement('button');
      btn.className = 'show-all-btn';
      btn.textContent = `Show all ${filtered.length} sessions`;
      btn.addEventListener('click', (e) => { e.stopPropagation(); showAllGroups.add(key); renderList(); });
      items.appendChild(btn);
    }

    group.appendChild(items);
    listEl.appendChild(group);
  }

  listEl.scrollTop = scrollTop;
}
