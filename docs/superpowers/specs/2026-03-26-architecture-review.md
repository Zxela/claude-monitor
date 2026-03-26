# Architecture Review — 4 Senior Engineers

**Date:** 2026-03-26
**Reviewers:** Go Backend (10yr), Frontend (12yr), SRE (8yr), Product (10yr)

## Scorecard

| Reviewer | Rating | Top Issue |
|----------|--------|-----------|
| Go Backend | 6/10 | 810-line main.go, race condition, search reads all files |
| Frontend | 6/10 | No API error handling, zero a11y, full DOM teardown |
| SRE/DevOps | 3/10 | Binds 0.0.0.0 no auth, WriteTimeout kills WS, no metrics |
| Product/UX | 6.5/10 | Replay undiscoverable, no empty state, flat typography |

## Critical Issues (refuse to ship)

### 1. Security: Binds 0.0.0.0 with no auth
- `main.go:781` binds to `:7700` (all interfaces)
- Exposes session content (source code, API keys, commands)
- WebSocket `CheckOrigin` returns `true` — any webpage can connect
- `/api/sessions/{id}/stop` can stop Docker containers
- **Fix:** Default to `127.0.0.1`, add `--bind` flag

### 2. WriteTimeout 15s kills WebSocket and SSE
- `main.go:787` sets `WriteTimeout: 15 * time.Second`
- WebSocket `pingPeriod` is 54 seconds — in direct conflict
- SSE replay streams killed after 15s of no writes
- **Fix:** Remove WriteTimeout for upgraded connections, or use per-handler timeouts

### 3. Race condition on prevActive/savedToHistory
- `main.go:339-389` — plain maps accessed from startup code AND ticker goroutine
- No synchronization between the two
- Go race detector would flag immediately
- **Fix:** Use mutex or move to single goroutine

### 4. Search reads ALL session files per request
- `main.go:557-613` — O(sessions × messages) per search
- 100 sessions × 500 lines = 50K JSON parse ops per request
- Trivially DoS-able from localhost
- **Fix:** Add search index, file size cap, or request timeout

### 5. api.ts has zero error handling
- Every `fetch` in `web/src/api.ts` assumes 200 success
- Non-200 response → `res.json()` throws confusing parse error
- **Fix:** Check `res.ok`, throw typed errors

### 6. JSON.parse in ws.ts not in try/catch
- `web/src/ws.ts:36` — `JSON.parse(e.data)` with no error boundary
- Malformed message crashes entire WS handler, stops all future events
- **Fix:** Wrap in try/catch

### 7. Zero accessibility
- No ARIA attributes anywhere
- No focus trapping in modals (help overlay, budget popover)
- Canvas views (graph, timeline) have no fallback
- Color-only status indicators
- **Fix:** Add ARIA labels, focus management, text alternatives

## High-Priority Fixes (gets to 7-8/10)

### Backend
- Extract handlers from main.go into `internal/api` package
- Add `SetMaxOpenConns(1)` on SQLite writer
- Add `-race` flag to CI test command
- Add Prometheus `/metrics` endpoint
- Bound the replay cache (LRU eviction)
- Fix health check to verify SQLite + watcher health
- Add structured logging (slog)

### Frontend
- Add error handling to all API calls in api.ts
- Consolidate 3 global keydown listeners into one dispatcher
- Deduplicate renderExpanded/renderCompact in session-card.ts
- Add loading states/skeletons for async operations
- Virtual scrolling for session list at 500+ sessions
- Extract inline styles to CSS classes

### Product
- Add replay button directly on session cards
- Add empty state guidance ("Start a Claude Code session to begin")
- Add burn-rate sparkline in topbar
- Make error count visually prominent (red border on card)
- Session annotations/labels for distinguishing "general-purpose" sessions
- Larger timeline button, more discoverable

### Operations
- Default bind to 127.0.0.1
- Fix Dockerfile Go version to match go.mod
- Add `HEALTHCHECK` to Dockerfile
- Add `go test -race` to CI
- Add `govulncheck` to CI
- Add container build/push to CI
- Add `USER` directive to Dockerfile (don't run as root)

## Full Reviews

The complete unabridged reviews from each engineer are saved in:
- Go Backend: `/tmp/claude-0/.../tasks/a9fbba531d0fd13e5.output`
- Frontend: `/tmp/claude-0/.../tasks/a5ed70121e4c1397f.output`
- SRE/DevOps: `/tmp/claude-0/.../tasks/aa7aa967283592484.output`
- Product/UX: `/tmp/claude-0/.../tasks/a651fc81a72ee0a59.output`
