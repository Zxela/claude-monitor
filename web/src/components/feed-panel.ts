// web/src/components/feed-panel.ts
import type { ParsedMessage, WsEvent } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchRecentMessages } from '../api';
import { onMessage } from '../ws';
import { renderFeedEntry, detectType } from './render-message';
import { escapeHtml } from '../utils';
import { setLastTool } from '../tool-tracker';
import { notify } from '../notifications';
import { open as openTimeline } from './timeline-view';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let feedContent: HTMLElement | null = null;
let lastToolEntry: HTMLElement | null = null;
let filterBar: HTMLElement | null = null;
let headerEl: HTMLElement | null = null;
let scrollLockBtn: HTMLElement | null = null;
let autoScroll = true;
let currentLoadSessionId: string | null = null;
const MAX_ENTRIES = 500;

// Maps toolUseId -> displayType so tool_result messages can inherit their
// originating tool_use call's display type for consistent grouping/styling.
const toolUseMap = new Map<string, string>(); // toolUseId -> displayType

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

  if (event.message.toolName && event.message.role === 'assistant') {
    const toolInfo = event.message.toolName + (event.message.toolDetail ? ': ' + event.message.toolDetail.slice(0, 60) : '');
    setLastTool(event.session.id, toolInfo);
  }

  if (event.message.isError) {
    const name = event.session.sessionName || event.session.projectName || 'Agent';
    notify('error', 'Agent Error', `${name}: ${(event.message.contentText || '').slice(0, 100)}`);
  }

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
  feedContent.setAttribute('aria-live', 'polite');
  feedContent.setAttribute('aria-label', 'Live event feed');
  feedContent.innerHTML = '<div class="feed-empty">WAITING FOR EVENTS...</div>';
  feedContent.addEventListener('scroll', () => {
    if (!feedContent) return;
    const atBottom = feedContent.scrollHeight - feedContent.scrollTop - feedContent.clientHeight < 30;
    autoScroll = atBottom;
    scrollLockBtn?.classList.toggle('visible', !atBottom);
  });
  container.appendChild(feedContent);

  // Scroll lock button
  if (!scrollLockBtn) {
    scrollLockBtn = document.createElement('button');
    scrollLockBtn.className = 'scroll-lock-btn';
    scrollLockBtn.textContent = '▼ RESUME SCROLL';
    scrollLockBtn.addEventListener('click', () => {
      if (feedContent) {
        feedContent.scrollTop = feedContent.scrollHeight;
        autoScroll = true;
        scrollLockBtn?.classList.remove('visible');
      }
    });
    document.body.appendChild(scrollLockBtn);
  }
}

function updateHeader(): void {
  if (!headerEl) return;
  if (state.selectedSessionId) {
    const sess = state.sessions.get(state.selectedSessionId);
    const name = sess ? (sess.sessionName || sess.projectName || sess.id) : state.selectedSessionId;
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">${escapeHtml(name)}</span>
      <span class="timeline-btn" role="button" tabindex="0" aria-label="Open timeline view" style="margin-left:8px;color:var(--yellow);font-size:9px;cursor:pointer;border:1px solid rgba(255,204,0,0.3);padding:1px 6px;border-radius:2px;letter-spacing:0.5px">TIMELINE</span>
      <span class="back-to-feed" role="button" tabindex="0" aria-label="Back to all sessions" style="margin-left:auto;color:var(--cyan);font-size:10px;cursor:pointer;letter-spacing:0.5px">← all</span>`;
    const backBtn = headerEl.querySelector('.back-to-feed');
    backBtn?.addEventListener('click', () => { update({ selectedSessionId: null }); });
    backBtn?.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
        e.preventDefault();
        update({ selectedSessionId: null });
      }
    });
    const timelineBtn = headerEl.querySelector('.timeline-btn');
    timelineBtn?.addEventListener('click', () => {
      if (state.selectedSessionId) openTimeline(state.selectedSessionId);
    });
    timelineBtn?.addEventListener('keydown', (e) => {
      if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
        e.preventDefault();
        if (state.selectedSessionId) openTimeline(state.selectedSessionId);
      }
    });
  } else {
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">all sessions</span>`;
  }
}

function handleFilterClick(type: string, e: MouseEvent): void {
  if (type === 'all') {
    // Toggle: if all on → all off, if any off → all on
    const allOn = FILTER_TYPES.every(t => t === 'all' || state.feedTypeFilters[t]);
    const filters: Record<string, boolean> = {};
    for (const t of FILTER_TYPES) {
      if (t !== 'all') filters[t] = !allOn;
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

  // Track tool_use IDs for result linking so tool_result messages can
  // inherit their display type from the originating tool_use call.
  if (msg.toolName && msg.role === 'assistant' && msg.toolUseId) {
    toolUseMap.set(msg.toolUseId, detectType(msg));
  }

  const entry = renderFeedEntry(msg, opts);
  const msgType = detectType(msg);

  // Tool results inherit type from originating call (clean up map entry).
  if (msgType === 'tool_result') {
    const forId = msg.forToolUseId;
    if (forId && toolUseMap.has(forId)) {
      const inheritedType = toolUseMap.get(forId)!;
      entry.dataset.type = inheritedType;
      // Keep tool_result styling but inherited type for filtering
      toolUseMap.delete(forId);
    }
  }

  const type = msgType;
  const visible = state.feedTypeFilters[type] ?? true;
  if (!visible) entry.style.display = 'none';

  if (type === 'tool_use') {
    entry.classList.add('tool-group-start');
    lastToolEntry = entry;
  } else if (type === 'tool_result' && lastToolEntry) {
    entry.classList.add('tool-group-end');
    lastToolEntry = null;
  } else {
    lastToolEntry = null;
  }

  feedContent.appendChild(entry);

  while (feedContent.children.length > MAX_ENTRIES) {
    feedContent.removeChild(feedContent.firstChild!);
  }

  if (autoScroll) {
    feedContent.scrollTop = feedContent.scrollHeight;
  }
}
