import './styles/base.css';
import { state, subscribe, update, updateSession } from './state';
import { connect } from './ws';
import { fetchGroupedSessions, fetchVersion } from './api';
import { render as renderTopbar } from './components/topbar';
import { render as renderSessionList } from './components/session-list';
import { render as renderSearch } from './components/search';
import { render as renderFeedPanel } from './components/feed-panel';
import { render as renderGraphView } from './components/graph-view';
import { render as renderTableView } from './components/table-view';
import { render as renderHistoryView } from './components/history-view';
import { render as renderBudget } from './components/budget-popover';
import { open as openReplay } from './components/replay';
import { toggle as toggleHelp } from './components/help-overlay';
import { init as initHash } from './hash';

// Mount components
const topbarMount = document.getElementById('topbar-mount')!;
const sessionsMount = document.getElementById('sessions-mount')!;
const feedMount = document.getElementById('feed-mount')!;

renderTopbar(topbarMount);
renderSessionList(sessionsMount);

// Feed panel + views all render into feed-mount
renderFeedPanel(feedMount);
renderGraphView(feedMount);
renderTableView(feedMount);
renderHistoryView(feedMount);

// Search dropdown
const searchBox = topbarMount.querySelector<HTMLElement>('.search-box');
if (searchBox) {
  renderSearch(searchBox);
}

// Budget popover — find the gear button and cost stat in topbar
const gearBtn = topbarMount.querySelector<HTMLElement>('.budget-gear');
const costStat = topbarMount.querySelector<HTMLElement>('[data-stat="cost"]');
if (gearBtn && costStat) {
  renderBudget(gearBtn, costStat, document.getElementById('app')!);
}

// Status bar updates
const connDot = document.getElementById('conn-dot')!;
const connIndicator = document.getElementById('conn-indicator')!;
const sbHost = document.getElementById('sb-host')!;
const sbEvents = document.getElementById('sb-events')!;
const sbVersion = document.getElementById('sb-version')!;

sbHost.textContent = location.host;

subscribe((_state, changed) => {
  if (changed.has('connected')) {
    const c = state.connected;
    connDot.className = `sb-dot ${c ? 'connected' : 'disconnected'}`;
    connIndicator.textContent = c ? 'CONNECTED' : 'DISCONNECTED';
    connIndicator.className = c ? 'connected' : 'disconnected';
  }
  if (changed.has('eventCount')) {
    sbEvents.textContent = String(state.eventCount);
  }
  if (changed.has('version')) {
    sbVersion.textContent = `CLAUDE MONITOR ${state.version}`;
  }
  // Handle replay trigger from history view
  if (changed.has('replaySessionId') && state.replaySessionId) {
    openReplay(state.replaySessionId);
  }
});

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;

  switch (e.key) {
    case 'g':
      update({ view: state.view === 'graph' ? 'list' : 'graph' });
      break;
    case 'h':
      update({ view: state.view === 'history' ? 'list' : 'history' });
      break;
    case 't':
      update({ view: state.view === 'table' ? 'list' : 'table' });
      break;
    case '?':
      toggleHelp();
      break;
    case 'Escape':
      update({ selectedSessionId: null, searchOpen: false });
      break;
  }
});

// Bootstrap
async function init() {
  try {
    const version = await fetchVersion();
    update({ version });
  } catch {
    update({ version: 'dev' });
  }

  try {
    const grouped = await fetchGroupedSessions();
    const allSessions = [
      ...grouped.active,
      ...grouped.lastHour,
      ...grouped.today,
      ...grouped.yesterday,
      ...grouped.thisWeek,
      ...grouped.older,
    ];
    for (const sess of allSessions) {
      state.sessions.set(sess.id, sess);
    }
    update({ grouped });
    if (allSessions.length > 0) {
      updateSession(allSessions[allSessions.length - 1]);
    }
  } catch (err) {
    console.error('Failed to load sessions:', err);
  }

  connect();
  initHash();
}

init();
