# Navigation Overhaul — Design Spec

**Date:** 2026-03-25
**Status:** Approved
**Priority:** P1

## Context

Claude Monitor is a real-time observability dashboard for Claude Code sessions. It currently has 455+ sessions visible, but navigation is a flat list with broken search and no time-based browsing. The primary users are solo developers monitoring their own sessions and team leads tracking activity across multiple agents.

### Problems Identified (Playwright Walkthrough)

1. **Search UX broken** — typing in search shows "Searching..." but never renders results inline. The API returns data, the UI doesn't display it.
2. **Session list is flat** — no grouping by time or project. 455 sessions in a single scrollable list.
3. **Session names truncated** — "nanoclaw-discord-general-tas..." with no way to see the full name without clicking.
4. **Help overlay missing** — `?` shortcut doesn't show a help panel.
5. **Mobile/tablet layouts broken** — stats wrap awkwardly, sessions panel disappears, graph view shows 2 floating dots.
6. **Graph view underwhelming** — only shows active sessions, not parent/child relationships from recent sessions.
7. **History view sparse** — only 9 entries despite $3,368 total spend across 455 sessions.
8. **4,100-line single HTML file** — unmaintainable for adding features.

### Key Navigation Patterns

- **Active session triage:** "Which agents are active right now and what are they working on?"
- **Time-based browsing:** "What happened in the last hour / today / this week?"

## Design

### 1. Session List Redesign

The flat session list is replaced with a two-tier, time-grouped layout.

#### Active Now (pinned top section)

- Always visible at the top of the session panel, separated by a subtle divider.
- Expanded cards showing: full session name, status badge (THINKING / TOOL / IDLE), task description (first line of what the agent is working on), cost, duration, live cost rate.
- A session is "active" if it has received an update within the last 30 seconds (matching existing Go backend logic).
- Sorted by most recently active.
- Clicking selects and opens the feed panel (same as today).
- Visual pulse/glow on cards that are actively streaming.

#### Timeline Below

Sessions grouped under collapsible time headers:
- **Last hour**
- **Today** (excluding last hour)
- **Yesterday**
- **This week**
- **Older**

Each group shows a count badge (e.g., "Today (23)"). Groups auto-collapse when they have more than ~15 sessions, with a "Show all" toggle.

Cards in timeline groups are compact — single line: name, cost, duration, model badge. Hover expands to show task description.

#### Filter Bar

The current "Active / Recent / All" filter becomes secondary — time grouping handles the browsing pattern. The filter bar moves to a row of small pills above the timeline, adding a project filter dropdown populated from `projectName` in session data.

### 2. Search Overhaul

#### Search Behavior

- `/` focuses search (preserved).
- As you type, a dropdown panel appears below the search input (command palette style).
- Results stream in from the existing `/api/search` endpoint.
- Debounced — waits 300ms after typing stops before querying.
- `Escape` clears search and closes the dropdown.

#### Result Cards

Each search result shows:
- Session name + project name (dimmed).
- Matched line with the search term highlighted in yellow.
- Timestamp of the matched message.
- Message type badge (user / assistant / tool / error).

Results grouped by session — if multiple hits in one session, show the first 3 with a "N more matches in this session" link.

#### Clicking a Result

- Selects that session.
- Opens the feed panel.
- Scrolls to the matched message (or as close as the feed allows).

#### Empty/No Results States

- No matches: "No results for 'query'" with suggestion to try broader terms.
- Still searching: spinner with "Searching..."
- Error: inline error message.

### 3. Frontend Architecture Migration

The 4,100-line `index.html` is split into a Vite + vanilla TypeScript project.

#### Project Structure

```
web/
├── index.html              # Shell HTML (mount point + status bar)
├── vite.config.ts
├── tsconfig.json
├── package.json
├── src/
│   ├── main.ts             # Entry point, WebSocket setup, router
│   ├── state.ts            # Centralized app state (sessions, filters, search)
│   ├── ws.ts               # WebSocket client with auto-reconnect
│   ├── api.ts              # REST API client (typed fetch wrappers)
│   ├── components/
│   │   ├── topbar.ts       # Top bar stats + search input
│   │   ├── session-list.ts # Two-tier session list (active + timeline)
│   │   ├── session-card.ts # Session card (expanded + compact variants)
│   │   ├── feed-panel.ts   # Live feed / message stream
│   │   ├── search.ts       # Search dropdown + results
│   │   ├── graph-view.ts   # D3 force graph (ported)
│   │   ├── table-view.ts   # Table overlay (ported)
│   │   ├── history-view.ts # History overlay (ported)
│   │   └── replay.ts       # Session replay panel
│   ├── styles/
│   │   ├── base.css        # Variables, reset, typography
│   │   ├── topbar.css
│   │   ├── sessions.css
│   │   ├── feed.css
│   │   └── views.css
│   └── types.ts            # Shared TypeScript interfaces
```

#### Key Decisions

- **No framework** — DOM manipulation stays manual, organized into modules that export `render()` and `update()` functions.
- **State management** — single `state.ts` module exporting a plain object + `subscribe()` function. Components subscribe to state changes.
- **Styling** — CSS files imported per component via Vite. CSS variables preserved from current design (dark theme, colors all stay the same).
- **Build output** — Vite builds to `cmd/claude-monitor/static/dist/`. Go server serves with `go:embed` as today.
- **Dev mode** — `npm run dev` runs Vite dev server with proxy to Go backend on :7700. Hot reload for frontend work.

#### Migration Strategy

Port the existing JS 1:1 into TypeScript modules first — no behavior changes. Then apply the navigation redesign on top. This verifies the port doesn't break anything before adding features.

### 4. Backend API Changes

Minimal additions to support the new frontend.

#### New: Time-bucketed sessions

```
GET /api/sessions/grouped
```

Returns sessions pre-grouped by time bucket:

```json
{
  "active": [...],
  "lastHour": [...],
  "today": [...],
  "yesterday": [...],
  "thisWeek": [...],
  "older": [...]
}
```

Avoids the frontend downloading all 455 sessions and sorting client-side. Each bucket includes the same session data as today's `/api/sessions`.

#### New: Project list

```
GET /api/projects
```

Returns distinct project names with session counts for the project filter dropdown:

```json
[
  {"name": "claude-monitor", "count": 12},
  {"name": "nanoclaw", "count": 43}
]
```

#### Search improvements

The existing `/api/search?q=` endpoint adds:
- `limit` param (default 50, max 200).
- Results include `sessionName` and `projectName` fields.
- Results grouped by `sessionId` in the response.

#### No changes to

- WebSocket protocol.
- `/api/sessions/{id}/recent` and replay endpoints.
- `/health`.
- History/SQLite persistence.
- Docker discovery.

### 5. Release / Build Fixes

The arm64 release binary doesn't work. Several issues found in the release pipeline:

#### Go version mismatch

`go.mod` requires `go 1.25.0` but `.github/workflows/release-please.yml` and `ci.yml` both use `go-version: '1.22'`. Update CI to use the Go version from `go.mod` dynamically:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version-file: 'go.mod'
```

#### Missing CGO_ENABLED=0

The release build doesn't set `CGO_ENABLED=0`. While Go auto-disables CGO for cross-compilation, being explicit ensures fully static binaries regardless of the CI environment:

```
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ...
```

#### Missing version variable

LDFLAGS inject `-X main.version=${VERSION}` but `main.go` has no `var version string`. Add:

```go
var version = "dev"
```

#### Add --version flag and /api/version endpoint

- `claude-monitor --version` prints the version and exits.
- `GET /api/version` returns `{"version": "v1.7.6"}`.
- The status bar in the UI already shows a version string — wire it to the real value.

#### Compressed release assets

Upload `.tar.gz` archives instead of raw binaries. This preserves execute permissions and reduces download size. Include a brief README with usage instructions in each archive.

## Out of Scope

- Cost dashboard with charts and trend analysis (Priority 2, separate spec).
- Agent activity summaries and progress tracking (Priority 3, separate spec).
- Pixel Office animated character experience (separate project).
- Mobile-first responsive redesign (fix critical breakages only).
- Framework adoption (Preact/React) — can be evaluated later if needed.
