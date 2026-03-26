# UI Improvements — Design Spec

**Date:** 2026-03-26

Three independent UI fixes bundled into one spec since they're all small, frontend-only changes.

---

## 1. Compact Card Stats

### Problem
The `renderCompact` session card only shows: dot, name, chevron, cost, timeAgo, duration, model. Token usage, error count, cache hit %, and cost rate are lost.

### Fix
Add a second line to the compact card showing key stats inline: token count, cache hit %, error count (if > 0), cost rate. Keep the single-line feel but allow wrapping to two lines when stats are present.

Also change sidebar width from 270px to 283px.

### Files
- `web/src/components/session-card.ts` — modify `renderCompact` to add stats line
- `web/src/styles/sessions.css` — change 270px to 283px

---

## 2. Agent Timeline View

### Problem
The graph view shows a static force-directed graph of parent-child relationships. The user wants to see agent interactions over time — a sequential view showing "research agent called → done → plan agent called → done → explore agent called".

### Approach
Replace or augment the existing graph view with an option to show a **swim-lane timeline**. Each session/subagent gets a horizontal lane. Events (start, messages, tool calls, end) are plotted left-to-right on a time axis. Parent-child relationships are shown by vertical arrows from parent lane to child lane at the moment the subagent was spawned.

Actually — the existing graph view is a force-directed DAG and is still useful. The user confirmed it should stay. The sequential visualization is a **new view mode** within the graph view — a toggle between "Graph" and "Timeline" layouts.

**Simpler approach:** Since we already have a timeline-view component (`timeline-view.ts`) that shows events on a time axis for a single session, and the graph view shows multi-session relationships, the best fit is to enhance the **existing graph view** with a timeline layout option.

**Simplest approach that delivers value:** Add a "Sequence" sub-tab within the graph view that shows a vertical list of agent interactions in chronological order, with indentation showing parent-child depth and connecting lines.

### Design
When graph view is active, add a toggle: **Graph | Sequence**

**Sequence mode:**
- Vertical chronological list of all active sessions and recent subagents
- Each entry shows: timestamp, session name, status (spawned/active/done), cost
- Child sessions are indented under their parent
- A vertical line connects parent to children
- Entries appear in chronological order of `startedAt`
- Auto-scrolls to bottom (latest activity)
- Updates in real-time via WebSocket events

This is essentially a simplified "activity log" showing the flow of agent orchestration.

### Files
- `web/src/components/graph-view.ts` — add sequence toggle, render sequence mode
- `web/src/styles/views.css` — add sequence view styles

---

## 3. Responsive Header Hamburger

### Problem
The topbar wraps onto two rows when the screen shrinks below 1100px. Stats, search, and view buttons overflow.

### Fix
At `max-width: 768px`, collapse the topbar into:
- Left: brand + hamburger button
- When hamburger is tapped, show a dropdown/slide-down panel with: stats, search, view toggles

At `max-width: 1100px` (current breakpoint), keep the flex-wrap behavior but hide the search box by default (show via a search icon toggle) to reduce wrapping.

### Design

**768px and below:**
- Topbar shows only: brand (left) + hamburger icon (right)
- Hamburger toggles a dropdown panel below the topbar containing:
  - Stats row (ACTIVE, SPEND, WORKING, CACHE, $/MIN)
  - Search input
  - View buttons (LIST, GRAPH, HISTORY, TABLE)
- Panel slides down with CSS transition
- Clicking outside or pressing Escape closes it

**1100px and below:**
- Keep current layout but search moves into a collapsible icon
- Click search icon → expands search input (existing pattern works, just needs the icon trigger)

### Files
- `web/src/components/topbar.ts` — add hamburger button, toggle logic
- `web/src/styles/base.css` — responsive hamburger styles, dropdown panel
