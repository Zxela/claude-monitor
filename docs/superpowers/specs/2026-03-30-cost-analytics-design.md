# Cost & Token Analytics

**Date:** 2026-03-30
**Status:** Approved
**Branch:** `feat/cost-analytics`

## Summary

Add a dedicated Analytics tab to the dashboard with cost trends, token consumption breakdowns, per-repo cost analysis, and efficiency metrics. Uses Chart.js for visualization. All metric cards are collapsible with state persisted to localStorage.

Also fixes token display across the dashboard — showing effective input tokens (input + cache_read + cache_creation) instead of the raw `input_tokens` field, which reports ~1 when prompt caching is active.

## Motivation

Users need to answer: "How much am I spending?", "Which project costs the most?", and "Am I getting more efficient?" The existing dashboard shows live session data but no historical trends. Many users are on Max/Team plans (not PAYG), so token consumption and efficiency metrics matter as much as dollar cost.

## Design Decisions

- **Single scrollable view with collapsible cards** over sub-tabs or configurable grid. Simplest to ship, hide/show toggle gives enough user control. Can evolve into sub-tabs later without a rewrite.
- **Chart.js** over vanilla Canvas or lighter libs (uPlot). Bundle size is irrelevant for a localhost tool. Chart.js has the best docs and supports all chart types we need (line, bar, doughnut, area).
- **One API endpoint** (`/api/stats/trends`) returns all data in one fetch. Avoids waterfall requests and keeps the frontend simple.
- **Effective input tokens** displayed everywhere. The Anthropic API reports `input_tokens` as ~1 when caching is active; the real input is `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`. Our cost calculations already use all fields correctly — this is a display fix only.

## API

### `GET /api/stats/trends?window={24h|7d|30d}&repo={repoID}`

**Parameters:**
- `window` (optional, default `7d`) — time range. `24h` uses hourly buckets, `7d` and `30d` use daily buckets.
- `repo` (optional) — filter to a single repo by ID.

**Response:**

```json
{
  "window": "7d",
  "buckets": [
    {
      "date": "2026-03-24",
      "cost": 8.42,
      "inputTokens": 1200000,
      "outputTokens": 180000,
      "cacheReadTokens": 3400000,
      "cacheCreationTokens": 420000,
      "sessionCount": 7,
      "cacheHitPct": 71.2,
      "avgSessionCost": 1.20,
      "medianSessionCost": 0.85,
      "p95SessionCost": 3.40,
      "avgSessionTokens": 740000,
      "outputInputRatio": 0.15
    }
  ],
  "byRepo": [
    {
      "repoId": "abc",
      "repoName": "claude-monitor",
      "cost": 22.50,
      "tokens": 5200000,
      "sessions": 18
    }
  ],
  "byModel": [
    {
      "model": "claude-opus-4-6",
      "cost": 35.10,
      "tokens": 8100000,
      "sessions": 25
    }
  ],
  "summary": {
    "totalCost": 47.23,
    "effectiveTokens": 14200000,
    "cacheHitPct": 72,
    "sessionCount": 43
  }
}
```

## SQL Queries (new methods in `sqlite.go`)

### `TrendBuckets(window string, repoID string) ([]TrendBucket, error)`

```sql
-- For 7d/30d (daily buckets):
SELECT
  DATE(started_at) AS date,
  SUM(total_cost) AS cost,
  SUM(input_tokens) AS input_tokens,
  SUM(output_tokens) AS output_tokens,
  SUM(cache_read_tokens) AS cache_read_tokens,
  SUM(cache_creation_tokens) AS cache_creation_tokens,
  COUNT(*) AS session_count,
  AVG(total_cost) AS avg_session_cost
FROM sessions
WHERE started_at >= datetime('now', '-7 days')
  AND parent_id IS NULL
GROUP BY DATE(started_at)
ORDER BY date

-- For 24h (hourly buckets):
-- Replace DATE(started_at) with strftime('%Y-%m-%d %H:00', started_at)
```

### Median and P95

SQLite lacks `PERCENTILE_CONT`. Fetch all session costs for the window in one query, ordered by date and cost, then compute percentiles in Go by grouping rows by bucket date and picking the value at the appropriate index (count/2 for median, count*0.95 for p95).

```sql
SELECT DATE(started_at) AS date, total_cost
FROM sessions
WHERE started_at >= datetime('now', '-7 days')
  AND parent_id IS NULL
ORDER BY DATE(started_at), total_cost
```

### Output/Input Ratio

Per bucket: `outputTokens / effectiveInputTokens` where effective input = `inputTokens + cacheReadTokens + cacheCreationTokens`. Returns 0 if effective input is 0.

### `TrendByRepo(window string) ([]RepoTrend, error)`

Reuses existing pattern from `AggregateStats.costByRepo`:

```sql
SELECT r.id, r.name, SUM(s.total_cost),
  SUM(s.input_tokens + s.cache_read_tokens + s.cache_creation_tokens + s.output_tokens),
  COUNT(*)
FROM sessions s
JOIN repos r ON s.repo_id = r.id
WHERE s.started_at >= datetime('now', '-7 days')
  AND s.parent_id IS NULL
GROUP BY r.id
ORDER BY SUM(s.total_cost) DESC
```

### `TrendByModel(window string, repoID string) ([]ModelTrend, error)`

Same pattern, grouped by `model`.

## Frontend

### New Files

| File | Purpose | Estimated LOC |
|------|---------|---------------|
| `web/src/components/analytics-view.ts` | Main view: mounts cards, time range toggle, repo filter, card collapse state | ~250 |
| `web/src/components/analytics-cards.ts` | Individual card render functions, Chart.js instances | ~400 |
| `web/src/chart-config.ts` | Shared Chart.js dark theme config | ~30 |

### npm Dependency

Add `chart.js` as a dev dependency in `web/package.json`. Imported by analytics components, tree-shaken by Vite to only include used chart types.

### Metric Cards (8 total)

| # | Card | Chart Type | Data Source | Default |
|---|------|-----------|-------------|---------|
| 1 | Cost Over Time | Area line | `buckets[].cost` | Expanded |
| 2 | Token Consumption | Stacked bar | `buckets[].inputTokens/outputTokens/cacheReadTokens` | Expanded |
| 3 | Cost by Repo | Horizontal bar | `byRepo[]` sorted by cost | Collapsed |
| 4 | Model Mix | Doughnut + line | `byModel[]` | Collapsed |
| 5 | Cache Efficiency Over Time | Line | `buckets[].cacheHitPct` | Collapsed |
| 6 | Session Cost Distribution | Multi-line (avg/median/p95) | `buckets[].avg/median/p95SessionCost` | Collapsed |
| 7 | Tokens per Session | Line | `buckets[].avgSessionTokens` | Collapsed |
| 8 | Output / Input Ratio | Line | `buckets[].outputInputRatio` | Collapsed |

### Card Behavior

- Click header to expand/collapse (▼/▶ indicator)
- Chart.js instance created on expand, destroyed on collapse (no offscreen rendering)
- Collapse state persisted to `localStorage.analyticsCardState` as `{cardId: {collapsed: boolean}}`
- Selected time window persisted to `localStorage.analyticsWindow`

### Navigation Integration

- `state.ts` — add `'analytics'` to the `View` type union
- `main.ts` — add `a` keyboard shortcut handler, mount/unmount `analytics-view`
- `topbar.ts` — add Analytics tab button in the view switcher
- `help-overlay.ts` — add `a` shortcut to the help reference

### Layout

1. **Time range bar** (sticky top) — 24h / 7d (default) / 30d toggle + repo filter dropdown
2. **Summary row** — 4 key numbers: total spend, effective tokens, cache hit %, session count
3. **Scrollable card list** — 8 collapsible cards

## Token Display Fix

Update these existing components to show effective input tokens:

- `topbar.ts` — aggregate token display
- `session-card.ts` — expanded card token stat
- `history-view.ts` — tokens column in the table
- `utils.ts` — add `effectiveInputTokens(session)` helper

Formula: `effectiveInput = inputTokens + cacheReadTokens + cacheCreationTokens`

No data model changes. No API changes. Presentation math only.

## Testing

### Backend
- `TestTrendBuckets_DailyGrouping` — sessions across multiple days aggregate correctly
- `TestTrendBuckets_HourlyGrouping` — 24h window uses hourly buckets
- `TestTrendBuckets_RepoFilter` — repo parameter filters results
- `TestTrendBuckets_EmptyWindow` — no sessions returns empty buckets array
- `TestTrendPercentiles` — median/p95 computed correctly
- `TestTrendByRepo` — repo breakdown matches expected totals
- `TestTrendByModel` — model breakdown matches expected totals
- Integration test for `GET /api/stats/trends` endpoint

### Frontend
- TypeScript type-check (existing `tsc --noEmit`)
- Manual verification of chart rendering with sample data
- Verify card collapse/expand state persists across page reload
- Verify time range switching re-fetches and re-renders

## Out of Scope

- Real-time updating of analytics (no WebSocket push — refresh on tab switch)
- Rate limit tracking (JSONL doesn't contain rate limit events)
- Export to CSV/PDF (future feature)
- Anomaly detection / alerting (future feature, builds on this data)
