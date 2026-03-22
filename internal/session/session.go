// Package session tracks per-session aggregates for Claude Code activity.
package session

import (
	"sync"
	"time"
)

// activeThreshold is how recent lastActive must be for a session to be considered active.
const activeThreshold = 30 * time.Second

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
	CacheTokens  int64     `json:"cacheTokens"`
	CacheHitPct  float64   `json:"cacheHitPct"`
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
	IsSubagent     bool             `json:"isSubagent,omitempty"`
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

	update(sess)

	// Invalidate replay cache since new data was written.
	delete(s.replayCache, sessionID)

	// Recalculate derived fields.
	sess.IsActive = time.Since(sess.LastActive) < activeThreshold
	if !sess.IsActive {
		sess.Status = "idle"
	}

	// NOTE: CacheTokens includes both cache reads and cache creation tokens.
	// Ideally CacheHitPct would use only cache read tokens, but we only store
	// the combined value. This is a known limitation.
	totalInput := sess.InputTokens + sess.CacheTokens
	if totalInput > 0 {
		sess.CacheHitPct = float64(sess.CacheTokens) / float64(totalInput) * 100
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
