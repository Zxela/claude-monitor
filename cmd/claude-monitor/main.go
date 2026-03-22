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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/docker"
	"github.com/zxela-claude/claude-monitor/internal/hub"
	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/replay"
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

// parentSessionIDFromPath extracts the parent session ID from a subagent JSONL
// file path. Subagent files live at:
//   .../projects/<hash>/<parent-session-id>/subagents/agent-<id>.jsonl
func parentSessionIDFromPath(filePath string) string {
	dir := filepath.Dir(filePath)   // .../subagents/
	dirName := filepath.Base(dir)
	if dirName == "subagents" {
		parentDir := filepath.Dir(dir) // .../<parent-session-id>/
		return filepath.Base(parentDir)
	}
	return ""
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(os.Stderr)

	port := flag.Int("port", 7700, "HTTP listen port")
	var extraPaths repeatable
	flag.Var(&extraPaths, "watch", "additional directory to watch (repeatable)")
	dockerEnabled := flag.Bool("docker", false, "auto-discover .claude/projects mounts from running Docker containers")
	dockerSocket := flag.String("docker-socket", "/var/run/docker.sock", "path to Docker socket")
	flag.Parse()

	// Auto-enable Docker discovery if the socket exists and --docker wasn't explicitly set.
	if !*dockerEnabled {
		if _, err := os.Stat(*dockerSocket); err == nil {
			*dockerEnabled = true
			log.Println("docker socket found, auto-enabling container discovery")
		}
	}

	store := session.NewStore()
	h := hub.NewHub()

	w, err := watcher.New([]string(extraPaths))
	if err != nil {
		log.Fatalf("failed to create watcher: %v", err)
	}

	// processEvent parses a watcher event and updates the session store.
	// Returns the parsed message, session, and whether it's a new session.
	processEvent := func(ev watcher.Event) (*parser.ParsedMessage, *session.Session, bool) {
		msg, err := parser.ParseLine(ev.Line)
		if err != nil {
			return nil, nil, false
		}
		_, exists := store.Get(ev.SessionID)
		isNew := !exists

		sess := store.Upsert(ev.SessionID, func(s *session.Session) {
			s.FilePath = ev.FilePath
			s.ProjectDir = ev.ProjectDir
			if ev.Label != "" {
				s.ProjectName = ev.Label + " / " + ev.ProjectDir
			} else if s.ProjectName == "" {
				s.ProjectName = ev.ProjectDir
			}
			if msg.CWD != "" {
				s.CWD = msg.CWD
			}
			if msg.GitBranch != "" {
				s.GitBranch = msg.GitBranch
			}
			if msg.Model != "" {
				s.Model = msg.Model
			}
			if msg.Type == "custom-title" && msg.ContentText != "" {
				s.SessionName = msg.ContentText
			} else if msg.Type == "agent-name" && msg.ContentText != "" && s.SessionName == "" {
				s.SessionName = msg.ContentText
			}
			if s.SessionName != "" {
				s.ProjectName = s.SessionName
			}
			if msg.IsSidechain || msg.ParentUUID != "" {
				if parentSID := parentSessionIDFromPath(ev.FilePath); parentSID != "" {
					if s.ParentID == "" {
						s.ParentID = parentSID
						s.IsSubagent = true
					}
				}
			}
			s.TotalCost += msg.CostUSD
			s.InputTokens += msg.InputTokens
			s.OutputTokens += msg.OutputTokens
			s.CacheReadTokens += msg.CacheReadTokens
				s.CacheCreationTokens += msg.CacheCreationTokens
			if msg.IsConversationMessage() {
				if s.SeenMessageIDs == nil {
					s.SeenMessageIDs = make(map[string]bool)
				}
				if msg.MessageID != "" {
					if !s.SeenMessageIDs[msg.MessageID] {
						s.SeenMessageIDs[msg.MessageID] = true
						s.MessageCount++
					}
				} else {
					s.MessageCount++
				}
			}
			if !msg.Timestamp.IsZero() {
				s.LastActive = msg.Timestamp
				if s.StartedAt.IsZero() || msg.Timestamp.Before(s.StartedAt) {
					s.StartedAt = msg.Timestamp
				}
			} else if !ev.Bootstrap {
				s.LastActive = time.Now()
			}
			if msg.StopReason == "end_turn" {
				s.Status = "waiting"
			} else if msg.StopReason == "tool_use" {
				s.Status = "tool_use"
			} else if msg.ToolName != "" {
				s.Status = "tool_use"
			} else if msg.Role == "assistant" {
				s.Status = "thinking"
			} else if msg.Role == "user" {
				s.Status = "thinking"
			}
		})

		if sess.IsSubagent && sess.ParentID != "" {
			store.LinkChild(sess.ParentID, ev.SessionID)
		}

		return msg, sess, isNew
	}

	// Bootstrap callback: process historical lines for stats only (no broadcast).
	w.SetBootstrapCallback(func(ev watcher.Event) {
		processEvent(ev)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start hub and watcher.
	go h.Run()
	events := w.Start(ctx)

	if *dockerEnabled {
		dc := docker.NewClient(*dockerSocket)
		dockerCh, err := docker.Watch(ctx, dc, 30*time.Second)
		if err != nil {
			log.Printf("docker discovery: %v (continuing without Docker)", err)
		} else {
			go func() {
				for ev := range dockerCh {
					if ev.Added {
						log.Printf("docker: watching %s (%s)", ev.HostPath, ev.ContainerName)
						w.Add(ev.HostPath, ev.ContainerName)
					} else {
						log.Printf("docker: stopped watching %s (%s)", ev.HostPath, ev.ContainerName)
						w.Remove(ev.HostPath)
					}
				}
			}()
		}
	}

	// Process watcher events: parse, update store, broadcast.
	go func() {
		for ev := range events {
			msg, sess, isNew := processEvent(ev)
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

	// Cross-session search — searches ContentText and ToolDetail across all sessions.
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
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

		queryLower := strings.ToLower(query)

		type searchResult struct {
			SessionID   string `json:"sessionId"`
			SessionName string `json:"sessionName"`
			parser.ParsedMessage
		}

		var results []searchResult
		for _, sess := range store.All() {
			if sess.FilePath == "" {
				continue
			}
			events, err := replay.ReadFile(sess.FilePath)
			if err != nil && len(events) == 0 {
				continue
			}
			displayName := sess.ProjectName
			if sess.SessionName != "" {
				displayName = sess.SessionName
			}
			for _, ev := range events {
				if len(results) >= limit {
					break
				}
				text := strings.ToLower(ev.ContentText + " " + ev.ToolDetail + " " + ev.ToolName)
				if strings.Contains(text, queryLower) {
					results = append(results, searchResult{
						SessionID:     sess.ID,
						SessionName:   displayName,
						ParsedMessage: ev.ParsedMessage,
					})
				}
			}
			if len(results) >= limit {
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	// Recent messages for a session — returns last N parsed messages for feed population.
	mux.HandleFunc("/api/sessions/{id}/recent", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sess, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if sess.FilePath == "" {
			http.Error(w, "session file not available", http.StatusBadRequest)
			return
		}
		events, err := replay.ReadFile(sess.FilePath)
		if err != nil && len(events) == 0 {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		json.NewEncoder(w).Encode(filtered)
	})

	// Replay SSE stream route — registered BEFORE the manifest route.
	mux.HandleFunc("/api/sessions/{id}/replay/stream", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sess, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if sess.FilePath == "" {
			http.Error(w, "session file not available", http.StatusBadRequest)
			return
		}
		events, err := replay.ReadFile(sess.FilePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		speed, _ := strconv.ParseFloat(r.URL.Query().Get("speed"), 64)
		replay.Stream(w, r, events, replay.StreamParams{FromIndex: from, Speed: speed})
	})

	// Replay manifest — returns all events with timestamps for the scrubber.
	mux.HandleFunc("/api/sessions/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sess, ok := store.Get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if sess.FilePath == "" {
			http.Error(w, "session file not available", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		// Return cached JSON if available (invalidated on each new message).
		if cached, hit := store.GetReplayJSON(id); hit {
			w.Write(cached)
			return
		}

		events, scanErr := replay.ReadFile(sess.FilePath)
		if scanErr != nil && len(events) == 0 {
			http.Error(w, scanErr.Error(), http.StatusInternalServerError)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Only cache if the file was fully read (no scanner errors).
		if scanErr == nil {
			store.SetReplayJSON(id, data)
		}
		w.Write(data)
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
