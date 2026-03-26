# Component Porting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port the remaining 6 UI features (feed panel, replay, graph, table, history, budget, help) from the old monolithic HTML into the new Vite + TypeScript modular frontend.

**Architecture:** Each feature becomes a self-contained TypeScript module under `web/src/components/` that exports a `render(container)` function and subscribes to state changes. A shared `render-message.ts` module handles message entry rendering for both the feed panel and replay. Views (graph/table/history) render into the `#feed-mount` area, toggled by the existing view state. `main.ts` orchestrates mounting and keyboard shortcuts.

**Tech Stack:** TypeScript 5, Vite 6, Canvas 2D (graph), SSE/EventSource (replay), localStorage (budget)

---

## Phase 1: Feed Panel + Replay

### Task 1: Add feed-related state fields

**Files:**
- Modify: `web/src/state.ts`
- Modify: `web/src/types.ts`

- [ ] **Step 1: Add state fields for feed, replay, and budget**

In `web/src/state.ts`, add these fields to the `AppState` interface (after line 16, before the closing `}`):

```typescript
  // Feed
  feedTypeFilters: Record<string, boolean>;

  // Replay
  replaySessionId: string | null;
  replayPlaying: boolean;

  // Budget
  budgetThreshold: number | null;
  budgetDismissed: boolean;
```

Add defaults to the `state` object (after line 36, before the closing `}`):

```typescript
  feedTypeFilters: { user: true, assistant: true, tool: true, result: true, agent: true, hook: true, error: true },
  replaySessionId: null,
  replayPlaying: false,
  budgetThreshold: null,
  budgetDismissed: false,
```

- [ ] **Step 2: Update fetchRecentMessages return type in api.ts**

In `web/src/api.ts`, change line 30 from:

```typescript
export async function fetchRecentMessages(sessionId: string): Promise<unknown[]> {
```

to:

```typescript
import type { GroupedSessions, ProjectEntry, SearchResult, Session, HistoryRow, ParsedMessage } from './types';

export async function fetchRecentMessages(sessionId: string): Promise<ParsedMessage[]> {
```

(Also update the import at line 1 to include `ParsedMessage`.)

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add web/src/state.ts web/src/api.ts
git commit -m "feat: add feed, replay, and budget state fields"
```

### Task 2: Create shared message renderer

**Files:**
- Create: `web/src/components/render-message.ts`

- [ ] **Step 1: Create render-message.ts**

```typescript
// web/src/components/render-message.ts
import type { ParsedMessage } from '../types';

export interface RenderOptions {
  showSessionId?: string;  // if set, show session ID on the right
}

type MessageType = 'user' | 'assistant' | 'tool' | 'result' | 'agent' | 'hook' | 'error' | 'system';

const TYPE_COLORS: Record<MessageType, string> = {
  user: '#5588ff',
  assistant: '#33dd99',
  tool: '#ddcc44',
  result: '#44cccc',
  agent: '#dd8844',
  hook: '#aa77dd',
  error: '#dd4455',
  system: '#666',
};

const TYPE_LABELS: Record<MessageType, string> = {
  user: 'USER',
  assistant: 'ASST',
  tool: 'TOOL',
  result: 'RESULT',
  agent: 'AGENT',
  hook: 'HOOK',
  error: 'ERROR',
  system: 'SYS',
};

export function detectType(msg: ParsedMessage): MessageType {
  if (msg.isError) return 'error';
  if (msg.hookEvent) return 'hook';
  if (msg.toolName && msg.role === 'assistant') return 'tool';
  if (msg.toolName && msg.role === 'tool') return 'result';
  if (msg.type === 'agent' || msg.type === 'agent-name') return 'agent';
  if (msg.role === 'user') return 'user';
  if (msg.role === 'assistant') return 'assistant';
  return 'system';
}

export function renderFeedEntry(msg: ParsedMessage, opts: RenderOptions = {}): HTMLElement {
  const type = detectType(msg);
  const el = document.createElement('div');
  el.className = `feed-entry type-${type}`;
  el.dataset.type = type;
  el.style.borderLeftColor = TYPE_COLORS[type];

  const time = formatTime(msg.timestamp);
  const label = TYPE_LABELS[type];
  const content = msg.contentText || msg.toolDetail || msg.toolName || '';
  const truncLen = type === 'result' ? 100 : 120;
  const truncated = content.length > truncLen ? content.substring(0, truncLen) + '...' : content;
  const hasMore = content.length > truncLen;

  let toolInfo = '';
  if (msg.toolName && type === 'tool') {
    toolInfo = `<span class="fe-tool">${escapeHtml(msg.toolName)}</span> `;
  }

  el.innerHTML = `
    <span class="fe-time">${time}</span>
    <span class="fe-type" style="color:${TYPE_COLORS[type]}">${label}</span>
    ${toolInfo}
    <span class="fe-content">${escapeHtml(truncated)}</span>
    ${hasMore ? '<span class="fe-expand">+</span>' : ''}
    ${opts.showSessionId ? `<span class="fe-sid">${opts.showSessionId}</span>` : ''}
  `;

  if (hasMore) {
    let expanded = false;
    const expandBtn = el.querySelector('.fe-expand')!;
    const contentEl = el.querySelector('.fe-content')!;
    expandBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      expanded = !expanded;
      contentEl.textContent = expanded ? content : truncated;
      expandBtn.textContent = expanded ? '−' : '+';
      el.classList.toggle('expanded', expanded);
    });
  }

  return el;
}

function formatTime(ts: string): string {
  if (!ts) return '—';
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '—';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function escapeHtml(s: string): string {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/render-message.ts
git commit -m "feat: add shared message renderer for feed and replay"
```

### Task 3: Add feed entry styles

**Files:**
- Modify: `web/src/styles/feed.css`

- [ ] **Step 1: Append feed entry styles to feed.css**

Add at the end of `web/src/styles/feed.css`:

```css
/* Feed entries */
.feed-entry {
  padding: 3px 8px;
  font-size: 12px;
  border-left: 3px solid transparent;
  display: flex;
  align-items: baseline;
  gap: 6px;
  line-height: 1.4;
}

.feed-entry:hover { background: var(--bg-hover); }

.feed-entry.expanded {
  flex-wrap: wrap;
}

.feed-entry.expanded .fe-content {
  white-space: pre-wrap;
  word-break: break-word;
  flex-basis: 100%;
  margin-top: 4px;
}

.fe-time {
  color: var(--text-dim);
  font-size: 10px;
  flex-shrink: 0;
  min-width: 65px;
}

.fe-type {
  font-size: 9px;
  font-weight: 700;
  flex-shrink: 0;
  min-width: 45px;
  text-transform: uppercase;
}

.fe-tool {
  color: var(--purple);
  font-size: 11px;
  flex-shrink: 0;
}

.fe-content {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-dim);
}

.fe-expand {
  color: var(--cyan);
  cursor: pointer;
  flex-shrink: 0;
  font-size: 11px;
  padding: 0 4px;
}

.fe-expand:hover { color: var(--text); }

.fe-sid {
  color: var(--text-dim);
  font-size: 9px;
  margin-left: auto;
  flex-shrink: 0;
  opacity: 0.5;
}

/* Feed type filter bar */
.feed-type-filters {
  display: flex;
  gap: 2px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.feed-filter-btn {
  font-size: 9px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 3px;
  border: 1px solid var(--border);
  background: none;
  color: var(--text-dim);
  cursor: pointer;
  font-family: var(--font-mono);
  text-transform: uppercase;
}

.feed-filter-btn:hover { color: var(--text); }
.feed-filter-btn.active { background: var(--border); color: var(--text); }
.feed-filter-btn.all-btn { color: var(--cyan); }

/* Replay panel */
.replay-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.replay-header {
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.replay-header .session-label {
  color: var(--cyan);
  font-weight: 600;
  font-size: 13px;
  flex: 1;
}

.replay-close-btn {
  background: none;
  border: none;
  color: var(--text-dim);
  cursor: pointer;
  font-size: 16px;
  font-family: var(--font-mono);
}

.replay-close-btn:hover { color: var(--text); }

.replay-feed {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
}

.replay-controls {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.replay-controls button {
  background: var(--bg-hover);
  border: 1px solid var(--border);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 4px 10px;
  cursor: pointer;
  border-radius: 3px;
}

.replay-controls button:hover { background: var(--border); }

.replay-controls select {
  background: var(--bg);
  border: 1px solid var(--border);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 3px 6px;
  border-radius: 3px;
}

.replay-scrubber {
  flex: 1;
  accent-color: var(--cyan);
}

.replay-progress {
  font-size: 11px;
  color: var(--text-dim);
  min-width: 60px;
  text-align: right;
}

.replay-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-dim);
  font-size: 13px;
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/styles/feed.css
git commit -m "feat: add feed entry and replay styles"
```

### Task 4: Create feed panel component

**Files:**
- Create: `web/src/components/feed-panel.ts`

- [ ] **Step 1: Create feed-panel.ts**

```typescript
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
const MAX_ENTRIES = 500;

const FILTER_TYPES = ['all', 'user', 'assistant', 'tool', 'result', 'agent', 'hook', 'error'] as const;

export function render(mount: HTMLElement): void {
  container = mount;
  renderEmpty();

  subscribe(onStateChange);
  onMessage(onWsMessage);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('selectedSessionId')) {
    if (state.selectedSessionId && state.view === 'list') {
      renderFeedPanel();
      loadRecentMessages(state.selectedSessionId);
    } else if (!state.selectedSessionId && state.view === 'list') {
      renderEmpty();
    }
  }
  if (changed.has('view')) {
    // Feed panel only shows in list view with a session selected
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
  feedContent.innerHTML = '';

  try {
    const messages = await fetchRecentMessages(sessionId);
    for (const msg of messages) {
      appendMessage(msg as ParsedMessage);
    }
  } catch (err) {
    feedContent.innerHTML = `<div class="feed-empty">Failed to load messages</div>`;
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
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/feed-panel.ts
git commit -m "feat: add live feed panel with type filters"
```

### Task 5: Create replay panel component

**Files:**
- Create: `web/src/components/replay.ts`

- [ ] **Step 1: Create replay.ts**

```typescript
// web/src/components/replay.ts
import type { ParsedMessage } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { renderFeedEntry } from './render-message';
import '../styles/feed.css';

let container: HTMLElement | null = null;
let feedEl: HTMLElement | null = null;
let playBtn: HTMLElement | null = null;
let scrubber: HTMLInputElement | null = null;
let progressEl: HTMLElement | null = null;
let es: EventSource | null = null;
let totalEvents = 0;
let currentIndex = 0;

export function render(mount: HTMLElement): void {
  container = mount;
}

export function open(sessionId: string): void {
  update({ replaySessionId: sessionId, replayPlaying: false });
  renderReplayPanel(sessionId);
  loadManifest(sessionId);
}

export function close(): void {
  stopStream();
  update({ replaySessionId: null, replayPlaying: false });
}

function renderReplayPanel(sessionId: string): void {
  if (!container) return;

  const session = state.sessions.get(sessionId);
  const name = session?.sessionName || session?.projectName || sessionId;

  container.innerHTML = '';
  const panel = document.createElement('div');
  panel.className = 'replay-panel';
  panel.innerHTML = `
    <div class="replay-header">
      <span class="session-label">${escapeHtml(name)}</span>
      <button class="replay-close-btn" title="Close replay">✕</button>
    </div>
    <div class="replay-feed">
      <div class="replay-empty">PRESS PLAY TO BEGIN</div>
    </div>
    <div class="replay-controls">
      <button class="replay-restart-btn" title="Restart">⏮</button>
      <button class="replay-play-btn">▶ PLAY</button>
      <select class="replay-speed">
        <option value="0.5">0.5x</option>
        <option value="1" selected>1x</option>
        <option value="2">2x</option>
        <option value="4">4x</option>
      </select>
      <input type="range" class="replay-scrubber" min="0" max="0" value="0" />
      <span class="replay-progress">0 / 0</span>
    </div>
  `;

  container.appendChild(panel);

  feedEl = panel.querySelector('.replay-feed')!;
  playBtn = panel.querySelector('.replay-play-btn')!;
  scrubber = panel.querySelector('.replay-scrubber')!;
  progressEl = panel.querySelector('.replay-progress')!;

  panel.querySelector('.replay-close-btn')!.addEventListener('click', close);
  panel.querySelector('.replay-restart-btn')!.addEventListener('click', restart);
  playBtn.addEventListener('click', togglePlay);
  scrubber.addEventListener('input', onScrub);
}

async function loadManifest(sessionId: string): Promise<void> {
  try {
    const res = await fetch(`/api/sessions/${sessionId}/replay`);
    const data = await res.json();
    totalEvents = data.events?.length ?? 0;
    if (scrubber) scrubber.max = String(totalEvents);
    updateProgress();
  } catch (err) {
    console.error('Failed to load replay manifest:', err);
  }
}

function togglePlay(): void {
  if (state.replayPlaying) {
    stopStream();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  } else {
    startStream();
    update({ replayPlaying: true });
    if (playBtn) playBtn.textContent = '⏸ PAUSE';
  }
}

function restart(): void {
  stopStream();
  currentIndex = 0;
  if (feedEl) feedEl.innerHTML = '<div class="replay-empty">PRESS PLAY TO BEGIN</div>';
  if (scrubber) scrubber.value = '0';
  updateProgress();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
}

function onScrub(): void {
  if (!scrubber) return;
  stopStream();
  currentIndex = parseInt(scrubber.value, 10);
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  updateProgress();
}

function startStream(): void {
  if (!state.replaySessionId) return;
  const speed = (container?.querySelector('.replay-speed') as HTMLSelectElement)?.value ?? '1';
  const url = `/api/sessions/${state.replaySessionId}/replay/stream?from=${currentIndex}&speed=${speed}`;

  // Clear placeholder
  if (feedEl && currentIndex === 0) {
    feedEl.innerHTML = '';
  }

  es = new EventSource(url);

  es.addEventListener('message', (e) => {
    const data = JSON.parse(e.data);
    if (data.message) {
      const entry = renderFeedEntry(data.message as ParsedMessage);
      feedEl?.appendChild(entry);
      feedEl!.scrollTop = feedEl!.scrollHeight;
    }
    currentIndex = (data.index ?? currentIndex) + 1;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  });

  es.addEventListener('done', () => {
    stopStream();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  });

  es.onerror = () => {
    stopStream();
    update({ replayPlaying: false });
    if (playBtn) playBtn.textContent = '▶ PLAY';
  };
}

function stopStream(): void {
  if (es) {
    es.close();
    es = null;
  }
}

function updateProgress(): void {
  if (progressEl) {
    progressEl.textContent = `${currentIndex} / ${totalEvents}`;
  }
}

function escapeHtml(s: string): string {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/replay.ts
git commit -m "feat: add replay panel with SSE stream and scrubber"
```

---

## Phase 2: Views (Graph, Table, History)

### Task 6: Create views.css

**Files:**
- Create: `web/src/styles/views.css`

- [ ] **Step 1: Create views.css**

```css
/* web/src/styles/views.css */

/* Graph view */
.graph-container {
  width: 100%;
  height: 100%;
  position: relative;
}

.graph-container canvas {
  display: block;
  width: 100%;
  height: 100%;
}

.graph-tooltip {
  position: absolute;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 6px 10px;
  font-size: 11px;
  color: var(--text);
  pointer-events: none;
  z-index: 50;
  white-space: nowrap;
  display: none;
}

.graph-tooltip.visible { display: block; }

/* Table and History shared */
.view-overlay {
  flex: 1;
  overflow: auto;
  padding: 0;
}

.view-overlay table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.view-overlay th {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  color: var(--text-dim);
  padding: 8px 10px;
  text-align: left;
  cursor: pointer;
  user-select: none;
  border-bottom: 1px solid var(--border);
  position: sticky;
  top: 0;
  background: var(--bg-card);
  white-space: nowrap;
}

.view-overlay th:hover { color: var(--text); }

.sort-arrow {
  margin-left: 4px;
  font-size: 9px;
}

.view-overlay td {
  padding: 5px 10px;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 200px;
}

.view-overlay tr:hover { background: rgba(255,255,255,0.03); }
.view-overlay tr.selected-row { background: var(--bg-hover); }

.view-overlay .col-cost { color: var(--yellow); }
.view-overlay .col-rate { color: var(--yellow); }
.view-overlay .col-tokens { color: var(--cyan); }
.view-overlay .col-cache { color: var(--purple); }
.view-overlay .col-err { color: var(--red); }
.view-overlay .col-model { color: var(--text-dim); font-size: 11px; }
.view-overlay .col-dim { color: var(--text-dim); }

/* Budget */
.budget-popover {
  position: absolute;
  top: 100%;
  left: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px;
  z-index: 200;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
  min-width: 200px;
}

.budget-popover input {
  background: var(--bg);
  border: 1px solid var(--border);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 13px;
  padding: 4px 8px;
  width: 100%;
  border-radius: 3px;
  margin-bottom: 8px;
}

.budget-popover .budget-actions {
  display: flex;
  gap: 6px;
}

.budget-popover button {
  flex: 1;
  background: var(--bg-hover);
  border: 1px solid var(--border);
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 4px;
  cursor: pointer;
  border-radius: 3px;
}

.budget-popover button:hover { background: var(--border); }

.budget-banner {
  background: rgba(248, 81, 73, 0.15);
  border-bottom: 1px solid var(--red);
  color: var(--red);
  padding: 6px 16px;
  font-size: 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.budget-banner.hidden { display: none; }

.over-budget { color: var(--red) !important; animation: budgetPulse 1s infinite; }

@keyframes budgetPulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}

/* Help overlay */
.help-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0,0,0,0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 500;
}

.help-content {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 24px;
  min-width: 320px;
  max-width: 400px;
}

.help-content h3 {
  color: var(--cyan);
  font-size: 13px;
  margin-bottom: 12px;
  margin-top: 16px;
}

.help-content h3:first-child { margin-top: 0; }

.help-row {
  display: flex;
  justify-content: space-between;
  padding: 3px 0;
  font-size: 12px;
}

.help-row kbd {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 3px;
  padding: 1px 6px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text);
}

.help-row span { color: var(--text-dim); }
```

- [ ] **Step 2: Commit**

```bash
git add web/src/styles/views.css
git commit -m "feat: add styles for graph, table, history, budget, and help"
```

### Task 7: Create graph view component

**Files:**
- Create: `web/src/components/graph-view.ts`

- [ ] **Step 1: Create graph-view.ts**

```typescript
// web/src/components/graph-view.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import '../styles/views.css';

interface GraphNode {
  id: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  radius: number;
  color: string;
  label: string;
  session: Session;
}

interface GraphEdge {
  source: string;
  target: string;
}

let container: HTMLElement | null = null;
let canvas: HTMLCanvasElement | null = null;
let tooltip: HTMLElement | null = null;
let ctx: CanvasRenderingContext2D | null = null;
let nodes: GraphNode[] = [];
let edges: GraphEdge[] = [];
let animFrame: number | null = null;
let dragging: GraphNode | null = null;
let hovering: GraphNode | null = null;
let prevNodeIds = '';

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view')) {
    if (state.view === 'graph') {
      show();
    } else {
      hide();
    }
  }
  if (changed.has('sessions') && state.view === 'graph') {
    rebuildNodes();
  }
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'graph-container';

  canvas = document.createElement('canvas');
  wrapper.appendChild(canvas);

  tooltip = document.createElement('div');
  tooltip.className = 'graph-tooltip';
  wrapper.appendChild(tooltip);

  container.appendChild(wrapper);

  resizeCanvas();
  window.addEventListener('resize', resizeCanvas);
  canvas.addEventListener('mousedown', onMouseDown);
  canvas.addEventListener('mousemove', onMouseMove);
  canvas.addEventListener('mouseup', onMouseUp);
  canvas.addEventListener('click', onClick);

  rebuildNodes();
  startAnimation();
}

function hide(): void {
  stopAnimation();
  window.removeEventListener('resize', resizeCanvas);
  canvas = null;
  ctx = null;
  tooltip = null;
}

function resizeCanvas(): void {
  if (!canvas || !container) return;
  canvas.width = container.clientWidth;
  canvas.height = container.clientHeight;
  ctx = canvas.getContext('2d');
}

function rebuildNodes(): void {
  if (!canvas) return;
  const now = Date.now();
  const threshold = 120_000; // 120 seconds

  const visibleSessions: Session[] = [];
  for (const sess of state.sessions.values()) {
    const lastActive = new Date(sess.lastActive).getTime();
    if (sess.isActive || (now - lastActive) < threshold) {
      visibleSessions.push(sess);
    }
  }

  // Include parents of visible nodes
  const visibleIds = new Set(visibleSessions.map(s => s.id));
  for (const sess of visibleSessions) {
    if (sess.parentId && !visibleIds.has(sess.parentId)) {
      const parent = state.sessions.get(sess.parentId);
      if (parent) {
        visibleSessions.push(parent);
        visibleIds.add(parent.id);
      }
    }
  }

  const nodeIds = [...visibleIds].sort().join(',');
  if (nodeIds === prevNodeIds) return;
  prevNodeIds = nodeIds;

  const oldNodes = new Map(nodes.map(n => [n.id, n]));
  const cx = (canvas?.width ?? 800) / 2;
  const cy = (canvas?.height ?? 600) / 2;

  nodes = visibleSessions.map(sess => {
    const old = oldNodes.get(sess.id);
    const radius = Math.min(30, Math.max(8, Math.log(sess.totalCostUSD + 1) * 5 + 8));
    const color = sess.isActive
      ? (sess.status === 'thinking' ? '#ffcc00' : sess.status === 'tool_use' ? '#4488ff' : '#00ff88')
      : '#44445a';
    const label = (sess.sessionName || sess.projectName || sess.id).substring(0, 16);

    return {
      id: sess.id,
      x: old?.x ?? cx + (Math.random() - 0.5) * 200,
      y: old?.y ?? cy + (Math.random() - 0.5) * 200,
      vx: old?.vx ?? 0,
      vy: old?.vy ?? 0,
      radius, color, label, session: sess,
    };
  });

  edges = [];
  for (const sess of visibleSessions) {
    if (sess.parentId && visibleIds.has(sess.parentId)) {
      edges.push({ source: sess.parentId, target: sess.id });
    }
  }
}

function simulate(): void {
  if (!canvas) return;
  const w = canvas.width;
  const h = canvas.height;

  // Repulsion
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i], b = nodes[j];
      const dx = b.x - a.x, dy = b.y - a.y;
      const dist = Math.max(1, Math.sqrt(dx * dx + dy * dy));
      const force = 2000 / (dist * dist);
      const fx = (dx / dist) * force, fy = (dy / dist) * force;
      a.vx -= fx; a.vy -= fy;
      b.vx += fx; b.vy += fy;
    }
  }

  // Attraction along edges
  const nodeMap = new Map(nodes.map(n => [n.id, n]));
  for (const edge of edges) {
    const a = nodeMap.get(edge.source), b = nodeMap.get(edge.target);
    if (!a || !b) continue;
    const dx = b.x - a.x, dy = b.y - a.y;
    const dist = Math.sqrt(dx * dx + dy * dy);
    const force = (dist - 100) * 0.01;
    const fx = (dx / dist) * force, fy = (dy / dist) * force;
    a.vx += fx; a.vy += fy;
    b.vx -= fx; b.vy -= fy;
  }

  // Center gravity + damping + bounds
  const cx = w / 2, cy = h / 2;
  for (const n of nodes) {
    n.vx += (cx - n.x) * 0.001;
    n.vy += (cy - n.y) * 0.001;
    n.vx *= 0.9;
    n.vy *= 0.9;
    if (n !== dragging) {
      n.x += n.vx;
      n.y += n.vy;
    }
    n.x = Math.max(n.radius, Math.min(w - n.radius, n.x));
    n.y = Math.max(n.radius, Math.min(h - n.radius, n.y));
  }
}

function draw(): void {
  if (!ctx || !canvas) return;
  ctx.clearRect(0, 0, canvas.width, canvas.height);

  // Edges
  const nodeMap = new Map(nodes.map(n => [n.id, n]));
  ctx.strokeStyle = 'rgba(100,100,140,0.3)';
  ctx.lineWidth = 1;
  for (const edge of edges) {
    const a = nodeMap.get(edge.source), b = nodeMap.get(edge.target);
    if (!a || !b) continue;
    ctx.beginPath();
    ctx.moveTo(a.x, a.y);
    ctx.lineTo(b.x, b.y);
    ctx.stroke();
  }

  // Nodes
  for (const n of nodes) {
    ctx.globalAlpha = n === hovering ? 1.0 : 0.7;
    ctx.fillStyle = n.color;
    ctx.beginPath();
    ctx.arc(n.x, n.y, n.radius + (n === hovering ? 2 : 0), 0, Math.PI * 2);
    ctx.fill();
    if (n === hovering) {
      ctx.strokeStyle = '#fff';
      ctx.lineWidth = 2;
      ctx.stroke();
    }
    ctx.globalAlpha = 1.0;

    // Label
    ctx.fillStyle = '#aaa';
    ctx.font = '10px monospace';
    ctx.textAlign = 'center';
    ctx.fillText(n.label, n.x, n.y + n.radius + 14);

    // Cost
    if (n.session.totalCostUSD > 0.01) {
      ctx.fillStyle = '#888';
      ctx.font = '8px monospace';
      ctx.fillText(`$${n.session.totalCostUSD.toFixed(2)}`, n.x, n.y + n.radius + 24);
    }
  }
}

function getNodeAt(x: number, y: number): GraphNode | null {
  for (const n of nodes) {
    const dx = x - n.x, dy = y - n.y;
    if (dx * dx + dy * dy < (n.radius + 4) * (n.radius + 4)) return n;
  }
  return null;
}

function onMouseDown(e: MouseEvent): void {
  const rect = canvas!.getBoundingClientRect();
  dragging = getNodeAt(e.clientX - rect.left, e.clientY - rect.top);
}

function onMouseMove(e: MouseEvent): void {
  if (!canvas || !tooltip) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left, my = e.clientY - rect.top;

  if (dragging) {
    dragging.x = mx;
    dragging.y = my;
    dragging.vx = 0;
    dragging.vy = 0;
    return;
  }

  const node = getNodeAt(mx, my);
  hovering = node;
  canvas.style.cursor = node ? 'pointer' : 'default';

  if (node) {
    tooltip.innerHTML = `<div><b>${escapeHtml(node.label)}</b></div>
      <div>$${node.session.totalCostUSD.toFixed(2)} · ${node.session.messageCount} msgs</div>
      <div>${node.session.status} · ${node.session.model || '?'}</div>`;
    tooltip.style.left = `${mx + 15}px`;
    tooltip.style.top = `${my + 15}px`;
    tooltip.classList.add('visible');
  } else {
    tooltip.classList.remove('visible');
  }
}

function onMouseUp(): void {
  dragging = null;
}

function onClick(e: MouseEvent): void {
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const node = getNodeAt(e.clientX - rect.left, e.clientY - rect.top);
  if (node) {
    update({ selectedSessionId: node.id, view: 'list' });
  }
}

function startAnimation(): void {
  function loop() {
    simulate();
    draw();
    animFrame = requestAnimationFrame(loop);
  }
  animFrame = requestAnimationFrame(loop);
}

function stopAnimation(): void {
  if (animFrame !== null) {
    cancelAnimationFrame(animFrame);
    animFrame = null;
  }
}

function escapeHtml(s: string): string {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/graph-view.ts
git commit -m "feat: add Canvas 2D force-directed graph view"
```

### Task 8: Create table view component

**Files:**
- Create: `web/src/components/table-view.ts`

- [ ] **Step 1: Create table-view.ts**

```typescript
// web/src/components/table-view.ts
import type { Session } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import '../styles/views.css';

let container: HTMLElement | null = null;
let sortCol = 'totalCostUSD';
let sortAsc = false;

type Column = { key: string; label: string; cls?: string; fmt: (s: Session) => string };

const COLUMNS: Column[] = [
  { key: 'projectName', label: 'Name', fmt: s => (s.sessionName || s.projectName || s.id).substring(0, 25) },
  { key: 'status', label: 'Status', cls: 'col-dim', fmt: s => s.status },
  { key: 'totalCostUSD', label: 'Cost', cls: 'col-cost', fmt: s => `$${s.totalCostUSD.toFixed(2)}` },
  { key: 'costRate', label: '$/min', cls: 'col-rate', fmt: s => s.costRate > 0 ? `$${s.costRate.toFixed(3)}` : '' },
  { key: 'duration', label: 'Duration', cls: 'col-dim', fmt: s => fmtDuration(s) },
  { key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: s => fmtNum(s.inputTokens + s.outputTokens + s.cacheReadTokens) },
  { key: 'cacheHitPct', label: 'Cache%', cls: 'col-cache', fmt: s => `${s.cacheHitPct.toFixed(0)}%` },
  { key: 'messageCount', label: 'Msgs', fmt: s => String(s.messageCount) },
  { key: 'errorCount', label: 'Errors', cls: 'col-err', fmt: s => s.errorCount > 0 ? String(s.errorCount) : '' },
  { key: 'model', label: 'Model', cls: 'col-model', fmt: s => (s.model || '').replace('claude-', '') },
];

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view') && state.view === 'table') show();
  if (changed.has('sessions') && state.view === 'table') show();
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'view-overlay';

  const table = document.createElement('table');
  const thead = document.createElement('thead');
  const headerRow = document.createElement('tr');

  for (const col of COLUMNS) {
    const th = document.createElement('th');
    th.innerHTML = `${col.label}${sortCol === col.key ? `<span class="sort-arrow">${sortAsc ? '▲' : '▼'}</span>` : ''}`;
    th.addEventListener('click', () => {
      if (sortCol === col.key) { sortAsc = !sortAsc; } else { sortCol = col.key; sortAsc = false; }
      show();
    });
    headerRow.appendChild(th);
  }
  thead.appendChild(headerRow);
  table.appendChild(thead);

  const tbody = document.createElement('tbody');
  const sessions = sortSessions(Array.from(state.sessions.values()));

  for (const sess of sessions) {
    const tr = document.createElement('tr');
    if (sess.id === state.selectedSessionId) tr.className = 'selected-row';
    for (const col of COLUMNS) {
      const td = document.createElement('td');
      td.textContent = col.fmt(sess);
      if (col.cls) td.className = col.cls;
      tr.appendChild(td);
    }
    tr.addEventListener('click', () => update({ selectedSessionId: sess.id, view: 'list' }));
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrapper.appendChild(table);
  container.appendChild(wrapper);
}

function sortSessions(sessions: Session[]): Session[] {
  return sessions.sort((a, b) => {
    let va: number | string, vb: number | string;
    switch (sortCol) {
      case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens; vb = b.inputTokens + b.outputTokens + b.cacheReadTokens; break;
      case 'duration': va = durationSecs(a); vb = durationSecs(b); break;
      case 'projectName': va = (a.sessionName || a.projectName || '').toLowerCase(); vb = (b.sessionName || b.projectName || '').toLowerCase(); break;
      case 'status': va = a.status; vb = b.status; break;
      case 'model': va = a.model || ''; vb = b.model || ''; break;
      default: va = (a as any)[sortCol] ?? 0; vb = (b as any)[sortCol] ?? 0;
    }
    const cmp = typeof va === 'string' ? va.localeCompare(vb as string) : (va as number) - (vb as number);
    return sortAsc ? cmp : -cmp;
  });
}

function durationSecs(s: Session): number {
  if (!s.startedAt) return 0;
  const start = new Date(s.startedAt).getTime();
  const end = s.lastActive ? new Date(s.lastActive).getTime() : Date.now();
  return (end - start) / 1000;
}

function fmtDuration(s: Session): string {
  const secs = durationSecs(s);
  if (secs < 60) return `${Math.floor(secs)}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60);
  return `${h}h${m}m`;
}

function fmtNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/table-view.ts
git commit -m "feat: add sortable table view"
```

### Task 9: Create history view component

**Files:**
- Create: `web/src/components/history-view.ts`

- [ ] **Step 1: Create history-view.ts**

```typescript
// web/src/components/history-view.ts
import type { HistoryRow } from '../types';
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { fetchHistory } from '../api';
import '../styles/views.css';

let container: HTMLElement | null = null;
let data: HistoryRow[] = [];
let sortCol = 'endedAt';
let sortAsc = false;
let loaded = false;

type Column = { key: string; label: string; cls?: string; fmt: (r: HistoryRow) => string };

const COLUMNS: Column[] = [
  { key: 'endedAt', label: 'Date', cls: 'col-dim', fmt: r => r.endedAt ? new Date(r.endedAt).toLocaleString() : '' },
  { key: 'projectName', label: 'Name', fmt: r => r.sessionName || r.projectName || r.id },
  { key: 'totalCost', label: 'Cost', cls: 'col-cost', fmt: r => `$${r.totalCost.toFixed(2)}` },
  { key: 'durationSeconds', label: 'Duration', cls: 'col-dim', fmt: r => fmtDuration(r.durationSeconds) },
  { key: 'tokens', label: 'Tokens', cls: 'col-tokens', fmt: r => fmtNum(r.inputTokens + r.outputTokens + r.cacheReadTokens) },
  { key: 'messageCount', label: 'Msgs', fmt: r => String(r.messageCount) },
  { key: 'errorCount', label: 'Errors', cls: 'col-err', fmt: r => r.errorCount > 0 ? String(r.errorCount) : '' },
  { key: 'model', label: 'Model', cls: 'col-model', fmt: r => (r.model || '').replace('claude-', '') },
];

export function render(mount: HTMLElement): void {
  container = mount;
  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('view') && state.view === 'history') {
    if (!loaded) {
      loadData();
    } else {
      show();
    }
  }
}

async function loadData(): Promise<void> {
  try {
    data = await fetchHistory(200, 0);
    loaded = true;
    show();
  } catch (err) {
    console.error('Failed to load history:', err);
  }
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'view-overlay';

  const table = document.createElement('table');
  const thead = document.createElement('thead');
  const headerRow = document.createElement('tr');

  for (const col of COLUMNS) {
    const th = document.createElement('th');
    th.innerHTML = `${col.label}${sortCol === col.key ? `<span class="sort-arrow">${sortAsc ? '▲' : '▼'}</span>` : ''}`;
    th.addEventListener('click', () => {
      if (sortCol === col.key) { sortAsc = !sortAsc; } else { sortCol = col.key; sortAsc = false; }
      show();
    });
    headerRow.appendChild(th);
  }
  thead.appendChild(headerRow);
  table.appendChild(thead);

  const tbody = document.createElement('tbody');
  const sorted = sortData([...data]);

  for (const row of sorted) {
    const tr = document.createElement('tr');
    for (const col of COLUMNS) {
      const td = document.createElement('td');
      td.textContent = col.fmt(row);
      if (col.cls) td.className = col.cls;
      if (col.key === 'projectName') td.title = row.taskDescription || '';
      tr.appendChild(td);
    }
    tr.addEventListener('click', () => {
      // Open replay for this session
      update({ replaySessionId: row.id, view: 'list' });
    });
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrapper.appendChild(table);
  container.appendChild(wrapper);
}

function sortData(rows: HistoryRow[]): HistoryRow[] {
  return rows.sort((a, b) => {
    let va: number | string, vb: number | string;
    switch (sortCol) {
      case 'tokens': va = a.inputTokens + a.outputTokens + a.cacheReadTokens; vb = b.inputTokens + b.outputTokens + b.cacheReadTokens; break;
      case 'projectName': va = (a.sessionName || a.projectName || '').toLowerCase(); vb = (b.sessionName || b.projectName || '').toLowerCase(); break;
      case 'model': va = a.model || ''; vb = b.model || ''; break;
      case 'endedAt': va = a.endedAt || ''; vb = b.endedAt || ''; break;
      default: va = (a as any)[sortCol] ?? 0; vb = (b as any)[sortCol] ?? 0;
    }
    const cmp = typeof va === 'string' ? va.localeCompare(vb as string) : (va as number) - (vb as number);
    return sortAsc ? cmp : -cmp;
  });
}

function fmtDuration(secs: number): string {
  if (secs < 60) return `${Math.floor(secs)}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60);
  return `${h}h${m}m`;
}

function fmtNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/history-view.ts
git commit -m "feat: add history view with sortable columns"
```

---

## Phase 3: Utilities + Wiring

### Task 10: Create budget popover component

**Files:**
- Create: `web/src/components/budget-popover.ts`

- [ ] **Step 1: Create budget-popover.ts**

```typescript
// web/src/components/budget-popover.ts
import { state, subscribe, update } from '../state';
import type { AppState } from '../state';
import '../styles/views.css';

let popover: HTMLElement | null = null;
let banner: HTMLElement | null = null;
let costStatEl: HTMLElement | null = null;

export function render(gearBtn: HTMLElement, costEl: HTMLElement, bannerMount: HTMLElement): void {
  costStatEl = costEl;

  // Create banner
  banner = document.createElement('div');
  banner.className = 'budget-banner hidden';
  bannerMount.prepend(banner);

  // Load saved threshold
  const saved = localStorage.getItem('budget');
  if (saved) {
    update({ budgetThreshold: parseFloat(saved) });
  }

  gearBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    togglePopover(gearBtn);
  });

  document.addEventListener('click', () => {
    if (popover) { popover.remove(); popover = null; }
  });

  subscribe(onStateChange);
}

function onStateChange(_state: AppState, changed: Set<string>): void {
  if (changed.has('sessions') || changed.has('budgetThreshold') || changed.has('budgetDismissed')) {
    checkBudget();
  }
}

function togglePopover(anchor: HTMLElement): void {
  if (popover) {
    popover.remove();
    popover = null;
    return;
  }

  popover = document.createElement('div');
  popover.className = 'budget-popover';
  popover.addEventListener('click', e => e.stopPropagation());
  popover.innerHTML = `
    <input type="number" step="1" placeholder="Budget threshold (USD)" value="${state.budgetThreshold ?? ''}" />
    <div class="budget-actions">
      <button class="set-btn">Set</button>
      <button class="clear-btn">Clear</button>
    </div>
  `;

  const input = popover.querySelector('input')!;
  popover.querySelector('.set-btn')!.addEventListener('click', () => {
    const val = parseFloat(input.value);
    if (!isNaN(val) && val > 0) {
      localStorage.setItem('budget', String(val));
      update({ budgetThreshold: val, budgetDismissed: false });
    }
  });

  popover.querySelector('.clear-btn')!.addEventListener('click', () => {
    localStorage.removeItem('budget');
    update({ budgetThreshold: null, budgetDismissed: false });
    if (popover) { popover.remove(); popover = null; }
  });

  anchor.parentElement!.style.position = 'relative';
  anchor.parentElement!.appendChild(popover);
}

function checkBudget(): void {
  if (!state.budgetThreshold || !costStatEl || !banner) return;

  const sessions = Array.from(state.sessions.values());
  const total = sessions.reduce((sum, s) => sum + s.totalCostUSD, 0);

  if (total >= state.budgetThreshold) {
    costStatEl.classList.add('over-budget');
    if (!state.budgetDismissed) {
      banner.className = 'budget-banner';
      banner.innerHTML = `Budget exceeded: $${total.toFixed(0)} / $${state.budgetThreshold}
        <button style="background:none;border:none;color:var(--red);cursor:pointer;font-family:var(--font-mono)">✕</button>`;
      banner.querySelector('button')!.addEventListener('click', () => {
        update({ budgetDismissed: true });
      });
    } else {
      banner.className = 'budget-banner hidden';
    }
  } else {
    costStatEl.classList.remove('over-budget');
    banner.className = 'budget-banner hidden';
  }
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/budget-popover.ts
git commit -m "feat: add budget popover with localStorage persistence"
```

### Task 11: Create help overlay component

**Files:**
- Create: `web/src/components/help-overlay.ts`

- [ ] **Step 1: Create help-overlay.ts**

```typescript
// web/src/components/help-overlay.ts
import '../styles/views.css';

let overlay: HTMLElement | null = null;

export function toggle(): void {
  if (overlay) {
    overlay.remove();
    overlay = null;
    return;
  }

  overlay = document.createElement('div');
  overlay.className = 'help-overlay';
  overlay.innerHTML = `
    <div class="help-content">
      <h3>Keyboard Shortcuts</h3>
      <div class="help-row"><span>Focus search</span><kbd>/</kbd></div>
      <div class="help-row"><span>Clear / deselect</span><kbd>Esc</kbd></div>
      <div class="help-row"><span>Navigate sessions</span><kbd>↑↓</kbd></div>
      <div class="help-row"><span>Select session</span><kbd>Enter</kbd></div>
      <div class="help-row"><span>Active filter</span><kbd>1</kbd></div>
      <div class="help-row"><span>Recent filter</span><kbd>2</kbd></div>
      <div class="help-row"><span>All filter</span><kbd>3</kbd></div>
      <div class="help-row"><span>Graph view</span><kbd>g</kbd></div>
      <div class="help-row"><span>History view</span><kbd>h</kbd></div>
      <div class="help-row"><span>Table view</span><kbd>t</kbd></div>
      <div class="help-row"><span>Help</span><kbd>?</kbd></div>
      <h3>Replay Controls</h3>
      <div class="help-row"><span>Play / pause</span><kbd>Space</kbd></div>
      <div class="help-row"><span>Restart</span><kbd>R</kbd></div>
      <div class="help-row"><span>Step backward / forward</span><kbd>←→</kbd></div>
    </div>
  `;

  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) { overlay!.remove(); overlay = null; }
  });

  document.body.appendChild(overlay);
}
```

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add web/src/components/help-overlay.ts
git commit -m "feat: add help overlay with keyboard shortcuts"
```

### Task 12: Wire all new components into main.ts

**Files:**
- Modify: `web/src/main.ts`
- Modify: `web/src/components/topbar.ts`

- [ ] **Step 1: Update main.ts to import and mount all components**

Replace the entire content of `web/src/main.ts` with:

```typescript
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
}

init();
```

- [ ] **Step 2: Add budget gear button to topbar**

In `web/src/components/topbar.ts`, find the TOTAL SPEND stat line in the `el.innerHTML` template and add a gear button:

Change:
```html
    <div class="topbar-stat">TOTAL SPEND <span class="val yellow" data-stat="cost">$0</span></div>
```

To:
```html
    <div class="topbar-stat">TOTAL SPEND <span class="budget-gear" style="cursor:pointer">⚙</span> <span class="val yellow" data-stat="cost">$0</span></div>
```

- [ ] **Step 3: Verify TypeScript compiles and Vite builds**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: No errors

- [ ] **Step 4: Verify Go embed works**

Run: `cd /root/claude-monitor && go build -o /tmp/cm-test ./cmd/claude-monitor`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add web/src/main.ts web/src/components/topbar.ts cmd/claude-monitor/static/
git commit -m "feat: wire feed, replay, graph, table, history, budget, help into main"
```

### Task 13: End-to-end verification

- [ ] **Step 1: Start the app**

```bash
cd /root/claude-monitor && go build -o /tmp/cm-e2e ./cmd/claude-monitor && /tmp/cm-e2e -port 17730 &
sleep 2
```

- [ ] **Step 2: Verify all APIs still work**

```bash
curl -s http://localhost:17730/health | jq .
curl -s http://localhost:17730/api/version | jq .
curl -s http://localhost:17730/api/sessions/grouped | jq 'keys'
```

- [ ] **Step 3: Verify frontend renders with all views**

Use Playwright or manual check:
- Dashboard loads with session cards
- Click a session — feed panel appears with messages
- Press `g` — graph view with nodes
- Press `t` — table view with sortable columns
- Press `h` — history view
- Press `?` — help overlay
- Type in search — dropdown with results
- Click ⚙ near cost — budget popover

- [ ] **Step 4: Run all Go tests**

```bash
go test ./... -count=1 -v
```
Expected: All PASS

- [ ] **Step 5: Clean up**

```bash
kill %1
```
