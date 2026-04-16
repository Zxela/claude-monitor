package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zxela/claude-monitor/api"
	"github.com/zxela/claude-monitor/internal/docker"
	"github.com/zxela/claude-monitor/internal/hub"
	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/watcher"
)

// Named constants replacing magic numbers (Issue 36).
const (
	defaultPageLimit = 50
	defaultHotDays   = 30
	defaultWarmDays  = 90
	shutdownTimeout  = 30 * time.Second
)

// weekStartTime returns the start of the ISO week (Monday 00:00) for the given time (Issue 37).
func weekStartTime(now time.Time) time.Time {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return todayStart.AddDate(0, 0, -(weekday - 1))
}

// handleWs upgrades the connection to WebSocket.
func handleWs(h *hub.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWs(h, w, r)
	}
}

// handleSessions serves the unified sessions endpoint.
func handleSessions(sessionStore *session.Store, historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// ?active=true — return only live active sessions
		if q.Get("active") == "true" {
			sessions := sessionStore.All()
			var active []*session.Session
			for _, s := range sessions {
				if s.IsActive {
					active = append(active, s)
				}
			}
			if active == nil {
				active = []*session.Session{}
			}
			writeJSON(w, active)
			return
		}

		// ?group=activity — return time-bucketed sessions
		if q.Get("group") == "activity" {
			now := time.Now()
			hourAgo := now.Add(-1 * time.Hour)
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			yesterdayStart := todayStart.Add(-24 * time.Hour)
			weekStart := weekStartTime(now)

			type grouped struct {
				Active    []*session.Session `json:"active"`
				LastHour  []*session.Session `json:"lastHour"`
				Today     []*session.Session `json:"today"`
				Yesterday []*session.Session `json:"yesterday"`
				ThisWeek  []*session.Session `json:"thisWeek"`
				Older     []*session.Session `json:"older"`
			}
			g := grouped{
				Active: []*session.Session{}, LastHour: []*session.Session{},
				Today: []*session.Session{}, Yesterday: []*session.Session{},
				ThisWeek: []*session.Session{}, Older: []*session.Session{},
			}

			// Start with live sessions (have real-time status).
			seen := make(map[string]bool)
			for _, s := range sessionStore.All() {
				seen[s.ID] = true
				if s.IsActive {
					g.Active = append(g.Active, s)
					continue
				}
				la := s.LastActive
				switch {
				case la.After(hourAgo):
					g.LastHour = append(g.LastHour, s)
				case la.After(todayStart):
					g.Today = append(g.Today, s)
				case la.After(yesterdayStart):
					g.Yesterday = append(g.Yesterday, s)
				case la.After(weekStart):
					g.ThisWeek = append(g.ThisWeek, s)
				default:
					g.Older = append(g.Older, s)
				}
			}

			// Fill in from DB for sessions not in the live store.
			if dbRows, err := historyDB.ListSessions(500, 0); err == nil {
				for _, row := range dbRows {
					if seen[row.ID] {
						continue
					}
					s := row.ToSession()
					la := s.LastActive
					switch {
					case la.After(hourAgo):
						g.LastHour = append(g.LastHour, s)
					case la.After(todayStart):
						g.Today = append(g.Today, s)
					case la.After(yesterdayStart):
						g.Yesterday = append(g.Yesterday, s)
					case la.After(weekStart):
						g.ThisWeek = append(g.ThisWeek, s)
					default:
						g.Older = append(g.Older, s)
					}
				}
			}

			writeJSON(w, g)
			return
		}

		// Default: paginated list from DB, with optional ?repo= filter
		limit := defaultPageLimit
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
			limit = n
		}
		offset := 0
		if n, err := strconv.Atoi(q.Get("offset")); err == nil && n >= 0 {
			offset = n
		}
		var rows []store.SessionRow
		var err error
		if repoID := q.Get("repo"); repoID != "" {
			rows, err = historyDB.ListSessionsByRepo(repoID, limit, offset)
		} else {
			rows, err = historyDB.ListSessions(limit, offset)
		}
		if err != nil {
			log.Printf("list sessions error: %v", err)
			writeJSONError(w, "failed to list sessions", http.StatusInternalServerError)
			return
		}
		sessions := store.SessionRowsToSessions(rows)
		if sessions == nil {
			sessions = []*session.Session{}
		}
		writeJSON(w, sessions)
	}
}

// handleSessionByID serves GET /api/sessions/{id}.
func handleSessionByID(sessionStore *session.Store, historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		// Try live store first
		if sess, ok := sessionStore.Get(id); ok {
			writeJSON(w, sess)
			return
		}
		// Fall back to DB
		row, err := historyDB.GetSession(id)
		if err != nil {
			writeJSONError(w, "failed to get session", http.StatusInternalServerError)
			return
		}
		if row == nil {
			writeJSONError(w, "session not found", http.StatusNotFound)
			return
		}
		writeJSON(w, row.ToSession())
	}
}

// handleStats serves GET /api/stats.
func handleStats(sessionStore *session.Store, historyDB *store.DB, fw *watcher.Watcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type statsResponse struct {
			TotalCost           float64            `json:"totalCost"`
			InputTokens         int64              `json:"inputTokens"`
			OutputTokens        int64              `json:"outputTokens"`
			CacheReadTokens     int64              `json:"cacheReadTokens"`
			CacheCreationTokens int64              `json:"cacheCreationTokens"`
			SessionCount        int                `json:"sessionCount"`
			ActiveSessions      int                `json:"activeSessions"`
			CacheHitPct         float64            `json:"cacheHitPct"`
			CostRate            float64            `json:"costRate"`
			CostByModel         map[string]float64 `json:"costByModel"`
			CostByRepo          map[string]float64 `json:"costByRepo"`
			DroppedEvents       int64              `json:"droppedEvents"`
		}

		now := time.Now()
		var since time.Time
		switch r.URL.Query().Get("window") {
		case "today":
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		case "week":
			since = weekStartTime(now)
		case "month":
			since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		}

		agg, err := historyDB.AggregateStats(since)
		if err != nil {
			log.Printf("stats aggregate error: %v", err)
			writeJSONError(w, "failed to compute stats", http.StatusInternalServerError)
			return
		}

		// Count active sessions and compute cost rate from live store.
		var activeSessions int
		var costRate float64
		for _, sess := range sessionStore.All() {
			if sess.IsActive {
				activeSessions++
				costRate += sess.CostRate
			}
		}

		resp := statsResponse{
			TotalCost:           agg.TotalCost,
			InputTokens:         agg.InputTokens,
			OutputTokens:        agg.OutputTokens,
			CacheReadTokens:     agg.CacheReadTokens,
			CacheCreationTokens: agg.CacheCreationTokens,
			SessionCount:        agg.SessionCount,
			ActiveSessions:      activeSessions,
			CostRate:            costRate,
			CostByModel:         agg.CostByModel,
			CostByRepo:          agg.CostByRepo,
			DroppedEvents:       fw.DroppedEvents(),
		}
		if resp.CostByModel == nil {
			resp.CostByModel = make(map[string]float64)
		}
		if resp.CostByRepo == nil {
			resp.CostByRepo = make(map[string]float64)
		}

		totalInput := resp.InputTokens + resp.CacheReadTokens + resp.CacheCreationTokens
		if totalInput > 0 {
			resp.CacheHitPct = float64(resp.CacheReadTokens) / float64(totalInput) * 100
		}

		writeJSON(w, resp)
	}
}

// handleStatsTrends serves GET /api/stats/trends.
func handleStatsTrends(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		writeJSON(w, result)
	}
}

// handleRepos serves GET /api/repos.
func handleRepos(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repos, err := historyDB.ListRepos()
		if err != nil {
			writeJSONError(w, "failed to list repos", http.StatusInternalServerError)
			return
		}
		if repos == nil {
			repos = []store.RepoRow{}
		}
		writeJSON(w, repos)
	}
}

// handleRepoStats serves GET /api/repos/{id}/stats.
func handleRepoStats(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID := r.PathValue("id")
		agg, err := historyDB.AggregateStatsByRepo(repoID)
		if err != nil {
			writeJSONError(w, "failed to get repo stats", http.StatusInternalServerError)
			return
		}
		writeJSON(w, agg)
	}
}

// handleRepoSessions serves GET /api/repos/{id}/sessions.
func handleRepoSessions(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repoID := r.PathValue("id")
		limit := defaultPageLimit
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
			limit = n
		}
		offset := 0
		if n, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && n >= 0 {
			offset = n
		}
		rows, err := historyDB.ListSessionsByRepo(repoID, limit, offset)
		if err != nil {
			writeJSONError(w, "failed to list repo sessions", http.StatusInternalServerError)
			return
		}
		sessions := store.SessionRowsToSessions(rows)
		if sessions == nil {
			sessions = []*session.Session{}
		}
		writeJSON(w, sessions)
	}
}

// handleSearch serves GET /api/search.
func handleSearch(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		limit := defaultPageLimit
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
			limit = n
		}
		results, err := historyDB.SearchFTS(query, limit)
		if err != nil {
			log.Printf("search error: %v", err)
			writeJSONError(w, "search failed", http.StatusInternalServerError)
			return
		}
		if results == nil {
			results = []store.EventRow{}
		}
		writeJSON(w, results)
	}
}

// handleSearchFull serves GET /api/search/full.
func handleSearchFull(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		limit := defaultPageLimit
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
			limit = n
		}
		results, err := historyDB.SearchFullContent(query, limit)
		if err != nil {
			log.Printf("full search error: %v", err)
			writeJSONError(w, "search failed", http.StatusInternalServerError)
			return
		}
		if results == nil {
			results = []store.EventRow{}
		}
		writeJSON(w, results)
	}
}

// handleSearchCombined serves GET /api/search/combined.
// It runs FTS5 first; if fewer than 10 results are returned, it also runs a
// full-content scan, merges the two result sets (deduplicating by event ID),
// and returns a wrapper with a meta.searchedFull boolean so callers can tell
// whether the slower path was exercised.
func handleSearchCombined(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[],"meta":{"searchedFull":false}}`))
			return
		}
		limit := defaultPageLimit
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 200 {
			limit = n
		}

		ftsResults, err := historyDB.SearchFTS(query, limit)
		if err != nil {
			log.Printf("combined search FTS error: %v", err)
			writeJSONError(w, "search failed", http.StatusInternalServerError)
			return
		}
		if ftsResults == nil {
			ftsResults = []store.EventRow{}
		}

		searchedFull := false
		if len(ftsResults) < 10 {
			fullResults, err := historyDB.SearchFullContent(query, limit)
			if err != nil {
				log.Printf("combined search full error: %v", err)
				writeJSONError(w, "search failed", http.StatusInternalServerError)
				return
			}
			searchedFull = true

			// Merge, deduplicating by event ID.
			seen := make(map[int64]bool, len(ftsResults))
			for _, r := range ftsResults {
				seen[r.ID] = true
			}
			for _, r := range fullResults {
				if !seen[r.ID] {
					seen[r.ID] = true
					ftsResults = append(ftsResults, r)
				}
			}
		}

		writeJSON(w, map[string]any{
			"results": ftsResults,
			"meta":    map[string]bool{"searchedFull": searchedFull},
		})
	}
}

// handleSessionEvents serves GET /api/sessions/{id}/events.
func handleSessionEvents(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		q := r.URL.Query()

		// ?pinned=true — all error + agent events
		if q.Get("pinned") == "true" || q.Get("errors") == "true" {
			events, err := historyDB.ListPinnedEvents(id)
			if err != nil {
				writeJSONError(w, "failed to list events", http.StatusInternalServerError)
				return
			}
			if events == nil {
				events = []store.EventRow{}
			}
			writeJSON(w, events)
			return
		}

		// ?last=N — most recent N events
		if lastStr := q.Get("last"); lastStr != "" {
			n := defaultPageLimit
			if v, err := strconv.Atoi(lastStr); err == nil && v > 0 {
				n = v
			}
			events, err := historyDB.ListRecentEvents(id, n)
			if err != nil {
				writeJSONError(w, "failed to list events", http.StatusInternalServerError)
				return
			}
			if events == nil {
				events = []store.EventRow{}
			}
			writeJSON(w, events)
			return
		}

		limit := 100
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
			limit = n
		}
		offset := 0
		if n, err := strconv.Atoi(q.Get("offset")); err == nil && n >= 0 {
			offset = n
		}
		events, err := historyDB.ListEvents(id, limit, offset)
		if err != nil {
			writeJSONError(w, "failed to list events", http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.EventRow{}
		}
		writeJSON(w, events)
	}
}

// handleSessionReplay serves GET /api/sessions/{id}/replay.
func handleSessionReplay(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		q := r.URL.Query()
		limit := 1000
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 10000 {
			limit = n
		}
		offset := 0
		if n, err := strconv.Atoi(q.Get("offset")); err == nil && n >= 0 {
			offset = n
		}
		events, err := historyDB.ListEvents(id, limit, offset)
		if err != nil {
			writeJSONError(w, "failed to list events", http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.EventRow{}
		}
		writeJSON(w, map[string]any{
			"sessionId": id,
			"events":    events,
			"limit":     limit,
			"offset":    offset,
		})
	}
}

// handleSettings serves GET /api/settings.
func handleSettings(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		settings, err := historyDB.AllSettings()
		if err != nil {
			writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
			return
		}
		writeJSON(w, settings)
	}
}

// validSettingKeys is the allowlist of setting keys that can be updated via the API.
var validSettingKeys = map[string]bool{
	"retention_hot_days":  true,
	"retention_warm_days": true,
}

// handleSettingsUpdate serves PUT /api/settings/{key}.
func handleSettingsUpdate(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if !validSettingKeys[key] {
			writeJSONError(w, "unknown setting key", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
		var body struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := historyDB.SetSetting(key, body.Value); err != nil {
			writeJSONError(w, "failed to update setting", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// handleStorage serves GET /api/storage.
func handleStorage(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := historyDB.StorageInfo()
		if err != nil {
			writeJSONError(w, "failed to get storage info", http.StatusInternalServerError)
			return
		}
		writeJSON(w, info)
	}
}

// handleCacheClear serves DELETE /api/cache/repos.
func handleCacheClear(resolver *repo.Resolver, historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resolver.ClearCache()
		if err := historyDB.ClearCwdRepos(); err != nil {
			writeJSONError(w, "failed to clear cache", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// handleSessionStop serves POST /api/sessions/{id}/stop.
func handleSessionStop(sessionStore *session.Store, dc **docker.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sess, ok := sessionStore.Get(id)
		if !ok {
			writeJSONError(w, "session not found", http.StatusNotFound)
			return
		}
		// Extract container name from sessionName (format: "container / project")
		containerName := ""
		if parts := strings.SplitN(sess.SessionName, " / ", 2); len(parts) == 2 {
			containerName = parts[0]
		}
		if containerName == "" || *dc == nil {
			writeJSONError(w, "not a Docker session or Docker not available", http.StatusBadRequest)
			return
		}
		// Validate container name contains only safe characters.
		if !validContainerName.MatchString(containerName) {
			writeJSONError(w, "invalid container name", http.StatusBadRequest)
			return
		}
		stopCtx, stopCancel := context.WithTimeout(r.Context(), shutdownTimeout)
		defer stopCancel()
		if err := (*dc).StopContainer(stopCtx, containerName); err != nil {
			log.Printf("stop container %s: %v", containerName, err)
			writeJSONError(w, "failed to stop container", http.StatusInternalServerError)
			return
		}
		log.Printf("stopped container %s for session %s", containerName, id)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

// handleHealth serves GET /health.
func handleHealth(historyDB *store.DB, fw *watcher.Watcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := historyDB.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			writeJSON(w, map[string]interface{}{"ok": false, "db": err.Error(), "droppedEvents": fw.DroppedEvents()})
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "db": "ok", "droppedEvents": fw.DroppedEvents()})
	}
}

// handleVersion serves GET /api/version.
func handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"version": version})
	}
}

// applyDBPricingToParser loads all model pricing from the DB and applies it to the
// parser's active pricing table. It is used both at startup (main.go) and after
// each upsert (handlePricingUpdate) to keep the in-memory table in sync.
func applyDBPricingToParser(historyDB *store.DB) error {
	dbPricing, err := historyDB.AllModelPricing()
	if err != nil {
		return err
	}
	converted := make(map[string]parser.ExternalPricing, len(dbPricing))
	for k, v := range dbPricing {
		converted[k] = parser.ExternalPricing{
			InputPerMTok:       v.InputPerMTok,
			OutputPerMTok:      v.OutputPerMTok,
			CacheReadPerMTok:   v.CacheReadPerMTok,
			CacheCreatePerMTok: v.CacheCreatePerMTok,
		}
	}
	parser.SetPricingTable(converted)
	return nil
}

// isValidPrice returns true when v is a finite, non-negative number.
func isValidPrice(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// handlePricingUpdate serves PUT /api/pricing/{model_prefix}.
// It persists new or updated pricing for a model prefix and immediately applies it
// to the parser's active pricing table for subsequent cost computations.
func handlePricingUpdate(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prefix := r.PathValue("model_prefix")
		if prefix == "" {
			writeJSONError(w, "model_prefix is required", http.StatusBadRequest)
			return
		}
		if len(prefix) < 5 {
			writeJSONError(w, "model_prefix must be at least 5 characters", http.StatusBadRequest)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
		var body struct {
			InputPerMTok       float64 `json:"input_per_mtok"`
			OutputPerMTok      float64 `json:"output_per_mtok"`
			CacheReadPerMTok   float64 `json:"cache_read_per_mtok"`
			CacheCreatePerMTok float64 `json:"cache_create_per_mtok"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		// Validate: all price fields must be finite and non-negative.
		switch {
		case !isValidPrice(body.InputPerMTok):
			writeJSONError(w, "input_per_mtok must be a finite non-negative number", http.StatusBadRequest)
			return
		case !isValidPrice(body.OutputPerMTok):
			writeJSONError(w, "output_per_mtok must be a finite non-negative number", http.StatusBadRequest)
			return
		case !isValidPrice(body.CacheReadPerMTok):
			writeJSONError(w, "cache_read_per_mtok must be a finite non-negative number", http.StatusBadRequest)
			return
		case !isValidPrice(body.CacheCreatePerMTok):
			writeJSONError(w, "cache_create_per_mtok must be a finite non-negative number", http.StatusBadRequest)
			return
		}
		p := store.ModelPricing{
			InputPerMTok:       body.InputPerMTok,
			OutputPerMTok:      body.OutputPerMTok,
			CacheReadPerMTok:   body.CacheReadPerMTok,
			CacheCreatePerMTok: body.CacheCreatePerMTok,
		}
		if err := historyDB.UpsertModelPricing(prefix, p); err != nil {
			log.Printf("UpsertModelPricing %s: %v", prefix, err)
			writeJSONError(w, "failed to update pricing", http.StatusInternalServerError)
			return
		}
		// Reload all pricing from DB and re-apply to parser.
		if err := applyDBPricingToParser(historyDB); err != nil {
			log.Printf("AllModelPricing after upsert: %v — in-memory pricing may be stale", err)
			writeJSONError(w, "pricing persisted but failed to reload in-memory table", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// handlePricingGet serves GET /api/pricing.
// It returns all current model pricing entries from the database as JSON.
func handlePricingGet(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbPricing, err := historyDB.AllModelPricing()
		if err != nil {
			log.Printf("AllModelPricing: %v", err)
			writeJSONError(w, "failed to retrieve pricing", http.StatusInternalServerError)
			return
		}
		if dbPricing == nil {
			dbPricing = map[string]store.ModelPricing{}
		}
		writeJSON(w, dbPricing)
	}
}

// handleSwaggerYAML serves GET /swagger/openapi.yaml using the embedded spec (Issue 13).
func handleSwaggerYAML() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(api.OpenAPIYAML)
	}
}

// handleSwaggerUI serves GET /swagger.
func handleSwaggerUI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(swaggerHTML))
	}
}
