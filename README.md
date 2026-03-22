# claude-monitor

A real-time observability dashboard for Claude Code sessions — live cost tracking, session status, tool execution feeds, cross-session search, and agent dependency visualization.

## Features

### Core Monitoring
- **Live session tracking** — watches `.claude/projects/` JSONL files via fsnotify + polling
- **Historical bootstrap** — reads full session history on startup for immediate stats
- **Model-specific pricing** — accurate cost for Opus, Sonnet, and Haiku models
- **Session status** — real-time status tracking: thinking, tool_use, waiting, idle
- **Docker auto-discovery** — finds `.claude` mounts in running containers automatically

### Dashboard
- **Session cards** with animated pixel sprites, status badges, cost rate, and model info
- **Session filtering** — Active / Recent (4h) / All views with live counts
- **Collapsible subagent hierarchy** — parent sessions expand to show child agents
- **Keyboard shortcuts** — press `?` for help (`/` search, `g` graph, `1`/`2`/`3` filters)

### Live Feed
- **Color-coded event stream** — user, assistant, tool calls, tool results, hooks
- **Feed type filters** — toggle visibility of each message type
- **Tool call details** — shows file paths, commands, search patterns, agent/skill names
- **Expandable content** — click `[+]` to reveal full tool output
- **Recent message loading** — click a session to immediately see its last 50 messages
- **Tool call + result grouping** — visual pairing of tool invocations with their results

### Visualizations
- **Session timeline / waterfall** — horizontal color-coded segments showing conversation flow
- **Agent dependency graph** — interactive force-directed node diagram of parent/child relationships
- **Inline pixel sprites** — animated characters in each session card reflecting status

### Analytics
- **Per-session metrics** — cost, tokens, cache hit %, message count, cost rate ($/min), duration
- **Global stats** — active count, total spend, active session cost, weighted cache hit %
- **Budget alerts** — configurable spend threshold with visual warning
- **Cross-session search** — full-text search across all session content

## Quick Start

```bash
# Build
go build -o claude-monitor ./cmd/claude-monitor

# Run (watches ~/.claude, auto-discovers Docker containers)
./claude-monitor

# Open dashboard
open http://localhost:7700

# Watch additional paths
./claude-monitor --watch /custom/.claude/projects
```

## Docker

```bash
docker build -t claude-monitor .

# Mount your .claude directories read-only
docker run \
  -v ~/.claude:/home/node/.claude:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -p 127.0.0.1:7700:7700 \
  claude-monitor
```

> **Security:** Always bind to `127.0.0.1` only. The dashboard shows all session content including tool inputs/outputs.

## Architecture

```
JSONL files (fsnotify + polling) --> parser --> session store
                                                    |
Docker containers (auto-discovery) -+               |
                                                    v
                                    WebSocket hub --> browser
                                         |
                                    REST API
                                    /api/sessions
                                    /api/sessions/{id}/recent
                                    /api/sessions/{id}/replay
                                    /api/search?q=...
```

## Watched Paths

By default monitors:
- `~/.claude/projects/`
- `/home/node/.claude/projects/`
- `/root/.claude/projects/`
- Docker containers with `.claude` bind mounts (auto-detected)

Add more with `--watch <path>`.

## API

| Endpoint | Description |
|----------|-------------|
| `GET /` | Dashboard |
| `GET /ws` | WebSocket (live JSON events) |
| `GET /api/sessions` | All sessions with stats |
| `GET /api/sessions/{id}/recent` | Last 50 messages for a session |
| `GET /api/sessions/{id}/replay` | Full replay manifest |
| `GET /api/sessions/{id}/replay/stream` | SSE replay stream |
| `GET /api/search?q=<query>` | Cross-session content search |
| `GET /health` | Health check |

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `/` | Focus search |
| `Esc` | Clear search / deselect session |
| `1` | Active sessions filter |
| `2` | Recent sessions filter |
| `3` | All sessions filter |
| `g` | Toggle graph view |
| `?` | Show help overlay |
