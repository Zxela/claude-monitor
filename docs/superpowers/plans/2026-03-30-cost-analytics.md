# Cost & Token Analytics — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an Analytics tab with cost trends, token consumption, per-repo breakdowns, and efficiency metrics via Chart.js, plus fix effective token display across the dashboard.

**Architecture:** One new API endpoint (`GET /api/stats/trends`) backed by new SQLite query methods returns time-bucketed trend data. Three new frontend files render Chart.js charts in collapsible cards. The view integrates into the existing tab system alongside List/Graph/History.

**Tech Stack:** Go (SQLite queries), TypeScript (Chart.js 4.x, vanilla DOM), Vite

---

### Task 1: Fix Effective Token Display

**Files:**
- Modify: `web/src/utils.ts:19-23`
- Modify: `web/src/components/session-card.ts:63`
- Modify: `web/src/components/history-view.ts:26`
- Modify: `web/src/types.ts` (add helper type)

This task fixes the existing token undercount display across the dashboard before adding analytics.

- [ ] **Step 1: Add effectiveInputTokens helper to utils.ts**

Add after the existing `formatTokens` function (line 23):

```typescript
/** Effective input = input + cache_read + cache_creation (raw input_tokens is ~1 with caching). */
export function effectiveInputTokens(s: { inputTokens: number; cacheReadTokens: number; cacheCreationTokens: number }): number {
  return s.inputTokens + s.cacheReadTokens + s.cacheCreationTokens;
}
```

- [ ] **Step 2: Update session-card.ts to use effective tokens**

In `renderExpanded` (line 63), change:
```typescript
// Old:
formatTokens(session.inputTokens + session.outputTokens + session.cacheReadTokens + session.cacheCreationTokens)
// New:
formatTokens(effectiveInputTokens(session) + session.outputTokens)
```

Add import at top: `import { effectiveInputTokens } from '../utils';`

- [ ] **Step 3: Update history-view.ts to use effective tokens**

In the COLUMNS formatter (line 26), change:
```typescript
// Old:
formatTokens(r.inputTokens + r.outputTokens + r.cacheReadTokens + (r.cacheCreationTokens || 0))
// New:
formatTokens(effectiveInputTokens(r) + r.outputTokens)
```

Add import: `import { effectiveInputTokens } from '../utils';`

- [ ] **Step 4: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build, no type errors

- [ ] **Step 5: Commit**

```bash
git add web/src/utils.ts web/src/components/session-card.ts web/src/components/history-view.ts
git commit -m "fix: show effective input tokens (input + cache) instead of raw input_tokens"
```

---

### Task 2: Backend — Trend Types and Query Methods

**Files:**
- Modify: `internal/store/sqlite.go:162-172` (add new types after AggregateResult)
- Create: `internal/store/sqlite_test.go` (add trend tests)

- [ ] **Step 1: Write failing tests for TrendBuckets**

Add to `internal/store/sqlite_test.go`:

```go
func TestTrendBuckets_DailyGrouping(t *testing.T) {
	db := setupTestDB(t)

	// Insert sessions on different days
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	insertTestSession(t, db, "s1", now, 5.0, 1000, 500, 8000, 200)
	insertTestSession(t, db, "s2", now, 3.0, 800, 400, 6000, 150)
	insertTestSession(t, db, "s3", yesterday, 2.0, 500, 300, 4000, 100)
	insertTestSession(t, db, "s4", twoDaysAgo, 7.0, 2000, 800, 10000, 300)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Buckets) < 3 {
		t.Fatalf("expected at least 3 daily buckets, got %d", len(result.Buckets))
	}

	// Find today's bucket
	todayStr := now.Format("2006-01-02")
	var todayBucket *TrendBucket
	for i := range result.Buckets {
		if result.Buckets[i].Date == todayStr {
			todayBucket = &result.Buckets[i]
			break
		}
	}
	if todayBucket == nil {
		t.Fatal("no bucket for today")
	}
	if todayBucket.Cost != 8.0 {
		t.Errorf("today cost = %f, want 8.0", todayBucket.Cost)
	}
	if todayBucket.SessionCount != 2 {
		t.Errorf("today sessions = %d, want 2", todayBucket.SessionCount)
	}
}

func TestTrendBuckets_EmptyWindow(t *testing.T) {
	db := setupTestDB(t)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatal(err)
	}

	if result.Buckets == nil {
		t.Error("buckets should be empty slice, not nil")
	}
	if len(result.Buckets) != 0 {
		t.Errorf("expected 0 buckets, got %d", len(result.Buckets))
	}
	if result.Summary.SessionCount != 0 {
		t.Errorf("expected 0 sessions in summary, got %d", result.Summary.SessionCount)
	}
}

func TestTrendBuckets_RepoFilter(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now()
	insertTestSessionWithRepo(t, db, "s1", "repo-a", now, 5.0, 1000, 500, 8000, 200)
	insertTestSessionWithRepo(t, db, "s2", "repo-b", now, 3.0, 800, 400, 6000, 150)

	result, err := db.TrendData("7d", "repo-a")
	if err != nil {
		t.Fatal(err)
	}

	if result.Summary.TotalCost != 5.0 {
		t.Errorf("filtered cost = %f, want 5.0", result.Summary.TotalCost)
	}
}

func TestTrendPercentiles(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now()
	// Insert 20 sessions with costs 1.0 through 20.0
	for i := 1; i <= 20; i++ {
		insertTestSession(t, db, fmt.Sprintf("s%d", i), now, float64(i), 100, 50, 500, 20)
	}

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatal(err)
	}

	todayStr := now.Format("2006-01-02")
	var bucket *TrendBucket
	for i := range result.Buckets {
		if result.Buckets[i].Date == todayStr {
			bucket = &result.Buckets[i]
			break
		}
	}
	if bucket == nil {
		t.Fatal("no bucket for today")
	}

	// Median of 1..20 = 10 or 11 (index 10 in 0-indexed sorted list)
	if bucket.MedianSessionCost < 10.0 || bucket.MedianSessionCost > 11.0 {
		t.Errorf("median = %f, want ~10.5", bucket.MedianSessionCost)
	}
	// P95 of 1..20 = 19 (index 19 in 0-indexed sorted list)
	if bucket.P95SessionCost < 19.0 {
		t.Errorf("p95 = %f, want >= 19.0", bucket.P95SessionCost)
	}
}

func TestTrendByRepo(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now()
	insertTestSessionWithRepo(t, db, "s1", "repo-a", now, 10.0, 1000, 500, 8000, 200)
	insertTestSessionWithRepo(t, db, "s2", "repo-a", now, 5.0, 800, 400, 6000, 150)
	insertTestSessionWithRepo(t, db, "s3", "repo-b", now, 3.0, 500, 300, 4000, 100)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ByRepo) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(result.ByRepo))
	}
	// Should be sorted by cost descending
	if result.ByRepo[0].Cost != 15.0 {
		t.Errorf("top repo cost = %f, want 15.0", result.ByRepo[0].Cost)
	}
}

func TestTrendByModel(t *testing.T) {
	db := setupTestDB(t)

	now := time.Now()
	insertTestSessionWithModel(t, db, "s1", "claude-opus-4-6", now, 10.0, 1000, 500, 8000, 200)
	insertTestSessionWithModel(t, db, "s2", "claude-sonnet-4-6", now, 3.0, 800, 400, 6000, 150)

	result, err := db.TrendData("7d", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ByModel) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.ByModel))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestTrend -v`
Expected: FAIL — `TrendData`, `TrendBucket` etc. not defined

- [ ] **Step 3: Add trend types to sqlite.go**

Add after `AggregateResult` struct (after line 172):

```go
// TrendBucket holds aggregated stats for a single time bucket (hour or day).
type TrendBucket struct {
	Date              string  `json:"date"`
	Cost              float64 `json:"cost"`
	InputTokens       int64   `json:"inputTokens"`
	OutputTokens      int64   `json:"outputTokens"`
	CacheReadTokens   int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	SessionCount      int     `json:"sessionCount"`
	CacheHitPct       float64 `json:"cacheHitPct"`
	AvgSessionCost    float64 `json:"avgSessionCost"`
	MedianSessionCost float64 `json:"medianSessionCost"`
	P95SessionCost    float64 `json:"p95SessionCost"`
	AvgSessionTokens  int64   `json:"avgSessionTokens"`
	OutputInputRatio  float64 `json:"outputInputRatio"`
}

// RepoTrend holds cost/token totals for a single repo.
type RepoTrend struct {
	RepoID   string  `json:"repoId"`
	RepoName string  `json:"repoName"`
	Cost     float64 `json:"cost"`
	Tokens   int64   `json:"tokens"`
	Sessions int     `json:"sessions"`
}

// ModelTrend holds cost/token totals for a single model.
type ModelTrend struct {
	Model    string  `json:"model"`
	Cost     float64 `json:"cost"`
	Tokens   int64   `json:"tokens"`
	Sessions int     `json:"sessions"`
}

// TrendSummary holds overall totals for the requested window.
type TrendSummary struct {
	TotalCost      float64 `json:"totalCost"`
	EffectiveTokens int64  `json:"effectiveTokens"`
	CacheHitPct    float64 `json:"cacheHitPct"`
	SessionCount   int     `json:"sessionCount"`
}

// TrendResult is the full response for the trends endpoint.
type TrendResult struct {
	Window  string        `json:"window"`
	Buckets []TrendBucket `json:"buckets"`
	ByRepo  []RepoTrend   `json:"byRepo"`
	ByModel []ModelTrend  `json:"byModel"`
	Summary TrendSummary  `json:"summary"`
}
```

- [ ] **Step 4: Implement TrendData method**

Add to `sqlite.go` after the `AggregateStats` method (after line 503):

```go
// TrendData returns time-bucketed trend data for the given window.
// Window must be "24h", "7d", or "30d". RepoID is optional (empty = all repos).
func (d *DB) TrendData(window string, repoID string) (*TrendResult, error) {
	var since string
	var dateFmt string
	switch window {
	case "24h":
		since = "datetime('now', '-1 day')"
		dateFmt = "strftime('%Y-%m-%d %H:00', started_at)"
	case "30d":
		since = "datetime('now', '-30 days')"
		dateFmt = "DATE(started_at)"
	default: // 7d
		window = "7d"
		since = "datetime('now', '-7 days')"
		dateFmt = "DATE(started_at)"
	}

	whereClause := "WHERE started_at >= " + since + " AND parent_id IS NULL"
	var args []interface{}
	if repoID != "" {
		whereClause += " AND repo_id = ?"
		args = append(args, repoID)
	}

	result := &TrendResult{
		Window:  window,
		Buckets: []TrendBucket{},
		ByRepo:  []RepoTrend{},
		ByModel: []ModelTrend{},
	}

	// 1. Buckets (daily or hourly aggregation)
	bucketQuery := fmt.Sprintf(`SELECT %s AS bucket,
		COALESCE(SUM(total_cost), 0),
		COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_creation_tokens), 0),
		COUNT(*), COALESCE(AVG(total_cost), 0)
		FROM sessions %s
		GROUP BY bucket ORDER BY bucket`, dateFmt, whereClause)

	rows, err := d.db.Query(bucketQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend buckets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var b TrendBucket
		if err := rows.Scan(&b.Date, &b.Cost, &b.InputTokens, &b.OutputTokens,
			&b.CacheReadTokens, &b.CacheCreationTokens, &b.SessionCount, &b.AvgSessionCost); err != nil {
			return nil, err
		}
		effInput := b.InputTokens + b.CacheReadTokens + b.CacheCreationTokens
		if effInput > 0 {
			b.CacheHitPct = float64(b.CacheReadTokens) / float64(effInput) * 100
			b.OutputInputRatio = float64(b.OutputTokens) / float64(effInput)
		}
		if b.SessionCount > 0 {
			b.AvgSessionTokens = (effInput + b.OutputTokens) / int64(b.SessionCount)
		}
		result.Buckets = append(result.Buckets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Percentiles — fetch all session costs ordered by date+cost, compute in Go
	percentileQuery := fmt.Sprintf(`SELECT %s AS bucket, total_cost
		FROM sessions %s
		ORDER BY bucket, total_cost`, dateFmt, whereClause)

	pRows, err := d.db.Query(percentileQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend percentiles: %w", err)
	}
	defer pRows.Close()

	// Group costs by bucket
	bucketCosts := make(map[string][]float64)
	for pRows.Next() {
		var bucket string
		var cost float64
		if err := pRows.Scan(&bucket, &cost); err != nil {
			return nil, err
		}
		bucketCosts[bucket] = append(bucketCosts[bucket], cost)
	}
	if err := pRows.Err(); err != nil {
		return nil, err
	}

	// Apply percentiles to buckets
	for i := range result.Buckets {
		costs := bucketCosts[result.Buckets[i].Date]
		if len(costs) == 0 {
			continue
		}
		// costs are already sorted (ORDER BY total_cost)
		medIdx := len(costs) / 2
		result.Buckets[i].MedianSessionCost = costs[medIdx]
		p95Idx := int(float64(len(costs)) * 0.95)
		if p95Idx >= len(costs) {
			p95Idx = len(costs) - 1
		}
		result.Buckets[i].P95SessionCost = costs[p95Idx]
	}

	// 3. By repo
	repoQuery := fmt.Sprintf(`SELECT COALESCE(r.id, ''), COALESCE(r.name, ''),
		COALESCE(SUM(s.total_cost), 0),
		COALESCE(SUM(s.input_tokens + s.cache_read_tokens + s.cache_creation_tokens + s.output_tokens), 0),
		COUNT(*)
		FROM sessions s LEFT JOIN repos r ON s.repo_id = r.id
		%s GROUP BY r.id ORDER BY SUM(s.total_cost) DESC`,
		strings.Replace(whereClause, "started_at", "s.started_at", -1))

	// Adjust args for repo query — repoID filter uses s.repo_id
	repoArgs := make([]interface{}, len(args))
	copy(repoArgs, args)

	rRows, err := d.db.Query(strings.Replace(repoQuery, "parent_id", "s.parent_id", -1),
		repoArgs...)
	if err != nil {
		return nil, fmt.Errorf("trend by repo: %w", err)
	}
	defer rRows.Close()

	for rRows.Next() {
		var r RepoTrend
		if err := rRows.Scan(&r.RepoID, &r.RepoName, &r.Cost, &r.Tokens, &r.Sessions); err != nil {
			return nil, err
		}
		if r.RepoID != "" {
			result.ByRepo = append(result.ByRepo, r)
		}
	}

	// 4. By model
	modelQuery := fmt.Sprintf(`SELECT COALESCE(model, ''),
		COALESCE(SUM(total_cost), 0),
		COALESCE(SUM(input_tokens + cache_read_tokens + cache_creation_tokens + output_tokens), 0),
		COUNT(*)
		FROM sessions %s GROUP BY model ORDER BY SUM(total_cost) DESC`, whereClause)

	mRows, err := d.db.Query(modelQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("trend by model: %w", err)
	}
	defer mRows.Close()

	for mRows.Next() {
		var m ModelTrend
		if err := mRows.Scan(&m.Model, &m.Cost, &m.Tokens, &m.Sessions); err != nil {
			return nil, err
		}
		if m.Model != "" {
			result.ByModel = append(result.ByModel, m)
		}
	}

	// 5. Summary — sum across all buckets
	for _, b := range result.Buckets {
		result.Summary.TotalCost += b.Cost
		result.Summary.EffectiveTokens += b.InputTokens + b.CacheReadTokens + b.CacheCreationTokens + b.OutputTokens
		result.Summary.SessionCount += b.SessionCount
	}
	totalInput := int64(0)
	totalCacheRead := int64(0)
	for _, b := range result.Buckets {
		totalInput += b.InputTokens + b.CacheReadTokens + b.CacheCreationTokens
		totalCacheRead += b.CacheReadTokens
	}
	if totalInput > 0 {
		result.Summary.CacheHitPct = float64(totalCacheRead) / float64(totalInput) * 100
	}

	return result, nil
}
```

- [ ] **Step 5: Add test helper functions**

Add to the test file (these helpers insert test sessions into the DB):

```go
func insertTestSession(t *testing.T, db *DB, id string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	insertTestSessionFull(t, db, id, "", "", startedAt, cost, input, output, cacheRead, cacheCreate)
}

func insertTestSessionWithRepo(t *testing.T, db *DB, id, repoID string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	// Ensure repo exists
	_, err := db.db.Exec(`INSERT OR IGNORE INTO repos (id, name) VALUES (?, ?)`, repoID, repoID)
	if err != nil {
		t.Fatal(err)
	}
	insertTestSessionFull(t, db, id, repoID, "", startedAt, cost, input, output, cacheRead, cacheCreate)
}

func insertTestSessionWithModel(t *testing.T, db *DB, id, model string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	insertTestSessionFull(t, db, id, "", model, startedAt, cost, input, output, cacheRead, cacheCreate)
}

func insertTestSessionFull(t *testing.T, db *DB, id, repoID, model string, startedAt time.Time, cost float64, input, output, cacheRead, cacheCreate int64) {
	t.Helper()
	started := startedAt.Format(time.RFC3339)
	ended := startedAt.Add(10 * time.Minute).Format(time.RFC3339)
	var repoVal interface{}
	if repoID != "" {
		repoVal = repoID
	}
	var modelVal interface{}
	if model != "" {
		modelVal = model
	}
	_, err := db.db.Exec(`INSERT INTO sessions (id, repo_id, model, started_at, ended_at, total_cost, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, message_count, event_count, error_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 10, 20, 0)`,
		id, repoVal, modelVal, started, ended, cost, input, output, cacheRead, cacheCreate)
	if err != nil {
		t.Fatal(err)
	}
}

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
```

Note: Check existing test file first — some of these helpers may already exist. Reuse or extend existing ones.

- [ ] **Step 6: Add missing imports to sqlite.go if needed**

Ensure `fmt` and `strings` are imported. `time` should already be imported.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestTrend -v`
Expected: All 5 tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: add TrendData query method for time-bucketed analytics"
```

---

### Task 3: Backend — Trends API Endpoint

**Files:**
- Modify: `cmd/claude-monitor/main.go:660` (add handler after stats endpoint)
- Modify: `cmd/claude-monitor/main_test.go` (add integration test)

- [ ] **Step 1: Write failing integration test**

Add to `cmd/claude-monitor/main_test.go`:

```go
func TestTrendsEndpoint(t *testing.T) {
	resp, err := http.Get(testServer.URL + "/api/stats/trends?window=7d")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result struct {
		Window  string `json:"window"`
		Buckets []struct {
			Date string  `json:"date"`
			Cost float64 `json:"cost"`
		} `json:"buckets"`
		Summary struct {
			TotalCost    float64 `json:"totalCost"`
			SessionCount int     `json:"sessionCount"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Window != "7d" {
		t.Errorf("window = %q, want '7d'", result.Window)
	}
	if result.Buckets == nil {
		t.Error("buckets should not be nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/claude-monitor/ -run TestTrends -v`
Expected: FAIL — 404 (endpoint doesn't exist)

- [ ] **Step 3: Add the trends endpoint handler**

Add after the stats handler (after line 660 in `main.go`):

```go
	mux.HandleFunc("GET /api/stats/trends", func(w http.ResponseWriter, r *http.Request) {
		window := r.URL.Query().Get("window")
		if window == "" {
			window = "7d"
		}
		if window != "24h" && window != "7d" && window != "30d" {
			writeJSONError(w, "window must be 24h, 7d, or 30d", http.StatusBadRequest)
			return
		}

		repoID := r.URL.Query().Get("repo")

		result, err := historyDB.TrendData(window, repoID)
		if err != nil {
			log.Printf("trends error: %v", err)
			writeJSONError(w, "failed to compute trends", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/claude-monitor/ -run TestTrends -v`
Expected: PASS

- [ ] **Step 5: Run full test suite**

Run: `make test`
Expected: All existing tests still pass

- [ ] **Step 6: Commit**

```bash
git add cmd/claude-monitor/main.go cmd/claude-monitor/main_test.go
git commit -m "feat: add GET /api/stats/trends endpoint"
```

---

### Task 4: Frontend — Install Chart.js and Create Chart Config

**Files:**
- Modify: `web/package.json`
- Create: `web/src/chart-config.ts`

- [ ] **Step 1: Install Chart.js**

Run: `cd web && npm install chart.js`

- [ ] **Step 2: Create shared chart config**

Create `web/src/chart-config.ts`:

```typescript
import { Chart, LineController, BarController, DoughnutController, LineElement, BarElement, ArcElement, PointElement, LinearScale, CategoryScale, TimeScale, Tooltip, Legend, Filler } from 'chart.js';

// Register only the components we use (tree-shaking)
Chart.register(
  LineController, BarController, DoughnutController,
  LineElement, BarElement, ArcElement, PointElement,
  LinearScale, CategoryScale,
  Tooltip, Legend, Filler
);

export const COLORS = {
  green: '#4ae68a',
  blue: '#6ab4ff',
  orange: '#ffa64a',
  purple: '#c49aff',
  red: '#ff6b6b',
  yellow: '#ffd166',
  gray: '#666',
  gridLine: 'rgba(255,255,255,0.06)',
  text: '#aaa',
};

export const DARK_THEME = {
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 300 },
  plugins: {
    legend: {
      labels: { color: COLORS.text, font: { family: 'monospace', size: 11 } },
    },
    tooltip: {
      backgroundColor: '#1e1e3a',
      borderColor: '#3a3a5a',
      borderWidth: 1,
      titleFont: { family: 'monospace', size: 12 },
      bodyFont: { family: 'monospace', size: 11 },
      titleColor: '#e0e0e0',
      bodyColor: '#ccc',
    },
  },
  scales: {
    x: {
      ticks: { color: COLORS.text, font: { family: 'monospace', size: 10 } },
      grid: { color: COLORS.gridLine },
    },
    y: {
      ticks: { color: COLORS.text, font: { family: 'monospace', size: 10 } },
      grid: { color: COLORS.gridLine },
    },
  },
} as const;

export { Chart };
```

- [ ] **Step 3: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add web/package.json web/package-lock.json web/src/chart-config.ts
git commit -m "feat: add Chart.js dependency and dark theme config"
```

---

### Task 5: Frontend — API Client and Types

**Files:**
- Modify: `web/src/types.ts:97` (add TrendResult types)
- Modify: `web/src/api.ts:46` (add fetchTrends function)

- [ ] **Step 1: Add trend types to types.ts**

Add after the `Stats` interface (after line 97):

```typescript
export interface TrendBucket {
  date: string;
  cost: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  sessionCount: number;
  cacheHitPct: number;
  avgSessionCost: number;
  medianSessionCost: number;
  p95SessionCost: number;
  avgSessionTokens: number;
  outputInputRatio: number;
}

export interface RepoTrend {
  repoId: string;
  repoName: string;
  cost: number;
  tokens: number;
  sessions: number;
}

export interface ModelTrend {
  model: string;
  cost: number;
  tokens: number;
  sessions: number;
}

export interface TrendSummary {
  totalCost: number;
  effectiveTokens: number;
  cacheHitPct: number;
  sessionCount: number;
}

export interface TrendResult {
  window: string;
  buckets: TrendBucket[];
  byRepo: RepoTrend[];
  byModel: ModelTrend[];
  summary: TrendSummary;
}
```

- [ ] **Step 2: Add fetchTrends to api.ts**

Add after the `fetchStats` function (after line 46):

```typescript
export type TrendWindow = '24h' | '7d' | '30d';

export async function fetchTrends(window: TrendWindow = '7d', repo?: string): Promise<TrendResult> {
  const params = new URLSearchParams({ window });
  if (repo) params.set('repo', repo);
  return request<TrendResult>(`${BASE}/api/stats/trends?${params}`);
}
```

Add `TrendResult` to the import at line 1.

- [ ] **Step 3: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add web/src/types.ts web/src/api.ts
git commit -m "feat: add trend types and fetchTrends API client"
```

---

### Task 6: Frontend — Analytics View Component

**Files:**
- Create: `web/src/components/analytics-view.ts`
- Modify: `web/src/state.ts:8` (add 'analytics' to view union)
- Modify: `web/src/main.ts` (add import, mount, keyboard shortcut)
- Modify: `web/src/components/topbar.ts:47` (add Analytics tab button)
- Modify: `web/src/components/help-overlay.ts:29` (add `a` shortcut)

- [ ] **Step 1: Add 'analytics' to view type in state.ts**

Change line 8:
```typescript
// Old:
view: 'list' | 'graph' | 'history';
// New:
view: 'list' | 'graph' | 'history' | 'analytics';
```

- [ ] **Step 2: Create analytics-view.ts**

Create `web/src/components/analytics-view.ts`:

```typescript
import { state, subscribe } from '../state';
import { fetchTrends, fetchRepos } from '../api';
import type { TrendWindow } from '../api';
import type { TrendResult, RepoEntry } from '../types';
import { formatTokens } from '../utils';
import { renderCards, destroyCards } from './analytics-cards';

let container: HTMLElement | null = null;
let root: HTMLElement | null = null;
let currentData: TrendResult | null = null;
let currentWindow: TrendWindow = (localStorage.getItem('claude-monitor-analytics-window') as TrendWindow) || '7d';
let currentRepo = '';
let repos: RepoEntry[] = [];

function getCardState(): Record<string, boolean> {
  try {
    return JSON.parse(localStorage.getItem('claude-monitor-analytics-cards') || '{}');
  } catch { return {}; }
}

function saveCardState(s: Record<string, boolean>): void {
  localStorage.setItem('claude-monitor-analytics-cards', JSON.stringify(s));
}

async function loadData(): Promise<void> {
  if (!root) return;
  const summaryEl = root.querySelector('.analytics-summary');
  if (summaryEl) summaryEl.classList.add('loading');

  try {
    const [data, repoList] = await Promise.all([
      fetchTrends(currentWindow, currentRepo),
      repos.length ? Promise.resolve(repos) : fetchRepos(),
    ]);
    currentData = data;
    repos = repoList;
    renderContent();
  } catch (err) {
    console.error('Failed to load analytics:', err);
    const cards = root?.querySelector('.analytics-cards');
    if (cards) cards.innerHTML = '<div class="feed-empty">Failed to load analytics data</div>';
  }
}

function renderContent(): void {
  if (!root || !currentData) return;

  // Summary
  const s = currentData.summary;
  const summaryEl = root.querySelector('.analytics-summary');
  if (summaryEl) {
    summaryEl.classList.remove('loading');
    summaryEl.innerHTML = `
      <div class="analytics-stat">
        <div class="analytics-stat-val green">$${s.totalCost.toFixed(2)}</div>
        <div class="analytics-stat-label">Total Spend</div>
      </div>
      <div class="analytics-stat">
        <div class="analytics-stat-val blue">${formatTokens(s.effectiveTokens)}</div>
        <div class="analytics-stat-label">Tokens (effective)</div>
      </div>
      <div class="analytics-stat">
        <div class="analytics-stat-val orange">${s.cacheHitPct.toFixed(0)}%</div>
        <div class="analytics-stat-label">Cache Hit Rate</div>
      </div>
      <div class="analytics-stat">
        <div class="analytics-stat-val purple">${s.sessionCount}</div>
        <div class="analytics-stat-label">Sessions</div>
      </div>
    `;
  }

  // Repo filter dropdown
  const repoSelect = root.querySelector<HTMLSelectElement>('.analytics-repo-filter');
  if (repoSelect && repoSelect.options.length <= 1) {
    for (const r of repos) {
      const opt = document.createElement('option');
      opt.value = r.id;
      opt.textContent = `${r.name} ($${r.totalCost.toFixed(2)})`;
      repoSelect.appendChild(opt);
    }
  }

  // Cards
  const cardsEl = root.querySelector<HTMLElement>('.analytics-cards');
  if (cardsEl) {
    renderCards(cardsEl, currentData, getCardState(), (id, collapsed) => {
      const s = getCardState();
      s[id] = collapsed;
      saveCardState(s);
    });
  }
}

export function render(mountPoint: HTMLElement): void {
  container = mountPoint;

  root = document.createElement('div');
  root.className = 'analytics-view';
  root.style.display = 'none';

  root.innerHTML = `
    <div class="analytics-toolbar">
      <div class="analytics-window-toggle">
        <button class="analytics-win-btn ${currentWindow === '24h' ? 'active' : ''}" data-window="24h">24H</button>
        <button class="analytics-win-btn ${currentWindow === '7d' ? 'active' : ''}" data-window="7d">7D</button>
        <button class="analytics-win-btn ${currentWindow === '30d' ? 'active' : ''}" data-window="30d">30D</button>
      </div>
      <div class="analytics-repo-selector">
        <select class="analytics-repo-filter">
          <option value="">All repos</option>
        </select>
      </div>
    </div>
    <div class="analytics-summary loading"></div>
    <div class="analytics-cards"></div>
  `;

  // Window toggle
  root.querySelectorAll<HTMLButtonElement>('.analytics-win-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      currentWindow = btn.dataset.window as TrendWindow;
      localStorage.setItem('claude-monitor-analytics-window', currentWindow);
      root!.querySelectorAll('.analytics-win-btn').forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      destroyCards();
      loadData();
    });
  });

  // Repo filter
  const repoSelect = root.querySelector<HTMLSelectElement>('.analytics-repo-filter');
  if (repoSelect) {
    repoSelect.addEventListener('change', () => {
      currentRepo = repoSelect.value;
      destroyCards();
      loadData();
    });
  }

  container.appendChild(root);

  subscribe((_state, changed) => {
    if (!changed.has('view')) return;
    if (!root) return;
    const visible = state.view === 'analytics';
    root.style.display = visible ? '' : 'none';
    if (visible && !currentData) {
      loadData();
    }
    if (!visible) {
      destroyCards();
    }
  });
}
```

- [ ] **Step 3: Add import and mount in main.ts**

Add import after line 11:
```typescript
import { render as renderAnalyticsView } from './components/analytics-view';
```

Add mount after line 33 (`renderTimeline(feedMount);`):
```typescript
renderAnalyticsView(feedMount);
```

Add keyboard shortcut in the switch block (after the `'h'` case, before `'?'`):
```typescript
    case 'a':
      update({ view: state.view === 'analytics' ? 'list' : 'analytics' });
      break;
```

- [ ] **Step 4: Add Analytics button to topbar.ts**

In the view-toggle nav (line 47), add before the closing `</nav>`:
```html
<button class="view-btn" data-view="analytics" aria-pressed="false">ANALYTICS</button>
```

- [ ] **Step 5: Add shortcut to help-overlay.ts**

Add after line 29 (`<div class="help-row"><span>History view</span><kbd>h</kbd></div>`):
```html
<div class="help-row"><span>Analytics view</span><kbd>a</kbd></div>
```

- [ ] **Step 6: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build (analytics-cards.ts doesn't exist yet — will fail on import). Create a stub first:

Create `web/src/components/analytics-cards.ts`:
```typescript
import type { TrendResult } from '../types';

export function renderCards(
  _container: HTMLElement,
  _data: TrendResult,
  _cardState: Record<string, boolean>,
  _onToggle: (id: string, collapsed: boolean) => void,
): void {
  // Stub — implemented in Task 7
}

export function destroyCards(): void {
  // Stub
}
```

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 7: Commit**

```bash
git add web/src/state.ts web/src/main.ts web/src/components/topbar.ts web/src/components/help-overlay.ts web/src/components/analytics-view.ts web/src/components/analytics-cards.ts
git commit -m "feat: add Analytics view shell with tab navigation and data loading"
```

---

### Task 7: Frontend — Analytics Cards with Chart.js

**Files:**
- Modify: `web/src/components/analytics-cards.ts` (replace stub)

- [ ] **Step 1: Implement the full analytics-cards.ts**

Replace the stub with the full implementation:

```typescript
import type { TrendResult } from '../types';
import { Chart, COLORS, DARK_THEME } from '../chart-config';
import { formatTokens } from '../utils';

type ToggleFn = (id: string, collapsed: boolean) => void;

interface CardDef {
  id: string;
  title: string;
  subtitle: string;
  defaultExpanded: boolean;
  render: (canvas: HTMLCanvasElement, data: TrendResult) => Chart;
}

const charts: Map<string, Chart> = new Map();

const CARD_DEFS: CardDef[] = [
  {
    id: 'cost-trend',
    title: 'Cost Over Time',
    subtitle: 'area chart',
    defaultExpanded: true,
    render: (canvas, data) => new Chart(canvas, {
      type: 'line',
      data: {
        labels: data.buckets.map(b => b.date),
        datasets: [{
          label: 'Cost ($)',
          data: data.buckets.map(b => b.cost),
          borderColor: COLORS.green,
          backgroundColor: COLORS.green + '30',
          fill: true,
          tension: 0.3,
          pointRadius: 3,
        }],
      },
      options: { ...DARK_THEME, plugins: { ...DARK_THEME.plugins, legend: { display: false } } },
    }),
  },
  {
    id: 'token-consumption',
    title: 'Token Consumption',
    subtitle: 'stacked bar',
    defaultExpanded: true,
    render: (canvas, data) => new Chart(canvas, {
      type: 'bar',
      data: {
        labels: data.buckets.map(b => b.date),
        datasets: [
          { label: 'Cache Read', data: data.buckets.map(b => b.cacheReadTokens), backgroundColor: COLORS.green },
          { label: 'Input', data: data.buckets.map(b => b.inputTokens), backgroundColor: COLORS.blue },
          { label: 'Cache Create', data: data.buckets.map(b => b.cacheCreationTokens), backgroundColor: COLORS.purple },
          { label: 'Output', data: data.buckets.map(b => b.outputTokens), backgroundColor: COLORS.orange },
        ],
      },
      options: {
        ...DARK_THEME,
        scales: {
          ...DARK_THEME.scales,
          x: { ...DARK_THEME.scales.x, stacked: true },
          y: { ...DARK_THEME.scales.y, stacked: true, ticks: { ...DARK_THEME.scales.y.ticks, callback: (v) => formatTokens(v as number) } },
        },
      },
    }),
  },
  {
    id: 'cost-by-repo',
    title: 'Cost by Repo',
    subtitle: 'horizontal bar',
    defaultExpanded: false,
    render: (canvas, data) => new Chart(canvas, {
      type: 'bar',
      data: {
        labels: data.byRepo.map(r => r.repoName || r.repoId),
        datasets: [{
          label: 'Cost ($)',
          data: data.byRepo.map(r => r.cost),
          backgroundColor: COLORS.blue,
        }],
      },
      options: { ...DARK_THEME, indexAxis: 'y' as const, plugins: { ...DARK_THEME.plugins, legend: { display: false } } },
    }),
  },
  {
    id: 'model-mix',
    title: 'Model Mix',
    subtitle: 'doughnut',
    defaultExpanded: false,
    render: (canvas, data) => {
      const modelColors = [COLORS.purple, COLORS.blue, COLORS.orange, COLORS.green, COLORS.yellow];
      return new Chart(canvas, {
        type: 'doughnut',
        data: {
          labels: data.byModel.map(m => m.model.replace('claude-', '').replace('-4-6', '')),
          datasets: [{
            data: data.byModel.map(m => m.cost),
            backgroundColor: data.byModel.map((_, i) => modelColors[i % modelColors.length]),
            borderWidth: 0,
          }],
        },
        options: {
          ...DARK_THEME,
          scales: undefined,
          plugins: {
            ...DARK_THEME.plugins,
            tooltip: {
              ...DARK_THEME.plugins.tooltip,
              callbacks: { label: (ctx) => `$${(ctx.raw as number).toFixed(2)}` },
            },
          },
        },
      });
    },
  },
  {
    id: 'cache-efficiency',
    title: 'Cache Efficiency Over Time',
    subtitle: 'line chart',
    defaultExpanded: false,
    render: (canvas, data) => new Chart(canvas, {
      type: 'line',
      data: {
        labels: data.buckets.map(b => b.date),
        datasets: [{
          label: 'Cache Hit %',
          data: data.buckets.map(b => b.cacheHitPct),
          borderColor: COLORS.orange,
          backgroundColor: COLORS.orange + '20',
          fill: true,
          tension: 0.3,
          pointRadius: 3,
        }],
      },
      options: {
        ...DARK_THEME,
        plugins: { ...DARK_THEME.plugins, legend: { display: false } },
        scales: { ...DARK_THEME.scales, y: { ...DARK_THEME.scales.y, min: 0, max: 100 } },
      },
    }),
  },
  {
    id: 'session-cost-dist',
    title: 'Session Cost Distribution',
    subtitle: 'avg / median / p95',
    defaultExpanded: false,
    render: (canvas, data) => new Chart(canvas, {
      type: 'line',
      data: {
        labels: data.buckets.map(b => b.date),
        datasets: [
          { label: 'Avg', data: data.buckets.map(b => b.avgSessionCost), borderColor: COLORS.blue, tension: 0.3, pointRadius: 2 },
          { label: 'Median', data: data.buckets.map(b => b.medianSessionCost), borderColor: COLORS.green, tension: 0.3, pointRadius: 2 },
          { label: 'P95', data: data.buckets.map(b => b.p95SessionCost), borderColor: COLORS.red, tension: 0.3, pointRadius: 2 },
        ],
      },
      options: DARK_THEME,
    }),
  },
  {
    id: 'tokens-per-session',
    title: 'Tokens per Session',
    subtitle: 'trend line',
    defaultExpanded: false,
    render: (canvas, data) => new Chart(canvas, {
      type: 'line',
      data: {
        labels: data.buckets.map(b => b.date),
        datasets: [{
          label: 'Avg Tokens/Session',
          data: data.buckets.map(b => b.avgSessionTokens),
          borderColor: COLORS.purple,
          tension: 0.3,
          pointRadius: 3,
        }],
      },
      options: {
        ...DARK_THEME,
        plugins: { ...DARK_THEME.plugins, legend: { display: false } },
        scales: { ...DARK_THEME.scales, y: { ...DARK_THEME.scales.y, ticks: { ...DARK_THEME.scales.y.ticks, callback: (v) => formatTokens(v as number) } } },
      },
    }),
  },
  {
    id: 'output-input-ratio',
    title: 'Output / Input Ratio',
    subtitle: 'efficiency',
    defaultExpanded: false,
    render: (canvas, data) => new Chart(canvas, {
      type: 'line',
      data: {
        labels: data.buckets.map(b => b.date),
        datasets: [{
          label: 'Output/Input',
          data: data.buckets.map(b => b.outputInputRatio),
          borderColor: COLORS.yellow,
          tension: 0.3,
          pointRadius: 3,
        }],
      },
      options: {
        ...DARK_THEME,
        plugins: { ...DARK_THEME.plugins, legend: { display: false } },
      },
    }),
  },
];

export function renderCards(
  container: HTMLElement,
  data: TrendResult,
  cardState: Record<string, boolean>,
  onToggle: ToggleFn,
): void {
  destroyCards();
  container.innerHTML = '';

  for (const def of CARD_DEFS) {
    const collapsed = cardState[def.id] ?? !def.defaultExpanded;

    const card = document.createElement('div');
    card.className = 'analytics-card';
    card.dataset.cardId = def.id;

    const header = document.createElement('div');
    header.className = 'analytics-card-header';
    header.innerHTML = `
      <span class="analytics-card-toggle">${collapsed ? '▶' : '▼'}</span>
      <span class="analytics-card-title">${def.title}</span>
      <span class="analytics-card-subtitle">${def.subtitle}</span>
    `;

    const body = document.createElement('div');
    body.className = 'analytics-card-body';
    body.style.display = collapsed ? 'none' : '';

    const canvas = document.createElement('canvas');
    canvas.height = 200;
    body.appendChild(canvas);

    header.addEventListener('click', () => {
      const isCollapsed = body.style.display === 'none';
      body.style.display = isCollapsed ? '' : 'none';
      header.querySelector('.analytics-card-toggle')!.textContent = isCollapsed ? '▼' : '▶';

      if (isCollapsed) {
        // Create chart on expand
        const chart = def.render(canvas, data);
        charts.set(def.id, chart);
      } else {
        // Destroy chart on collapse
        charts.get(def.id)?.destroy();
        charts.delete(def.id);
      }

      onToggle(def.id, !isCollapsed);
    });

    card.appendChild(header);
    card.appendChild(body);
    container.appendChild(card);

    // Create chart if expanded by default
    if (!collapsed) {
      const chart = def.render(canvas, data);
      charts.set(def.id, chart);
    }
  }
}

export function destroyCards(): void {
  for (const chart of charts.values()) {
    chart.destroy();
  }
  charts.clear();
}
```

- [ ] **Step 2: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add web/src/components/analytics-cards.ts
git commit -m "feat: implement 8 analytics metric cards with Chart.js"
```

---

### Task 8: Frontend — Analytics CSS

**Files:**
- Create: `web/src/styles/analytics.css`
- Modify: `web/src/components/analytics-view.ts:1` (add CSS import)

- [ ] **Step 1: Create analytics.css**

Create `web/src/styles/analytics.css`:

```css
/* Analytics view */
.analytics-view {
  padding: 0;
  height: 100%;
  overflow-y: auto;
  background: var(--bg-primary, #0d0d1a);
}

.analytics-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 20px;
  border-bottom: 1px solid var(--border, #2a2a4a);
  background: var(--bg-secondary, #16162a);
  position: sticky;
  top: 0;
  z-index: 10;
}

.analytics-window-toggle {
  display: flex;
  gap: 4px;
}

.analytics-win-btn {
  padding: 4px 14px;
  background: var(--bg-tertiary, #2a2a4a);
  border: 1px solid transparent;
  border-radius: 4px;
  color: var(--text-secondary, #888);
  cursor: pointer;
  font-family: monospace;
  font-size: 12px;
  transition: all 0.15s;
}
.analytics-win-btn:hover {
  background: var(--bg-hover, #3a3a5a);
  color: var(--text-primary, #e0e0e0);
}
.analytics-win-btn.active {
  background: var(--accent, #4a3aaa);
  color: white;
  border-color: var(--accent, #4a3aaa);
}

.analytics-repo-filter {
  background: var(--bg-tertiary, #2a2a4a);
  border: 1px solid var(--border, #2a2a4a);
  border-radius: 4px;
  color: var(--text-primary, #e0e0e0);
  padding: 4px 12px;
  font-family: monospace;
  font-size: 12px;
  cursor: pointer;
}

/* Summary row */
.analytics-summary {
  display: flex;
  gap: 1px;
  background: var(--border, #2a2a4a);
}
.analytics-summary.loading {
  opacity: 0.5;
}
.analytics-stat {
  flex: 1;
  padding: 16px 20px;
  background: var(--bg-primary, #0d0d1a);
  text-align: center;
}
.analytics-stat-val {
  font-size: 24px;
  font-weight: bold;
  font-family: monospace;
}
.analytics-stat-val.green { color: #4ae68a; }
.analytics-stat-val.blue { color: #6ab4ff; }
.analytics-stat-val.orange { color: #ffa64a; }
.analytics-stat-val.purple { color: #c49aff; }
.analytics-stat-label {
  font-size: 11px;
  color: var(--text-secondary, #888);
  margin-top: 4px;
  font-family: monospace;
}

/* Cards */
.analytics-cards {
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.analytics-card {
  border: 1px solid var(--border, #2a2a4a);
  border-radius: 8px;
  overflow: hidden;
}
.analytics-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px;
  background: var(--bg-secondary, #1e1e3a);
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}
.analytics-card-header:hover {
  background: var(--bg-hover, #252545);
}
.analytics-card-toggle {
  font-size: 10px;
  color: var(--text-secondary, #888);
  width: 12px;
}
.analytics-card-title {
  font-weight: bold;
  font-size: 13px;
  color: var(--text-primary, #e0e0e0);
  font-family: monospace;
}
.analytics-card-subtitle {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-secondary, #666);
  font-family: monospace;
}
.analytics-card-body {
  padding: 16px;
  background: var(--bg-primary, #16162a);
  min-height: 200px;
}
.analytics-card-body canvas {
  width: 100% !important;
}
```

- [ ] **Step 2: Add CSS import to analytics-view.ts**

Add at the top of `web/src/components/analytics-view.ts`:
```typescript
import '../styles/analytics.css';
```

- [ ] **Step 3: Verify build passes**

Run: `cd web && npm run build`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add web/src/styles/analytics.css web/src/components/analytics-view.ts
git commit -m "feat: add analytics view CSS with dark theme styling"
```

---

### Task 9: Full Build and Integration Test

**Files:** None (verification only)

- [ ] **Step 1: Full backend build and test**

Run: `make build`
Expected: Clean build with no errors

- [ ] **Step 2: Run Go test suite**

Run: `make test`
Expected: All tests pass (including new trend tests)

- [ ] **Step 3: Manual smoke test**

Run: `./claude-monitor`
Open: `http://localhost:7700`

Verify:
1. Analytics tab appears in the topbar
2. Pressing `a` toggles Analytics view
3. Time range buttons (24H/7D/30D) work
4. Charts render if historical data exists
5. Cards collapse/expand correctly
6. Switching back to List view works
7. Token counts on session cards show effective values (not raw input_tokens)

- [ ] **Step 4: Commit any fixes found during smoke test**

```bash
git add -A
git commit -m "fix: address issues found during analytics smoke test"
```

(Skip this step if no fixes needed.)

- [ ] **Step 5: Final commit if all clean**

```bash
git add -A
git commit -m "feat: complete cost & token analytics view"
```
