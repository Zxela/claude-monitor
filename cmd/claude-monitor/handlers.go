package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zxela/claude-monitor/api"
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

			// Fill in from DB for sessions not in the live store. Page through the
			// whole table rather than capping at 500 rows, so older buckets are
			// complete (the previous single 500-row slice silently dropped ~1100+
			// older sessions from the grouped sidebar).
			const groupPageSize = 1000
			for off := 0; ; off += groupPageSize {
				dbRows, err := historyDB.ListSessions(groupPageSize, off)
				if err != nil {
					break
				}
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
				if len(dbRows) < groupPageSize {
					break
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
		switch {
		case q.Get("workflow") != "":
			rows, err = historyDB.ListSessionsByWorkflow(q.Get("workflow"), limit, offset)
		case q.Get("repo") != "":
			rows, err = historyDB.ListSessionsByRepo(q.Get("repo"), limit, offset)
		default:
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
		switch window := r.URL.Query().Get("window"); window {
		case "today":
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		case "week":
			since = weekStartTime(now)
		case "month":
			since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		case "", "all":
			// lifetime aggregate (since stays zero)
		default:
			writeJSONError(w, "window must be today, week, month, or all", http.StatusBadRequest)
			return
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

// handleToolUsage serves GET /api/stats/tools — tool- and skill-invocation
// counts (with error counts) for the given window and optional repo, scoped by
// the owning session. Window vocabulary matches /api/stats/trends.
func handleToolUsage(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		var since time.Time
		switch window := r.URL.Query().Get("window"); window {
		case "24h":
			since = now.Add(-24 * time.Hour)
		case "7d":
			since = now.AddDate(0, 0, -7)
		case "30d":
			since = now.AddDate(0, 0, -30)
		case "", "all":
			// lifetime aggregate (since stays zero)
		default:
			writeJSONError(w, "window must be 24h, 7d, 30d, or all", http.StatusBadRequest)
			return
		}

		usage, err := historyDB.ToolUsage(since, r.URL.Query().Get("repo"))
		if err != nil {
			log.Printf("tool usage error: %v", err)
			writeJSONError(w, "failed to compute tool usage", http.StatusInternalServerError)
			return
		}
		writeJSON(w, usage)
	}
}

// handleSessionSkills serves GET /api/skills/sessions — a sparse map of
// sessionID → skills invoked (with use/error counts), used by History to badge
// the rows whose sessions invoked skills.
func handleSessionSkills(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m, err := historyDB.SessionSkills()
		if err != nil {
			log.Printf("session skills error: %v", err)
			writeJSONError(w, "failed to load session skills", http.StatusInternalServerError)
			return
		}
		writeJSON(w, m)
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
		exists, err := historyDB.RepoExists(repoID)
		if err != nil {
			writeJSONError(w, "failed to get repo stats", http.StatusInternalServerError)
			return
		}
		if !exists {
			writeJSONError(w, "repo not found", http.StatusNotFound)
			return
		}
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
		exists, err := historyDB.RepoExists(repoID)
		if err != nil {
			writeJSONError(w, "failed to list repo sessions", http.StatusInternalServerError)
			return
		}
		if !exists {
			writeJSONError(w, "repo not found", http.StatusNotFound)
			return
		}
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

// handleWorkflows serves GET /api/workflows.
func handleWorkflows(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workflows, err := historyDB.ListWorkflows()
		if err != nil {
			log.Printf("list workflows error: %v", err)
			writeJSONError(w, "failed to list workflows", http.StatusInternalServerError)
			return
		}
		if workflows == nil {
			workflows = []store.WorkflowRow{}
		}
		writeJSON(w, workflows)
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

		// ?last=N — most recent N events. Gate on presence (not a non-empty
		// value) so a bare ?last= defaults to the documented 50 most-recent.
		if q.Has("last") {
			n := defaultPageLimit
			if v, err := strconv.Atoi(q.Get("last")); err == nil && v > 0 {
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

// handleSessionAutopsy serves GET /api/sessions/{id}/autopsy as markdown.
func handleSessionAutopsy(sessionStore *session.Store, historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeJSONError(w, "missing session id", http.StatusBadRequest)
			return
		}

		var sess *session.Session
		if live, ok := sessionStore.Get(id); ok {
			sess = live
		} else {
			row, err := historyDB.GetSession(id)
			if err != nil {
				writeJSONError(w, "failed to get session", http.StatusInternalServerError)
				return
			}
			if row == nil {
				writeJSONError(w, "session not found", http.StatusNotFound)
				return
			}
			sess = row.ToSession()
		}

		events, err := historyDB.ListEvents(id, 5000, 0)
		if err != nil {
			writeJSONError(w, "failed to list session events", http.StatusInternalServerError)
			return
		}

		report := buildSessionAutopsyMarkdown(sess, events)
		filename := fmt.Sprintf("claude-monitor-autopsy-%s.md", safeFilenamePart(id))
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		_, _ = w.Write([]byte(report))
	}
}

func buildSessionAutopsyMarkdown(sess *session.Session, events []store.EventRow) string {
	const (
		maxCommands = 20
		maxErrors   = 12
	)

	var commands, errors []string
	seenCommands := make(map[string]bool)
	for _, ev := range events {
		if len(commands) < maxCommands && (strings.EqualFold(ev.ToolName, "bash") || strings.EqualFold(ev.ToolName, "shell")) {
			cmd := oneLine(firstNonEmpty(ev.ToolDetail, ev.ContentPreview, ev.FullContent))
			if cmd != "" && !seenCommands[cmd] {
				seenCommands[cmd] = true
				commands = append(commands, truncateRunes(cmd, 140))
			}
		}
		if len(errors) < maxErrors && ev.IsError {
			detail := firstNonEmpty(ev.Stderr, ev.ContentPreview, ev.ToolDetail, ev.FullContent)
			if detail == "" {
				detail = "error event"
			}
			errors = append(errors, mdInline(truncateRunes(detail, 180)))
		}
	}

	var sb strings.Builder
	sb.WriteString("# Session Autopsy\n\n")
	fmt.Fprintf(&sb, "- **Session ID:** `%s`\n", sess.ID)
	fmt.Fprintf(&sb, "- **Task:** %s\n", mdInline(sess.TaskDescription))
	fmt.Fprintf(&sb, "- **Model:** %s\n", mdInline(sess.Model))
	fmt.Fprintf(&sb, "- **Repo ID:** %s\n", mdInline(sess.RepoID))
	fmt.Fprintf(&sb, "- **Branch:** %s\n", mdInline(sess.GitBranch))
	fmt.Fprintf(&sb, "- **Started:** %s\n", mdInline(formatRFC3339(sess.StartedAt)))
	fmt.Fprintf(&sb, "- **Ended:** %s\n", mdInline(formatRFC3339(sess.LastActive)))
	fmt.Fprintf(&sb, "- **Duration:** %s\n", mdInline(sessionDuration(sess)))

	sb.WriteString("\n## Cost & Usage\n\n")
	fmt.Fprintf(&sb, "- **Total cost:** $%.2f\n", sess.TotalCost)
	fmt.Fprintf(&sb, "- **Input tokens:** %d\n", sess.InputTokens)
	fmt.Fprintf(&sb, "- **Output tokens:** %d\n", sess.OutputTokens)
	fmt.Fprintf(&sb, "- **Cache read tokens:** %d\n", sess.CacheReadTokens)
	fmt.Fprintf(&sb, "- **Cache creation tokens:** %d\n", sess.CacheCreationTokens)
	fmt.Fprintf(&sb, "- **Messages:** %d\n", sess.MessageCount)
	fmt.Fprintf(&sb, "- **Events:** %d\n", sess.EventCount)
	fmt.Fprintf(&sb, "- **Errors:** %d\n", sess.ErrorCount)

	sb.WriteString("\n## Commands Run\n\n")
	if len(commands) == 0 {
		sb.WriteString("_No shell command events captured._\n")
	} else {
		for _, cmd := range commands {
			fmt.Fprintf(&sb, "- `%s`\n", cmd)
		}
	}

	sb.WriteString("\n## Errors\n\n")
	if len(errors) == 0 {
		sb.WriteString("_No errors recorded._\n")
	} else {
		for _, errLine := range errors {
			fmt.Fprintf(&sb, "- %s\n", errLine)
		}
	}

	return sb.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

func mdInline(s string) string {
	s = oneLine(s)
	if s == "" {
		return "n/a"
	}
	return strings.ReplaceAll(s, "`", "'")
}

func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.Format(time.RFC3339)
}

func sessionDuration(sess *session.Session) string {
	if sess.StartedAt.IsZero() || sess.LastActive.IsZero() || sess.LastActive.Before(sess.StartedAt) {
		return "n/a"
	}
	return fmtDuration(sess.LastActive.Sub(sess.StartedAt))
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func safeFilenamePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "session"
	}
	return out
}

// handleSessionReplay serves GET /api/sessions/{id}/replay.
func handleSessionReplay(historyDB *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		q := r.URL.Query()
		const maxReplayLimit = 10000
		limit := 1000
		// Clamp an out-of-range limit to the max instead of silently reverting
		// to the default, so a client can request the full set.
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
			limit = n
			if limit > maxReplayLimit {
				limit = maxReplayLimit
			}
		}
		offset := 0
		if n, err := strconv.Atoi(q.Get("offset")); err == nil && n >= 0 {
			offset = n
		}
		events, err := historyDB.ListReplayEvents(id, limit, offset)
		if err != nil {
			writeJSONError(w, "failed to list events", http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.EventRow{}
		}
		total, err := historyDB.CountReplayEvents(id)
		if err != nil {
			writeJSONError(w, "failed to count events", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"sessionId": id,
			"events":    events,
			"limit":     limit,
			"offset":    offset,
			"total":     total,
			"hasMore":   offset+len(events) < total,
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
	"preview_max_length":  true,
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
		// Key-specific validation.
		if key == "preview_max_length" {
			n, err := strconv.Atoi(body.Value)
			if err != nil || n < 50 || n > 2000 {
				writeJSONError(w, "preview_max_length must be an integer between 50 and 2000", http.StatusBadRequest)
				return
			}
		}
		if key == "retention_hot_days" || key == "retention_warm_days" {
			n, err := strconv.Atoi(body.Value)
			if err != nil || n < 1 {
				writeJSONError(w, key+" must be a positive integer", http.StatusBadRequest)
				return
			}
		}
		if err := historyDB.SetSetting(key, body.Value); err != nil {
			writeJSONError(w, "failed to update setting", http.StatusInternalServerError)
			return
		}
		// Apply preview_max_length live so new parses use the updated value immediately.
		if key == "preview_max_length" {
			if n, err := strconv.Atoi(body.Value); err == nil {
				parser.SetPreviewMaxLength(n)
			}
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

// handleSwaggerYAML serves GET /api/openapi.yaml using the embedded spec (Issue 13).
func handleSwaggerYAML() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(api.OpenAPIYAML)
	}
}

// handleSwaggerUI serves GET /api (Swagger UI).
func handleSwaggerUI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(swaggerHTML))
	}
}
