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
	"syscall"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/docker"
	"github.com/zxela-claude/claude-monitor/internal/hub"
	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/pipeline"
	"github.com/zxela-claude/claude-monitor/internal/repo"
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
	Event   string           `json:"event"`
	Session *session.Session `json:"session,omitempty"`
	Data    *parser.Event    `json:"data,omitempty"`
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


func handleHook(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "install":
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("cannot determine home directory: %v", err)
		}

		// Install the hook script
		hookDir := filepath.Join(homeDir, ".claude-monitor", "hooks")
		if err := os.MkdirAll(hookDir, 0o755); err != nil {
			log.Fatalf("cannot create hooks directory: %v", err)
		}

		hookScript := filepath.Join(hookDir, "ensure-running.sh")
		scriptContent := `#!/bin/sh
# Claude Code SessionStart hook — ensures claude-monitor is running.
if curl -sf http://127.0.0.1:${CLAUDE_MONITOR_PORT:-7700}/health >/dev/null 2>&1; then
  exit 0
fi
nohup claude-monitor ${CLAUDE_MONITOR_ARGS:-} >/dev/null 2>&1 &
for i in 1 2 3; do
  sleep 0.5
  if curl -sf http://127.0.0.1:${CLAUDE_MONITOR_PORT:-7700}/health >/dev/null 2>&1; then
    echo "claude-monitor started on http://127.0.0.1:${CLAUDE_MONITOR_PORT:-7700}"
    exit 0
  fi
done
echo "claude-monitor started in background"
`
		if err := os.WriteFile(hookScript, []byte(scriptContent), 0o755); err != nil {
			log.Fatalf("cannot write hook script: %v", err)
		}

		// Print the settings.json snippet for the user
		fmt.Println("Hook script installed to:", hookScript)
		fmt.Println()
		fmt.Println("Add this to your ~/.claude/settings.json (or .claude/settings.json):")
		fmt.Println()
		fmt.Printf(`{
  "hooks": {
    "SessionStart": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "%s"
          }
        ]
      }
    ]
  }
}
`, hookScript)
		fmt.Println()
		fmt.Println("claude-monitor will auto-start when you open Claude Code.")

	default:
		fmt.Fprintf(os.Stderr, "Usage: claude-monitor hook install\n")
		fmt.Fprintf(os.Stderr, "\nInstalls a Claude Code hook that auto-starts claude-monitor on session start.\n")
		os.Exit(1)
	}
}

// requireSession looks up a session by path parameter "id" and writes an HTTP
// error if not found.
func requireSession(store *session.Store, w http.ResponseWriter, r *http.Request) (*session.Session, bool) {
	id := r.PathValue("id")
	sess, ok := store.Get(id)
	if !ok {
		writeJSONError(w, "session not found", http.StatusNotFound)
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

	// Handle subcommands before flag.Parse().
	if len(os.Args) >= 2 && os.Args[1] == "migrate" {
		handleMigrate(os.Args[2:])
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "hook" {
		handleHook(os.Args[2:])
		return
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `claude-monitor — real-time observability dashboard for Claude Code sessions.

Usage:
  claude-monitor [flags]
  claude-monitor hook install
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
	h := hub.NewHub()

	// Open SQLite database.
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
		log.Fatalf("cannot open database: %v", err)
	}
	defer historyDB.Close()

	// Repo resolver with persisted cwd→repo cache.
	resolver := repo.NewResolver()
	if cached, err := historyDB.LoadCwdRepos(); err == nil {
		resolver.LoadCache(cached)
	}

	w, err := watcher.New([]string(extraPaths))
	if err != nil {
		log.Fatalf("failed to create watcher: %v", err)
	}

	// Create pipeline with broadcast callback.
	pipe := pipeline.New(sessionStore, historyDB, resolver, func(event *parser.Event, sess *session.Session, isNew, sendDetail bool) {
		// Always send session_update
		eventType := "session_update"
		if isNew {
			eventType = "session_new"
		}
		payload, err := json.Marshal(broadcastEvent{
			Event:   eventType,
			Session: sess,
		})
		if err == nil {
			h.Broadcast(payload)
		}

		// Send full event detail for meaningful events
		if sendDetail {
			detail, err := json.Marshal(broadcastEvent{
				Event:   "event",
				Session: sess,
				Data:    event,
			})
			if err == nil {
				h.Broadcast(detail)
			}
		}
	})
	defer pipe.Stop()

	// Bootstrap callback: process through pipeline (no broadcast — handled internally).
	w.SetBootstrapCallback(func(ev watcher.Event) {
		pipe.Process(ev)
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

	// Process watcher events through the pipeline.
	// The pipeline handles: parse → resolve repo → apply session → broadcast + batch persist.
	go func() {
		for ev := range events {
			pipe.Process(ev)
		}
	}()

	// Retention compaction — runs hourly, compresses/deletes old event content.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hotDays := 30
				warmDays := 90
				if v, err := historyDB.GetSetting("retention_hot_days"); err == nil {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						hotDays = n
					}
				}
				if v, err := historyDB.GetSetting("retention_warm_days"); err == nil {
					if n, err := strconv.Atoi(v); err == nil && n > 0 {
						warmDays = n
					}
				}
				if compressed, err := historyDB.CompactHotToWarm(hotDays); err != nil {
					log.Printf("retention compact hot→warm error: %v", err)
				} else if compressed > 0 {
					log.Printf("retention: compressed %d event content entries (hot→warm)", compressed)
				}
				if deleted, err := historyDB.CompactWarmToCold(warmDays); err != nil {
					log.Printf("retention compact warm→cold error: %v", err)
				} else if deleted > 0 {
					log.Printf("retention: deleted %d event content entries (warm→cold)", deleted)
				}
			}
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

	// Unified sessions endpoint — replaces /api/sessions, /api/sessions/grouped, /api/history.
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(active)
			return
		}

		// ?group=activity — return time-bucketed sessions
		// Merges live sessions (for active status) with DB sessions (for history).
		if q.Get("group") == "activity" {
			now := time.Now()
			hourAgo := now.Add(-1 * time.Hour)
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			yesterdayStart := todayStart.Add(-24 * time.Hour)
			weekday := int(now.Weekday())
			if weekday == 0 { weekday = 7 }
			weekStart := todayStart.AddDate(0, 0, -(weekday - 1))

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

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(g)
			return
		}

		// Default: paginated list from DB, with optional ?repo= filter
		limit := 50
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	})

	// Single session by ID.
	mux.HandleFunc("GET /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		// Try live store first
		if sess, ok := sessionStore.Get(id); ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sess)
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(row.ToSession())
	})

	// Aggregate stats — reads from DB (pipeline keeps it up to date).
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
			CostByRepo          map[string]float64 `json:"costByRepo"`
		}

		now := time.Now()
		var since time.Time
		switch r.URL.Query().Get("window") {
		case "today":
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		case "week":
			weekday := int(now.Weekday())
			if weekday == 0 { weekday = 7 }
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(weekday - 1))
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
		}
		if resp.CostByModel == nil { resp.CostByModel = make(map[string]float64) }
		if resp.CostByRepo == nil { resp.CostByRepo = make(map[string]float64) }

		totalInput := resp.InputTokens + resp.CacheReadTokens + resp.CacheCreationTokens
		if totalInput > 0 {
			resp.CacheHitPct = float64(resp.CacheReadTokens) / float64(totalInput) * 100
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Repos endpoint — replaces /api/projects.
	mux.HandleFunc("GET /api/repos", func(w http.ResponseWriter, r *http.Request) {
		repos, err := historyDB.ListRepos()
		if err != nil {
			writeJSONError(w, "failed to list repos", http.StatusInternalServerError)
			return
		}
		if repos == nil {
			repos = []store.RepoRow{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(repos)
	})

	// Per-repo stats.
	mux.HandleFunc("GET /api/repos/{id}/stats", func(w http.ResponseWriter, r *http.Request) {
		repoID := r.PathValue("id")
		agg, err := historyDB.AggregateStatsByRepo(repoID)
		if err != nil {
			writeJSONError(w, "failed to get repo stats", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agg)
	})

	// Sessions for a repo.
	mux.HandleFunc("GET /api/repos/{id}/sessions", func(w http.ResponseWriter, r *http.Request) {
		repoID := r.PathValue("id")
		limit := 50
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	})

	// Search — FTS5 on preview + tool_detail.
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		limit := 50
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	// Full content search (slower, for key leak detection).
	mux.HandleFunc("GET /api/search/full", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}
		limit := 50
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	// Events for a session — from DB.
	mux.HandleFunc("GET /api/sessions/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		q := r.URL.Query()

		// ?pinned=true — all error + agent events (always visible regardless of window)
		if q.Get("pinned") == "true" || q.Get("errors") == "true" {
			events, err := historyDB.ListPinnedEvents(id)
			if err != nil {
				writeJSONError(w, "failed to list events", http.StatusInternalServerError)
				return
			}
			if events == nil {
				events = []store.EventRow{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(events)
			return
		}

		// ?last=N — most recent N events
		if lastStr := q.Get("last"); lastStr != "" {
			n := 50
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(events)
			return
		}

		limit := 100
		if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	// Replay — returns events from DB for playback.
	mux.HandleFunc("GET /api/sessions/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		events, err := historyDB.ListEvents(id, 10000, 0)
		if err != nil {
			writeJSONError(w, "failed to list events", http.StatusInternalServerError)
			return
		}
		if events == nil {
			events = []store.EventRow{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sessionId": id,
			"events":    events,
		})
	})

	// Settings API.
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		settings, err := historyDB.AllSettings()
		if err != nil {
			writeJSONError(w, "failed to get settings", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(settings)
	})

	mux.HandleFunc("PUT /api/settings/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	// Storage info.
	mux.HandleFunc("GET /api/storage", func(w http.ResponseWriter, r *http.Request) {
		info, err := historyDB.StorageInfo()
		if err != nil {
			writeJSONError(w, "failed to get storage info", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	// Cache clear endpoint.
	mux.HandleFunc("DELETE /api/cache/repos", func(w http.ResponseWriter, r *http.Request) {
		resolver.ClearCache()
		if err := historyDB.ClearCwdRepos(); err != nil {
			writeJSONError(w, "failed to clear cache", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	})

	// Stop session (Docker container).
	mux.HandleFunc("POST /api/sessions/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
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

		// Pipeline.Stop() flushes remaining events and saves sessions.
		// (Called via deferred pipe.Stop() in main)

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
