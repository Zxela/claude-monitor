import './styles/base.css';
import { state, subscribe, update, updateSession } from './state';
import { connect } from './ws';
import { fetchGroupedSessions, fetchVersion } from './api';
import { render as renderTopbar } from './components/topbar';
import { render as renderSessionList } from './components/session-list';
import { render as renderSearch } from './components/search';
import { render as renderFeedPanel } from './components/feed-panel';
import { render as renderGraphView } from './components/graph-view';
import { render as renderHistoryView } from './components/history-view';
import { render as renderTimeline } from './components/timeline-view';
import { render as renderAnalyticsView } from './components/analytics-view';
import { render as renderBudget } from './components/budget-popover';
import { toggle as toggleHelp } from './components/help-overlay';
import { dismiss as dismissCostBreakdown } from './components/cost-breakdown';
import { dismiss as dismissBudget } from './components/budget-popover';
import { init as initHash } from './hash';
import { init as initOnboarding } from './components/onboarding';
import { render as renderUpdateBanner } from './components/update-banner';
import { startSampling } from './burn-rate';

// Mount components
const topbarMount = document.getElementById('topbar-mount')!;
const sessionsMount = document.getElementById('sessions-mount')!;
const feedMount = document.getElementById('feed-mount')!;

renderTopbar(topbarMount);
renderUpdateBanner(document.getElementById('app')!);
renderSessionList(sessionsMount);

// Feed panel + views all render into feed-mount
renderFeedPanel(feedMount);
renderGraphView(feedMount);
renderHistoryView(feedMount);
renderTimeline(feedMount);
renderAnalyticsView(feedMount);

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
    case 'a':
      update({ view: state.view === 'analytics' ? 'list' : 'analytics' });
      break;
    case '?':
      toggleHelp();
      break;
    case 'Escape':
      dismissCostBreakdown();
      dismissBudget();
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
      if (state.focusedSessionId) {
        update({ selectedSessionId: state.focusedSessionId });
      }
      break;
    }
    case 'ArrowLeft': {
      if (state.focusedSessionId) {
        update({ selectedSessionId: state.selectedSessionId === state.focusedSessionId ? null : state.focusedSessionId });
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
    const feedArea = document.getElementById('feed-mount');
    if (feedArea) {
      const banner = document.createElement('div');
      banner.className = 'error-banner';
      banner.style.cssText = 'background:rgba(255,60,60,0.15);color:#ff6b6b;padding:8px 12px;font-size:12px;border:1px solid rgba(255,60,60,0.3);border-radius:4px;margin:8px;';
      banner.textContent = 'Failed to load sessions. Check that the server is running and try refreshing.';
      feedArea.prepend(banner);
    }
  }

  connect();
  initHash();
  initOnboarding();
  startSampling();
}

init();
