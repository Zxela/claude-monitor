// web/src/components/feed-panel.ts
import type { ParsedMessage, WsEvent } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchRecentMessages } from '../api';
import { onMessage } from '../ws';
import { renderFeedEntry, detectType } from './render-message';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let feedContent: HTMLElement | null = null;
let filterBar: HTMLElement | null = null;
let autoScroll = true;
let currentLoadSessionId: string | null = null;
const MAX_ENTRIES = 500;

const FILTER_TYPES = ['all', 'user', 'assistant', 'tool', 'result', 'agent', 'hook', 'error'] as const;

export function render(mount: HTMLElement): void {
  container = mount;
  renderEmpty();

  subscribe(onStateChange);
  onMessage(onWsMessage);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  const sessionChanged = changed.has('selectedSessionId');
  const viewChanged = changed.has('view');

  // Handle both changing at once, or either individually
  if (sessionChanged || viewChanged) {
    if (state.view === 'list' && state.selectedSessionId) {
      renderFeedPanel();
      loadRecentMessages(state.selectedSessionId);
    } else if (state.view === 'list') {
      renderEmpty();
    }
    // Other views (graph/table/history) take over the mount — don't render feed
  }
}

function onWsMessage(event: WsEvent): void {
  if (!event.message) return;
  if (!state.selectedSessionId) return;
  if (state.view !== 'list') return;
  if (event.session.id !== state.selectedSessionId) return;

  appendMessage(event.message);
}

function renderEmpty(): void {
  if (!container) return;
  container.innerHTML = '<div class="feed-empty">Select a session to view its feed</div>';
  feedContent = null;
  filterBar = null;
}

function renderFeedPanel(): void {
  if (!container) return;
  container.innerHTML = '';

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
  feedContent.addEventListener('scroll', () => {
    if (!feedContent) return;
    const atBottom = feedContent.scrollHeight - feedContent.scrollTop - feedContent.clientHeight < 30;
    autoScroll = atBottom;
  });
  container.appendChild(feedContent);
}

function handleFilterClick(type: string, e: MouseEvent): void {
  if (type === 'all') {
    // Reset all filters to true
    const filters: Record<string, boolean> = {};
    for (const t of FILTER_TYPES) {
      if (t !== 'all') filters[t] = true;
    }
    update({ feedTypeFilters: filters });
  } else if (e.shiftKey) {
    // Solo mode — show only this type
    const filters: Record<string, boolean> = {};
    for (const t of FILTER_TYPES) {
      if (t !== 'all') filters[t] = (t === type);
    }
    update({ feedTypeFilters: filters });
  } else {
    // Toggle single type
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
  if (currentLoadSessionId === sessionId) return; // prevent concurrent loads for same session
  currentLoadSessionId = sessionId;
  feedContent.innerHTML = '';

  try {
    const messages = await fetchRecentMessages(sessionId);
    // Guard: if session changed while loading, discard results
    if (currentLoadSessionId !== sessionId) return;
    for (const msg of messages) {
      appendMessage(msg as ParsedMessage);
    }
  } catch (err) {
    if (currentLoadSessionId === sessionId) {
      feedContent.innerHTML = `<div class="feed-empty">Failed to load messages</div>`;
    }
  }
}

function appendMessage(msg: ParsedMessage): void {
  if (!feedContent) return;

  const entry = renderFeedEntry(msg);
  const type = detectType(msg);
  const visible = state.feedTypeFilters[type] ?? true;
  if (!visible) entry.style.display = 'none';

  feedContent.appendChild(entry);

  // Trim excess entries
  while (feedContent.children.length > MAX_ENTRIES) {
    feedContent.removeChild(feedContent.firstChild!);
  }

  // Auto-scroll
  if (autoScroll) {
    feedContent.scrollTop = feedContent.scrollHeight;
  }
}
