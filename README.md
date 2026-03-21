# claude-monitor

A real-time dashboard for monitoring all Claude Code sessions — live token usage, cost, cache hit rates, and tool execution streams.

## Features

- **Live session tracking** — watches all `.claude/projects/` JSONL files via fsnotify
- **Per-session metrics** — cost, input/output tokens, cache hit %, message count
- **Live event feed** — color-coded stream of user/assistant/tool messages as they happen
- **WebSocket streaming** — sub-second latency from JSONL write to browser
- **Dark terminal UI** — htop-inspired, feels like watching a live system
- **OpenTelemetry ready** — stretch goal: pipe metrics to Grafana

## Quick Start

```bash
# Build
go build -o claude-monitor ./cmd/claude-monitor

# Run (watches ~/.claude and /home/node/.claude automatically)
./claude-monitor

# Open dashboard
open http://localhost:7700

# Watch additional paths
./claude-monitor --watch /custom/.claude/projects
```

## Docker (Phase 2)

```bash
docker build -t claude-monitor .

# Mount your .claude directories read-only
docker run \
  -v ~/.claude:/home/node/.claude:ro \
  -p 127.0.0.1:7700:7700 \
  claude-monitor
```

> **Security:** Always bind to `127.0.0.1` only. The dashboard shows all session content including tool inputs/outputs.

## Architecture

```
JSONL files (fsnotify) → parser → session store
                                ↓
                          WebSocket hub → browser
                                ↓
                          REST API (/api/sessions)
```

## Watched Paths

By default monitors:
- `~/.claude/projects/`
- `/home/node/.claude/projects/`
- `/root/.claude/projects/`

Add more with `--watch <path>`.

## API

- `GET /` — dashboard
- `GET /ws` — WebSocket (JSON events)
- `GET /api/sessions` — all sessions JSON
- `GET /health` — `{"ok":true}`

### WebSocket Event Format

```json
{"event": "message", "session": {...}, "message": {...}}
{"event": "session_new", "session": {...}, "message": {...}}
{"event": "session_update", "session": {...}}
```

## Roadmap

- [ ] OpenTelemetry export (OTLP → Grafana)
- [ ] Docker Compose with Prometheus + Grafana
- [ ] Discord bot integration (send alerts on high cost sessions)
- [ ] Session replay
- [ ] Multi-host aggregation
