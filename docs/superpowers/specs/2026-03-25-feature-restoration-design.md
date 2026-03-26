# Feature Restoration + Timeline View — Design Spec

**Date:** 2026-03-25
**Status:** Approved
**Priority:** P1
**Depends on:** Component Porting (completed)

## Context

The migration from monolithic HTML to Vite + TypeScript lost 12 features. This spec covers restoring them plus building a proper timeline/waterfall view.

## Features to Restore

### 1. Keyboard Navigation

Add keyboard-driven session navigation to `main.ts` and `session-list.ts`:

- **↑↓** moves a `focusedIndex` through visible session cards. The focused card gets a `.focused` CSS class (dashed outline, `outline: 1px dashed var(--text-dim); outline-offset: -1px`).
- **Enter** selects the focused session (same as clicking it).
- **←** collapses subagent children on the focused session (if expanded).
- **→** expands subagent children on the focused session (if collapsed).
- Focus state is tracked as `focusedSessionId: string | null` in app state.
- Focus resets when the session list re-renders (e.g., filter change).
- The focused card is scrolled into view via `scrollIntoView({ block: 'nearest' })`.

### 2. URL Hash Persistence

New module `web/src/hash.ts`:

- On state change (`selectedSessionId`, `view`, active filter): write to `location.hash` as `#session=ID&view=list&filter=recent`.
- On page load: parse `location.hash` and call `update()` to restore state.
- Debounce hash writes by 200ms to avoid thrashing during rapid state changes.
- Listen for `hashchange` event (browser back/forward) and restore state.
- Only persist: `selectedSessionId`, `view`, active filter. Don't persist transient state like search query or feed filters.

### 3. Scroll Lock Button

In `feed-panel.ts` and `feed.css`:

- When user scrolls up from the bottom (auto-scroll becomes false), show a fixed button: "▼ RESUME SCROLL" positioned at `bottom: 38px; right: 14px` (above status bar).
- Style: cyan border, semi-transparent background, 10px font, monospace.
- Click: scroll feed to bottom, set `autoScroll = true`, hide button.
- Button disappears automatically when user scrolls back to bottom.

### 4. Current Tool Display

In `session-card.ts` and `sessions.css`:

- Below the stats row in expanded cards, show a `.session-current-tool` div.
- Content: the session's last tool name + detail (from WebSocket messages — track in a `lastTool` map keyed by session ID).
- Only visible when `session.status === 'tool_use'`.
- Style: 10px font, `color: var(--text-dim)`, `opacity: 0.7`, truncated to 220px max-width.
- Cleared when status changes away from `tool_use`.

State tracking: add a module-level `Map<string, string>` in `feed-panel.ts` that stores the last tool info per session ID, updated on each `tool_use` WebSocket message. Export a `getLastTool(sessionId)` function that `session-card.ts` can import.

### 5. Feed Tool Grouping

In `feed-panel.ts`, `render-message.ts`, and `feed.css`:

- When a `tool_use` entry is appended to the feed, add CSS class `tool-group-start` (no bottom border, top border-radius).
- When the next `tool_result` entry arrives, add CSS class `tool-group-end` (no top border, indented 28px left padding, bottom border-radius, reduced opacity 0.75, left border `2px solid rgba(0,204,255,0.2)`).
- Track the last tool entry element in `feed-panel.ts` to apply grouping classes.

### 6. Error Count Click

In `session-card.ts`:

- The `.session-error-count` span gets a click handler (with `stopPropagation`).
- On click: select the session and set feed type filters to show only errors:
  ```
  update({
    selectedSessionId: session.id,
    feedTypeFilters: { user: false, assistant: false, tool_use: false, tool_result: false, agent: false, hook: false, error: true }
  })
  ```

### 7. Search Results in Feed

In `feed-panel.ts`:

- When `state.searchOpen` is false and `state.searchResults.length > 0` and the user clicked a search result (detected by a new `state.searchResultsInFeed` boolean):
  - Render search results in the feed area with highlighted matches (reuse the `highlightMatch` logic from `search.ts`, extract to `utils.ts`).
  - Show a "← Back to live feed" button at the top of the feed.
  - Click the back button: clear search results, return to live feed.
- When a search result in the feed is clicked: select that session and scroll the feed.

Actually, the new search dropdown already handles result clicking well. Simplify this: just add a "← Back to live feed" link in the feed header when viewing a selected session (from any source), so the user can return to multi-session mode easily.

### 8. Browser Notifications

New module `web/src/notifications.ts`:

- `requestPermission()`: called once on first budget/error event.
- `notify(title, body)`: creates a `new Notification(title, { body })` if permission granted.
- Settings stored in `localStorage['notif-settings']` as JSON: `{ budget: true, error: true }`.
- In `budget-popover.ts`: add two checkboxes to the popover (Budget exceeded, Agent errored). On budget breach, call `notify()`.
- In `feed-panel.ts`: on error messages from WebSocket, call `notify()` if error notifications enabled.

### 9. Replay Arrow Keys

In `main.ts` and `replay.ts`:

- When `state.replaySessionId` is set (replay panel is open):
  - **Space** toggles play/pause (call exported `togglePlay()`).
  - **R** restarts (call exported `restart()`).
  - **←** steps backward one event (pause if playing, decrement index, re-render).
  - **→** steps forward one event (pause if playing, increment index, re-render).
- Export `togglePlay()`, `restart()`, `stepForward()`, `stepBackward()` from `replay.ts`.
- Step functions: stop SSE stream, adjust `currentIndex`, render the single event at that index from the cached manifest.

### 10. "Back to Live Feed" Link

In `feed-panel.ts`:

- When a session is selected, the feed header shows the session name + a small "✕" or "← all" link.
- Clicking it deselects the session (`update({ selectedSessionId: null })`) returning to multi-session mode.

## Timeline/Waterfall View (New Feature)

### Overview

A horizontal timeline visualization of events for a selected session, showing the sequence and duration of tool calls, thinking, and user interactions.

### Trigger

- Button in the feed header: "TIMELINE" (only visible when a session is selected).
- Keyboard shortcut: none (accessed via click).
- Can also be triggered from the topbar view toggle if desired, but primarily lives in the feed header.

### Data Source

`GET /api/sessions/{id}/replay` — returns the full event manifest with timestamps and indexes. Already implemented.

### Layout

- Renders in the feed-mount area, replacing the feed content.
- **Horizontal axis** = time (left to right). Labels at the top showing timestamps.
- **Vertical lanes** (rows):
  - **User** — blue bars for user messages
  - **Assistant** — green bars for assistant responses
  - **Tools** — yellow bars for tool calls, cyan bars for tool results. Tool name shown inside the bar if wide enough.
- Each event is a colored rectangle. Width = proportional to time between this event and the next (minimum 4px for visibility).
- **Hover** shows tooltip: timestamp, type, content preview (first 100 chars), cost if > 0.
- **Click** an event bar to scroll the feed to that message index.

### Rendering

Canvas 2D (same approach as graph-view). Handles hundreds of events without DOM overhead.

- **Horizontal scroll** via mouse drag or scroll wheel (shift+scroll for horizontal).
- **Zoom** via ctrl+scroll wheel — adjusts the time-per-pixel ratio.
- **Minimap** at the top — thin bar showing the full timeline with a viewport indicator.

### Color Coding

- User: `#5588ff` (blue)
- Assistant: `#33dd99` (green)
- Tool call: `#ddcc44` (yellow)
- Tool result: `#44cccc` (cyan)
- Hook: `#aa77dd` (purple)
- Error: `#dd4455` (red)

### File Structure

- `web/src/components/timeline-view.ts` — Canvas rendering, zoom/pan, event mapping
- Styles in `web/src/styles/views.css` — timeline container, tooltip

### Interaction with Feed

When the timeline is open and an event bar is clicked, close the timeline and switch to the feed with that session selected, scrolled to the approximate position of that event.

## Files Modified

| File | Changes |
|------|---------|
| `web/src/state.ts` | Add `focusedSessionId`, `searchResultsInFeed` |
| `web/src/main.ts` | Keyboard handlers for ↑↓/Enter/←→, replay keys, import hash module |
| `web/src/hash.ts` | **New** — URL hash read/write/restore |
| `web/src/notifications.ts` | **New** — browser notification wrapper |
| `web/src/components/session-list.ts` | Focus tracking, `.focused` class, scrollIntoView |
| `web/src/components/session-card.ts` | Current tool display, error count click handler |
| `web/src/components/feed-panel.ts` | Scroll lock button, tool grouping, back-to-feed link, last-tool tracking |
| `web/src/components/replay.ts` | Export step functions for arrow key control |
| `web/src/components/timeline-view.ts` | **New** — Canvas timeline/waterfall |
| `web/src/components/budget-popover.ts` | Notification checkboxes |
| `web/src/components/search.ts` | Extract `highlightMatch` to utils |
| `web/src/utils.ts` | Add `highlightMatch` |
| `web/src/styles/sessions.css` | `.focused`, `.session-current-tool` |
| `web/src/styles/feed.css` | Scroll lock button, tool-group-start/end |
| `web/src/styles/views.css` | Timeline container, tooltip |

## Out of Scope

- Pixel sprite animations (separate project)
- Mobile hamburger menu (responsive work)
- Docker stop button (removed per user request)
