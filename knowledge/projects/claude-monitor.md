# claude-monitor

A real-time dashboard daemon for monitoring Claude API sessions.

## Current state

The core daemon is complete. It watches Claude API sessions via WebSocket and serves a rich terminal-aesthetic dashboard at `static/index.html`.

**Completed features:**
- Live session list with cost, token counts, cache hit %, and age
- Real-time WebSocket feed of all Claude messages
- Session replay player: scrubber, play/pause, variable speed (0.5× – 10×)
- Pixel agent view: each session rendered as an animated pixel-art character at a desk, reflecting its current state (idle / thinking / tool / typing), with cost badges and state-colored card borders

## Next up

- Cost breakdown charts (by project / by day)
- Alert thresholds for budget overruns
- Multi-daemon aggregation

## Architecture

- Go daemon (`cmd/claude-monitor/`) — HTTP + WebSocket server
- Single-file frontend (`static/index.html`) — pure HTML/CSS/JS, no build step
- Replay via SSE streams from session log files
