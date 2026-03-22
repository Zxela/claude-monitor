# Killer Features Design Spec

## Context

claude-monitor has grown from a basic JSONL watcher to a functional dashboard with session tracking, stats, filtering, and live feed. With 188 sessions, model-specific pricing, and parent/child hierarchy working, the next step is five killer features that transform it from a monitoring tool into a full observability platform.

## Feature 1: Combined Pixel + List View

**Goal**: Merge the pixel sprites into session cards. Remove the separate PIXELS view toggle.

**Changes**:
- `static/index.html`: In `renderSessions()`, add a 30x48px sprite container to the left of each session card
- Use existing sprite animation frames (IDLE, THINK, TOOL, TYPE) driven by `sessionState[id].state`
- Remove the LIST/PIXELS toggle buttons from the top bar
- Remove the `#pixel-view` div and `renderPixelView()` function
- Each card's sprite animates independently via the existing `tickSprites` interval

**Session card layout**:
```
[sprite] [dot] session-name    [status badge]
         cost · tokens · cache%
         id · model · age · duration
                              [REPLAY]
```

**Files**: `static/index.html` only

## Feature 2: Live Cost Ticker + Budget Alerts

**Goal**: Animated cost counter and configurable spend alerts.

**Changes**:

### Cost animation
- `static/index.html`: Replace `setStatVal` for cost fields with a smooth counter animation
- New function `animateCost(elementId, fromVal, toVal)` — uses `requestAnimationFrame` to interpolate over 500ms
- Track previous values in state to compute deltas

### Cost rate per session
- In `renderSessions()`, compute `costPerMin = totalCostUSD / ((lastActive - startedAt) / 60000)` and display as `$X.XX/min`

### Budget alerts
- State: `state.budgetThreshold` (loaded from `localStorage.getItem('budget')`, default null = disabled)
- Gear icon in top bar opens a small popover input to set threshold
- When total spend crosses threshold: flash the cost stat red, show a dismissable banner
- CSS class `.over-budget` on cost elements with red color + pulse animation

**Files**: `static/index.html` only

## Feature 3: Session Timeline / Waterfall

**Goal**: Horizontal timeline showing message flow with proportional time segments.

**Changes**:

### Backend
- No new endpoint needed — uses existing `/api/sessions/{id}/replay` manifest

### Frontend
- New `#timeline-panel` div, shown when user clicks a "TIMELINE" button on a session card
- Replaces or overlays the feed panel
- `renderTimeline(events)` function:
  - Computes duration between consecutive events
  - Renders colored segments as `<div>` elements with `width` proportional to duration
  - Color map: user=blue, assistant=green, tool_use=yellow, tool_result=cyan, hook=purple
  - Total timeline width = panel width, segments scaled proportionally
  - Hover tooltip shows: type, tool name, content preview, duration
  - Click scrolls feed to that event
- Timeline scrubber: draggable position indicator

**Layout**:
```
[====user===][==========thinking==========][==Bash==][=result=][====thinking====][==Edit==]
0s           2s                            8s       9s        10s                15s      16s
```

**Files**: `static/index.html` (HTML + CSS + JS)

## Feature 4: Cross-Session Search

**Goal**: Search across all session content.

**Changes**:

### Backend
- `cmd/claude-monitor/main.go`: New endpoint `GET /api/search?q=<query>&limit=50`
- Iterates all sessions from store, reads their JSONL files
- Parses each line, checks if `ContentText` or `ToolDetail` contains query (case-insensitive)
- Returns array of `{sessionId, sessionName, timestamp, type, contentText, toolName, toolDetail}`
- Capped at `limit` results, newest first

### Frontend
- Search input in the top bar (right side, before the view toggle area)
- On Enter or after 300ms debounce: `GET /api/search?q=...`
- Results displayed in the feed panel (replaces live feed temporarily)
- Each result is a feed entry with session name badge — click to select that session
- ESC or clear input returns to live feed
- Loading spinner during search

**Files**: `cmd/claude-monitor/main.go`, `static/index.html`

## Feature 5: Agent Dependency Graph

**Goal**: Interactive node-link visualization of parent/child session relationships.

**Changes**:

### Frontend
- New view mode: GRAPH (alongside LIST in the top bar, replacing PIXELS which is now merged)
- `#graph-view` canvas element, full panel size
- `renderGraph()` function:
  - Builds nodes from `state.sessions` (filter by current session filter)
  - Builds edges from `parentId` → child relationships
  - Force-directed layout (simple spring simulation, no external lib):
    - Repulsion between all nodes
    - Attraction along edges
    - Gravity toward center
    - 60fps animation loop with damping
  - Node rendering on canvas:
    - Circle, radius proportional to `log(totalCostUSD + 1) * 5 + 8`
    - Fill color by status: green=active, yellow=thinking, blue=tool, gray=idle
    - Label below: projectName or sessionName, truncated
    - Edge: gray line from parent to child
  - Interaction:
    - Hover: highlight node + edges, show tooltip with stats
    - Click: select session (updates feed, highlights in list)
    - Drag: reposition nodes
- Respects session filter (Active/Recent/All)

**Files**: `static/index.html`

## Implementation Order

These are independent enough to parallelize in 3 units:

| Unit | Features | Files | Complexity |
|------|----------|-------|------------|
| A | #1 (pixels in cards) + #2 (cost ticker) | `static/index.html` | Medium |
| B | #3 (timeline) + #4 (search) | `main.go` + `static/index.html` | High |
| C | #5 (dependency graph) | `static/index.html` | High |

Units A and C are frontend-only and don't conflict. Unit B touches both backend and frontend but the backend change is isolated (new endpoint).

Conflict risk: All three touch `static/index.html`, but in different sections:
- Unit A: `renderSessions()`, top bar, sprite code
- Unit B: feed panel, top bar search input, new timeline panel
- Unit C: new graph view, top bar view toggle

To avoid conflicts, implement sequentially: A first (removes pixel view, restructures cards), then B+C in parallel (they touch different parts of the restructured code).

## Verification

After implementation:
1. `go test ./...` — all tests pass
2. `go build ./cmd/claude-monitor` — builds cleanly
3. Manual verification:
   - Session cards show inline sprites animating by status
   - Cost ticks up smoothly when messages arrive
   - Budget alert fires when threshold crossed
   - Timeline shows proportional segments for a session
   - Search finds content across sessions
   - Graph shows parent/child relationships with interactive nodes
