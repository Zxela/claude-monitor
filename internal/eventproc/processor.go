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

// agentMeta caches subagent metadata read from .meta.json files.
type agentMeta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Name        string `json:"name"`
}

// Result holds the output of processing a single watcher event.
type Result struct {
	Message *parser.ParsedMessage
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
		p.metaMu.Unlock()
	}

	sess := p.store.Upsert(ev.SessionID, func(s *session.Session) {
		s.FilePath = ev.FilePath
		s.ProjectDir = ev.ProjectDir
		if ev.Label != "" {
			s.ProjectName = ev.Label + " / " + ev.ProjectDir
		} else if s.ProjectName == "" {
			s.ProjectName = ev.ProjectDir
		}
		if msg.CWD != "" {
			s.CWD = msg.CWD
			// Derive a cleaner project name from CWD if we only have
			// the raw dir hash (e.g. "-root-claude-monitor").
			if s.SessionName == "" && strings.HasPrefix(s.ProjectName, "-") {
				s.ProjectName = filepath.Base(msg.CWD)
			}
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
			if parentSID := ParentSessionIDFromPath(ev.FilePath); parentSID != "" {
				if s.ParentID == "" {
					s.ParentID = parentSID
					s.IsSubagent = true

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
							s.ProjectName = name
						}
					}
				}
			}
		}
		// Team agent detection: link to team lead session via config file.
		if msg.TeamName != "" && s.ParentID == "" {
			if leadSID := p.resolveTeamLead(msg.TeamName, ev.SessionID); leadSID != "" {
				s.ParentID = leadSID
				s.IsSubagent = true
				if msg.AgentName != "" && s.SessionName == "" {
					s.SessionName = msg.AgentName
					s.ProjectName = msg.AgentName
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
			if s.IsSubagent && s.SessionName == "" && s.ProjectName == "subagents" {
				aid := s.ID
				if strings.HasPrefix(aid, "agent-") {
					aid = aid[6:]
				}
				if len(aid) > 8 {
					aid = aid[:8]
				}
				s.ProjectName = "agent " + aid
			}
		}
	})

	if sess.IsSubagent && sess.ParentID != "" {
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

// teamConfig is the minimal structure of ~/.claude/teams/{name}/config.json.
type teamConfig struct {
	LeadSessionID string `json:"leadSessionId"`
}

// resolveTeamLead reads the team config file and returns the lead session ID.
// Results are cached in metaCache (keyed by "team:" + teamName).
func (p *Processor) resolveTeamLead(teamName, sessionID string) string {
	cacheKey := "team:" + teamName

	// Check cache first.
	p.metaMu.Lock()
	if cached, ok := p.metaCache[cacheKey]; ok {
		p.metaMu.Unlock()
		if cached != nil {
			return cached.Description // repurpose Description field for leadSessionID
		}
		return ""
	}
	p.metaMu.Unlock()

	// Read team config outside the lock (file I/O).
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(homeDir, ".claude", "teams", teamName, "config.json")
	data, err := os.ReadFile(configPath)

	// Cache the result atomically.
	p.metaMu.Lock()
	defer p.metaMu.Unlock()

	// Re-check: another goroutine may have populated the cache while we did I/O.
	if cached, ok := p.metaCache[cacheKey]; ok {
		if cached != nil {
			return cached.Description
		}
		return ""
	}

	if err != nil {
		p.metaCache[cacheKey] = nil
		return ""
	}
	var cfg teamConfig
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.LeadSessionID == "" || cfg.LeadSessionID == sessionID {
		p.metaCache[cacheKey] = nil
		return ""
	}
	p.metaCache[cacheKey] = &agentMeta{Description: cfg.LeadSessionID}
	return cfg.LeadSessionID
}
