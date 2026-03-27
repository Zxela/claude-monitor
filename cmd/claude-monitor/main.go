// claude-monitor daemon — watches Claude Code JSONL session files and
// broadcasts activity over WebSocket, with a REST API for session data.
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/docker"
	"github.com/zxela-claude/claude-monitor/internal/eventproc"
	"github.com/zxela-claude/claude-monitor/internal/hub"
	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/replay"
	"github.com/zxela-claude/claude-monitor/internal/search"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/store"
	"github.com/zxela-claude/claude-monitor/internal/store/migrations"
	"github.com/zxela-claude/claude-monitor/internal/update"
	"github.com/zxela-claude/claude-monitor/internal/watcher"
	_ "modernc.org/sqlite"
)

// version is set by -ldflags at build time.
var version = "dev"

// validContainerName matches safe Docker container names (alphanumeric, dash, underscore, dot).
var validContainerName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// writeJSONError writes a JSON error response with the given message and status code.
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("json encode error response: %v", err)
	}
}

//go:embed static
var staticFiles embed.FS

// broadcastEvent is the envelope sent to WebSocket clients.
type broadcastEvent struct {
	Event   string          `json:"event"`
	Session *session.Session `json:"session"`
	Message *parser.ParsedMessage `json:"message,omitempty"`
}

// repeatable is a flag.Value that accumulates multiple --watch values.
type repeatable []string

func (r *repeatable) String() string { return fmt.Sprintf("%v", *r) }
func (r *repeatable) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func handleMigrate(args []string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	dbPath := filepath.Join(homeDir, ".claude-monitor", "history.db")

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("cannot create data directory: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Fatalf("cannot set WAL mode: %v", err)
	}

	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "", "up":
		applied, err := migrations.RunUp(db)
		if err != nil {
			log.Fatalf("migration failed: %v", err)
		}
		if applied == 0 {
			current, _, _, _ := migrations.Status(db)
			fmt.Printf("Schema version: %d (up to date)\n", current)
		} else {
			current, _, _, _ := migrations.Status(db)
			fmt.Printf("Applied %d migration(s). Schema version: %d (up to date)\n", applied, current)
		}

	case "status":
		current, latest, pending, err := migrations.Status(db)
		if err != nil {
			log.Fatalf("cannot read status: %v", err)
		}
		fmt.Printf("Database: %s\n", dbPath)
		fmt.Printf("Schema version: %d (latest: %d)\n", current, latest)
		if len(pending) > 0 {
			fmt.Println("Pending migrations:")
			for _, name := range pending {
				fmt.Printf("  - %s\n", name)
			}
		} else {
			fmt.Println("No pending migrations.")
		}

	case "rollback":
		name, err := migrations.RunDown(db)
		if err != nil {
			log.Fatalf("rollback failed: %v", err)
		}
		current, _, _, _ := migrations.Status(db)
		fmt.Printf("Rolled back: %s\nSchema version: %d\n", name, current)

	default:
		fmt.Fprintf(os.Stderr, "Unknown migrate command: %s\nUsage: claude-monitor migrate [status|rollback]\n", sub)
		os.Exit(1)
	}
}


// sessionFinder can locate JSONL files for sessions not in the live store.
var sessionFinder interface {
	FindSessionFile(sessionID string) string
}

// requireSession looks up a session by path parameter "id", validates it has a
// file path, and writes an HTTP error if not. Falls back to searching watched
// paths for historical sessions not in the live store.
func requireSession(store *session.Store, w http.ResponseWriter, r *http.Request) (*session.Session, bool) {
	id := r.PathValue("id")
	sess, ok := store.Get(id)
	if !ok {
		// Try to find the JSONL file on disk for historical sessions.
		if sessionFinder != nil {
			if filePath := sessionFinder.FindSessionFile(id); filePath != "" {
				// Return transient session — do NOT persist to live store.
				return &session.Session{ID: id, FilePath: filePath}, true
			}
		}
		writeJSONError(w, "session not found", http.StatusNotFound)
		return nil, false
	}
	if sess.FilePath == "" {
		writeJSONError(w, "session file not available", http.StatusBadRequest)
		return nil, false
	}
	return sess, true
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware logs method, path, status code, and duration for each request.
// Long-lived connections (WebSocket and SSE) are passed through without logging.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ws") || (strings.HasPrefix(r.URL.Path, "/api/sessions/") && strings.HasSuffix(r.URL.Path, "/replay/stream")) {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

const swaggerHTML = `<!DOCTYPE html>
<html><head>
<title>Claude Monitor API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head><body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
SwaggerUIBundle({
  url: '/swagger/openapi.yaml',
  dom_id: '#swagger-ui',
  presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
  layout: "BaseLayout"
});
</script>
</body></html>`

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(os.Stderr)

	port := flag.Int("port", 7700, "HTTP listen port")
	bind := flag.String("bind", "127.0.0.1", "address to bind to (use 0.0.0.0 for all interfaces)")
	broadcast := flag.Bool("broadcast", false, "listen on all interfaces (shorthand for --bind 0.0.0.0)")
	var extraPaths repeatable
	flag.Var(&extraPaths, "watch", "additional directory to watch (repeatable)")
	dockerEnabled := flag.Bool("docker", false, "auto-discover .claude/projects mounts from running Docker containers")
	dockerSocket := flag.String("docker-socket", "/var/run/docker.sock", "path to Docker socket")
	swaggerEnabled := flag.Bool("swagger", false, "serve Swagger UI at /swagger")
	// Handle --version before any other initialization.
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("claude-monitor %s\n", version)
		os.Exit(0)
	}

	// Handle 'migrate' subcommand before flag.Parse().
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		handleMigrate(os.Args[2:])
		return
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `claude-monitor — real-time observability dashboard for Claude Code sessions.

Usage:
  claude-monitor [flags]
  claude-monitor migrate [status|rollback]
  claude-monitor --version

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Environment:
  CLAUDE_MONITOR_NO_UPDATE_CHECK=1   Disable startup update check

Data:
  Session history is stored in ~/.claude-monitor/history.db

Examples:
  claude-monitor                                  # Start with defaults (localhost:7700)
  claude-monitor --port 8080 --watch /extra/path  # Custom port + extra watch dir
  claude-monitor --broadcast --swagger            # All interfaces + Swagger UI
`)
	}

	flag.Parse()

	// --broadcast is shorthand for --bind 0.0.0.0
	if *broadcast {
		*bind = "0.0.0.0"
	}

	// Auto-enable Docker discovery if the socket exists and --docker wasn't explicitly set.
	if !*dockerEnabled {
		if _, err := os.Stat(*dockerSocket); err == nil {
			*dockerEnabled = true
			log.Println("docker socket found, auto-enabling container discovery")
		}
	}

	sessionStore := session.NewStore()
	searchIdx := search.New()
	h := hub.NewHub()

	// Open SQLite history database.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	dbDir := filepath.Join(homeDir, ".claude-monitor")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		log.Fatalf("cannot create data directory: %v", err)
	}
	historyDB, err := store.Open(filepath.Join(dbDir, "history.db"))
	if err != nil {
		log.Fatalf("cannot open history database: %v", err)
	}
	defer historyDB.Close()

	// Track which sessions were previously active for inactivity transition detection.
	// mu protects prevActive and savedToHistory from concurrent goroutine access.
	var mu sync.Mutex
	prevActive := make(map[string]bool)

	w, err := watcher.New([]string(extraPaths))
	if err != nil {
		log.Fatalf("failed to create watcher: %v", err)
	}

	proc := eventproc.New(sessionStore)
	sessionFinder = w // allow requireSession to find historical JSONL files

	// processAndIndex runs the event processor and feeds the search index.
	processAndIndex := func(ev watcher.Event) (*parser.ParsedMessage, *session.Session, bool) {
		res := proc.Process(ev)
		if res.Message != nil && res.Session != nil {
			displayName := res.Session.ProjectName
			if res.Session.SessionName != "" {
				displayName = res.Session.SessionName
			}
			searchIdx.Add(ev.SessionID, displayName, res.Session.ProjectName, *res.Message)
		}
		return res.Message, res.Session, res.IsNew
	}

	// Bootstrap callback: process historical lines for stats only (no broadcast).
	w.SetBootstrapCallback(func(ev watcher.Event) {
		processAndIndex(ev)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start hub and watcher.
	go h.Run()

	// Check for updates in the background (non-blocking).
	if os.Getenv("CLAUDE_MONITOR_NO_UPDATE_CHECK") != "1" && os.Getenv("CLAUDE_MONITOR_NO_UPDATE_CHECK") != "true" {
		go func() {
			rel, err := update.CheckLatest(version)
			if err != nil {
				log.Printf("update check: %v", err)
				return
			}
			if rel != nil {
				log.Printf("update available: %s (current: %s) — %s", rel.Version, version, rel.URL)
				payload, err := json.Marshal(map[string]string{
					"event":   "update_available",
					"version": rel.Version,
					"url":     rel.URL,
				})
				if err == nil {
					h.Broadcast(payload)
				}
			}
		}()
	}

	events := w.Start(ctx)

	var dc *docker.Client
	if *dockerEnabled {
		dc = docker.NewClient(*dockerSocket)
		dockerCh, err := docker.Watch(ctx, dc, 5*time.Second)
		if err != nil {
			log.Printf("docker discovery: %v (continuing without Docker)", err)
		} else {
			go func() {
				for {
					select {
					case ev, ok := <-dockerCh:
						if !ok {
							return
						}
						if ev.Added {
							log.Printf("docker: watching %s (%s)", ev.HostPath, ev.ContainerName)
							w.Add(ev.HostPath, ev.ContainerName)
						} else {
							log.Printf("docker: stopped watching %s (%s)", ev.HostPath, ev.ContainerName)
							w.Remove(ev.HostPath)
						}
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	}

	// Immediately persist all inactive sessions from bootstrap to history DB.
	savedToHistory := make(map[string]bool)
	mu.Lock()
	{
		saved := 0
		for _, sess := range sessionStore.All() {
			if !sess.IsActive && sess.MessageCount > 0 {
				if err := historyDB.SaveSession(sess); err != nil {
					log.Printf("history save error for %s: %v", sess.ID, err)
				} else {
					saved++
				}
				savedToHistory[sess.ID] = true
			}
			prevActive[sess.ID] = sess.IsActive
		}
		if saved > 0 {
			log.Printf("persisted %d sessions to history on startup", saved)
		}
	}
	mu.Unlock()

	// Periodic goroutine: persist history on inactivity transitions.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				for _, sess := range sessionStore.All() {
					nowActive := sess.IsActive
					wasActive := prevActive[sess.ID]

					// Save on active→inactive transition
					shouldSave := wasActive && !nowActive
					// Also save inactive sessions we haven't persisted yet
					// (catches sessions that went inactive before this server started)
					if !nowActive && !savedToHistory[sess.ID] && sess.MessageCount > 0 {
						shouldSave = true
					}

					if shouldSave {
						if err := historyDB.SaveSession(sess); err != nil {
							log.Printf("history save error for %s: %v", sess.ID, err)
						}
						savedToHistory[sess.ID] = true
					}
					prevActive[sess.ID] = nowActive
				}
				// Prevent unbounded growth: clear the map when it gets too large.
				// Sessions will simply be re-saved (idempotent upsert) on the next tick.
				if len(savedToHistory) > 1000 {
					savedToHistory = make(map[string]bool)
				}
				mu.Unlock()
			}
		}
	}()

	// Process watcher events: parse, update store, broadcast.
	go func() {
		for ev := range events {
			msg, sess, isNew := processAndIndex(ev)
			if msg == nil {
				continue
			}

			// Don't broadcast historical data — only used for bootstrapping stats.
			if ev.Bootstrap {
				continue
			}

			eventType := "message"
			if isNew {
				eventType = "session_new"
			}

			broadcastMsg := msg
			if eventType == "message" {
				if msg.Role == "assistant" && msg.StopReason == "" && msg.ToolName == "" {
					// Skip intermediate streaming chunks (no stop_reason, no tool)
					broadcastMsg = nil
				} else if !msg.IsConversationMessage() && msg.ContentText == "" {
					// Skip empty non-conversation messages
					broadcastMsg = nil
				}
			}

			payload, err := json.Marshal(broadcastEvent{
				Event:   eventType,
				Session: sess,
				Message: broadcastMsg,
			})
			if err != nil {
				log.Printf("marshal error: %v", err)
				continue
			}
			h.Broadcast(payload)
		}
	}()

	// HTTP routes.
	mux := http.NewServeMux()

	// Static files — strip the "static/" prefix from embedded FS.
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static embed error: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// WebSocket endpoint.
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWs(h, w, r)
	})

	// History REST API.
	mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
			limit = n
		}
		offsetStr := r.URL.Query().Get("offset")
		offset := 0
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
		rows, err := historyDB.ListHistory(limit, offset)
		if err != nil {
			log.Printf("history list error: %v", err)
			writeJSONError(w, "failed to retrieve history", http.StatusInternalServerError)
			return
		}
		if rows == nil {
			rows = []store.HistoryRow{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(rows); err != nil {
			log.Printf("json encode: %v", err)
		}
	})

	// Sessions REST API.
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		sessions := sessionStore.All()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(sessions); err != nil {
			log.Printf("api/sessions encode error: %v", err)
		}
	})

	// Time-bucketed sessions for the new navigation UI.
	mux.HandleFunc("GET /api/sessions/grouped", func(w http.ResponseWriter, r *http.Request) {
		sessions := sessionStore.All()
		now := time.Now()
		hourAgo := now.Add(-1 * time.Hour)
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		yesterdayStart := todayStart.Add(-24 * time.Hour)
		weekStart := todayStart.Add(-time.Duration(now.Weekday()) * 24 * time.Hour)

		type grouped struct {
			Active    []*session.Session `json:"active"`
			LastHour  []*session.Session `json:"lastHour"`
			Today     []*session.Session `json:"today"`
			Yesterday []*session.Session `json:"yesterday"`
			ThisWeek  []*session.Session `json:"thisWeek"`
			Older     []*session.Session `json:"older"`
		}
		g := grouped{
			Active:    []*session.Session{},
			LastHour:  []*session.Session{},
			Today:     []*session.Session{},
			Yesterday: []*session.Session{},
			ThisWeek:  []*session.Session{},
			Older:     []*session.Session{},
		}

		for _, s := range sessions {
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

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(g); err != nil {
			log.Printf("json encode: %v", err)
		}
	})

	// Aggregate stats endpoint — merges SQLite history with live active sessions.
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
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
		}

		// Parse window query param.
		now := time.Now()
		var since time.Time
		switch r.URL.Query().Get("window") {
		case "today":
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		case "week":
			weekday := int(now.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(weekday - 1))
		case "month":
			since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		default:
			// "all" or empty — zero time, no filter.
		}

		// Get SQLite aggregates for the window.
		agg, err := historyDB.AggregateStats(since)
		if err != nil {
			log.Printf("stats aggregate error: %v", err)
			writeJSONError(w, "failed to compute stats", http.StatusInternalServerError)
			return
		}

		resp := statsResponse{
			TotalCost:           agg.TotalCost,
			InputTokens:         agg.InputTokens,
			OutputTokens:        agg.OutputTokens,
			CacheReadTokens:     agg.CacheReadTokens,
			CacheCreationTokens: agg.CacheCreationTokens,
			SessionCount:        agg.SessionCount,
			CostByModel:         agg.CostByModel,
		}
		if resp.CostByModel == nil {
			resp.CostByModel = make(map[string]float64)
		}

		// Collect active top-level sessions within the window.
		allSessions := sessionStore.All()
		var activeSessions []*session.Session
		var activeIDs []string
		for _, sess := range allSessions {
			if sess.IsSubagent {
				continue
			}
			if !sess.IsActive {
				continue
			}
			// Filter by window: session must have started within the window.
			if !since.IsZero() && sess.StartedAt.Before(since) {
				continue
			}
			activeSessions = append(activeSessions, sess)
			activeIDs = append(activeIDs, sess.ID)
		}

		// Get last-saved snapshots for active sessions.
		snapshots, err := historyDB.GetSessionSnapshots(activeIDs)
		if err != nil {
			log.Printf("stats snapshot error: %v", err)
			writeJSONError(w, "failed to compute stats", http.StatusInternalServerError)
			return
		}

		// Merge active session deltas into the aggregate.
		for _, sess := range activeSessions {
			if snap, ok := snapshots[sess.ID]; ok {
				// Session is in SQLite — add only the delta (live - saved).
				resp.TotalCost += sess.TotalCost - snap.TotalCost
				resp.InputTokens += sess.InputTokens - snap.InputTokens
				resp.OutputTokens += sess.OutputTokens - snap.OutputTokens
				resp.CacheReadTokens += sess.CacheReadTokens - snap.CacheReadTokens
				resp.CacheCreationTokens += sess.CacheCreationTokens - snap.CacheCreationTokens
			} else {
				// Session not in SQLite — add full live values.
				resp.TotalCost += sess.TotalCost
				resp.InputTokens += sess.InputTokens
				resp.OutputTokens += sess.OutputTokens
				resp.CacheReadTokens += sess.CacheReadTokens
				resp.CacheCreationTokens += sess.CacheCreationTokens
				resp.SessionCount++
				if sess.Model != "" {
					resp.CostByModel[sess.Model] += sess.TotalCost
				}
			}
			resp.CostRate += sess.CostRate
		}

		resp.ActiveSessions = len(activeSessions)

		// Compute derived cache hit percentage.
		totalInput := resp.InputTokens + resp.CacheReadTokens + resp.CacheCreationTokens
		if totalInput > 0 {
			resp.CacheHitPct = float64(resp.CacheReadTokens) / float64(totalInput) * 100
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("json encode stats: %v", err)
		}
	})

	// Distinct project names with session counts.
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, r *http.Request) {
		sessions := sessionStore.All()
		counts := make(map[string]int)
		for _, s := range sessions {
			name := s.ProjectName
			if name == "" {
				name = s.ProjectDir
			}
			counts[name]++
		}

		type projectEntry struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		}
		result := make([]projectEntry, 0, len(counts))
		for name, count := range counts {
			result = append(result, projectEntry{Name: name, Count: count})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("json encode: %v", err)
		}
	})

	// Cross-session search — uses the in-memory search index instead of
	// reading every JSONL file from disk.
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		limitStr := r.URL.Query().Get("limit")
		limit := 50
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			limit = n
		}

		results := searchIdx.Search(query, limit)
		if results == nil {
			results = []search.Entry{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(results); err != nil {
			log.Printf("json encode: %v", err)
		}
	})

	// Recent messages for a session — returns last N parsed messages for feed population.
	mux.HandleFunc("GET /api/sessions/{id}/recent", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(sessionStore, w, r)
		if !ok {
			return
		}
		events, err := replay.ReadFile(sess.FilePath)
		if err != nil && len(events) == 0 {
			log.Printf("read session file %s: %v", sess.FilePath, err)
			writeJSONError(w, "failed to read session file", http.StatusInternalServerError)
			return
		}

		// Filter to meaningful messages only
		var filtered []replay.Event
		for _, ev := range events {
			if ev.ContentText == "" && ev.ToolName == "" && ev.HookEvent == "" {
				continue
			}
			if ev.ContentText == "[thinking...]" {
				continue
			}
			// Include conversation messages, hooks, and agent/skill calls
			isHook := ev.HookEvent != ""
			if !ev.IsConversationMessage() && !isHook {
				continue
			}
			filtered = append(filtered, ev)
		}

		// Return last 50
		limit := 50
		if len(filtered) > limit {
			filtered = filtered[len(filtered)-limit:]
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(filtered); err != nil {
			log.Printf("json encode: %v", err)
		}
	})

	// Replay SSE stream route — registered BEFORE the manifest route.
	mux.HandleFunc("GET /api/sessions/{id}/replay/stream", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(sessionStore, w, r)
		if !ok {
			return
		}
		events, err := replay.ReadFile(sess.FilePath)
		if err != nil {
			log.Printf("read session file %s: %v", sess.FilePath, err)
			writeJSONError(w, "failed to read session file", http.StatusInternalServerError)
			return
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		speed, _ := strconv.ParseFloat(r.URL.Query().Get("speed"), 64)
		replay.Stream(w, r, events, replay.StreamParams{FromIndex: from, Speed: speed})
	})

	// Replay manifest — returns all events with timestamps for the scrubber.
	mux.HandleFunc("GET /api/sessions/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
		sess, ok := requireSession(sessionStore, w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")

		w.Header().Set("Content-Type", "application/json")

		// Return cached JSON if available (invalidated on each new message).
		if cached, hit := sessionStore.GetReplayJSON(id); hit {
			w.Write(cached)
			return
		}

		events, scanErr := replay.ReadFile(sess.FilePath)
		if scanErr != nil && len(events) == 0 {
			log.Printf("read session file %s: %v", sess.FilePath, scanErr)
			writeJSONError(w, "failed to read session file", http.StatusInternalServerError)
			return
		}

		type manifestEvent struct {
			Index       int       `json:"index"`
			Timestamp   time.Time `json:"timestamp"`
			Type        string    `json:"type"`
			Role        string    `json:"role"`
			ContentText string    `json:"contentText"`
			ToolName    string    `json:"toolName,omitempty"`
			CostUSD     float64   `json:"costUSD"`
		}
		out := make([]manifestEvent, len(events))
		for i, e := range events {
			out[i] = manifestEvent{
				Index:       e.Index,
				Timestamp:   e.Timestamp,
				Type:        e.Type,
				Role:        e.Role,
				ContentText: e.ContentText,
				ToolName:    e.ToolName,
				CostUSD:     e.CostUSD,
			}
		}
		data, err := json.Marshal(map[string]any{
			"sessionId": id,
			"events":    out,
		})
		if err != nil {
			log.Printf("json marshal replay manifest: %v", err)
			writeJSONError(w, "failed to encode replay data", http.StatusInternalServerError)
			return
		}
		// Only cache if the file was fully read (no scanner errors).
		if scanErr == nil {
			sessionStore.SetReplayJSON(id, data)
		}
		w.Write(data)
	})

	// Stop session (Docker container).
	mux.HandleFunc("POST /api/sessions/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sess, ok := sessionStore.Get(id)
		if !ok {
			writeJSONError(w, "session not found", http.StatusNotFound)
			return
		}
		// Extract container name from projectName (format: "container / project")
		containerName := ""
		if parts := strings.SplitN(sess.ProjectName, " / ", 2); len(parts) == 2 {
			containerName = parts[0]
		}
		if containerName == "" || dc == nil {
			writeJSONError(w, "not a Docker session or Docker not available", http.StatusBadRequest)
			return
		}
		// Validate container name contains only safe characters.
		if !validContainerName.MatchString(containerName) {
			writeJSONError(w, "invalid container name", http.StatusBadRequest)
			return
		}
		stopCtx, stopCancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer stopCancel()
		if err := dc.StopContainer(stopCtx, containerName); err != nil {
			log.Printf("stop container %s: %v", containerName, err)
			writeJSONError(w, "failed to stop container", http.StatusInternalServerError)
			return
		}
		log.Printf("stopped container %s for session %s", containerName, id)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := historyDB.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "db": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "db": "ok"})
	})

	// Version endpoint.
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"version": version}); err != nil {
			log.Printf("json encode: %v", err)
		}
	})

	// Swagger UI (opt-in via --swagger flag).
	if *swaggerEnabled {
		mux.HandleFunc("GET /swagger/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "api/openapi.yaml")
		})
		mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(swaggerHTML))
		})
		log.Println("swagger UI enabled at /swagger")
	}

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	srv := &http.Server{
		Addr:        addr,
		Handler:     loggingMiddleware(mux),
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
		// WriteTimeout intentionally omitted: WebSocket (54s ping) and SSE
		// streams need long-lived writes. Per-write deadlines are enforced
		// by gorilla/websocket writeWait and http.ResponseController.
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		log.Printf("received signal %s, shutting down", sig)
		cancel()

		// Flush unsaved sessions to history before shutting down.
		mu.Lock()
		flushed := 0
		for _, sess := range sessionStore.All() {
			if !savedToHistory[sess.ID] && sess.MessageCount > 0 {
				if err := historyDB.SaveSession(sess); err != nil {
					log.Printf("shutdown history save error for %s: %v", sess.ID, err)
				} else {
					flushed++
				}
				savedToHistory[sess.ID] = true
			}
		}
		mu.Unlock()
		if flushed > 0 {
			log.Printf("flushed %d sessions to history on shutdown", flushed)
		}

		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	log.Printf("claude-monitor listening on http://%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server error: %v", err)
	}
	log.Println("claude-monitor stopped")
}
