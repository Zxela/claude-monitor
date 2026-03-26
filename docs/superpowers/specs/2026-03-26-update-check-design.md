# Update Check on Startup — Design Spec

**Date:** 2026-03-26

## Goal

Check for new releases on daemon startup and surface a notification in both the server log and the web UI, with an opt-out env var.

## Architecture

### Backend: `internal/update` package

New package with a single public function:

```go
type Release struct {
    Version string // e.g. "v1.17.0"
    URL     string // GitHub release URL
}

func CheckLatest(currentVersion string) (*Release, error)
```

- Makes GET request to `https://api.github.com/repos/Zxela/claude-monitor/releases/latest`
- Parses `tag_name` and `html_url` from JSON response
- Compares semver: if `tag_name` > `currentVersion`, returns the Release
- Returns `nil, nil` if current version is latest or newer
- Returns `nil, err` on network/parse failure
- 5-second timeout on the HTTP request
- If `currentVersion` is `"dev"`, skip check (return nil, nil) — dev builds don't need update prompts

### Integration in `main.go`

On startup, after flag parsing:

1. Check `CLAUDE_MONITOR_NO_UPDATE_CHECK` env var — if set to `"1"` or `"true"`, skip
2. Launch goroutine: call `update.CheckLatest(version)`
3. If update available: log to stderr, broadcast WebSocket event

```go
go func() {
    rel, err := update.CheckLatest(version)
    if err != nil {
        log.Printf("update check failed: %v", err)
        return
    }
    if rel != nil {
        log.Printf("update available: %s (current: %s) — %s", rel.Version, version, rel.URL)
        // broadcast to WebSocket hub
    }
}()
```

### WebSocket Event

New event type broadcast through existing hub:

```json
{
    "type": "update_available",
    "version": "v1.17.0",
    "url": "https://github.com/Zxela/claude-monitor/releases/tag/v1.17.0"
}
```

### Frontend: Update Banner

- Listen for `update_available` WebSocket event in `ws.ts` handlers
- Render a dismissable banner at the top of the page
- Banner text: "Update available: v1.17.0" with a link to the release page
- Dismiss stores flag in `sessionStorage` so it reappears on full page reload but not on SPA navigation
- Styling: subtle info bar, not intrusive — similar to existing topbar color scheme

### Env Var Opt-Out

- `CLAUDE_MONITOR_NO_UPDATE_CHECK=1` or `=true` — skips the check entirely
- No flag needed — env var is sufficient for daemon/service configuration

## Files

| File | Action | Purpose |
|------|--------|---------|
| `internal/update/update.go` | Create | CheckLatest function |
| `internal/update/update_test.go` | Create | Tests with httptest mock server |
| `cmd/claude-monitor/main.go` | Modify | Call CheckLatest on startup, broadcast result |
| `web/src/ws.ts` | Modify | Handle `update_available` event type |
| `web/src/components/update-banner.ts` | Create | Dismissable update banner component |
| `web/src/app.ts` or equivalent | Modify | Mount the banner |
| `README.md` | Modify | Document env var and update check behavior |

## Semver Comparison

Use simple string comparison on version tags after stripping the `v` prefix and splitting on `.`. Compare major, minor, patch as integers. No need for a dependency — this is straightforward.

## Edge Cases

- **Dev builds** (`version = "dev"`): skip check
- **Network failure**: log debug message, don't show banner
- **Rate limited** (403): treat as network failure, skip silently
- **Malformed response**: skip silently
- **Same version**: no banner
- **Newer local version** (pre-release/dev): no banner

## Non-Goals

- Auto-update / download
- Periodic re-checking while running
- Service registration (separate feature)
