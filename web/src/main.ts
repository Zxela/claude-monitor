import './styles/base.css';
import { state, subscribe, update } from './state';
import { connect } from './ws';
import { fetchGroupedSessions, fetchVersion } from './api';
import { render as renderTopbar } from './components/topbar';
import { render as renderSessionList } from './components/session-list';
import { render as renderSearch } from './components/search';

// Mount components
const topbarMount = document.getElementById('topbar-mount')!;
const sessionsMount = document.getElementById('sessions-mount')!;

renderTopbar(topbarMount);
renderSessionList(sessionsMount);

// Attach search dropdown to the search box in the topbar
const searchBox = topbarMount.querySelector<HTMLElement>('.search-box');
if (searchBox) {
  renderSearch(searchBox);
}

// Status bar updates
const connIndicator = document.getElementById('conn-indicator')!;
const sbHost = document.getElementById('sb-host')!;
const sbEvents = document.getElementById('sb-events')!;
const sbVersion = document.getElementById('sb-version')!;

sbHost.textContent = location.host;

subscribe((_state, changed) => {
  if (changed.has('connected')) {
    connIndicator.textContent = state.connected ? 'CONNECTED' : 'DISCONNECTED';
    connIndicator.style.color = state.connected ? 'var(--green)' : 'var(--red)';
  }
  if (changed.has('eventCount')) {
    sbEvents.textContent = `${state.eventCount} EVENTS`;
  }
  if (changed.has('version')) {
    sbVersion.textContent = `CLAUDE MONITOR ${state.version}`;
  }
});

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;

  switch (e.key) {
    case 'g':
      update({ view: state.view === 'graph' ? 'list' : 'graph' });
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
  } catch (err) {
    console.error('Failed to load sessions:', err);
  }

  connect();
}

init();
