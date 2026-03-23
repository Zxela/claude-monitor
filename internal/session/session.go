// Package session tracks per-session aggregates for Claude Code activity.
package session

import (
	"sync"
	"time"
)

// activeThreshold is how recent lastActive must be for a session to be considered active.
const activeThreshold = 30 * time.Second

// stuckTimeout is how long a status must remain unchanged before the session is considered stuck.
const stuckTimeout = 3 * time.Minute

// maxRecentTools is the number of recent tool names tracked for loop detection.
const maxRecentTools = 10

// maxSeenMessageIDs caps the deduplication map to prevent unbounded memory growth.
const maxSeenMessageIDs = 10000

// Session holds aggregated stats for a single Claude Code session (one JSONL file).
type Session struct {
	ID           string    `json:"id"`
	ProjectDir   string    `json:"projectDir"`
	ProjectName  string    `json:"projectName"`
	SessionName  string    `json:"sessionName,omitempty"`
	FilePath     string    `json:"filePath"`
	TotalCost    float64   `json:"totalCostUSD"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	CacheHitPct         float64 `json:"cacheHitPct"`
	MessageCount   int              `json:"messageCount"`
	LastActive     time.Time        `json:"lastActive"`
	IsActive       bool             `json:"isActive"` // true if lastActive < 30s ago
	StartedAt      time.Time        `json:"startedAt"`
	Status         string           `json:"status"` // idle, thinking, tool_use, waiting
	SeenMessageIDs map[string]bool  `json:"-"`      // tracks message IDs to deduplicate streaming chunks
	ParentID       string           `json:"parentId,omitempty"`
	Children       []string         `json:"children,omitempty"`
	CWD            string           `json:"cwd,omitempty"`
	GitBranch      string           `json:"gitBranch,omitempty"`
	Model          string           `json:"model,omitempty"`
	CostRate       float64          `json:"costRate"`  // dollars per minute (active sessions only)
	ErrorCount     int              `json:"errorCount"`
	IsSubagent     bool             `json:"isSubagent,omitempty"`
	StatusSince    time.Time        `json:"statusSince"`
	IsStuck        bool             `json:"isStuck"`
	RecentTools    []string         `json:"-"` // tracks last 10 tool names
	Outcome         string          `json:"outcome"`
	TaskDescription string          `json:"taskDescription"`
}

// Store is a thread-safe registry of sessions keyed by session ID.
type Store struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	replayCache map[string][]byte // cached JSON bytes per session ID
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		sessions:    make(map[string]*Session),
		replayCache: make(map[string][]byte),
	}
}

// GetReplayJSON returns cached manifest JSON for the given session ID, if present.
func (s *Store) GetReplayJSON(id string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.replayCache[id]
	return data, ok
}

// SetReplayJSON stores manifest JSON for the given session ID.
// Uses a check-then-set under the write lock so concurrent callers don't
// overwrite a freshly-invalidated entry with stale data.
func (s *Store) SetReplayJSON(id string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.replayCache[id]; !exists {
		s.replayCache[id] = data
	}
}

// InvalidateReplayJSON removes the cached manifest JSON for the given session ID.
func (s *Store) InvalidateReplayJSON(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.replayCache, id)
}

// Upsert creates or updates the session identified by sessionID.
// The update func is called with the (possibly newly created) Session pointer
// while the store lock is held. The updated Session is returned.
func (s *Store) Upsert(sessionID string, update func(*Session)) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[sessionID]
	if !ok {
		sess = &Session{
			ID: sessionID,
			// StartedAt will be set from first message timestamp
		}
		s.sessions[sessionID] = sess
	}

	prevMsgCount := sess.MessageCount
	update(sess)

	// Only invalidate replay cache when message count actually changed.
	if sess.MessageCount != prevMsgCount {
		delete(s.replayCache, sessionID)
	}

	// Prevent unbounded growth of the deduplication map.
	if len(sess.SeenMessageIDs) > maxSeenMessageIDs {
		sess.SeenMessageIDs = make(map[string]bool)
	}

	// Recalculate derived fields.
	sess.IsActive = time.Since(sess.LastActive) < activeThreshold
	if !sess.IsActive {
		sess.Status = "idle"
		sess.CostRate = 0
	}

	// Cost velocity: dollars per minute for active sessions.
	if sess.IsActive && sess.TotalCost > 0 && !sess.StartedAt.IsZero() {
		mins := time.Since(sess.StartedAt).Minutes()
		if mins >= 1 {
			sess.CostRate = sess.TotalCost / mins
		}
	}

	// Cache hit % = cache reads / total input tokens (including cache creation).
	totalInput := sess.InputTokens + sess.CacheReadTokens + sess.CacheCreationTokens
	if totalInput > 0 {
		sess.CacheHitPct = float64(sess.CacheReadTokens) / float64(totalInput) * 100
	}

	return sess
}

// All returns a snapshot slice of all sessions (unordered).
func (s *Store) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		// Return a copy to avoid callers mutating shared state.
		cp := *sess
		out = append(out, &cp)
	}
	return out
}

// LinkChild records that childID is a subagent of parentID.
func (s *Store) LinkChild(parentID, childID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent, ok := s.sessions[parentID]; ok {
		for _, c := range parent.Children {
			if c == childID {
				return // already linked
			}
		}
		parent.Children = append(parent.Children, childID)
	}
}

// Get returns the session for the given ID and whether it was found.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	cp := *sess
	return &cp, true
}

// CheckHealth iterates all sessions and marks them stuck if:
// a) Session is active AND status unchanged for >3 minutes
// b) OR RecentTools has 10 entries and all are identical (looping detection)
// Returns a list of session IDs whose IsStuck value changed.
func (s *Store) CheckHealth() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var changed []string

	for id, sess := range s.sessions {
		wasStuck := sess.IsStuck
		active := time.Since(sess.LastActive) < activeThreshold

		if !active {
			if wasStuck {
				sess.IsStuck = false
				changed = append(changed, id)
			}
			continue
		}

		stuck := false

		// (a) Status unchanged for >3 minutes while active
		if !sess.StatusSince.IsZero() && now.Sub(sess.StatusSince) > stuckTimeout {
			stuck = true
		}

		// (b) Last 10 tool calls are all the same tool (looping)
		if len(sess.RecentTools) >= maxRecentTools {
			allSame := true
			first := sess.RecentTools[0]
			for _, t := range sess.RecentTools[1:] {
				if t != first {
					allSame = false
					break
				}
			}
			if allSame {
				stuck = true
			}
		}

		sess.IsStuck = stuck
		if wasStuck != stuck {
			changed = append(changed, id)
		}
	}

	return changed
}
