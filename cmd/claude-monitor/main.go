// claude-monitor daemon — watches Claude Code JSONL session files and
// broadcasts activity over WebSocket, with a REST API for session data.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/hub"
	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/watcher"
)

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

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(os.Stderr)

	port := flag.Int("port", 7700, "HTTP listen port")
	var extraPaths repeatable
	flag.Var(&extraPaths, "watch", "additional directory to watch (repeatable)")
	flag.Parse()

	store := session.NewStore()
	h := hub.NewHub()

	w, err := watcher.New([]string(extraPaths))
	if err != nil {
		log.Fatalf("failed to create watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start hub and watcher.
	go h.Run()
	events := w.Start(ctx)

	// Process watcher events: parse, update store, broadcast.
	go func() {
		for ev := range events {
			msg, err := parser.ParseLine(ev.Line)
			if err != nil {
				log.Printf("parse error (%s): %v", ev.FilePath, err)
				continue
			}
			// Determine whether this is the first message for this session.
			_, isNew := store.Get(ev.SessionID)
			isNew = !isNew

			sess := store.Upsert(ev.SessionID, func(s *session.Session) {
				s.FilePath = ev.FilePath
				s.ProjectDir = ev.ProjectDir
				s.ProjectName = ev.ProjectDir // use dir name as display name
				s.TotalCost += msg.CostUSD
				s.InputTokens += msg.InputTokens
				s.OutputTokens += msg.OutputTokens
				s.CacheTokens += msg.CacheTokens
				s.MessageCount++
				if !msg.Timestamp.IsZero() {
					s.LastActive = msg.Timestamp
				} else {
					s.LastActive = time.Now()
				}
			})

			eventType := "message"
			if isNew {
				eventType = "session_new"
			}

			payload, err := json.Marshal(broadcastEvent{
				Event:   eventType,
				Session: sess,
				Message: msg,
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
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		hub.ServeWs(h, w, r)
	})

	// Sessions REST API.
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		sessions := store.All()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(sessions); err != nil {
			log.Printf("api/sessions encode error: %v", err)
		}
	})

	// Health check.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	addr := fmt.Sprintf(":%d", *port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		log.Printf("received signal %s, shutting down", sig)
		cancel()

		shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	log.Printf("claude-monitor listening on http://localhost%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server error: %v", err)
	}
	log.Println("claude-monitor stopped")
}
