# Feature Restoration + Timeline View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore 11 features lost in the HTML→TypeScript migration and build a new Canvas 2D timeline/waterfall view.

**Architecture:** Small additions to existing TypeScript components plus two new modules (`hash.ts` for URL persistence, `notifications.ts` for browser notifications, `timeline-view.ts` for the waterfall). All features are independent — each task produces a working commit.

**Tech Stack:** TypeScript, Canvas 2D, Browser Notification API, URL hash API

---

## Task 1: Keyboard navigation (↑↓ Enter ←→)

**Files:**
- Modify: `web/src/state.ts` — add `focusedSessionId`
- Modify: `web/src/main.ts` — add keyboard handlers
- Modify: `web/src/components/session-list.ts` — apply `.focused` class, scrollIntoView
- Modify: `web/src/components/session-card.ts` — add `data-session-id` to compact cards (already on expanded)
- Modify: `web/src/styles/sessions.css` — add `.focused` style

- [ ] **Step 1: Add focusedSessionId to state**

In `web/src/state.ts`, add to `AppState` interface (after `renderVersion`):

```typescript
  focusedSessionId: string | null;
```

Add default to `state` object:

```typescript
  focusedSessionId: null,
```

- [ ] **Step 2: Add .focused CSS**

Append to `web/src/styles/sessions.css`:

```css
.session-card.focused { outline: 1px dashed var(--text-dim); outline-offset: -1px; }
.session-card-compact.focused { outline: 1px dashed var(--text-dim); outline-offset: -1px; }
```

- [ ] **Step 3: Apply focused class in session-list.ts**

In `web/src/components/session-list.ts`, import `expandedParents` from session-card:

```typescript
import { renderExpanded, renderCompact, expandedParents } from './session-card';
```

Add to `onStateChange` — listen for `focusedSessionId` changes:

```typescript
  if (changed.has('focusedSessionId')) {
    // Update focused class on all cards
    listEl?.querySelectorAll<HTMLElement>('.session-card, .session-card-compact').forEach(card => {
      card.classList.toggle('focused', card.dataset.sessionId === state.focusedSessionId);
    });
    // Scroll focused card into view
    const focused = listEl?.querySelector<HTMLElement>('.focused');
    if (focused) focused.scrollIntoView({ block: 'nearest' });
  }
```

- [ ] **Step 4: Add keyboard handlers in main.ts**

In `web/src/main.ts`, import `expandedParents` from session-card:

```typescript
import { expandedParents } from './components/session-card';
```

Add a helper function to get the list of visible session IDs:

```typescript
function getVisibleSessionIds(): string[] {
  const cards = document.querySelectorAll<HTMLElement>('[data-session-id]');
  return Array.from(cards).map(c => c.dataset.sessionId!).filter(Boolean);
}
```

Add these cases to the existing keyboard switch in `main.ts` (inside the keydown handler):

```typescript
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
        expandedParents.add(state.focusedSessionId);
        update({ renderVersion: state.renderVersion + 1 });
      }
      break;
    }
    case 'ArrowLeft': {
      if (state.focusedSessionId) {
        expandedParents.delete(state.focusedSessionId);
        update({ renderVersion: state.renderVersion + 1 });
      }
      break;
    }
```

- [ ] **Step 5: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: add keyboard navigation — ↑↓ focus, Enter select, ←→ expand/collapse"`

---

## Task 2: URL hash persistence

**Files:**
- Create: `web/src/hash.ts`
- Modify: `web/src/main.ts` — import and init

- [ ] **Step 1: Create hash.ts**

```typescript
// web/src/hash.ts
import { state, subscribe, update } from './state';

let debounceTimer: ReturnType<typeof setTimeout> | null = null;

export function init(): void {
  // Restore state from hash on load
  restoreFromHash();

  // Listen for browser back/forward
  window.addEventListener('hashchange', restoreFromHash);

  // Write hash on state changes
  subscribe((_state, changed) => {
    if (changed.has('selectedSessionId') || changed.has('view')) {
      if (debounceTimer) clearTimeout(debounceTimer);
      debounceTimer = setTimeout(writeHash, 200);
    }
  });
}

function writeHash(): void {
  const parts: string[] = [];
  if (state.selectedSessionId) parts.push(`session=${state.selectedSessionId}`);
  if (state.view !== 'list') parts.push(`view=${state.view}`);
  const hash = parts.length > 0 ? '#' + parts.join('&') : '';
  if (location.hash !== hash) {
    history.replaceState(null, '', hash || location.pathname);
  }
}

function restoreFromHash(): void {
  const hash = location.hash.slice(1);
  if (!hash) return;
  const params = new URLSearchParams(hash);
  const changes: Record<string, unknown> = {};
  const session = params.get('session');
  if (session) changes.selectedSessionId = session;
  const view = params.get('view');
  if (view && ['list', 'graph', 'history', 'table'].includes(view)) {
    changes.view = view;
  }
  if (Object.keys(changes).length > 0) {
    update(changes as Partial<typeof state>);
  }
}
```

- [ ] **Step 2: Import in main.ts**

In `web/src/main.ts`, add import:

```typescript
import { init as initHash } from './hash';
```

Call it at the end of the `init()` function, after `connect()`:

```typescript
  initHash();
```

- [ ] **Step 3: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: persist session/view in URL hash, restore on load"`

---

## Task 3: Scroll lock button + back-to-feed link

**Files:**
- Modify: `web/src/components/feed-panel.ts`
- Modify: `web/src/styles/feed.css`

- [ ] **Step 1: Add scroll lock button CSS**

Append to `web/src/styles/feed.css`:

```css
/* Scroll lock button */
.scroll-lock-btn {
  position: fixed;
  bottom: 38px;
  right: 14px;
  background: rgba(68,136,255,0.15);
  border: 1px solid var(--cyan);
  color: var(--cyan);
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 4px 10px;
  cursor: pointer;
  letter-spacing: 1px;
  border-radius: 2px;
  z-index: 50;
  display: none;
}

.scroll-lock-btn:hover {
  background: rgba(68,136,255,0.3);
}

.scroll-lock-btn.visible { display: block; }
```

- [ ] **Step 2: Add scroll lock button to feed-panel.ts**

In `web/src/components/feed-panel.ts`, add a module-level variable:

```typescript
let scrollLockBtn: HTMLElement | null = null;
```

In `renderFeedPanel()`, after creating `feedContent`, add:

```typescript
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
```

Update the scroll event listener in `feedContent` to show/hide the button:

Replace the existing scroll listener:

```typescript
  feedContent.addEventListener('scroll', () => {
    if (!feedContent) return;
    const atBottom = feedContent.scrollHeight - feedContent.scrollTop - feedContent.clientHeight < 30;
    autoScroll = atBottom;
    scrollLockBtn?.classList.toggle('visible', !atBottom);
  });
```

- [ ] **Step 3: Add back-to-feed link in header**

In `updateHeader()`, when a session is selected, add a "← all" link:

```typescript
function updateHeader(): void {
  if (!headerEl) return;
  if (state.selectedSessionId) {
    const sess = state.sessions.get(state.selectedSessionId);
    const name = sess ? (sess.sessionName || sess.projectName || sess.id) : state.selectedSessionId;
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">${escapeHtml(name)}</span>
      <span class="back-to-feed" style="margin-left:auto;color:var(--cyan);font-size:10px;cursor:pointer;letter-spacing:0.5px">← all</span>`;
    headerEl.querySelector('.back-to-feed')?.addEventListener('click', () => {
      update({ selectedSessionId: null });
    });
  } else {
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">all sessions</span>`;
  }
}
```

- [ ] **Step 4: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: add scroll lock button and back-to-feed link"`

---

## Task 4: Current tool display + error count click

**Files:**
- Modify: `web/src/components/feed-panel.ts` — track last tool per session
- Modify: `web/src/components/session-card.ts` — show current tool, error click handler
- Modify: `web/src/styles/sessions.css` — `.session-current-tool`

- [ ] **Step 1: Track last tool in feed-panel.ts**

In `web/src/components/feed-panel.ts`, add a module-level map and export a getter:

```typescript
const lastToolBySession = new Map<string, string>();

export function getLastTool(sessionId: string): string | undefined {
  return lastToolBySession.get(sessionId);
}
```

In `onWsMessage()`, before `appendMessage`, track tool info:

```typescript
  if (event.message.toolName && event.message.role === 'assistant') {
    const toolInfo = event.message.toolName + (event.message.toolDetail ? ': ' + event.message.toolDetail.slice(0, 60) : '');
    lastToolBySession.set(event.session.id, toolInfo);
  }
```

- [ ] **Step 2: Add current tool display to session-card.ts**

In `web/src/components/session-card.ts`, import `getLastTool`:

```typescript
import { getLastTool } from './feed-panel';
```

In `renderExpanded()`, after the session-meta div in the innerHTML template, add:

```typescript
      ${session.status === 'tool_use' ? `<div class="session-current-tool">${escapeHtml(getLastTool(session.id) || '')}</div>` : ''}
```

- [ ] **Step 3: Add current tool CSS**

Append to `web/src/styles/sessions.css`:

```css
.session-current-tool {
  font-size: 10px;
  color: var(--text-dim);
  opacity: 0.7;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 220px;
  margin-top: 2px;
}
```

- [ ] **Step 4: Add error count click handler**

In `web/src/components/session-card.ts`, in `renderExpanded()`, after the `container.appendChild(el)` line, add:

```typescript
  // Error count click — filter feed to errors only
  const errEl = el.querySelector('.session-error-count');
  if (errEl) {
    errEl.addEventListener('click', (e) => {
      e.stopPropagation();
      update({
        selectedSessionId: session.id,
        feedTypeFilters: { user: false, assistant: false, tool_use: false, tool_result: false, agent: false, hook: false, error: true },
      });
    });
  }
```

- [ ] **Step 5: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: show current tool on cards, click error count to filter"`

---

## Task 5: Feed tool grouping

**Files:**
- Modify: `web/src/components/feed-panel.ts`
- Modify: `web/src/styles/feed.css`

- [ ] **Step 1: Add tool grouping CSS**

Append to `web/src/styles/feed.css`:

```css
/* Tool call + result grouping */
.feed-entry.tool-group-start {
  margin-bottom: 0;
  border-bottom: none;
  border-radius: 3px 3px 0 0;
}

.feed-entry.tool-group-end {
  margin-top: 0;
  padding-left: 28px;
  border-radius: 0 0 3px 3px;
  font-size: 11px;
  opacity: 0.75;
  border-left: 2px solid rgba(0,204,255,0.2);
}
```

- [ ] **Step 2: Add grouping logic in feed-panel.ts**

In `web/src/components/feed-panel.ts`, add a module-level variable:

```typescript
let lastToolEntry: HTMLElement | null = null;
```

In `appendMessage()`, after creating the entry element and before appending to feedContent, add:

```typescript
  // Tool grouping: mark tool call as group-start, result as group-end
  const msgType = detectType(msg);
  if (msgType === 'tool_use') {
    entry.classList.add('tool-group-start');
    lastToolEntry = entry;
  } else if (msgType === 'tool_result' && lastToolEntry) {
    entry.classList.add('tool-group-end');
    lastToolEntry = null;
  } else {
    lastToolEntry = null;
  }
```

- [ ] **Step 3: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: visual grouping of tool calls with their results"`

---

## Task 6: Browser notifications

**Files:**
- Create: `web/src/notifications.ts`
- Modify: `web/src/components/budget-popover.ts` — add notification checkboxes, trigger on budget breach
- Modify: `web/src/components/feed-panel.ts` — trigger on errors

- [ ] **Step 1: Create notifications.ts**

```typescript
// web/src/notifications.ts

interface NotifSettings {
  budget: boolean;
  error: boolean;
}

let settings: NotifSettings = { budget: true, error: true };

export function loadSettings(): void {
  try {
    const saved = localStorage.getItem('notif-settings');
    if (saved) settings = JSON.parse(saved);
  } catch { /* ignore */ }
}

export function saveSettings(s: NotifSettings): void {
  settings = s;
  localStorage.setItem('notif-settings', JSON.stringify(s));
}

export function getSettings(): NotifSettings {
  return { ...settings };
}

export function notify(type: 'budget' | 'error', title: string, body: string): void {
  if (type === 'budget' && !settings.budget) return;
  if (type === 'error' && !settings.error) return;
  if (Notification.permission === 'default') {
    Notification.requestPermission();
    return;
  }
  if (Notification.permission !== 'granted') return;
  new Notification(title, { body });
}
```

- [ ] **Step 2: Add notification checkboxes to budget-popover.ts**

In `web/src/components/budget-popover.ts`, import notifications:

```typescript
import { loadSettings, saveSettings, getSettings } from '../notifications';
```

In the `render()` function, call `loadSettings()`.

In `togglePopover()`, after the budget actions div in the innerHTML, add:

```html
    <div style="margin-top:8px;font-size:10px;color:var(--text-dim)">
      <label style="display:block;margin:3px 0;cursor:pointer">
        <input type="checkbox" class="notif-budget" ${getSettings().budget ? 'checked' : ''} /> Budget exceeded
      </label>
      <label style="display:block;margin:3px 0;cursor:pointer">
        <input type="checkbox" class="notif-error" ${getSettings().error ? 'checked' : ''} /> Agent errored
      </label>
    </div>
```

After attaching the popover to DOM, add change listeners:

```typescript
  popover.querySelector('.notif-budget')?.addEventListener('change', (e) => {
    const s = getSettings();
    s.budget = (e.target as HTMLInputElement).checked;
    saveSettings(s);
  });
  popover.querySelector('.notif-error')?.addEventListener('change', (e) => {
    const s = getSettings();
    s.error = (e.target as HTMLInputElement).checked;
    saveSettings(s);
  });
```

In `checkBudget()`, when budget is exceeded and not dismissed, call:

```typescript
import { notify } from '../notifications';

// Inside checkBudget, after showing the banner:
notify('budget', 'Budget Exceeded', `Total spend $${total.toFixed(0)} exceeds $${state.budgetThreshold}`);
```

- [ ] **Step 3: Add error notifications in feed-panel.ts**

In `web/src/components/feed-panel.ts`, import:

```typescript
import { notify } from '../notifications';
```

In `onWsMessage()`, after the tool tracking, add:

```typescript
  if (event.message.isError) {
    const name = event.session.sessionName || event.session.projectName || 'Agent';
    notify('error', 'Agent Error', `${name}: ${(event.message.contentText || '').slice(0, 100)}`);
  }
```

- [ ] **Step 4: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: browser notifications for budget exceeded and agent errors"`

---

## Task 7: Replay keyboard controls

**Files:**
- Modify: `web/src/components/replay.ts` — export control functions, add manifest cache for stepping
- Modify: `web/src/main.ts` — add replay keyboard handlers

- [ ] **Step 1: Export control functions and add stepping to replay.ts**

In `web/src/components/replay.ts`, make `togglePlay` and `restart` already exported. Add `stepForward` and `stepBackward`:

Change function signatures to export:

```typescript
export function togglePlay(): void { ... }
export function restart(): void { ... }
```

Add a cached manifest for stepping:

```typescript
let manifestEvents: ParsedMessage[] = [];
```

In `loadManifest`, cache the events:

```typescript
async function loadManifest(sessionId: string): Promise<void> {
  try {
    const res = await fetch(`/api/sessions/${sessionId}/replay`);
    const data = await res.json();
    manifestEvents = (data.events ?? []).map((e: Record<string, unknown>) => e as ParsedMessage);
    totalEvents = manifestEvents.length;
    if (scrubber) scrubber.max = String(totalEvents);
    updateProgress();
  } catch (err) {
    console.error('Failed to load replay manifest:', err);
  }
}
```

Add step functions:

```typescript
export function stepForward(): void {
  if (!state.replaySessionId) return;
  stopStream();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  if (currentIndex < totalEvents) {
    const evt = manifestEvents[currentIndex];
    if (evt && feedEl) {
      const empty = feedEl.querySelector('.replay-empty');
      if (empty) empty.remove();
      feedEl.appendChild(renderFeedEntry(evt));
      feedEl.scrollTop = feedEl.scrollHeight;
    }
    currentIndex++;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  }
}

export function stepBackward(): void {
  if (!state.replaySessionId) return;
  stopStream();
  update({ replayPlaying: false });
  if (playBtn) playBtn.textContent = '▶ PLAY';
  if (currentIndex > 0 && feedEl) {
    if (feedEl.lastElementChild) feedEl.removeChild(feedEl.lastElementChild);
    currentIndex--;
    if (scrubber) scrubber.value = String(currentIndex);
    updateProgress();
  }
}
```

- [ ] **Step 2: Add replay keyboard handlers in main.ts**

In `web/src/main.ts`, update the replay import:

```typescript
import { open as openReplay, render as renderReplay, togglePlay as replayToggle, restart as replayRestart, stepForward as replayForward, stepBackward as replayBack } from './components/replay';
```

In the keyboard switch, add (before the `Escape` case):

```typescript
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
```

And update the existing ArrowLeft/ArrowRight cases to check for replay first:

```typescript
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
```

- [ ] **Step 3: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: replay keyboard controls — Space play/pause, R restart, ←→ step"`

---

## Task 8: Timeline/Waterfall View

**Files:**
- Create: `web/src/components/timeline-view.ts`
- Modify: `web/src/styles/views.css` — timeline styles
- Modify: `web/src/components/feed-panel.ts` — add TIMELINE button to header
- Modify: `web/src/main.ts` — mount timeline view

- [ ] **Step 1: Add timeline CSS**

Append to `web/src/styles/views.css`:

```css
/* Timeline/Waterfall view */
.timeline-container {
  width: 100%;
  height: 100%;
  position: relative;
  overflow: hidden;
}

.timeline-container canvas {
  display: block;
  width: 100%;
  height: 100%;
  cursor: grab;
}

.timeline-container canvas:active { cursor: grabbing; }

.timeline-tooltip {
  position: absolute;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 6px 10px;
  font-size: 11px;
  color: var(--text);
  pointer-events: none;
  z-index: 50;
  max-width: 300px;
  white-space: nowrap;
  display: none;
}

.timeline-tooltip.visible { display: block; }

.timeline-header {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 20px;
  background: rgba(0,0,0,0.3);
  font-size: 9px;
  color: var(--text-dim);
  pointer-events: none;
  z-index: 10;
}
```

- [ ] **Step 2: Create timeline-view.ts**

```typescript
// web/src/components/timeline-view.ts
import type { AppState } from '../state';
import { state, subscribe, update } from '../state';
import { escapeHtml, formatDurationSecs } from '../utils';
import '../styles/views.css';

interface TimelineEvent {
  index: number;
  timestamp: string;
  type: string;
  role: string;
  contentText: string;
  toolName?: string;
  costUSD: number;
}

const TYPE_COLORS: Record<string, string> = {
  user: '#5588ff',
  assistant: '#33dd99',
  tool_use: '#ddcc44',
  tool_result: '#44cccc',
  hook: '#aa77dd',
  error: '#dd4455',
  system: '#444',
};

const LANE_LABELS = ['User', 'Assistant', 'Tools'];

let container: HTMLElement | null = null;
let canvas: HTMLCanvasElement | null = null;
let ctx: CanvasRenderingContext2D | null = null;
let tooltip: HTMLElement | null = null;
let events: TimelineEvent[] = [];
let sessionId: string | null = null;

// View state
let offsetX = 0;
let pixelsPerMs = 0.05; // zoom level
let isDragging = false;
let dragStartX = 0;
let dragStartOffset = 0;

export function render(mount: HTMLElement): void {
  container = mount;
}

export async function open(sid: string): Promise<void> {
  sessionId = sid;
  await loadEvents(sid);
  show();
}

export function close(): void {
  sessionId = null;
  events = [];
}

async function loadEvents(sid: string): Promise<void> {
  try {
    const res = await fetch(`/api/sessions/${sid}/replay`);
    const data = await res.json();
    events = data.events ?? [];
  } catch (err) {
    console.error('Failed to load timeline events:', err);
  }
}

function show(): void {
  if (!container) return;
  container.innerHTML = '';

  const wrapper = document.createElement('div');
  wrapper.className = 'timeline-container';

  canvas = document.createElement('canvas');
  wrapper.appendChild(canvas);

  tooltip = document.createElement('div');
  tooltip.className = 'timeline-tooltip';
  wrapper.appendChild(tooltip);

  container.appendChild(wrapper);

  resizeCanvas();
  window.addEventListener('resize', resizeCanvas);
  canvas.addEventListener('mousedown', onMouseDown);
  canvas.addEventListener('mousemove', onMouseMove);
  canvas.addEventListener('mouseup', onMouseUp);
  canvas.addEventListener('mouseleave', onMouseUp);
  canvas.addEventListener('wheel', onWheel, { passive: false });

  // Reset view
  offsetX = 0;
  if (events.length > 1) {
    const t0 = new Date(events[0].timestamp).getTime();
    const t1 = new Date(events[events.length - 1].timestamp).getTime();
    const span = t1 - t0;
    if (span > 0 && canvas) {
      pixelsPerMs = (canvas.width - 40) / span;
    }
  }

  draw();
}

function resizeCanvas(): void {
  if (!canvas || !container) return;
  canvas.width = container.clientWidth;
  canvas.height = container.clientHeight;
  ctx = canvas.getContext('2d');
  draw();
}

function getLane(evt: TimelineEvent): number {
  if (evt.role === 'user') return 0;
  if (evt.toolName || evt.type === 'tool_use' || evt.type === 'tool_result') return 2;
  return 1; // assistant, hook, system, etc.
}

function getColor(evt: TimelineEvent): string {
  if (evt.toolName && evt.role === 'assistant') return TYPE_COLORS.tool_use;
  if (evt.toolName && evt.role === 'tool') return TYPE_COLORS.tool_result;
  return TYPE_COLORS[evt.role] || TYPE_COLORS[evt.type] || TYPE_COLORS.system;
}

function draw(): void {
  if (!ctx || !canvas || events.length === 0) return;
  const w = canvas.width, h = canvas.height;
  ctx.clearRect(0, 0, w, h);

  const laneH = (h - 30) / 3; // 30px for top time labels
  const topY = 30;

  // Draw lane backgrounds
  ctx.fillStyle = 'rgba(255,255,255,0.02)';
  for (let i = 0; i < 3; i++) {
    if (i % 2 === 0) ctx.fillRect(0, topY + i * laneH, w, laneH);
  }

  // Draw lane labels
  ctx.fillStyle = '#44445a';
  ctx.font = '10px monospace';
  ctx.textAlign = 'left';
  for (let i = 0; i < LANE_LABELS.length; i++) {
    ctx.fillText(LANE_LABELS[i], 4, topY + i * laneH + 14);
  }

  if (events.length < 2) return;

  const t0 = new Date(events[0].timestamp).getTime();

  // Draw time axis labels
  ctx.fillStyle = '#44445a';
  ctx.font = '9px monospace';
  ctx.textAlign = 'center';
  const step = Math.max(1000, Math.pow(10, Math.floor(Math.log10(1 / pixelsPerMs * 100))));
  for (let t = 0; ; t += step) {
    const x = t * pixelsPerMs + offsetX;
    if (x > w) break;
    if (x < 0) continue;
    ctx.fillText(formatDurationSecs(t / 1000), x, 14);
    ctx.strokeStyle = 'rgba(255,255,255,0.05)';
    ctx.beginPath();
    ctx.moveTo(x, 20);
    ctx.lineTo(x, h);
    ctx.stroke();
  }

  // Draw events as bars
  for (let i = 0; i < events.length; i++) {
    const evt = events[i];
    const ts = new Date(evt.timestamp).getTime();
    const nextTs = i < events.length - 1 ? new Date(events[i + 1].timestamp).getTime() : ts + 1000;
    const duration = Math.max(nextTs - ts, 200); // min 200ms for visibility

    const x = (ts - t0) * pixelsPerMs + offsetX;
    const barW = Math.max(duration * pixelsPerMs, 4);
    const lane = getLane(evt);
    const y = topY + lane * laneH + 4;
    const barH = laneH - 8;

    if (x + barW < 0 || x > w) continue; // off screen

    ctx.fillStyle = getColor(evt);
    ctx.globalAlpha = 0.7;
    ctx.fillRect(x, y, barW, barH);
    ctx.globalAlpha = 1;

    // Label inside bar if wide enough
    if (barW > 40) {
      const label = evt.toolName || evt.role || '';
      ctx.fillStyle = '#000';
      ctx.font = '9px monospace';
      ctx.textAlign = 'left';
      ctx.fillText(label.slice(0, Math.floor(barW / 6)), x + 3, y + barH / 2 + 3);
    }
  }
}

function getEventAt(mx: number, my: number): TimelineEvent | null {
  if (events.length < 2 || !canvas) return null;
  const h = canvas.height;
  const laneH = (h - 30) / 3;
  const topY = 30;
  const t0 = new Date(events[0].timestamp).getTime();

  for (let i = 0; i < events.length; i++) {
    const evt = events[i];
    const ts = new Date(evt.timestamp).getTime();
    const nextTs = i < events.length - 1 ? new Date(events[i + 1].timestamp).getTime() : ts + 1000;
    const duration = Math.max(nextTs - ts, 200);
    const x = (ts - t0) * pixelsPerMs + offsetX;
    const barW = Math.max(duration * pixelsPerMs, 4);
    const lane = getLane(evt);
    const y = topY + lane * laneH + 4;
    const barH = laneH - 8;

    if (mx >= x && mx <= x + barW && my >= y && my <= y + barH) {
      return evt;
    }
  }
  return null;
}

function onMouseDown(e: MouseEvent): void {
  isDragging = true;
  dragStartX = e.clientX;
  dragStartOffset = offsetX;
}

function onMouseMove(e: MouseEvent): void {
  if (!canvas || !tooltip) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left;
  const my = e.clientY - rect.top;

  if (isDragging) {
    offsetX = dragStartOffset + (e.clientX - dragStartX);
    draw();
    return;
  }

  const evt = getEventAt(mx, my);
  if (evt) {
    canvas.style.cursor = 'pointer';
    const time = new Date(evt.timestamp).toLocaleTimeString();
    const content = (evt.contentText || '').slice(0, 80);
    tooltip.innerHTML = `<div><b>${time}</b> [${evt.type || evt.role}]</div>
      ${evt.toolName ? `<div>${escapeHtml(evt.toolName)}</div>` : ''}
      <div style="color:var(--text-dim)">${escapeHtml(content)}</div>
      ${evt.costUSD > 0 ? `<div style="color:var(--yellow)">$${evt.costUSD.toFixed(4)}</div>` : ''}`;
    tooltip.style.left = `${mx + 15}px`;
    tooltip.style.top = `${my + 15}px`;
    tooltip.classList.add('visible');
  } else {
    canvas.style.cursor = 'grab';
    tooltip.classList.remove('visible');
  }
}

function onMouseUp(): void {
  isDragging = false;
}

function onWheel(e: WheelEvent): void {
  e.preventDefault();
  if (!canvas) return;
  const rect = canvas.getBoundingClientRect();
  const mx = e.clientX - rect.left;

  if (e.ctrlKey || e.metaKey) {
    // Zoom
    const zoomFactor = e.deltaY > 0 ? 0.8 : 1.25;
    const oldPxPerMs = pixelsPerMs;
    pixelsPerMs *= zoomFactor;
    pixelsPerMs = Math.max(0.001, Math.min(1, pixelsPerMs));
    // Keep the point under cursor stable
    offsetX = mx - (mx - offsetX) * (pixelsPerMs / oldPxPerMs);
  } else {
    // Pan
    offsetX -= e.deltaY;
  }
  draw();
}
```

- [ ] **Step 3: Add TIMELINE button to feed header**

In `web/src/components/feed-panel.ts`, import timeline:

```typescript
import { open as openTimeline } from './timeline-view';
```

In `updateHeader()`, when a session is selected, add a TIMELINE button:

Replace the selected-session header HTML with:

```typescript
    headerEl.innerHTML = `<span style="color:var(--cyan);letter-spacing:1px">LIVE FEED</span>
      <span style="color:var(--text-dim);font-size:10px">${escapeHtml(name)}</span>
      <span class="timeline-btn" style="margin-left:8px;color:var(--yellow);font-size:9px;cursor:pointer;border:1px solid rgba(255,204,0,0.3);padding:1px 6px;border-radius:2px;letter-spacing:0.5px">TIMELINE</span>
      <span class="back-to-feed" style="margin-left:auto;color:var(--cyan);font-size:10px;cursor:pointer;letter-spacing:0.5px">← all</span>`;
    headerEl.querySelector('.back-to-feed')?.addEventListener('click', () => {
      update({ selectedSessionId: null });
    });
    headerEl.querySelector('.timeline-btn')?.addEventListener('click', () => {
      if (state.selectedSessionId) openTimeline(state.selectedSessionId);
    });
```

- [ ] **Step 4: Mount timeline in main.ts**

In `web/src/main.ts`, add:

```typescript
import { render as renderTimeline } from './components/timeline-view';
```

After `renderHistoryView(feedMount);`:

```typescript
renderTimeline(feedMount);
```

- [ ] **Step 5: Verify and commit**

Run: `cd web && npx tsc --noEmit && npm run build`
Commit: `git commit -m "feat: add Canvas 2D timeline/waterfall view with zoom and pan"`

---

## Task 9: Build, test, and push

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./... -count=1 -timeout 60s`
Expected: All PASS

- [ ] **Step 2: Build frontend and Go binary**

Run: `make build`

- [ ] **Step 3: Push**

Run: `git pull --rebase origin main && git push origin main`
