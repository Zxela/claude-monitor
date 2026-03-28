// Package eventproc processes watcher events into session updates.
package eventproc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/watcher"
)

// maxMetaCacheSize is the maximum number of entries in the metaCache before
// it is cleared to prevent unbounded memory growth.
const maxMetaCacheSize = 500

// agentMeta caches subagent metadata read from .meta.json files.
type agentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Name        string `json:"name"`
}

// Result holds the output of processing a single watcher event.
type Result struct {
	Message *parser.Event
	Session *session.Session
	IsNew   bool
}

// Processor parses watcher events and updates the session store.
type Processor struct {
	store     *session.Store
	metaMu    sync.Mutex
	metaCache map[string]*agentMeta // sessionID -> meta
}

// New creates a Processor backed by the given session store.
func New(store *session.Store) *Processor {
	return &Processor{
		store:     store,
		metaCache: make(map[string]*agentMeta),
	}
}

// Process parses a watcher event and updates the session store.
// Returns the parsed message, session, and whether it's a new session.
func (p *Processor) Process(ev watcher.Event) Result {
	msg, err := parser.ParseLine(ev.Line)
	if err != nil {
		return Result{}
	}
	_, exists := p.store.Get(ev.SessionID)
	isNew := !exists

	// Pre-read subagent meta.json outside the store lock (only once per session).
	if isNew {
		p.metaMu.Lock()
		if _, cached := p.metaCache[ev.SessionID]; !cached {
			metaPath := strings.TrimSuffix(ev.FilePath, ".jsonl") + ".meta.json"
			if metaData, err := os.ReadFile(metaPath); err == nil {
				var meta agentMeta
				if json.Unmarshal(metaData, &meta) == nil {
					p.metaCache[ev.SessionID] = &meta
				}
			}
		}
		if len(p.metaCache) > maxMetaCacheSize {
			p.metaCache = make(map[string]*agentMeta)
		}
		p.metaMu.Unlock()
	}

	sess := p.store.Upsert(ev.SessionID, func(s *session.Session) {
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
		if msg.IsSidechain || msg.ParentUUID != "" {
			if parentSID := ParentSessionIDFromPath(ev.FilePath); parentSID != "" {
				if s.ParentID == "" {
					s.ParentID = parentSID

					// Apply cached meta.json for agent name/type.
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
		// Team agent detection via parent path (team agents use same
		// subagent file structure — teamName in JSONL is not reliably set).
		if msg.TeamName != "" && s.ParentID == "" {
			if parentSID := ParentSessionIDFromPath(ev.FilePath); parentSID != "" {
				s.ParentID = parentSID
				if msg.AgentName != "" && s.SessionName == "" {
					s.SessionName = msg.AgentName
				}
			}
		}
		if msg.IsError && msg.MessageID != "" {
			if s.SeenMessageIDs == nil {
				s.SeenMessageIDs = make(map[string]bool)
			}
			errKey := "err:" + msg.MessageID
			if !s.SeenMessageIDs[errKey] {
				s.SeenMessageIDs[errKey] = true
				s.ErrorCount++
			}
		} else if msg.IsError {
			s.ErrorCount++
		}
		// Deduplicate cost/token accumulation by message ID.
		// Multiple JSONL lines share the same message ID (streaming chunks)
		// with cumulative token counts — only count the delta vs last seen.
		if msg.MessageID != "" && (msg.CostUSD > 0 || msg.InputTokens > 0 || msg.OutputTokens > 0) {
			if s.SeenMessageCosts == nil {
				s.SeenMessageCosts = make(map[string]session.MessageCosts)
			}
			prev := s.SeenMessageCosts[msg.MessageID]
			s.TotalCost += msg.CostUSD - prev.CostUSD
			s.InputTokens += msg.InputTokens - prev.InputTokens
			s.OutputTokens += msg.OutputTokens - prev.OutputTokens
			s.CacheReadTokens += msg.CacheReadTokens - prev.CacheReadTokens
			s.CacheCreationTokens += msg.CacheCreationTokens - prev.CacheCreationTokens
			s.SeenMessageCosts[msg.MessageID] = session.MessageCosts{
				CostUSD:             msg.CostUSD,
				InputTokens:         msg.InputTokens,
				OutputTokens:        msg.OutputTokens,
				CacheReadTokens:     msg.CacheReadTokens,
				CacheCreationTokens: msg.CacheCreationTokens,
			}
		}
		if msg.IsConversationTurn() {
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

		// Determine new status
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

		// Capture task description from first user message.
		if s.TaskDescription == "" && msg.Role == "user" && msg.ContentText != "" {
			desc := msg.ContentText
			if len([]rune(desc)) > 200 {
				runes := []rune(desc)
				desc = string(runes[:200])
			}
			s.TaskDescription = desc

			// Fallback for subagents without meta.json: use short agent ID.
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
	})

	if sess.ParentID != "" {
		p.store.LinkChild(sess.ParentID, ev.SessionID)
	}

	return Result{
		Message: msg,
		Session: sess,
		IsNew:   isNew,
	}
}

// ParentSessionIDFromPath extracts the parent session ID from a subagent JSONL
// file path. Subagent files live at:
//
//	.../projects/<hash>/<parent-session-id>/subagents/agent-<id>.jsonl
func ParentSessionIDFromPath(filePath string) string {
	dir := filepath.Dir(filePath)  // .../subagents/
	dirName := filepath.Base(dir)
	if dirName == "subagents" {
		parentDir := filepath.Dir(dir) // .../<parent-session-id>/
		return filepath.Base(parentDir)
	}
	return ""
}

