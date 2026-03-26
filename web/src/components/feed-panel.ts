// web/src/components/feed-panel.ts
import type { ParsedMessage, WsEvent } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchRecentMessages } from '../api';
import { onMessage } from '../ws';
import { renderFeedEntry, detectType } from './render-message';
import { escapeHtml } from '../utils';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let feedContent: HTMLElement | null = null;
let filterBar: HTMLElement | null = null;
let headerEl: HTMLElement | null = null;
let autoScroll = true;
let currentLoadSessionId: string | null = null;
const MAX_ENTRIES = 500;

const FILTER_TYPES = ['all', 'user', 'assistant', 'tool_use', 'tool_result', 'agent', 'hook', 'error'] as const;

export function render(mount: HTMLElement): void {
  container = mount;

  // Start with the feed panel showing (multi-session mode)
  renderFeedPanel();

  subscribe(onStateChange);
  onMessage(onWsMessage);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  const sessionChanged = changed.has('selectedSessionId');
  const viewChanged = changed.has('view');

  if (sessionChanged || viewChanged) {
    if (state.view !== 'list') return; // other views take over the mount

    if (state.selectedSessionId) {
      renderFeedPanel();
      loadRecentMessages(state.selectedSessionId);
    } else {
      // Back to multi-session mode
      renderFeedPanel();
      currentLoadSessionId = null;
    }
  }
}

function onWsMessage(event: WsEvent): void {
  if (!event.message) return;
  if (state.view !== 'list') return;
  if (!feedContent) return;

  // In single-session mode, only show messages for selected session
  if (state.selectedSessionId && event.session.id !== state.selectedSessionId) return;

  const sessionName = event.session.sessionName || event.session.projectName || event.session.id.slice(0, 8);
  const opts = state.selectedSessionId ? {} : { showSessionId: sessionName };
  appendMessage(event.message, opts);
}

function renderFeedPanel(): void {
  if (!container) return;
  container.innerHTML = '';

  // Header
  headerEl = document.createElement('div');
  headerEl.className = 'feed-header';
  updateHeader();
  container.appendChild(headerEl);

  // Filter bar
  filterBar = document.createElement('div');
  filterBar.className = 'feed-type-filters';
  for (const type of FILTER_TYPES) {
    const btn = document.createElement('button');
    btn.className = `feed-filter-btn ${type === 'all' ? 'all-btn' : ''} ${type === 'all' || state.feedTypeFilters[type] ? 'active' : ''}`;
    btn.textContent = type.toUpperCase();
    btn.dataset.type = type;
    btn.addEventListener('click', (e) => handleFilterClick(type, e as MouseEvent));
    filterBar!.appendChild(btn);
  }
  container.appendChild(filterBar);

  // Feed content area
  feedContent = document.createElement('div');
  feedContent.className = 'feed-content';
  feedContent.innerHTML = '<div class="feed-empty">WAITING FOR EVENTS...</div>';
  feedContent.addEventListener('scroll', () => {
    if (!feedContent) return;
    const atBottom = feedContent.scrollHeight - feedContent.scrollTop - feedContent.clientHeight < 30;
    autoScroll = atBottom;
  });
  container.appendChild(feedContent);
}

function updateHeader(): void {
  if (!headerEl) return;
  if (state.selectedSessionId) {
    const sess = state.sessions.get(state.selectedSessionId);
    const name = sess ? (sess.sessionName || sess.projectName || sess.id) : state.selectedSessionId;
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">${escapeHtml(name)}</span>`;
  } else {
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">all sessions</span>`;
  }
}

function handleFilterClick(type: string, e: MouseEvent): void {
  if (type === 'all') {
    const filters: Record<string, boolean> = {};
    for (const t of FILTER_TYPES) {
      if (t !== 'all') filters[t] = true;
    }
    update({ feedTypeFilters: filters });
  } else if (e.shiftKey) {
    const filters: Record<string, boolean> = {};
    for (const t of FILTER_TYPES) {
      if (t !== 'all') filters[t] = (t === type);
    }
    update({ feedTypeFilters: filters });
  } else {
    const filters = { ...state.feedTypeFilters };
    filters[type] = !filters[type];
    update({ feedTypeFilters: filters });
  }
  updateFilterButtons();
  applyFilters();
}

function updateFilterButtons(): void {
  if (!filterBar) return;
  filterBar.querySelectorAll<HTMLButtonElement>('.feed-filter-btn').forEach(btn => {
    const t = btn.dataset.type!;
    if (t === 'all') return;
    btn.classList.toggle('active', state.feedTypeFilters[t] ?? true);
  });
}

function applyFilters(): void {
  if (!feedContent) return;
  feedContent.querySelectorAll<HTMLElement>('.feed-entry').forEach(entry => {
    const type = entry.dataset.type || 'system';
    const visible = state.feedTypeFilters[type] ?? true;
    entry.style.display = visible ? '' : 'none';
  });
}

async function loadRecentMessages(sessionId: string): Promise<void> {
  if (!feedContent) return;
  if (currentLoadSessionId === sessionId) return;
  currentLoadSessionId = sessionId;
  feedContent.innerHTML = '';

  try {
    const messages = await fetchRecentMessages(sessionId);
    if (currentLoadSessionId !== sessionId) return;
    for (const msg of messages) {
      appendMessage(msg as ParsedMessage);
    }
  } catch {
    if (currentLoadSessionId === sessionId) {
      feedContent.innerHTML = '<div class="feed-empty">Failed to load messages</div>';
    }
  }
}

function appendMessage(msg: ParsedMessage, opts: { showSessionId?: string } = {}): void {
  if (!feedContent) return;

  // Remove the "WAITING FOR EVENTS..." placeholder
  const empty = feedContent.querySelector('.feed-empty');
  if (empty) empty.remove();

  const entry = renderFeedEntry(msg, opts);
  const type = detectType(msg);
  const visible = state.feedTypeFilters[type] ?? true;
  if (!visible) entry.style.display = 'none';

  feedContent.appendChild(entry);

  while (feedContent.children.length > MAX_ENTRIES) {
    feedContent.removeChild(feedContent.firstChild!);
  }

  if (autoScroll) {
    feedContent.scrollTop = feedContent.scrollHeight;
  }
}
