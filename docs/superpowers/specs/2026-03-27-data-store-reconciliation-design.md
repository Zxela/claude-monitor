# Data Store Reconciliation: SQLite-Primary with Server-Side Merge

**Date:** 2026-03-27
**Status:** Approved

## Problem

The dashboard has two independent data stores with no reconciliation:

1. **Live session store** (in-memory `session.Store`) — bootstrapped from JSONL files on disk
2. **History store** (SQLite `store.DB`) — populated on session inactivity transitions

This causes:

- **Topbar "TOTAL SPEND" is wrong** — only counts sessions with JSONL files still on disk, missing deleted/cleaned-up sessions that exist only in SQLite
- **Cost breakdown popover has the same gap** — sources from live store only
- **Field naming split** — `totalCostUSD` (Session) vs `totalCost` (HistoryRow) for the same value
- **History cache% shows `—` for valid data** — `!r.cacheCreationTokens` heuristic conflates missing data with zero
- **`requireSession` pollutes live store** — creates zero-valued skeleton sessions for replay lookups
- **Token display excludes `cacheCreationTokens`** — inconsistent with cache% denominator
- **No configurable time window** — users can't scope the topbar to "today" vs "all time"

## Design

### Approach: SQLite-primary with batched writes + server-side merge

SQLite is the authoritative store, written to on the existing ~30s tick. When serving aggregate endpoints, the Go handler merges SQLite totals with in-memory deltas for active sessions that haven't been flushed yet. The merge happens server-side; the frontend sees a single response.

### 1. New `GET /api/stats` endpoint

**Request:** `GET /api/stats?window=<all|today|week|month>`

**Response:**

```json
{
  "totalCost": 142.37,
  "inputTokens": 12840000,
  "outputTokens": 3210000,
  "cacheReadTokens": 9800000,
  "cacheCreationTokens": 1200000,
  "sessionCount": 87,
  "activeSessions": 3,
  "cacheHitPct": 38.2,
  "costRate": 0.042,
  "costByModel": { "claude-opus-4-6": 98.20, "claude-sonnet-4-6": 44.17 }
}
```

**`window` parameter mapping:**

| Value | SQL filter on `started_at` |
|-------|---------------------------|
| `all` | No filter |
| `today` | `>= start of today (server local time)` |
| `week` | `>= start of this week (server local time, Monday)` |
| `month` | `>= start of this month (server local time)` |

Filter by `started_at` (not `ended_at`) so sessions that started within the window are included even if still active. The Go handler computes the `since` timestamp in `time.Now().Location()` and formats it as RFC3339 for the SQLite query, matching the stored format.

### 2. New SQLite query: `AggregateStats`

New method on `store.DB`:

```go
type AggregateResult struct {
    TotalCost           float64
    InputTokens         int64
    OutputTokens        int64
    CacheReadTokens     int64
    CacheCreationTokens int64
    SessionCount        int
    CostByModel         map[string]float64
}

func (d *DB) AggregateStats(since time.Time) (*AggregateResult, error)
```

Two queries: one `SELECT SUM(...)` for the totals, one `SELECT model, SUM(total_cost) GROUP BY model` for the per-model breakdown. Both filter `WHERE started_at >= ?` when `since` is non-zero; omit the filter for "all". Both queries exclude subagents via `WHERE parent_id = ''` to match the current topbar behavior of counting only top-level sessions.

Also add a bulk lookup for active session costs:

```go
type SessionSnapshot struct {
    TotalCost           float64
    InputTokens         int64
    OutputTokens        int64
    CacheReadTokens     int64
    CacheCreationTokens int64
}

func (d *DB) GetSessionSnapshots(ids []string) (map[string]SessionSnapshot, error)
```

Returns last-saved cost and token values for the given IDs in one `WHERE id IN (...)` query. Used by the merge function to compute deltas for all fields, not just cost.

### 3. Server-side merge function

In the `/api/stats` handler (~20 lines):

1. Call `historyDB.AggregateStats(windowStart)` for SQLite totals
2. Get all active sessions from `sessionStore.All()` that fall within the window
3. Bulk-query their last-saved snapshots via `historyDB.GetSessionSnapshots(activeIDs)`
4. For each active top-level (non-subagent) session:
   - If in SQLite: add deltas (`live.TotalCost - saved.TotalCost`, same for all token fields)
   - If not yet in SQLite: add full live values
5. Compute derived fields:
   - `cacheHitPct = cacheReadTokens / (inputTokens + cacheReadTokens + cacheCreationTokens) * 100`
   - `costRate = sum of active session costRates`
   - `activeSessions = count of active sessions` (from live store, not SQLite)
6. Return single merged response

### 4. Normalize JSON field naming

Rename `session.Session.TotalCost` JSON tag from `totalCostUSD` to `totalCost`:

```go
// session.go
TotalCost    float64   `json:"totalCost"`   // was: json:"totalCostUSD"
```

Update all frontend references:

- `types.ts`: `Session.totalCostUSD` -> `Session.totalCost`
- `session-card.ts`: all `session.totalCostUSD` -> `session.totalCost`
- `cost-breakdown.ts`: all `s.totalCostUSD` -> `s.totalCost`
- `topbar.ts`: remove client-side cost aggregation (replaced by `/api/stats`)
- `graph-view.ts`: any references to `totalCostUSD`

No API versioning needed — local dashboard with no external consumers.

### 5. Frontend topbar changes

**Remove client-side aggregation.** The `updateStats()` function in `topbar.ts` currently iterates `state.sessions` to compute cost, cache%, active count, and rate. Replace with:

- New `state.stats` field holding the `/api/stats` response
- Poll every 5s while connected (simple interval, no WS-event debouncing — the endpoint is cheap and this avoids complexity)
- Topbar renders directly from `state.stats`

**Remove "WORKING" stat.** No actionable distinction from "ACTIVE". Topbar becomes:

```
ACTIVE 3 | TOTAL SPEND [ALL|TODAY|WEEK|MONTH] $142 | CACHE HIT 38% | $/MIN $0.042
```

**Time window selector:**

- Inline toggle buttons next to "TOTAL SPEND", styled like existing filter buttons
- Default: `today`
- Persisted in `localStorage` key `claude-monitor-stats-window`
- Changing window triggers immediate re-fetch of `/api/stats?window=<new>`

**Cost breakdown popover:** Refactored to source from `state.stats`. The `costByModel` field provides the donut chart data. The existing ALL/TODAY filter is replaced by the shared window selector.

### 6. Bug fixes bundled in this change

**History cache% `—` for valid data:**

Add a `cache_hit_pct` column to `session_history` (new migration 004). Computed on save using the same formula as `session.go`. History view reads the pre-computed value instead of guessing from token presence.

**`requireSession` skeleton pollution:**

Stop upserting into the live session store for replay/recent lookups. Return a transient session object:

```go
func requireSession(...) (*session.Session, bool) {
    // ... existing lookup ...
    if !ok {
        if filePath := sessionFinder.FindSessionFile(id); filePath != "" {
            // Return transient session, do NOT persist to store
            return &session.Session{ID: id, FilePath: filePath}, true
        }
    }
}
```

**Token display excludes `cacheCreationTokens`:**

Add `cacheCreationTokens` to the token total in:

- `session-card.ts`: `inputTokens + outputTokens + cacheReadTokens + cacheCreationTokens`
- `history-view.ts`: same formula for the Tokens column

### 7. What doesn't change

- **WebSocket broadcast** — still pushes individual session updates for real-time UI (card status dots, feed messages). The topbar just stops deriving aggregates from these.
- **Session store** — still bootstraps from JSONL, still used for session list/cards/graph/replay. Not going away.
- **History save cadence** — stays at ~30s tick. The merge function compensates for the lag.
- **`/api/history` endpoint** — unchanged. Still returns `HistoryRow[]` for the history table. The table continues to show per-session rows, not aggregates.
- **Active threshold** — backend 30s / frontend 45s mismatch becomes cosmetic-only since aggregate "active" count now comes from server-side `/api/stats`.

## Files affected

### Go (backend)

| File | Change |
|------|--------|
| `internal/store/sqlite.go` | Add `AggregateStats()`, `GetSessionCosts()` methods |
| `internal/store/migrations/004_add_cache_hit_pct.go` | New migration adding `cache_hit_pct` column |
| `internal/session/session.go` | Change JSON tag `totalCostUSD` -> `totalCost` |
| `cmd/claude-monitor/main.go` | Add `/api/stats` handler with merge logic; fix `requireSession` |

### Frontend (TypeScript)

| File | Change |
|------|--------|
| `web/src/types.ts` | `Session.totalCostUSD` -> `totalCost`; add `Stats` interface |
| `web/src/state.ts` | Add `stats: Stats \| null`, `statsWindow` fields |
| `web/src/api.ts` | Add `fetchStats(window)` |
| `web/src/components/topbar.ts` | Source from `state.stats`; add window toggle; remove WORKING; remove client-side aggregation |
| `web/src/components/cost-breakdown.ts` | Source from `state.stats.costByModel` |
| `web/src/components/session-card.ts` | `totalCostUSD` -> `totalCost`; add `cacheCreationTokens` to token total |
| `web/src/components/history-view.ts` | Use `cache_hit_pct` column; add `cacheCreationTokens` to token total |
| `web/src/components/graph-view.ts` | `totalCostUSD` -> `totalCost` |
