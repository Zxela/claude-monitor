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

	"github.com/zxela/claude-monitor/internal/docker"
	"github.com/zxela/claude-monitor/internal/hub"
	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/pipeline"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/store/migrations"
	"github.com/zxela/claude-monitor/internal/update"
	"github.com/zxela/claude-monitor/internal/watcher"
	_ "modernc.org/sqlite"
)

// version is set by -ldflags at build time.
var version = "dev"

// validContainerName matches safe Docker container names (alphanumeric, dash, underscore, dot).
var validContainerName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// writeJSON writes a JSON success response, logging any encoding error.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

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
		if strings.HasPrefix(r.URL.Path, "/ws") {
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

	fw, err := watcher.New([]string(extraPaths))
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
		if err != nil {
			log.Printf("broadcast marshal error: %v", err)
		} else {
			h.Broadcast(payload)
		}

		// Send full event detail for meaningful events
		if sendDetail {
			detail, err := json.Marshal(broadcastEvent{
				Event:   "event",
				Session: sess,
				Data:    event,
			})
			if err != nil {
				log.Printf("broadcast marshal error: %v", err)
			} else {
				h.Broadcast(detail)
			}
		}
	})
	defer pipe.Stop()

	// Bootstrap callback: process through pipeline (no broadcast — handled internally).
	fw.SetBootstrapCallback(func(ev watcher.Event) {
		pipe.Process(ev)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start hub and watcher.
	go h.Run()

	// Check for updates in the background (non-blocking).
	// Issue 40: cache env var to avoid double-read.
	noUpdateCheck := os.Getenv("CLAUDE_MONITOR_NO_UPDATE_CHECK")
	if noUpdateCheck != "1" && noUpdateCheck != "true" {
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

	events := fw.Start(ctx)

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
							fw.Add(ev.HostPath, ev.ContainerName)
						} else {
							log.Printf("docker: stopped watching %s (%s)", ev.HostPath, ev.ContainerName)
							fw.Remove(ev.HostPath)
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
	eventsDone := make(chan struct{})
	go func() {
		for ev := range events {
			pipe.Process(ev)
		}
		close(eventsDone)
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
				hotDays := defaultHotDays
				warmDays := defaultWarmDays
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

	// HTTP routes — handlers are defined in handlers.go.
	mux := http.NewServeMux()

	// Static files — strip the "static/" prefix from embedded FS.
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("static embed error: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("GET /ws", handleWs(h))
	mux.HandleFunc("GET /api/sessions", handleSessions(sessionStore, historyDB))
	mux.HandleFunc("GET /api/sessions/{id}", handleSessionByID(sessionStore, historyDB))
	mux.HandleFunc("GET /api/stats", handleStats(sessionStore, historyDB, fw))
	mux.HandleFunc("GET /api/stats/trends", handleStatsTrends(historyDB))
	mux.HandleFunc("GET /api/repos", handleRepos(historyDB))
	mux.HandleFunc("GET /api/repos/{id}/stats", handleRepoStats(historyDB))
	mux.HandleFunc("GET /api/repos/{id}/sessions", handleRepoSessions(historyDB))
	mux.HandleFunc("GET /api/search", handleSearch(historyDB))
	mux.HandleFunc("GET /api/search/full", handleSearchFull(historyDB))
	mux.HandleFunc("GET /api/sessions/{id}/events", handleSessionEvents(historyDB))
	mux.HandleFunc("GET /api/sessions/{id}/replay", handleSessionReplay(historyDB))
	mux.HandleFunc("GET /api/settings", handleSettings(historyDB))
	mux.HandleFunc("PUT /api/settings/{key}", handleSettingsUpdate(historyDB))
	mux.HandleFunc("GET /api/storage", handleStorage(historyDB))
	mux.HandleFunc("DELETE /api/cache/repos", handleCacheClear(resolver, historyDB))
	mux.HandleFunc("POST /api/sessions/{id}/stop", handleSessionStop(sessionStore, &dc))
	mux.HandleFunc("GET /health", handleHealth(historyDB, fw))
	mux.HandleFunc("GET /api/version", handleVersion())

	// Swagger UI (opt-in via --swagger flag).
	if *swaggerEnabled {
		mux.HandleFunc("GET /swagger/openapi.yaml", handleSwaggerYAML())
		mux.HandleFunc("GET /swagger", handleSwaggerUI())
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

		// 1. Cancel context — stops watcher (closes events channel), docker watcher, retention ticker.
		cancel()

		// 2. Wait for event processing goroutine to drain remaining events.
		<-eventsDone

		// 3. Flush pipeline — saves batched events and session data.
		pipe.Stop()

		// 4. Shutdown HTTP server (closes WebSocket connections).
		shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
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
