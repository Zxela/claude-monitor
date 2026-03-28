// web/src/components/feed-panel.ts
import type { ParsedMessage, WsEvent } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchSessionEvents, fetchSessionErrors } from '../api';
import { onMessage } from '../ws';
import { renderFeedEntry, detectType } from './render-message';
import { escapeHtml, sessionDisplayName } from '../utils';
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

const FILTER_TYPES = ['all', 'user', 'assistant', 'tool_use', 'tool_result', 'agent', 'command', 'hook', 'error'] as const;

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
  if (event.event !== 'event' || !event.data || !event.session) return;
  if (state.view !== 'list') return;
  if (!feedContent) return;

  const msg = event.data;

  // In single-session mode, only show messages for selected session
  if (state.selectedSessionId && event.session.id !== state.selectedSessionId) return;

  if (msg.toolName && msg.role === 'assistant') {
    const toolInfo = msg.toolName + (msg.toolDetail ? ': ' + msg.toolDetail.slice(0, 60) : '');
    setLastTool(event.session.id, toolInfo);
  }

  if (msg.isError) {
    const name = sessionDisplayName(event.session);
    notify('error', 'Agent Error', `${name}: ${(msg.contentPreview || '').slice(0, 100)}`);
  }

  const sessionName = sessionDisplayName(event.session);
  const opts = state.selectedSessionId ? {} : { showSessionId: sessionName };
  appendMessage(msg as ParsedMessage, opts);
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
  const sess = state.selectedSessionId ? state.sessions.get(state.selectedSessionId) : null;
  feedContent.setAttribute('aria-label', sess?.isActive !== false ? 'Live event feed' : 'Session history');
  feedContent.innerHTML = '<div class="feed-empty">No events yet — start a Claude Code session and activity will appear here automatically</div>';
  feedContent.addEventListener('scroll', () => {
    if (!feedContent) return;
    const atBottom = feedContent.scrollHeight - feedContent.scrollTop - feedContent.clientHeight < 30;
    autoScroll = atBottom;
    scrollLockBtn?.classList.toggle('visible', !atBottom);
  });
  container.appendChild(feedContent);

  // Scroll lock button — recreate on each render since container.innerHTML
  // clearing detaches any previously appended button from the DOM.
  if (scrollLockBtn && scrollLockBtn.parentNode) {
    scrollLockBtn.parentNode.removeChild(scrollLockBtn);
  }
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

function updateHeader(): void {
  if (!headerEl) return;
  if (state.selectedSessionId) {
    const sess = state.sessions.get(state.selectedSessionId);
    const name = sess ? (sessionDisplayName(sess)) : state.selectedSessionId;
    const isLive = sess?.isActive ?? false;
    const label = isLive ? 'LIVE FEED' : 'SESSION HISTORY';
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">${label}</span>
      <span style="color:var(--text-dim);font-size:10px;letter-spacing:0.5px">${escapeHtml(name)}</span>
      <span class="timeline-btn" role="button" tabindex="0" aria-label="Open timeline view" style="margin-left:8px;color:var(--yellow);font-size:9px;cursor:pointer;border:1px solid rgba(255,204,0,0.3);padding:1px 6px;border-radius:2px;letter-spacing:0.5px">TIMELINE</span>
      ${!isLive ? '<span class="replay-btn" role="button" tabindex="0" aria-label="Replay session" style="margin-left:4px;color:var(--purple,#a855f7);font-size:9px;cursor:pointer;border:1px solid rgba(168,85,247,0.3);padding:1px 6px;border-radius:2px;letter-spacing:0.5px">▶ REPLAY</span>' : ''}
      <span class="back-to-feed" role="button" tabindex="0" aria-label="Back to all sessions" style="margin-left:auto;color:var(--cyan);font-size:10px;cursor:pointer;letter-spacing:0.5px">← ALL</span>`;
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
    if (!isLive) {
      const replayBtn = headerEl.querySelector('.replay-btn');
      replayBtn?.addEventListener('click', () => {
        if (state.selectedSessionId) {
          update({ replaySessionId: state.selectedSessionId });
        }
      });
      replayBtn?.addEventListener('keydown', (e) => {
        if ((e as KeyboardEvent).key === 'Enter' || (e as KeyboardEvent).key === ' ') {
          e.preventDefault();
          if (state.selectedSessionId) {
            update({ replaySessionId: state.selectedSessionId });
          }
        }
      });
    }
  } else {
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px;letter-spacing:0.5px">ALL SESSIONS</span>`;
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
    // Disabling tool_use also disables tool_result (but not the reverse)
    if (type === 'tool_use' && !filters[type]) {
      filters['tool_result'] = false;
    }
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
  let visibleCount = 0;
  feedContent.querySelectorAll<HTMLElement>('.feed-entry').forEach(entry => {
    const type = entry.dataset.type || 'system';
    const visible = state.feedTypeFilters[type] ?? true;
    entry.style.display = visible ? '' : 'none';
    if (visible) visibleCount++;
  });

  // Remove any existing filter-empty message
  const existing = feedContent.querySelector('.feed-filter-empty');
  if (existing) existing.remove();

  // Show message when filters are active but nothing matches
  const hasEntries = feedContent.querySelectorAll('.feed-entry').length > 0;
  if (hasEntries && visibleCount === 0) {
    const msg = document.createElement('div');
    msg.className = 'feed-empty feed-filter-empty';
    msg.textContent = 'No messages match the current filters';
    feedContent.appendChild(msg);
  }
}

async function loadRecentMessages(sessionId: string): Promise<void> {
  if (!feedContent) return;
  if (currentLoadSessionId === sessionId) return;
  currentLoadSessionId = sessionId;
  feedContent.innerHTML = '<div class="feed-empty">Loading...</div>';

  try {
    const [messages, errors] = await Promise.all([
      fetchSessionEvents(sessionId, 50),
      fetchSessionErrors(sessionId),
    ]);
    if (currentLoadSessionId !== sessionId) return;

    // Deduplicate: DB may contain duplicate rows from re-bootstraps.
    // Use timestamp + content as a fingerprint since IDs aren't stable.
    const seen = new Set<string>();
    const dedup = (events: typeof messages) => events.filter(e => {
      const key = `${e.timestamp}|${e.contentPreview ?? ''}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    });

    // Merge error events that aren't in the recent messages window.
    const recentDeduped = dedup(messages);
    const missingErrors = dedup(errors);
    const merged = [...missingErrors, ...recentDeduped].sort(
      (a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()
    );

    for (const msg of merged) {
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

  // Suppress thinking-only assistant messages — they're streaming intermediates
  // that will be replaced by the actual response with the same messageId.
  // These have no contentPreview text (renderer would fall back to "[thinking...]")
  // and no tool info. Keep them if they have expandable fullContent.
  if (msg.role === 'assistant' && !msg.toolName && !msg.isAgent) {
    const preview = (msg.contentPreview || '').replace(/^\[thinking\.\.\.\]$/, '');
    if (!preview && !msg.fullContent) {
      return;
    }
  }

  // Suppress empty system messages (e.g. queue-operation/remove with no content).
  if (!msg.role && !msg.contentPreview && !msg.fullContent) {
    return;
  }

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
