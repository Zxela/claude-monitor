# Component Porting — Design Spec

**Date:** 2026-03-25
**Status:** Approved
**Priority:** P1
**Depends on:** Navigation Overhaul (completed)

## Context

The navigation overhaul migrated claude-monitor's frontend from a 4,100-line monolithic HTML file to a Vite + TypeScript modular architecture. The core session list, search, topbar, and state management are working. This spec covers porting the remaining 6 components from the old HTML into the new module system.

## Porting Groups

### Group 1: Feed Panel + Replay (tightly coupled — share message rendering)

### Group 2: Graph, Table, History Views (independent view overlays)

### Group 3: Budget Popover + Help Overlay (small UI utilities)

---

## Group 1: Feed Panel + Replay

### Shared Module: Message Renderer (`render-message.ts`)

A shared function that both the feed panel and replay panel use to render individual message entries.

`renderFeedEntry(message, options)` creates a `.feed-entry` DOM element:

- **Type detection logic:** Checks `message.hookEvent` (hook), `message.toolName` (tool/tool_result), `message.role` (user/assistant), `message.isError` (error). Falls back to system.
- **Type colors:** user=#5588ff, assistant=#33dd99, tool_use=#ddcc44, agent=#dd8844, tool_result=#44cccc, hook=#aa77dd, error=#dd4455, system=dim
- **Entry structure:** timestamp (HH:MM:SS) | type badge | content preview | expand button | session ID (optional)
- **Content truncation:** 120 chars for user/assistant, 100 chars for tool results. Full content available via expand/collapse toggle.
- **Expand/collapse:** Click the expand button (`+`/`-`) to show full content. Toggled via CSS class `.expanded`.

### Feed Panel (`feed-panel.ts`)

Renders in the right side of the main layout when a session is selected.

**Initial load:** When `state.selectedSessionId` changes, fetch `GET /api/sessions/{id}/recent` (last 50 messages) and render them.

**Live updates:** Subscribe to WebSocket messages. When a message arrives for the selected session, append a new feed entry. Auto-scroll to bottom.

**Type filter bar:** Row of buttons above the feed, one per message type (ALL, user, assistant, tools, results, agents, hooks, errors). Click to toggle visibility. Shift+click for solo mode (show only that type, hide all others). Click ALL to reset.

**Filter state:** Stored in `state.feedTypeFilters` as `Record<string, boolean>`. Persists while session is selected.

**DOM cap:** Maximum ~500 entries in the DOM. When exceeded, remove entries from the top.

**Empty state:** "WAITING FOR EVENTS..." when session is selected but no messages yet. "Select a session to view its feed" when no session selected.

### Replay Panel (`replay.ts`)

Opens when user clicks a replay button. Replaces the feed panel content.

**Manifest load:** Fetch `GET /api/sessions/{id}/replay` to get total event count for the scrubber.

**SSE stream:** Connect to `GET /api/sessions/{id}/replay/stream?from={index}&speed={speed}`. Each SSE `message` event contains JSON with the event index and parsed message. SSE `done` event signals completion.

**Controls:**
- Play/Pause button — toggles SSE stream connection
- Restart button — clears feed, resets index to 0, restarts stream
- Speed dropdown — 0.5x, 1x, 2x, 4x (default 1x)
- Scrubber — range input from 0 to total events. Dragging stops playback, reconnects stream from new position on release.
- Progress text — "X / Y" showing current event index and total

**Message rendering:** Uses the same `renderFeedEntry()` from render-message.ts. Auto-scrolls during playback.

**Close:** Close button returns to the feed panel view.

---

## Group 2: Views

### Graph View (`graph-view.ts`)

Canvas 2D force-directed graph rendered on an HTML `<canvas>` element. No external libraries (no D3).

**Node selection:** Shows sessions active in last 120 seconds, plus parents of visible sessions (for edge rendering). Rebuilds node set when session data changes.

**Node appearance:**
- Radius: `Math.log(cost + 1) * 5 + 8`, clamped to [8, 30]
- Color by status: thinking=#ffcc00, tool_use=#4488ff, waiting=#00ff88, idle=#44445a
- Label: project name truncated to 16 chars, rendered below node
- Cost label: rendered below name if cost > $0.01

**Edge rendering:** Gray lines (`rgba(100,100,140,0.3)`) between parent and child sessions.

**Force simulation (per frame):**
- Repulsion between all node pairs: `force = 2000 / distance²`
- Attraction along edges: `force = (distance - 100) * 0.01`
- Center gravity: `velocity += (center - position) * 0.001`
- Damping: `velocity *= 0.9`
- Bounds clamping to canvas dimensions

**Interaction:**
- Drag nodes to reposition (clears velocity)
- Hover shows tooltip: name, cost, messages, status, model
- Click selects session and switches to list view

**Animation:** `requestAnimationFrame` loop. Starts when graph view becomes visible, stops when hidden.

### Table View (`table-view.ts`)

Dense sortable table overlay showing all sessions.

**Columns (10):** Name, Status, Cost, $/min, Duration, Tokens, Cache%, Messages, Errors, Model

**Sorting:** Click any column header to sort. Click again to reverse. Default: cost descending. Sort arrow indicator (▲/▼) on active column.

**Row interaction:** Click to select session and switch to list view.

**Data source:** `state.sessions` Map — rebuilds table when sessions change.

### History View (`history-view.ts`)

Table of completed sessions from SQLite history.

**Data source:** `GET /api/history?limit=200` — fetched once when history view opens. Stored in component-local state.

**Columns (8):** Date, Name, Cost, Duration, Tokens, Messages, Errors, Model

**Sorting:** Client-side sort on any column. Default: date descending.

**Row interaction:** Click to open replay for that session.

---

## Group 3: Utilities

### Budget Popover (`budget-popover.ts`)

Small popover anchored below the "TOTAL SPEND" stat in the topbar.

**Trigger:** Click the gear icon (⚙) next to the cost stat.

**Popover content:** Number input for threshold in USD, Set button, Clear button.

**Persistence:** `localStorage.setItem('budget', threshold)`. Loaded on init.

**Alert behavior:**
- When total spend exceeds threshold: cost stat gets `.over-budget` class (red text, pulse animation)
- Banner appears at top of page: "Budget exceeded: $X / $Y" with dismiss button
- Dismiss hides banner until spend crosses a new threshold
- `state.budgetThreshold` and `state.budgetDismissed` track this

### Help Overlay (`help-overlay.ts`)

Modal overlay triggered by `?` key.

**Content — Main View shortcuts:**
- `/` Focus search
- `Esc` Clear / deselect / unfocus
- `↑↓` Navigate sessions
- `Enter` Select focused session
- `1` Active filter, `2` Recent filter, `3` All filter
- `g` Graph view, `h` History view, `t` Table view
- `?` Toggle help

**Content — Replay Controls:**
- `Space` Play/pause
- `R` Restart
- `←→` Step backward/forward

**Behavior:** Click anywhere outside content to dismiss. Fixed overlay with centered dialog.

---

## File Structure

```
web/src/components/
├── render-message.ts   # Shared message entry renderer (NEW)
├── feed-panel.ts       # Live feed + type filters (NEW)
├── replay.ts           # Replay with SSE + scrubber (NEW)
├── graph-view.ts       # Canvas 2D force graph (NEW)
├── table-view.ts       # Sortable session table (NEW)
├── history-view.ts     # Historical sessions table (NEW)
├── budget-popover.ts   # Budget threshold + alerts (NEW)
├── help-overlay.ts     # Keyboard shortcuts modal (NEW)
├── topbar.ts           # (existing — add budget gear click handler)
├── session-list.ts     # (existing)
├── session-card.ts     # (existing)
└── search.ts           # (existing)

web/src/styles/
├── feed.css            # Extend with feed entry + replay styles
├── views.css           # NEW — graph, table, history styles
├── base.css            # (existing)
├── topbar.css          # (existing)
└── sessions.css        # (existing)
```

## State Changes

Add to `AppState` in `state.ts`:

```typescript
// Feed
feedTypeFilters: Record<string, boolean>;
feedMessages: ParsedMessage[];

// Replay
replaySessionId: string | null;
replayPlaying: boolean;

// Budget
budgetThreshold: number | null;
budgetDismissed: boolean;
```

## Integration Points

- **main.ts** — mount feed-panel into `#feed-mount`, register keyboard shortcuts for `?`, `h`, `t`, arrow keys
- **topbar.ts** — add budget gear click handler, wire budget popover
- **session-list.ts** — add replay button to session cards (or feed panel header)
- **WebSocket handler** — route messages to feed panel when session matches

## Out of Scope

- Mobile responsive improvements
- Feed persistence / infinite scroll of historical messages
- URL hash state persistence
- Notification settings (browser notifications for budget/errors)
