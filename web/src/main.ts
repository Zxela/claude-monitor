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
import { open as openReplay, togglePlay as replayToggle, restart as replayRestart, stepForward as replayForward, stepBackward as replayBack } from './components/replay';
import { toggle as toggleHelp } from './components/help-overlay';
import { init as initHash } from './hash';
import { expandedParents } from './components/session-card';

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

function getVisibleSessionIds(): string[] {
  const cards = document.querySelectorAll<HTMLElement>('[data-session-id]');
  return Array.from(cards).map(c => c.dataset.sessionId!).filter(Boolean);
}

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
    case ' ': {
      if (state.replaySessionId) {
        e.preventDefault();
        replayToggle();
      }
      break;
    }
    case 'r':
    case 'R': {
      if (state.replaySessionId) {
        replayRestart();
      }
      break;
    }
    case 'Escape':
      update({ selectedSessionId: null, searchOpen: false });
      break;
    case 'ArrowDown': {
      e.preventDefault();
      const ids = getVisibleSessionIds();
      if (ids.length === 0) break;
      const idx = state.focusedSessionId ? ids.indexOf(state.focusedSessionId) : -1;
      const next = ids[Math.min(idx + 1, ids.length - 1)];
      update({ focusedSessionId: next });
      break;
    }
    case 'ArrowUp': {
      e.preventDefault();
      const ids = getVisibleSessionIds();
      if (ids.length === 0) break;
      const idx = state.focusedSessionId ? ids.indexOf(state.focusedSessionId) : ids.length;
      const prev = ids[Math.max(idx - 1, 0)];
      update({ focusedSessionId: prev });
      break;
    }
    case 'Enter': {
      if (state.focusedSessionId) {
        update({ selectedSessionId: state.focusedSessionId === state.selectedSessionId ? null : state.focusedSessionId });
      }
      break;
    }
    case 'ArrowRight': {
      if (state.replaySessionId) {
        replayForward();
      } else if (state.focusedSessionId) {
        expandedParents.add(state.focusedSessionId);
        update({ renderVersion: state.renderVersion + 1 });
      }
      break;
    }
    case 'ArrowLeft': {
      if (state.replaySessionId) {
        replayBack();
      } else if (state.focusedSessionId) {
        expandedParents.delete(state.focusedSessionId);
        update({ renderVersion: state.renderVersion + 1 });
      }
      break;
    }
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
