# Dashboard Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Comprehensive dashboard refresh implementing all 4 design reviewers' suggestions — visual, UX, information architecture, and product features.

**Architecture:** Incremental batches, each producing a working commit. Backend changes in Go (session model, API routes, health detection). Frontend changes in single-file `static/index.html`. Sync to `cmd/claude-monitor/static/index.html` after each batch.

**Tech Stack:** Go 1.22+, vanilla JS, WebSocket, SSE, CSS custom properties, SQLite (for historical persistence).

---

## Batch 1: Quick Visual & UX Wins

### Task 1.1: Show current tool/action on session cards

**Files:**
- Modify: `static/index.html` (session card HTML template in `_renderSessionsNow`, ~line 2096)

- [ ] In `_renderSessionsNow()`, after the status badge, add a line showing `sessionState[sess.id]?.toolName` and tool detail. Display like "Edit: src/main.ts" or "Bash: npm test" in a dim, truncated single line beneath the stats row.
- [ ] Ensure `updateSessionState()` tracks `toolDetail` from the message (it already tracks `toolName`).
- [ ] Sync HTML, build, test, commit.

### Task 1.2: Soften background, desaturate accents, increase feed spacing

**Files:**
- Modify: `static/index.html` (CSS variables ~line 10-26, feed entry styles ~line 417)

- [ ] Change `--bg` from `#0a0a0f` to `#121218`.
- [ ] Desaturate accent colors ~20-30%: `--green: #33dd99`, `--blue: #5588ff`, `--cyan: #44cccc`, `--yellow: #ddcc44`, `--red: #dd4455`, `--purple: #aa77dd`, `--orange: #dd8844`.
- [ ] Increase feed entry padding from `2px 12px` to `5px 12px`.
- [ ] Add subtle separator: `.feed-entry { border-bottom: 1px solid rgba(255,255,255,0.03); }`.
- [ ] Widen left type border from 2px to 3px.
- [ ] Add `--font-prose` variable with `'Inter', system-ui, sans-serif` and apply to `.fe-content`, `.fe-full-content`.
- [ ] Sync HTML, build, test, commit.

### Task 1.3: URL hash state persistence

**Files:**
- Modify: `static/index.html` (add hashchange handling, update setSessionFilter/toggleSession/setView)

- [ ] Add `updateHash()` function that writes `#session=<id>&filter=<f>&view=<v>` to `location.hash`.
- [ ] Call `updateHash()` from `toggleSession()`, `setSessionFilter()`, `setView()`.
- [ ] Add `restoreFromHash()` function that parses hash on page load and restores state.
- [ ] Call `restoreFromHash()` after `loadInitialSessions()`.
- [ ] Sync HTML, build, test, commit.

### Task 1.4: Multi-session feed when nothing selected

**Files:**
- Modify: `static/index.html` (modify `handleWsMessage` and feed rendering)

- [ ] When `state.selectedSession === null`, `addFeedEntry()` should still add entries to the feed, tagged with session name.
- [ ] Show session name prefix in feed entries when in multi-session mode (use `.fe-sid` column).
- [ ] On session deselect, don't clear feed — switch to multi-session mode showing recent entries across all sessions.
- [ ] Sync HTML, build, test, commit.

### Task 1.5: Remove inline sprites from session list, keep in graph

**Files:**
- Modify: `static/index.html` (session card template in `_renderSessionsNow`)

- [ ] Remove the `.inline-sprite-wrap` div from session card HTML template.
- [ ] Keep the status dot (`.session-dot`) as the card's status indicator.
- [ ] Reclaim the 40px horizontal space for wider session info.
- [ ] Sync HTML, build, test, commit.

---

## Batch 2: Error Visibility & Session Card Improvements

### Task 2.1: Track error count per session (backend)

**Files:**
- Modify: `internal/session/session.go` (add `ErrorCount` field)
- Modify: `internal/parser/parser.go` (detect errors in parsed messages)
- Modify: `cmd/claude-monitor/main.go` (increment error count in processEvent)
- Modify: `internal/session/session_test.go`

- [ ] Add `ErrorCount int` field to `Session` struct with json tag `errorCount`.
- [ ] In parser, add `IsError bool` field to `ParsedMessage` — set true when content contains error patterns (tool_result with `is_error: true`, or type contains "error").
- [ ] In `processEvent`, increment `s.ErrorCount` when `msg.IsError`.
- [ ] Add test for error detection.
- [ ] Commit.

### Task 2.2: Error count in top bar + error badges on session cards

**Files:**
- Modify: `static/index.html` (top bar HTML, `_updateTopBarNow`, session card template)

- [ ] Add ERROR stat to top bar: `<div class="topbar-stat"><span>ERRORS</span><span class="val red" id="stat-errors">0</span></div>`.
- [ ] In `_updateTopBarNow()`, compute total error count across active sessions.
- [ ] In session card template, show red error badge when `sess.errorCount > 0`.
- [ ] Sync HTML, build, test, commit.

### Task 2.3: Move TIMELINE/REPLAY buttons to feed panel header

**Files:**
- Modify: `static/index.html` (session card template, feed panel header)

- [ ] Remove TIMELINE and REPLAY buttons from session card HTML.
- [ ] Add them to the feed panel header area, visible only when a session is selected.
- [ ] Sync HTML, build, test, commit.

### Task 2.4: Replace ACTIVE COST with working/idle breakdown

**Files:**
- Modify: `static/index.html` (top bar HTML and `_updateTopBarNow`)

- [ ] Replace ACTIVE COST stat with "WORKING" count — sessions with status `thinking` or `tool_use`.
- [ ] Sync HTML, build, test, commit.

---

## Batch 3: Stuck Agent Detection & Actionability

### Task 3.1: Stuck detection (backend)

**Files:**
- Modify: `internal/session/session.go` (add health fields)
- Modify: `cmd/claude-monitor/main.go` (detect stuck/looping)

- [ ] Add fields to Session: `StatusSince time.Time`, `RecentToolCalls []string` (last 10 tool names, json:"-"), `IsStuck bool`.
- [ ] In `processEvent`, update `StatusSince` when status changes. Track last 10 tool call names in a ring buffer.
- [ ] Add `checkHealth()` method on Store that marks sessions stuck if: status unchanged for >3 minutes while active, OR last 10 tool calls are identical.
- [ ] Call `checkHealth()` from a periodic goroutine (every 30s).
- [ ] Broadcast `session_update` events when stuck status changes.
- [ ] Add tests.
- [ ] Commit.

### Task 3.2: Stuck indicators in UI

**Files:**
- Modify: `static/index.html` (session card template, top bar)

- [ ] Show "STUCK" badge with red pulsing animation when `sess.isStuck`.
- [ ] Auto-sort stuck sessions to top of list.
- [ ] Add stuck count to top bar errors area.
- [ ] Sync HTML, build, test, commit.

### Task 3.3: Stop/kill button for agents

**Files:**
- Modify: `static/index.html` (feed panel header)
- Modify: `cmd/claude-monitor/main.go` (add kill API route)
- Modify: `internal/docker/docker.go` (add container stop method)

- [ ] Add `DELETE /api/sessions/{id}` route that attempts to stop the agent. For Docker containers, call `docker stop`. For local sessions, send SIGTERM to process.
- [ ] Add `StopContainer(ctx, containerName)` to docker Client.
- [ ] In UI, show a red "STOP" button in the feed panel header for active sessions.
- [ ] Confirm dialog before stopping.
- [ ] Sync HTML, build, test, commit.

---

## Batch 4: Keyboard Navigation & Interaction

### Task 4.1: Arrow key navigation for session list

**Files:**
- Modify: `static/index.html` (keyboard handler, session rendering)

- [ ] Track `state.focusedSession` (separate from `selectedSession`).
- [ ] Up/Down arrows move focus through visible session list.
- [ ] Enter selects the focused session.
- [ ] Right arrow expands subagents, Left collapses.
- [ ] Show focus ring (dotted border) distinct from selection (solid border).
- [ ] Sync HTML, build, test, commit.

### Task 4.2: Replay keyboard controls

**Files:**
- Modify: `static/index.html` (keyboard handler)

- [ ] When replay panel is visible: Space = play/pause, R = restart, Left/Right = step -1/+1 event.
- [ ] Update help overlay with replay shortcuts.
- [ ] Sync HTML, build, test, commit.

### Task 4.3: Feed filter solo mode + ALL toggle

**Files:**
- Modify: `static/index.html` (feed filter buttons)

- [ ] Shift+click on a filter button solos it (disables all others, enables only clicked one). Shift+click again restores previous state.
- [ ] Add "ALL" button at start of filter row to enable/disable all.
- [ ] Sync HTML, build, test, commit.

### Task 4.4: Search improvements — highlighting and back-to-results

**Files:**
- Modify: `static/index.html` (search rendering)

- [ ] Highlight matched text in search results using `<mark>` tags.
- [ ] After navigating to a session from search, show a "Back to results" button.
- [ ] Keep search query in input after navigation.
- [ ] Sync HTML, build, test, commit.

### Task 4.5: Session selection loading state

**Files:**
- Modify: `static/index.html` (`toggleSession` function)

- [ ] Instead of clearing feed immediately on session select, show "Loading..." placeholder.
- [ ] Replace with actual content when fetch completes.
- [ ] Show error state with retry button on fetch failure.
- [ ] Sync HTML, build, test, commit.

---

## Batch 5: Responsive Layout & Accessibility

### Task 5.1: Responsive breakpoints

**Files:**
- Modify: `static/index.html` (CSS)

- [ ] Add `@media (max-width: 1024px)`: collapse sessions panel to overlay drawer with toggle button.
- [ ] Add `@media (max-width: 768px)`: stack layout vertically, session list as horizontal scrollable strip at top.
- [ ] Ensure feed panel takes full width on narrow screens.
- [ ] Sync HTML, build, test, commit.

### Task 5.2: Focus states and accessibility

**Files:**
- Modify: `static/index.html` (CSS)

- [ ] Add `:focus-visible` outlines to all interactive elements (filter buttons, session cards, view toggle, feed filter buttons).
- [ ] Remove `user-select: none` from timestamps and type labels.
- [ ] Sync HTML, build, test, commit.

### Task 5.3: Cost velocity metric + sparklines

**Files:**
- Modify: `internal/session/session.go` (add CostRate field)
- Modify: `cmd/claude-monitor/main.go` (compute cost rate)
- Modify: `static/index.html` (top bar, session cards)

- [ ] Add `CostRate float64` ($/min) to Session, computed from `TotalCost / duration`.
- [ ] Show cost rate in top bar as aggregate across active sessions.
- [ ] Show per-session cost rate more prominently on session cards.
- [ ] Sync HTML, build, test, commit.

---

## Batch 6: Historical Persistence & Advanced Features

### Task 6.1: SQLite persistence layer

**Files:**
- Create: `internal/store/sqlite.go`
- Create: `internal/store/sqlite_test.go`
- Modify: `cmd/claude-monitor/main.go` (initialize DB, save session summaries)
- Modify: `go.mod` (add sqlite dependency)

- [ ] Add `modernc.org/sqlite` dependency (pure Go, no CGO).
- [ ] Create `store` package with `DB` type wrapping SQLite.
- [ ] Schema: `sessions` table with id, project_name, session_name, total_cost, input_tokens, output_tokens, message_count, started_at, ended_at, duration_seconds, outcome, model, cwd, git_branch.
- [ ] `SaveSession(session)` upserts a session summary.
- [ ] `ListHistory(limit, offset)` returns historical sessions.
- [ ] In main.go, save session summary when session transitions to inactive.
- [ ] Add tests.
- [ ] Commit.

### Task 6.2: History API route and UI

**Files:**
- Modify: `cmd/claude-monitor/main.go` (add /api/history route)
- Modify: `static/index.html` (add history view)

- [ ] Add `GET /api/history?limit=50&offset=0` route returning historical session list.
- [ ] Add "History" view toggle (alongside List/Graph).
- [ ] Show sortable table: date, name, cost, duration, tokens, outcome.
- [ ] Click a row to open replay for that session.
- [ ] Sync HTML, build, test, commit.

### Task 6.3: Outcome tracking

**Files:**
- Modify: `internal/session/session.go` (add Outcome field)
- Modify: `cmd/claude-monitor/main.go` (detect outcomes)

- [ ] Add `Outcome string` to Session (success, error, abandoned, running).
- [ ] Detect error: last message was an error or session has high error count.
- [ ] Detect success: session went idle after `end_turn` with no errors.
- [ ] Detect abandoned: session inactive for >5 min without clean end.
- [ ] Show outcome badge on session cards and in history view.
- [ ] Add tests.
- [ ] Commit.

### Task 6.4: Browser notifications

**Files:**
- Modify: `static/index.html`

- [ ] Request Notification permission on page load (if not already granted).
- [ ] Send browser notification for: budget exceeded, agent stuck, agent errored, all agents completed.
- [ ] Add notification preferences (checkboxes in a settings panel or budget popover).
- [ ] Sync HTML, build, test, commit.

### Task 6.5: Comparison table view

**Files:**
- Modify: `static/index.html`

- [ ] Add "Table" view alongside List/Graph/History.
- [ ] Render all sessions in a dense sortable table: name, status, cost, $/min, duration, tokens, cache%, messages, errors.
- [ ] Click column headers to sort. Click row to select session.
- [ ] Sync HTML, build, test, commit.

### Task 6.6: Task description from initial prompt

**Files:**
- Modify: `internal/session/session.go` (add TaskDescription field)
- Modify: `cmd/claude-monitor/main.go` (extract from first user message)

- [ ] Add `TaskDescription string` to Session.
- [ ] In `processEvent`, when the first `user` role message arrives, capture its ContentText (truncated to 200 chars) as `TaskDescription`.
- [ ] Show on session card as a subtitle beneath the project name.
- [ ] Sync HTML, build, test, commit.
