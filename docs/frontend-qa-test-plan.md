# Frontend QA Test Plan — Claude Monitor

**Date:** 2026-03-29
**Scope:** Manual QA of all frontend features using Playwright MCP against the production build
**Environment:** Backend on `localhost:7700` serving built frontend, 1.4GB real SQLite database

---

## How to use this plan

Work through each section iteratively. For each test case:
1. Navigate to the relevant feature using Playwright MCP
2. Perform the action described
3. Verify the expected outcome visually (screenshot if needed)
4. Mark as PASS, FAIL, or SKIP with notes

Test cases marked with `[LIVE]` require an active Claude Code session running during the test.

---

## Section 1: Prerequisites

- [ ] Backend running on `localhost:7700` (`curl localhost:7700/api/version` returns JSON)
- [ ] WebSocket endpoint accessible (`/ws`)
- [ ] Database has real session data (historical + ideally at least one active session)
- [ ] Browser dev tools accessible for inspecting network/console

---

## Section 2: Session List (Sidebar)

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 2.1 | Initial load | Page loads, topbar shows stats (active count, total cost, cache%), session list populates with time-grouped sections | |
| 2.2 | Time grouping | Sessions correctly bucketed into Active / Last hour / Today / Yesterday / This week / Older | |
| 2.3 | Active filter (key: 1) | Only shows active/running sessions. Count matches topbar "ACTIVE" stat | |
| 2.4 | Recent filter (key: 2) | Shows active + last hour + today only | |
| 2.5 | All filter (key: 3) | Shows all time groups including older | |
| 2.6 | Session card content | Card shows: status dot, session name, cost, token count, cache%, error count, model, duration | |
| 2.7 | Session card status indicators | Active sessions show colored dot (green=active, yellow=thinking, blue=tool_use). Idle sessions show gray | |
| 2.8 | Subagent expansion | Sessions with children show chevron + child count. Click chevron expands to show child sessions indented | |
| 2.9 | Show idle subagents toggle | Toggle shows/hides idle child sessions in expanded view. Shows "SHOW N IDLE" / "HIDE N IDLE" | |
| 2.10 | Overflow handling | Groups with >15 sessions show "Show all" button. Clicking reveals remaining | |
| 2.11 | Session selection | Click a session -> it highlights, feed panel switches to single-session mode | |
| 2.12 | Keyboard navigation | Arrow up/down moves focus. Enter selects focused session. Esc deselects | |
| 2.13 | Trivial session filtering | Sessions with cost=0, tokens=0, messages<4 are excluded from counts (but active trivial sessions still show) | |
| 2.14 | Error count click -> filter errors | Click the error count badge on a session card -> selects that session AND filters feed to errors-only | |
| 2.15 | Auto-expand on select | Click a session with children -> children auto-expand (if not already expanded) | |
| 2.16 | Task description display | Session cards show truncated task description. Hover shows full text in tooltip | |
| 2.17 | Current tool display `[LIVE]` | When session status is `tool_use`, card shows the name of the current tool being used | |
| 2.18 | Cost rate on card `[LIVE]` | Active sessions with costRate > 0 show $/min rate on the card | |
| 2.19 | Model name formatting | Model names are shortened: "claude-" prefix removed, "-4-6" suffix removed (e.g., "opus" not "claude-opus-4-6") | |
| 2.20 | Collapsed groups default | Yesterday, This week, Older groups start collapsed by default. Click group header to expand | |
| 2.21 | Children sort order | Expanded children sorted by startedAt descending (newest first) | |
| 2.22 | Active count is top-level only | ACTIVE filter count excludes subagents — only top-level sessions counted | |

---

## Section 3: Feed Panel

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 3.1 | Multi-session mode | With no session selected, feed shows live events from all active sessions with session labels | |
| 3.2 | Single-session mode | Select a session -> feed shows full event history + live events for that session only | |
| 3.3 | Event type rendering | Each event type has correct badge: user (blue), assistant (green), tool_use (yellow), tool_result (cyan), agent (purple), hook (magenta), error (red) | |
| 3.4 | Event content | Events show timestamp, content preview, and relevant metadata (tool name, duration, token cost) | |
| 3.5 | Content expansion | Click + button on an event -> expands to show full content. Click - -> collapses back | |
| 3.6 | Type filter buttons | 9 filter buttons visible: ALL, USER, ASSISTANT, TOOL_USE, TOOL_RESULT, AGENT, COMMAND, HOOK, ERROR | |
| 3.7 | Filter toggle | Click a type -> toggles it off/on. Filtered-out events disappear from feed | |
| 3.8 | Filter isolate (Shift+click) | Shift+click a type -> only that type shown, all others hidden | |
| 3.9 | Filter cascade | Disabling tool_use also disables tool_result automatically | |
| 3.10 | Group highlighting | Hover over a tool_use event -> all related events (tool_use + hooks + tool_result in same group) highlight together | |
| 3.11 | Auto-scroll `[LIVE]` | New events arrive -> feed scrolls to bottom automatically | |
| 3.12 | Scroll lock | Scroll up manually -> auto-scroll stops. "RESUME SCROLL" button appears. Click it -> resumes auto-scroll | |
| 3.13 | Max entries | Feed caps at 500 entries (verify with a high-traffic session that older entries get pruned) | |
| 3.14 | Deduplication | Same timestamp + content events don't appear twice | |
| 3.15 | Live updates `[LIVE]` | With an active session selected, new WebSocket events appear in real-time in the feed | |
| 3.16 | Feed header — LIVE vs HISTORY label | Active session shows "LIVE FEED" header. Idle/finished session shows "SESSION HISTORY" | |
| 3.17 | TIMELINE button in feed header | Click "TIMELINE" button in single-session header -> opens timeline view for that session | |
| 3.18 | REPLAY button in feed header | For non-live sessions, "REPLAY" button appears. Click -> opens replay. Button hidden for active sessions | |
| 3.19 | <- ALL back button | Click "<- ALL" in feed header -> deselects session, returns to multi-session live feed | |
| 3.20 | Agent -> navigate arrow | Agent entries show a "->" arrow. Click it -> navigates to the spawned subagent session (matches by closest start time) | |
| 3.21 | Tool summaries | Verify intelligent summaries: Bash shows `$ command`, Read/Write shows file path, Grep shows `/pattern/`, Agent shows description | |
| 3.22 | Tool expand formatting | Expand a Bash tool_use -> shows `# description\n$ command`. Expand file tools -> shows `file: path` with old/new strings | |
| 3.23 | Turn separators | Turn-duration system events render as "Turn completed -- Xs, N messages" separator lines between conversation turns | |
| 3.24 | Thinking message suppression | Assistant messages that are only `[thinking...]` with no content are NOT shown in the feed | |
| 3.25 | Pinned events merge | In single-session mode, errors and agent events from outside the last-200 window still appear (fetched via pinned endpoint) | |
| 3.26 | Multi-session initial populate | On first load (no session selected), feed pre-populates with last ~20 events from each active session, sorted chronologically | |
| 3.27 | Filter empty state | Enable only one filter type that has no events -> "No messages match the current filters" message appears | |
| 3.28 | Loading state | Switch to a new session -> "Loading..." shows briefly, then events render | |
| 3.29 | Error notification on agent error `[LIVE]` | When an error event arrives via WebSocket, browser notification fires (if error notifications enabled and permissions granted) | |
| 3.30 | All filter toggle | Click "ALL" button: if all types on -> turns all off. If any off -> turns all on | |

---

## Section 4: Graph View

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 4.1 | View toggle | Press `g` or click GRAPH button -> switches to graph view. Canvas renders | |
| 4.2 | Node rendering | Each session appears as a node. Node size proportional to cost (log scale) | |
| 4.3 | Node colors | Active sessions colored by status: green=active, yellow=thinking, blue=tool_use, gray=idle | |
| 4.4 | Edge rendering | Parent-child relationships shown as connecting lines between nodes | |
| 4.5 | Force-directed layout | Nodes spread out via physics simulation, don't overlap heavily, settle after animation | |
| 4.6 | Hover tooltip | Hover over node -> tooltip shows session name, cost, message count, status, model | |
| 4.7 | Click to select | Click a node -> session selected in sidebar, feed switches to that session, view switches to list | |
| 4.8 | Drag interaction | Click and drag a node -> repositions it, other nodes react via physics | |
| 4.9 | Idle detection | After nodes settle, animation stops (no continuous CPU burn). Verify via dev tools Performance tab | |
| 4.10 | Visibility threshold | Only sessions active or with activity in last 120s shown as nodes. Older sessions not rendered | |
| 4.11 | Parent inclusion | If a child session is visible but its parent is older than 120s, parent is still included as a node | |
| 4.12 | Node label truncation | Labels truncated to 16 characters | |
| 4.13 | Cost label below node | Sessions with cost > $0.01 show cost below the name label | |
| 4.14 | Hover glow effect | Hovered node gets white border stroke and slightly larger radius | |
| 4.15 | Click switches to list view | Clicking a graph node sets the selected session AND switches view to list | |
| 4.16 | Drag restarts animation | After graph settles (animation stopped), dragging a node restarts the physics simulation | |

---

## Section 5: Sequence View

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 5.1 | Sequence sub-mode | Within graph view, click "SEQUENCE" toggle -> shows sorted list instead of canvas | |
| 5.2 | Session entries | Each entry shows status dot, session name, cost, status text, start time | |
| 5.3 | Subagent indentation | Child sessions indented with `--` connector under their parent | |
| 5.4 | Click to select | Click a session in sequence -> selects it, switches to list view, feed loads | |
| 5.5 | Temporal ordering | Sessions sorted by startedAt ascending | |
| 5.6 | Visibility threshold | Only shows sessions active or with activity in last 120 seconds | |
| 5.7 | Empty state | No active/recent sessions -> shows "NO ACTIVE SESSIONS" message | |
| 5.8 | Click switches to list view | Clicking a sequence entry sets selected session AND switches to list view | |
| 5.9 | Status text | Active sessions show status (thinking, tool use, active). Done sessions show "done" | |

---

## Section 6: History View

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 6.1 | View toggle | Press `h` or click HISTORY button -> switches to history table view | |
| 6.2 | Column rendering | Table shows columns: Date, Name, Cost, Duration, Tokens, Cache%, Msgs, Errors, Model | |
| 6.3 | Sort by column | Click each column header -> sorts ascending. Click again -> descending. Arrow indicator on active sort column | |
| 6.4 | Sort correctness | Sort by Cost descending -> most expensive sessions at top. Sort by Date -> chronological order | |
| 6.5 | Trivial session exclusion | Sessions with cost=0, tokens=0, messages<4 excluded from table | |
| 6.6 | Parent/child grouping | Top-level sessions visible. Subagents nested under their parent with collapsible hierarchy | |
| 6.7 | Show subagents toggle | "MINIMIZE ALL" checkbox in toolbar. Checked -> children hidden. Unchecked -> children visible indented under parents | |
| 6.8 | CSV export | Click EXPORT CSV button -> downloads CSV file. Open CSV -> data matches table contents and column order | |
| 6.9 | Session selection | Click a row -> selects that session, switches to list view, feed panel updates | |
| 6.10 | Data loading | With 1.4GB of data, verify table loads without hanging (fetches max 200 sessions) | |
| 6.11 | Auto-refresh | When new sessions arrive via WebSocket, history table updates (debounced 5s) | |
| 6.12 | Disclosure triangles | Parents with children show triangles. Click triangle -> toggles expand/collapse | |
| 6.13 | Subagent badge when collapsed | Collapsed parent shows "(N subagents, $X.XX)" summary badge after name | |
| 6.14 | Individual expand overrides global | With MINIMIZE ALL checked, click individual triangle -> that parent expands, switches to per-parent collapse mode | |
| 6.15 | 200 session limit | History fetches max 200 sessions. No broken display at the boundary | |
| 6.16 | Name cell tooltip | Hovering a Name cell shows full taskDescription as tooltip | |
| 6.17 | Clicking row switches to list | Click a history row -> selects session AND switches view to list | |
| 6.18 | Orphan children | Child sessions whose parent isn't in the 200-row result set render as top-level rows | |
| 6.19 | Sort by all column types | Verify sort works for every column: string (Name, Model), numeric (Cost, Duration, Tokens, Msgs, Errors), percent (Cache%), date (Date) | |

---

## Section 7: Timeline View

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 7.1 | Timeline activation | Select a session -> click TIMELINE button in feed header -> canvas timeline renders | |
| 7.2 | Lane layout | 3 lanes visible: User, Assistant, Tools — labeled on the left | |
| 7.3 | Time axis | Horizontal time axis at top with duration labels and vertical grid lines | |
| 7.4 | Event coloring | Events colored by type: user=blue, assistant=green, tool_use=yellow, tool_result=cyan, hook=purple, error=red | |
| 7.5 | Hover tooltip | Hover over an event bar -> tooltip shows timestamp, type, tool name, content preview, cost | |
| 7.6 | Zoom | Ctrl+wheel (or Cmd+wheel) zooms in/out. Point under cursor stays stable during zoom. Time axis scale updates | |
| 7.7 | Pan | Regular mouse wheel scrolls horizontally. Click+drag also pans | |
| 7.8 | Long session handling | Select a session with many events -> timeline renders without lag, zoom/pan still responsive | |
| 7.9 | Auto-fit on open | Timeline auto-calculates zoom level to fit all events in view on initial open | |
| 7.10 | Span grouping | Consecutive same-lane events within 2s gap are grouped into bars. Bar label shows tool name + event count | |
| 7.11 | Minimum bar width | Very short events still have minimum 4px width, don't become invisible | |
| 7.12 | Bar labels | Bars wider than 40px show label text inside them. Narrower bars have no label | |
| 7.13 | Time axis grid lines | Vertical grid lines at regular intervals with duration labels at top | |

---

## Section 8: Replay

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 8.1 | Replay open | Select a non-live session -> click REPLAY button in feed header -> replay panel opens, loads manifest | |
| 8.2 | Play/Pause | Press Space or click PLAY -> events play forward in sequence. Press again -> pauses. Button text toggles | |
| 8.3 | Forward/Backward | Arrow keys step one event at a time in each direction | |
| 8.4 | Speed control | Cycle through 0.5x / 1x / 2x / 4x speeds via dropdown. Playback rate changes visibly | |
| 8.5 | Scrubber | Drag slider -> changes position index. Counter updates (e.g., "42 / 200") | |
| 8.6 | Restart | Press R or click restart button -> jumps back to first event, shows "PRESS PLAY TO BEGIN" | |
| 8.7 | Event rendering | Events render in feed-like format with correct type badges and content | |
| 8.8 | Close replay | Click X button -> replay panel removed, returns to normal feed view, state cleaned up | |
| 8.9 | Initial placeholder | Before first play, feed area shows "PRESS PLAY TO BEGIN" | |
| 8.10 | Step backward removes events | Arrow left removes the last rendered event from the DOM visually | |
| 8.11 | Scrub stops playback | Dragging scrubber while playing -> playback stops, button changes to PLAY | |
| 8.12 | Speed affects playback rate | 4x is noticeably faster than 0.5x. Verify by timing a few events at each speed | |
| 8.13 | Auto-stop at end | Playback automatically stops when reaching the last event. Button changes back to "PLAY" | |

---

## Section 9: Search

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 9.1 | Search activation | Click search box in topbar or press `/` -> search input gains focus | |
| 9.2 | Quick search | Type a query -> results appear in dropdown grouped by session after brief debounce | |
| 9.3 | Full-text search | Results show event previews with type badges and timestamps | |
| 9.4 | Result selection | Click a search result -> navigates to that session, search clears and closes | |
| 9.5 | Empty query | Clear search input -> dropdown closes | |
| 9.6 | No results | Search for gibberish -> shows "No results for ..." message, doesn't error | |
| 9.7 | `/` key focuses search | Press `/` when not in an input -> search input gains focus. Listed in help overlay | |
| 9.8 | Search debounce | Type quickly -> only one API call fires after 300ms pause, not one per keystroke | |
| 9.9 | Search result grouping | Results grouped by session, max 3 shown per group. "X more matches in this session" overflow shown | |
| 9.10 | Search result highlighting | Query terms highlighted with yellow `<mark>` in result previews | |
| 9.11 | Search result badges | Each result shows type badge (tool, error, user, assistant) and timestamp | |

---

## Section 10: Cost Calculations & Stats

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 10.1 | Topbar stats | Topbar shows: ACTIVE count, TOTAL SPEND, CACHE HIT %, $/MIN rate | |
| 10.2 | Stats window toggle | Click TODAY / WEEK / MONTH / ALL buttons -> stats update, cost changes appropriately | |
| 10.3 | Cost breakdown popover | Click cost value -> popover opens with donut chart by model, token bar charts, top 5 sessions | |
| 10.4 | Cost tiers | Verify visual styling: sessions <$0.50 (low/green), <$2 (mid), <$5 (high), >=$5 (extreme/red) | |
| 10.5 | Per-session cost | Select a session -> cost shown on card. Verify it's a reasonable sum of event costs | |
| 10.6 | Cost rate `[LIVE]` | Active sessions show $/min rate. Verify it's reasonable (not NaN, not negative) | |
| 10.7 | Token breakdown | Cost breakdown shows input/output/cache bar charts with formatted totals (k/M) | |
| 10.8 | Cost by model | Donut chart shows correct breakdown per model (opus, sonnet, haiku) | |
| 10.9 | Budget threshold | Set a budget threshold via gear -> banner appears when exceeded. Dismiss -> stays dismissed | |
| 10.10 | Budget notifications | Enable budget notification checkbox -> browser notification fires when budget exceeded | |
| 10.11 | Stats window persistence | Change window to MONTH -> refresh page -> still MONTH (localStorage) | |
| 10.12 | Budget gear button | Click gear icon next to TOTAL SPEND -> budget settings popover opens (separate from cost breakdown) | |
| 10.13 | Cost breakdown donut chart | Canvas-rendered donut chart with colored slices per model. Center shows total cost as integer | |
| 10.14 | Cost breakdown legend | Legend next to donut shows model short names, dollar costs, and percentages | |
| 10.15 | Cost stat click opens breakdown | Click the cost VALUE (not gear) -> cost breakdown popover opens | |
| 10.16 | Gear click opens budget settings | Click the gear button -> budget settings popover opens with threshold input + notification toggles | |
| 10.17 | Mutual popover dismissal | Opening cost breakdown closes budget popover and vice versa | |
| 10.18 | Outside click closes popovers | Click anywhere outside cost breakdown or budget popover -> popover closes | |
| 10.19 | Stat flash animation | When a stat value changes (e.g., cost updates), it briefly flashes | |
| 10.20 | Stats 5s refresh | Stats refresh every 5 seconds automatically (verify via network tab) | |
| 10.21 | Error notification toggle | Budget popover has "Agent errored" checkbox. Enable -> errors trigger browser notifications. Disable -> they don't | |
| 10.22 | Budget set/clear | Enter budget value -> click "Set" -> threshold saved. Click "Clear" -> threshold removed, banner hidden | |

---

## Section 11: Active Session Monitoring

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 11.1 | Active count accuracy `[LIVE]` | Topbar "ACTIVE" count matches actual number of top-level sessions with activity in last 45s | |
| 11.2 | Status transitions `[LIVE]` | Active session goes idle -> dot changes from green/yellow/blue to gray within ~45s | |
| 11.3 | Stale parent detection | Parent session stale but has active children -> shows 'waiting' status | |
| 11.4 | WebSocket connectivity | Page connected to WebSocket. Status bar shows green dot + "CONNECTED" | |
| 11.5 | WebSocket reconnect | Kill and restart backend -> frontend reconnects automatically, re-syncs full session state | |
| 11.6 | New session appearance `[LIVE]` | Start a new Claude session -> it appears in session list under "Active" group automatically | |
| 11.7 | Multi-session feed `[LIVE]` | With no session selected, live events from multiple active sessions interleave correctly in feed | |
| 11.8 | Session completion `[LIVE]` | Active session finishes -> moves from Active group to appropriate time group (Last hour / Today) | |

---

## Section 12: Keyboard Shortcuts & Navigation

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 12.1 | Help overlay | Press `?` -> help overlay opens showing all keyboard shortcuts. Click outside or Esc closes it | |
| 12.2 | View toggles | `g` -> graph (toggle), `h` -> history (toggle). Pressing same key again returns to list | |
| 12.3 | Escape behavior | Esc closes: help overlay, cost breakdown, budget popover, deselects session, closes search (priority order) | |
| 12.4 | Session navigation | Arrow up/down navigates sessions. Enter selects. Left/right expands/collapses children | |
| 12.5 | Filter shortcuts | `1` / `2` / `3` switch active/recent/all filters in session list | |
| 12.6 | Replay controls | Space=play/pause, R=restart, arrows=step (when replay is open) | |
| 12.7 | `t` key for table view | Press `t` -> table view (toggle). Press `t` again -> return to list view | |
| 12.8 | Shortcuts disabled in form controls | While focused in search input, textarea, repo `<select>`, or other editable controls, shortcuts (`g`, `h`, `a`, `t`, `1`, `2`, `3`) should not trigger view/filter switches | |
| 12.9 | `/` to focus search | Press `/` outside of input -> search box focuses (handler in topbar.ts) | |
| 12.10 | Search Escape | While typing in search box, press Escape -> clears search text, closes dropdown, blurs input | |
| 12.11 | Space on focused card | Space bar on a focused session card -> selects it (via keydown handler). Does NOT trigger page scroll | |
| 12.12 | Enter toggles selection | Enter on already-selected session -> deselects it (toggle behavior) | |
| 12.13 | Replay keyboard scoping | During replay: Space=play/pause, arrows=step. Without replay: Space does nothing, arrows navigate sessions | |

---

## Section 13: URL Routing & Hash State

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 13.1 | Hash routing | Select a session -> URL hash updates to `#session=<id>`. Refresh page -> same session re-selected | |
| 13.2 | View in hash | Switch to graph -> hash shows `#view=graph`. Switch to history -> `#view=history`. List view omits view param | |
| 13.3 | Direct URL navigation | Manually type a session hash -> page loads with that session selected | |
| 13.4 | Invalid hash | Navigate to a non-existent session hash -> graceful fallback (no crash, shows list) | |
| 13.5 | Browser back/forward | Select session A -> select session B -> browser back -> session A re-selected | |

---

## Section 14: Status Bar

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 14.1 | Connection indicator | Green dot + "CONNECTED" when WebSocket is live. Red dot + "DISCONNECTED" when connection drops | |
| 14.2 | Host display | Shows current host (e.g., localhost:7700) | |
| 14.3 | Event count `[LIVE]` | Counter increments as WebSocket events arrive in real-time | |
| 14.4 | Version display | Shows "CLAUDE MONITOR dev" (or real version string) | |

---

## Section 15: Responsive / Hamburger Menu

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 15.1 | Hamburger visibility | At narrow viewport widths, hamburger button appears in topbar | |
| 15.2 | Collapsible toggle | Click hamburger -> stats, search, view toggle slide open. Click again -> collapse | |
| 15.3 | Escape closes menu | With hamburger menu open, Esc closes it | |
| 15.4 | aria-expanded | Hamburger button aria-expanded toggles between "true" and "false" correctly | |
| 15.5 | Sidebar at narrow width | Session list behavior when viewport is narrow — does it remain usable or collapse? | |

---

## Section 16: Persistence & localStorage

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 16.1 | Stats window persists | Set stats to "WEEK" -> refresh page -> still shows "WEEK" (key: `claude-monitor-stats-window`) | |
| 16.2 | Onboarding dismissed | First visit shows welcome tip with keyboard hints. Click "Got it" -> refresh -> tip doesn't reappear (key: `claude-monitor-onboarded`) | |
| 16.3 | Onboarding auto-dismiss | If not clicked, welcome tip auto-dismisses after 15 seconds | |
| 16.4 | Budget settings persist | Set budget threshold -> refresh -> threshold still set (key: `budget`) | |
| 16.5 | Update banner dismiss | Dismiss update banner -> persists for session only (sessionStorage `update-dismissed`). New tab -> banner reappears | |

---

## Section 17: Error Handling & Edge Cases

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 17.1 | Backend down on load | Stop backend, load page -> error banner: "Failed to load sessions. Check that the server is running and try refreshing." | |
| 17.2 | Long session names | Sessions with very long names -> truncated with ellipsis, not breaking layout | |
| 17.3 | Zero-event sessions | Select a session with 0 or very few events -> feed shows gracefully (empty state or minimal content) | |
| 17.4 | High error count | Sessions with many errors -> error count badge displays correctly, isn't clipped | |
| 17.5 | Cost formatting | Verify formatting across ranges: tiny costs ($0.00), normal ($1.42), expensive ($15.83) — no NaN, no negative | |
| 17.6 | Duration formatting | Short sessions (seconds), medium (minutes), long (hours) all display readable durations | |
| 17.7 | Cache% edge cases | 0% cache hit and ~100% cache hit both render correctly, no divide-by-zero | |
| 17.8 | Empty database state | (If testable) No sessions at all -> page loads without crash, shows empty feed placeholder | |
| 17.9 | Search error handling | Search fails (e.g., server connection issue) -> shows "Search failed -- check server connection", doesn't crash | |
| 17.10 | WebSocket parse error | Malformed WS message -> logged to console, doesn't crash UI | |
| 17.11 | Feed load failure | If fetching session events fails -> shows "Failed to load messages" error state in feed | |
| 17.12 | History load failure | If fetching history sessions fails -> console error logged, view doesn't crash | |
| 17.13 | Replay load failure | If replay manifest fetch fails -> console error, replay doesn't crash | |
| 17.14 | Timeline load failure | If timeline events fetch fails -> console error, timeline shows blank canvas gracefully | |
| 17.15 | Internal XML tag stripping | Session taskDescription containing internal XML tags (e.g., `<system-reminder>`) -> tags stripped from display, not shown raw | |

---

## Section 18: Cross-Feature Interactions

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 18.1 | Selected session across view switches | Select a session in list -> switch to graph (g) -> session still highlighted as selected node. Switch to history (h) -> row highlighted | |
| 18.2 | Filter state across views | Set "ACTIVE" filter in list -> switch to graph -> switch back to list -> still on ACTIVE filter | |
| 18.3 | Search -> session selection | Search for text -> click result -> search closes, session selected, feed loads that session's events | |
| 18.4 | Replay vs. keyboard scoping | Open replay -> Space plays/pauses replay (not triggering other actions). Close replay -> Space no longer captured | |
| 18.5 | Replay vs. arrow keys | During replay, left/right step through events. Without replay, left/right expand/collapse session children | |
| 18.6 | Graph click -> feed update | Click a node in graph -> view switches to list, feed panel loads that session's events | |
| 18.7 | History row -> replay | Select session from history -> switch to list view -> open replay from feed header -> replay loads correctly, close returns to feed | |
| 18.8 | Rapid view switching | Quickly press g h g h -> no rendering glitches, canvas cleanup, no memory leaks | |
| 18.9 | Window resize with canvas | Resize browser while graph or timeline is visible -> canvas resizes, content doesn't clip or distort | |

---

## Section 19: Data Integrity & Rendering Accuracy

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 19.1 | Token counts match | Select a session -> card shows total tokens. Verify this is sum of input + output + cache read + cache creation | |
| 19.2 | Cache% calculation | cache% = cacheReadTokens / (inputTokens + cacheReadTokens + cacheCreationTokens) * 100. Verify formula matches display | |
| 19.3 | Cost tier styling | Pick sessions at different cost levels and verify: <$0.50 green, <$2 yellow-ish, <$5 orange-ish, >=$5 red | |
| 19.4 | Duration calculation | Duration = lastActive - startedAt. Verify displayed duration matches for a known session | |
| 19.5 | timeAgo display | Verify "Xm ago", "Xh ago" labels are accurate relative to current time | |
| 19.6 | Token formatting | Large numbers formatted as K/M: 1500 -> "1.5k", 2000000 -> "2.0M" | |
| 19.7 | Cost breakdown bar percentages | Token bars in cost breakdown are proportional. Input + Output + Cache widths are roughly correct | |
| 19.8 | Top 5 sessions accuracy | Cost breakdown top 5 sessions are actually the 5 most expensive sessions | |

---

## Section 20: Canvas HiDPI & Rendering

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 20.1 | Canvas DPR scaling | Canvas renders at correct pixel density — text and shapes not blurry (check via devicePixelRatio) | |
| 20.2 | Graph resize handler | Resize browser window while graph is visible -> canvas resizes, nodes reposition within bounds | |
| 20.3 | Timeline resize handler | Resize browser window while timeline is visible -> canvas resizes, events still visible | |
| 20.4 | Graph cleanup on view switch | Switch away from graph -> animation stops, resize listener removed (verify no background CPU burn via Performance tab) | |
| 20.5 | Timeline cleanup | Close timeline or switch away -> no lingering event listeners or animation frames | |

---

## Section 21: Browser Navigation

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 21.1 | Hash format | Select session + switch view -> URL shows `#session=<id>&view=graph` (or just `#session=<id>` for list) | |
| 21.2 | Back/forward navigation | Select session A -> select session B -> browser back -> session A re-selected | |
| 21.3 | Hash debounce | Rapid session switches don't spam browser history entries (200ms debounce on hash writes) | |
| 21.4 | List view omitted from hash | List view is default — switching to list view removes `view=` from hash (clean URL) | |
| 21.5 | Deep link on fresh load | Copy URL with `#session=<id>&view=history` -> paste in new tab -> loads with that session selected and history view active | |

---

## Section 22: Concurrent State & Race Conditions

| # | Test Case | What to verify | Status |
|---|-----------|----------------|--------|
| 22.1 | Rapid session switching | Click 5+ different sessions quickly -> feed loads the LAST selected session, no stale data from earlier selections | |
| 22.2 | View switch during feed load | Select a session -> immediately press `g` for graph -> no crash, feed load cancelled cleanly | |
| 22.3 | WebSocket events during session load `[LIVE]` | While loading a session's events, new WebSocket events arrive -> they appear after load completes, no duplicates | |
| 22.4 | Search while switching sessions | Type a search while switching sessions -> both resolve without interfering | |
| 22.5 | Multiple rapid filter toggles | Click feed filter buttons rapidly -> final state is consistent, no rendering glitches | |

---

## Summary

| Section | Count |
|---------|-------|
| 2. Session List | 22 |
| 3. Feed Panel | 30 |
| 4. Graph View | 16 |
| 5. Sequence View | 9 |
| 6. History View | 19 |
| 7. Timeline View | 13 |
| 8. Replay | 13 |
| 9. Search | 11 |
| 10. Cost/Stats | 22 |
| 11. Active Monitoring | 8 |
| 12. Keyboard | 13 |
| 13. URL Routing | 5 |
| 14. Status Bar | 4 |
| 15. Responsive | 5 |
| 16. Persistence | 5 |
| 17. Error Handling | 15 |
| 18. Cross-Feature | 9 |
| 19. Data Integrity | 8 |
| 20. Canvas/HiDPI | 5 |
| 21. Browser Nav | 5 |
| 22. Race Conditions | 5 |
| **TOTAL** | **242** |

### Known potential bugs to investigate

- **12.7**: Help overlay lists `t` as "Table view" shortcut, but `main.ts` has no handler for `t` — only `h` for history. Either the help overlay is wrong or the handler is missing.

### Test data requirements

- Historical sessions with various cost levels ($0 to $10+)
- Sessions with subagents (parent-child relationships)
- Sessions with errors
- Sessions with many events (>200 for pinned merge testing)
- At least one active session for `[LIVE]` tests
- Sessions across multiple repos (for costByRepo breakdown)
