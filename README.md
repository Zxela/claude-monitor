<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go 1.25" />
  <img src="https://img.shields.io/badge/TypeScript-5.7-3178C6?logo=typescript&logoColor=white" alt="TypeScript" />
  <img src="https://img.shields.io/badge/Vite-6-646CFF?logo=vite&logoColor=white" alt="Vite" />
  <a href="https://github.com/Zxela/claude-monitor/actions/workflows/ci.yml"><img src="https://github.com/Zxela/claude-monitor/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/Zxela/claude-monitor/releases/latest"><img src="https://img.shields.io/github/v/release/Zxela/claude-monitor?color=green" alt="Release" /></a>
  <img src="https://img.shields.io/github/license/Zxela/claude-monitor" alt="License" />
</p>

<h1 align="center">claude-monitor</h1>

<p align="center">
  Real-time observability dashboard for <a href="https://claude.com/claude-code">Claude Code</a> sessions.<br/>
  Live cost tracking, agent hierarchy, tool execution feeds, session replay, and more.
</p>

---

<p align="center">
  <img src="docs/screenshots/feed.png" alt="Claude Monitor — live feed with active agents" width="100%" />
</p>

## Quick Start

```bash
brew install Zxela/tap/claude-monitor   # or: curl -fsSL https://raw.githubusercontent.com/Zxela/claude-monitor/main/install.sh | sh
claude-monitor                          # start the dashboard
open http://localhost:7700              # view in browser
claude-monitor hook install             # (optional) auto-start with Claude Code
```

## Requirements

- **Go** >= 1.25
- **Node.js** >= 18
- **npm**
- **make**

## Install

### Homebrew (macOS / Linux)

```bash
brew install Zxela/tap/claude-monitor
```

### Shell script

```bash
curl -fsSL https://raw.githubusercontent.com/Zxela/claude-monitor/main/install.sh | sh
```

### Build from source

```bash
git clone https://github.com/Zxela/claude-monitor.git
cd claude-monitor
make build
```

## Usage

```bash
# Start (watches ~/.claude, auto-discovers Docker containers)
claude-monitor

# Open dashboard
open http://localhost:7700

# Custom port + additional watch paths
claude-monitor --port 8080 --watch /path/to/.claude/projects

# API docs (Swagger UI) are always available — no flag needed
# Visit http://localhost:7700/api
```

## Configuration

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `7700` | HTTP listen port |
| `--bind` | `127.0.0.1` | Address to bind to (use `0.0.0.0` for all interfaces) |
| `--broadcast` | `false` | Listen on all interfaces (shorthand for `--bind 0.0.0.0`) |
| `--watch` | — | Additional directory to watch (repeatable) |
| `--docker` | `false` | Auto-discover `.claude/projects` mounts from running Docker containers |
| `--docker-socket` | `/var/run/docker.sock` | Path to Docker socket |
| `--swagger` | `false` | Deprecated no-op (retained for compatibility); API docs are always served at `/api` |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `CLAUDE_MONITOR_NO_UPDATE_CHECK` | Set to `1` or `true` to disable the startup update check |

### Update Notifications

On startup, claude-monitor checks GitHub for newer releases. If an update is available, a banner appears in the web UI with a link to the release page.

To disable the update check:

```bash
CLAUDE_MONITOR_NO_UPDATE_CHECK=1 claude-monitor
```

### Database Migrations

Use the `migrate` subcommand to manage the SQLite schema:

```bash
# Apply pending migrations (also runs automatically on startup)
claude-monitor migrate

# Show current schema version and pending migrations
claude-monitor migrate status

# Roll back the last migration
claude-monitor migrate rollback
```

### Auto-Start with Claude Code

Install a Claude Code hook to start claude-monitor automatically when you open a session:

```bash
claude-monitor hook install
```

This installs a `SessionStart` hook script and prints the JSON snippet to add to your `~/.claude/settings.json`. Once configured, claude-monitor starts in the background whenever you use Claude Code.

### Data Storage

Session history is persisted to a SQLite database at `~/.claude-monitor/history.db`.

## Features

### Session Monitoring

- **Live session tracking** via fsnotify + polling of `.claude/projects/` JSONL files
- **Model-specific pricing** for Opus ($5/$25), Sonnet ($3/$15), Haiku ($1/$5), loaded from SQLite and editable via the pricing API
- **Real-time status** tracking: thinking, tool_use, waiting, idle
- **Docker auto-discovery** of `.claude` mounts in running containers
- **Budget alerts** with configurable threshold and browser notifications

### Dashboard

<table>
<tr><td width="50%">

**Session Panel**
- Time-grouped sessions: Active Now, Last Hour, Today, Yesterday, This Week, Older
- Active/Recent/All filter tabs with counts
- Collapsible subagent hierarchy with idle toggle
- Current tool display on active cards
- Click error count to filter feed to errors

</td><td width="50%">

**Live Feed**
- Color-coded event stream (user, assistant, tool, result, hook, error)
- Type filter toggles with shift+click solo mode
- Tool call + result visual grouping
- Expandable content with code-block styling
- Multi-session mode by default, single-session on click

</td></tr>
</table>

<details>
<summary><strong>Dashboard Overview</strong> — click to expand</summary>
<img src="docs/screenshots/dashboard.png" alt="Dashboard" width="100%" />
</details>

<details>
<summary><strong>Search</strong> — click to expand</summary>
<img src="docs/screenshots/search.png" alt="Search" width="100%" />
</details>

### Views

| View | Shortcut | Description |
|------|----------|-------------|
| **List** | default | Session cards + live feed |
| **Graph** | `g` | Force-directed agent dependency graph (Canvas 2D) |
| **Table** | `t` | Dense sortable comparison table of all sessions |
| **History** | `h` | SQLite-backed table of completed sessions |
| **Timeline** | click | Horizontal waterfall of events with zoom/pan |
| **Replay** | click | Session replay with scrubber, speed control, SSE stream |

<details>
<summary><strong>Graph View</strong> — click to expand</summary>
<img src="docs/screenshots/graph.png" alt="Graph View" width="100%" />
</details>

<details>
<summary><strong>Table View</strong> — click to expand</summary>
<img src="docs/screenshots/table.png" alt="Table View" width="100%" />
</details>

<details>
<summary><strong>History View</strong> — click to expand</summary>
<img src="docs/screenshots/history.png" alt="History View" width="100%" />
</details>

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `/` | Focus search |
| `Esc` | Clear search / deselect / close |
| `↑` `↓` | Navigate sessions |
| `Enter` | Select focused session |
| `←` `→` | Collapse / expand subagents |
| `1` `2` `3` | Active / Recent / All filter |
| `g` `h` `t` | Graph / History / Table view |
| `?` | Help overlay |
| `Space` | Replay: play / pause |
| `R` | Replay: restart |

### Analytics

- **Per-session**: cost, tokens, cache hit %, messages, errors, cost rate ($/min), duration
- **Global stats**: active count, total spend, working agents, weighted cache %, aggregate $/min
- **Cross-session search** with highlighted results grouped by session
- **Session history** persisted to SQLite for historical analysis

## Docker

```bash
docker build -t claude-monitor .

docker run \
  -v ~/.claude:/home/node/.claude:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -p 127.0.0.1:7700:7700 \
  claude-monitor
```

> **Security:** Always bind to `127.0.0.1`. The dashboard exposes all session content including tool inputs/outputs.

## Architecture

```
JSONL files ─────────┐
  (fsnotify + poll)  │
                     ├──> Parser ──> Session Store ──> WebSocket Hub ──> Browser
Docker containers ───┘                    │
  (auto-discovery)                        │
                                     REST API ──> SQLite History
```

### Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.25, stdlib `net/http`, gorilla/websocket |
| Frontend | TypeScript 5.7, Vite 6, vanilla DOM (no framework) |
| Database | modernc.org/sqlite (pure Go, WAL mode) |
| File watching | fsnotify + 5s polling fallback |
| Graphs | Canvas 2D (no D3) |
| Build | Makefile (`make build`, `make dev`, `make test`) |

## API

Full OpenAPI spec at [`api/openapi.yaml`](api/openapi.yaml). Interactive Swagger UI is always served at `/api` (spec at `/api/openapi.yaml`); no flag required.

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check (includes `droppedEvents` counter) |
| `GET /api/version` | Server version |
| `GET /api/sessions` | List sessions (supports `?active=true`, `?group=activity`, `?repo=`, `?workflow=`, pagination) |
| `GET /api/sessions/{id}` | Single session lookup (live store, then DB fallback) |
| `GET /api/sessions/{id}/events` | Session events (supports `?pinned`, `?errors`, `?last=N`, pagination) |
| `GET /api/sessions/{id}/replay` | Replay manifest (session + its child agents, up to 10k events) |
| `GET /api/stats` | Aggregated stats with `?window=` (all, today, week, month) |
| `GET /api/stats/trends` | Trend data with `?window=` (24h, 7d, 30d) and optional `?repo=` |
| `GET /api/repos` | Repository list with total costs |
| `GET /api/repos/{id}/stats` | Per-repo aggregate statistics |
| `GET /api/repos/{id}/sessions` | Paginated sessions for a repo |
| `GET /api/workflows` | Workflow list with agent count and total cost |
| `GET /api/search?q=&limit=` | FTS5 full-text search across sessions |
| `GET /api/search/full?q=&limit=` | Full-content substring search (slower, searches complete text) |
| `GET /api/settings` | Get all user-configurable settings |
| `PUT /api/settings/{key}` | Update a single setting by key |
| `GET /api/storage` | Database storage info (size, event counts) |
| `DELETE /api/cache/repos` | Clear repo resolver cache |
| `GET /ws` | WebSocket (live events) |
| `GET /api` | Interactive Swagger UI (always served) |
| `GET /api/openapi.yaml` | OpenAPI 3 spec (YAML) |

## Development

```bash
# Install frontend dependencies
make install

# Start Go backend + Vite dev server with hot reload
make dev

# Run all tests
make test

# Type-check frontend + Go vet
make lint

# Build production binary
make build
```

## Watched Paths

Default directories monitored:
- `~/.claude/projects/`
- `/home/node/.claude/projects/`
- `/root/.claude/projects/`
- Docker containers with `.claude` bind mounts (auto-detected via Docker socket)

Add more with `--watch <path>` (repeatable).

## License

MIT
