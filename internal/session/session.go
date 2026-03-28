// Package session tracks per-session aggregates for Claude Code activity.
package session

import (
	"sync"
	"time"
)

// activeThreshold is how recent lastActive must be for a session to be considered active.
const activeThreshold = 30 * time.Second

// subagentWaitingThreshold is a shorter timeout for subagents in "waiting" status.
// Subagents that finish their task emit end_turn then stop writing — no need to wait 30s.
const subagentWaitingThreshold = 5 * time.Second

// maxReplayCacheSize caps the number of entries in the replay cache to prevent unbounded growth.
const maxReplayCacheSize = 100

// maxSeenMessageIDs caps the deduplication map to prevent unbounded memory growth.
const maxSeenMessageIDs = 10000

// MessageCosts tracks the last-seen cost/tokens for a single message ID,
// so we can replace (not double-add) when streaming sends multiple lines per message.
type MessageCosts struct {
	CostUSD             float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// Session holds aggregated stats for a single Claude Code session (one JSONL file).
type Session struct {
	ID              string    `json:"id"`
	RepoID          string    `json:"repoId,omitempty"`
	SessionName     string    `json:"sessionName,omitempty"`
	TotalCost       float64   `json:"totalCost"`
	InputTokens     int64     `json:"inputTokens"`
	OutputTokens    int64     `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	MessageCount   int              `json:"messageCount"`
	EventCount     int              `json:"eventCount"`
	LastActive     time.Time        `json:"lastActive"`
	IsActive       bool             `json:"isActive"` // true if lastActive < 30s ago
	StartedAt      time.Time        `json:"startedAt"`
	Status         string           `json:"status"` // idle, thinking, tool_use, waiting
	SeenMessageIDs   map[string]bool          `json:"-"` // tracks message IDs to deduplicate streaming chunks
	SeenMessageCosts map[string]MessageCosts  `json:"-"` // tracks per-message cost/tokens for dedup
	ParentID       string           `json:"parentId,omitempty"`
	Children       []string         `json:"children,omitempty"`
	CWD            string           `json:"cwd,omitempty"`
	GitBranch      string           `json:"gitBranch,omitempty"`
	Model          string           `json:"model,omitempty"`
	CostRate       float64          `json:"costRate"`  // dollars per minute (active sessions only)
	ErrorCount     int              `json:"errorCount"`
	TaskDescription string          `json:"taskDescription"`
}

// Store is a thread-safe registry of sessions keyed by session ID.
type Store struct {
	mu              sync.RWMutex
	sessions        map[string]*Session
	replayCache     map[string][]byte // cached JSON bytes per session ID
	replayCacheOrder []string          // insertion order for LRU eviction
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
// The cache is bounded to maxReplayCacheSize entries; the oldest entry is
// evicted when the cache is full.
func (s *Store) SetReplayJSON(id string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.replayCache[id]; !exists {
		// Evict oldest entry if at capacity.
		if len(s.replayCache) >= maxReplayCacheSize {
			oldest := s.replayCacheOrder[0]
			s.replayCacheOrder = s.replayCacheOrder[1:]
			delete(s.replayCache, oldest)
		}
		s.replayCache[id] = data
		s.replayCacheOrder = append(s.replayCacheOrder, id)
	}
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
		// Remove from insertion order tracking.
		for i, id := range s.replayCacheOrder {
			if id == sessionID {
				s.replayCacheOrder = append(s.replayCacheOrder[:i], s.replayCacheOrder[i+1:]...)
				break
			}
		}
	}

	// Prevent unbounded growth of the deduplication maps.
	if len(sess.SeenMessageIDs) > maxSeenMessageIDs {
		sess.SeenMessageIDs = make(map[string]bool)
	}
	// Note: we do NOT cap SeenMessageCosts — clearing it would break the
	// delta-based cost accumulation (cleared entries re-add full cost).
	// 10k entries ≈ 500KB, acceptable for correctness.

	// Recalculate derived fields.
	// Subagents in "waiting" use a shorter threshold — they emit end_turn then stop.
	threshold := activeThreshold
	if sess.ParentID != "" && sess.Status == "waiting" {
		threshold = subagentWaitingThreshold
	}
	sess.IsActive = time.Since(sess.LastActive) < threshold
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

	return sess
}

// All returns a snapshot slice of all sessions (unordered).
// IsActive is recalculated for each session based on current time.
func (s *Store) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		cp := *sess
		cp.SeenMessageIDs = nil   // don't share internal dedup map
		cp.SeenMessageCosts = nil // don't share internal dedup map
		// Recalculate IsActive so callers always see fresh status.
		th := activeThreshold
		if cp.ParentID != "" && cp.Status == "waiting" {
			th = subagentWaitingThreshold
		}
		cp.IsActive = time.Since(cp.LastActive) < th
		if !cp.IsActive {
			cp.Status = "idle"
			cp.CostRate = 0
		}
		out = append(out, &cp)
	}

	// Second pass: parents with active children stay as "waiting", not "idle".
	for _, sess := range out {
		if sess.Status == "idle" && len(sess.Children) > 0 {
			for _, childID := range sess.Children {
				if child, ok := s.sessions[childID]; ok {
					if time.Since(child.LastActive) < activeThreshold {
						sess.IsActive = true
						sess.Status = "waiting"
						break
					}
				}
			}
		}
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
	cp.SeenMessageIDs = nil
	cp.SeenMessageCosts = nil
	return &cp, true
}

