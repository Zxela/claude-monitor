// Package pipeline implements the staged event processing pipeline.
//
// Flow: Parse → Resolve Repo → Apply Session → fork(Broadcast, Buffer → Persist)
package pipeline

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zxela/claude-monitor/internal/parser"
	"github.com/zxela/claude-monitor/internal/repo"
	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
	"github.com/zxela/claude-monitor/internal/watcher"
)

const (
	flushInterval  = 2 * time.Second
	maxBatchSize   = 200
	maxMetaCache   = 500
)

// BroadcastFunc is called for each processed event. It receives the event,
// session, whether this is a new session, and whether the full event detail
// should be sent (vs session-update-only).
type BroadcastFunc func(event *parser.Event, sess *session.Session, isNew bool, sendDetail bool)

// agentMeta caches subagent metadata read from .meta.json files.
type agentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Name        string `json:"name"`
}

// Pipeline orchestrates the event processing stages.
type Pipeline struct {
	sessions  *session.Store
	db        *store.DB
	resolver  *repo.Resolver
	broadcast BroadcastFunc

	metaMu    sync.Mutex
	metaCache map[string]*agentMeta
	metaOrder []string

	bufMu  sync.Mutex
	buffer []store.EventInsert

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New creates a Pipeline.
func New(sessions *session.Store, db *store.DB, resolver *repo.Resolver, broadcast BroadcastFunc) *Pipeline {
	p := &Pipeline{
		sessions:  sessions,
		db:        db,
		resolver:  resolver,
		broadcast: broadcast,
		metaCache: make(map[string]*agentMeta),
		stopCh:    make(chan struct{}),
	}
	p.wg.Add(1)
	go p.flushLoop()
	return p
}

// Stop flushes remaining events and stops the background flush goroutine.
// Safe to call multiple times.
func (p *Pipeline) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	p.wg.Wait()
	p.flush() // final flush
}

// Process handles a single watcher event through the full pipeline.
func (p *Pipeline) Process(ev watcher.Event) {
	// Stage 1: Parse
	event, err := parser.ParseLine(ev.Line)
	if err != nil {
		if !ev.Bootstrap {
			log.Printf("parse error (%s): %v (line prefix: %.120s)", ev.FilePath, err, ev.Line)
		}
		return
	}

	// Stage 2: Resolve Repo
	var resolvedRepo *repo.Repo
	if event.CWD != "" {
		var resolveErr error
		resolvedRepo, resolveErr = p.resolver.Resolve(event.CWD, ev.Label)
		if resolveErr != nil {
			log.Printf("debug: repo resolve (%s): %v", event.CWD, resolveErr)
		}
	}

	// Pre-read subagent meta.json (only for new sessions).
	_, exists := p.sessions.Get(ev.SessionID)
	isNew := !exists
	if isNew {
		p.loadMeta(ev)
		// Rebuild dedup state from DB if this session was previously persisted.
		// After a restart, in-memory dedup maps are empty, which would cause
		// duplicate events to double-count costs, messages, and errors.
		if ids, costs, err := p.db.LoadMessageDedup(ev.SessionID); err == nil && len(ids) > 0 {
			// Also restore session aggregates so totals aren't reset to zero.
			dbSess, _ := p.db.GetSession(ev.SessionID)
			p.sessions.Upsert(ev.SessionID, func(s *session.Session) {
				s.SetDedupState(ids, costs)
				if dbSess != nil {
					s.TotalCost = dbSess.TotalCost
					s.InputTokens = dbSess.InputTokens
					s.OutputTokens = dbSess.OutputTokens
					s.CacheReadTokens = dbSess.CacheReadTokens
					s.CacheCreationTokens = dbSess.CacheCreationTokens
					s.MessageCount = dbSess.MessageCount
					s.EventCount = dbSess.EventCount
					s.ErrorCount = dbSess.ErrorCount
				}
			})
		}
	}

	// Stage 3: Apply Session
	sess := p.sessions.Upsert(ev.SessionID, func(s *session.Session) {
		p.applyEvent(s, event, ev, resolvedRepo)
	})

	// Link child to parent
	if sess.ParentID != "" {
		p.sessions.LinkChild(sess.ParentID, ev.SessionID)
	}

	// Stage 4a: Broadcast (immediate)
	if p.broadcast != nil && !ev.Bootstrap {
		sendDetail := !skipDetail(event)
		p.broadcast(event, sess, isNew, sendDetail)
	}

	// Stage 4b: Buffer for batch persist
	fullContent := event.FullContent
	if fullContent == "" && len(event.ContentText) > 200 {
		fullContent = event.ContentText
	}

	p.bufMu.Lock()
	p.buffer = append(p.buffer, store.EventInsert{
		SessionID:   ev.SessionID,
		Event:       event,
		FullContent: fullContent,
	})
	needFlush := len(p.buffer) >= maxBatchSize
	p.bufMu.Unlock()

	if needFlush {
		p.flush()
	}
}

// ProcessBootstrap handles a batch of events from a single JSONL file.
// Events are processed through the pipeline and flushed as a single batch.
func (p *Pipeline) ProcessBootstrap(events []watcher.Event) {
	for _, ev := range events {
		p.Process(ev)
	}
	p.flush()
}

// flush persists buffered events and updates session aggregates.
func (p *Pipeline) flush() {
	p.bufMu.Lock()
	if len(p.buffer) == 0 {
		p.bufMu.Unlock()
		return
	}
	batch := p.buffer
	p.buffer = nil
	p.bufMu.Unlock()

	// Persist events
	if err := p.db.PersistBatch(&store.EventBatch{Events: batch}); err != nil {
		log.Printf("batch persist error: %v", err)
	}

	// Persist affected sessions + repo mappings
	seen := make(map[string]bool)
	for _, ei := range batch {
		if seen[ei.SessionID] {
			continue
		}
		seen[ei.SessionID] = true

		sess, ok := p.sessions.Get(ei.SessionID)
		if !ok {
			continue
		}
		if err := p.db.SaveSession(sess); err != nil {
			log.Printf("session save error for %s: %v", ei.SessionID, err)
		}

		// Persist repo mapping if we have a CWD and RepoID
		if sess.CWD != "" && sess.RepoID != "" {
			p.db.UpsertCwdRepo(sess.CWD, sess.RepoID)
		}
	}
}

func (p *Pipeline) flushLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.flush()
		}
	}
}

// applyEvent updates session state from a parsed event.
// It delegates to focused helpers for each concern.
func (p *Pipeline) applyEvent(s *session.Session, msg *parser.Event, ev watcher.Event, r *repo.Repo) {
	p.applyRepoResolution(s, msg, r)
	p.applySessionMeta(s, msg, ev)
	p.applyParentDetection(s, msg, ev)
	p.applyCostDedup(s, msg)
	p.applyStatusUpdate(s, msg, ev)
}

// applyRepoResolution persists the resolved repo and copies CWD/branch/model metadata.
func (p *Pipeline) applyRepoResolution(s *session.Session, msg *parser.Event, r *repo.Repo) {
	if r != nil && s.RepoID == "" {
		s.RepoID = r.ID
		if err := p.db.UpsertRepo(r); err != nil {
			log.Printf("upsert repo error: %v", err)
		}
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
	if msg.Version != "" && s.Version == "" {
		s.Version = msg.Version
	}
	if msg.Entrypoint != "" && s.Entrypoint == "" {
		s.Entrypoint = msg.Entrypoint
	}
}

// applySessionMeta updates session naming, source file tracking, and task description.
func (p *Pipeline) applySessionMeta(s *session.Session, msg *parser.Event, ev watcher.Event) {
	// Track which file is providing events for this session.
	if s.SourceFile == "" {
		s.SourceFile = ev.FilePath
	} else if s.SourceFile != ev.FilePath {
		log.Printf("session %s: source file changed from %s to %s", ev.SessionID, s.SourceFile, ev.FilePath)
		s.SourceFile = ev.FilePath
	}

	// Session naming
	if msg.Type == "custom-title" && msg.ContentText != "" {
		s.SessionName = msg.ContentText
	} else if msg.Type == "agent-name" && msg.ContentText != "" && s.SessionName == "" {
		s.SessionName = msg.ContentText
	}

	// Task description from first user message
	if s.TaskDescription == "" && msg.Role == "user" && msg.ContentText != "" {
		desc := msg.ContentText
		if len([]rune(desc)) > 200 {
			desc = string([]rune(desc)[:200])
		}
		s.TaskDescription = desc

		// Fallback name for subagents without meta.json
		if s.ParentID != "" && s.SessionName == "" {
			aid := s.ID
			if strings.HasPrefix(aid, "agent-") {
				aid = aid[6:]
			}
			if len(aid) > 8 {
				aid = aid[:8]
			}
			s.SessionName = "agent " + aid
		}
	}
}

// applyParentDetection sets ParentID for subagents and team agents.
func (p *Pipeline) applyParentDetection(s *session.Session, msg *parser.Event, ev watcher.Event) {
	// Subagent detection via sidechain/parentUUID
	if msg.IsSidechain || msg.ParentUUID != "" {
		if parentSID := parentSessionIDFromPath(ev.FilePath); parentSID != "" {
			if s.ParentID == "" {
				s.ParentID = parentSID
				p.metaMu.Lock()
				meta := p.metaCache[ev.SessionID]
				p.metaMu.Unlock()
				if meta != nil {
					name := meta.Name
					if name == "" {
						name = meta.AgentType
					}
					if name != "" && s.SessionName == "" {
						s.SessionName = name
					}
				}
			}
		}
	}

	// Team agent detection via parent path
	if msg.TeamName != "" && s.ParentID == "" {
		if parentSID := parentSessionIDFromPath(ev.FilePath); parentSID != "" {
			s.ParentID = parentSID
			if msg.AgentName != "" && s.SessionName == "" {
				s.SessionName = msg.AgentName
			}
		}
	}
}

// applyCostDedup handles error tracking, cost/token dedup, and message counting.
func (p *Pipeline) applyCostDedup(s *session.Session, msg *parser.Event) {
	// Error tracking
	if msg.IsError && msg.MessageID != "" {
		errKey := "err:" + msg.MessageID
		if !s.HasSeenMessageID(errKey) {
			s.MarkMessageIDSeen(errKey)
			s.ErrorCount++
		}
	} else if msg.IsError {
		s.ErrorCount++
	}

	// Cost/token dedup
	if msg.MessageID != "" && (msg.CostUSD > 0 || msg.InputTokens > 0 || msg.OutputTokens > 0) {
		prev := s.GetMessageCosts(msg.MessageID)
		s.TotalCost += msg.CostUSD - prev.CostUSD
		s.InputTokens += msg.InputTokens - prev.InputTokens
		s.OutputTokens += msg.OutputTokens - prev.OutputTokens
		s.CacheReadTokens += msg.CacheReadTokens - prev.CacheReadTokens
		s.CacheCreationTokens += msg.CacheCreationTokens - prev.CacheCreationTokens
		s.SetMessageCosts(msg.MessageID, session.MessageCosts{
			CostUSD:             msg.CostUSD,
			InputTokens:         msg.InputTokens,
			OutputTokens:        msg.OutputTokens,
			CacheReadTokens:     msg.CacheReadTokens,
			CacheCreationTokens: msg.CacheCreationTokens,
		})
	}

	// Message count (conversation turns only)
	if msg.IsConversationTurn() {
		if msg.MessageID != "" {
			if !s.HasSeenMessageID(msg.MessageID) {
				s.MarkMessageIDSeen(msg.MessageID)
				s.MessageCount++
			}
		} else {
			s.MessageCount++
		}
	}

	// Event count (all JSONL lines)
	s.EventCount++
}

// applyStatusUpdate updates timestamps, handles summary resets, and sets session status.
func (p *Pipeline) applyStatusUpdate(s *session.Session, msg *parser.Event, ev watcher.Event) {
	// Timestamps
	if !msg.Timestamp.IsZero() {
		s.LastActive = msg.Timestamp
		if s.StartedAt.IsZero() || msg.Timestamp.Before(s.StartedAt) {
			s.StartedAt = msg.Timestamp
		}
	} else if !ev.Bootstrap {
		s.LastActive = time.Now()
	}

	// On compact/summary, reset message-ID dedup (IDs change after compact)
	// but preserve cost tracking to maintain running totals.
	if msg.Type == "summary" {
		s.ResetMessageIDs()
	}

	// Status
	if msg.Type == "summary" {
		s.Status = "thinking" // session stays active through compact
	} else if msg.StopReason == "end_turn" {
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
}

func (p *Pipeline) loadMeta(ev watcher.Event) {
	p.metaMu.Lock()
	defer p.metaMu.Unlock()
	if _, cached := p.metaCache[ev.SessionID]; cached {
		return
	}
	metaPath := strings.TrimSuffix(ev.FilePath, ".jsonl") + ".meta.json"
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta agentMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			log.Printf("debug: unmarshal meta.json (session=%s, path=%s): %v", ev.SessionID, metaPath, err)
		} else {
			p.metaCache[ev.SessionID] = &meta
			p.metaOrder = append(p.metaOrder, ev.SessionID)
		}
	}
	if len(p.metaCache) > maxMetaCache {
		half := len(p.metaOrder) / 2
		for _, key := range p.metaOrder[:half] {
			delete(p.metaCache, key)
		}
		p.metaOrder = p.metaOrder[half:]
	}
}

// skipDetail returns true for events that should only trigger a session_update,
// not a full event broadcast.
func skipDetail(event *parser.Event) bool {
	switch event.Type {
	case "file-history-snapshot", "queue-operation":
		return true
	case "system":
		// Allow turn_duration through; skip other system subtypes.
		return event.Subtype != "turn_duration"
	}
	return false
}

// parentSessionIDFromPath extracts the parent session ID from a subagent JSONL path.
// Subagent files: .../projects/<hash>/<parent-session-id>/subagents/agent-<id>.jsonl
func parentSessionIDFromPath(filePath string) string {
	dir := filepath.Dir(filePath)
	if filepath.Base(dir) == "subagents" {
		return filepath.Base(filepath.Dir(dir))
	}
	return ""
}
