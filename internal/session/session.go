// Package session tracks per-session aggregates for Claude Code activity.
package session

import (
	"sync"
	"time"

	"github.com/zxela/claude-monitor/internal/repo"
)

// MessageCosts tracks the last-seen cost/tokens for a single message ID,
// so we can replace (not double-add) when streaming sends multiple lines per message.
type MessageCosts struct {
	CostUSD             float64
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// PRLink is a pull/merge request Claude Code linked to the session.
type PRLink struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	Repository string `json:"repository,omitempty"`
}

// Session holds aggregated stats for a single Claude Code session (one JSONL file).
type Session struct {
	ID                  string                  `json:"id"`
	RepoID              string                  `json:"repoId,omitempty"`
	SessionName         string                  `json:"sessionName,omitempty"`
	AITitle             string                  `json:"aiTitle,omitempty"` // Claude Code's generated title; shown when SessionName is empty
	PRLinks             []PRLink                `json:"prLinks,omitempty"`
	TotalCost           float64                 `json:"totalCost"`
	InputTokens         int64                   `json:"inputTokens"`
	OutputTokens        int64                   `json:"outputTokens"`
	CacheReadTokens     int64                   `json:"cacheReadTokens"`
	CacheCreationTokens int64                   `json:"cacheCreationTokens"`
	MessageCount        int                     `json:"messageCount"`
	EventCount          int                     `json:"eventCount"`
	LastActive          time.Time               `json:"lastActive"`
	IsActive            bool                    `json:"isActive"` // derived in state.go, as are Status and NeedsAttention
	StartedAt           time.Time               `json:"startedAt"`
	Status              string                  `json:"status"` // idle, thinking, tool_use, waiting
	seenMessageCosts    map[string]MessageCosts `json:"-"`      // last cost/tokens per Message; never evicted, since a dropped entry would re-add that Message's full cost
	seenRows            map[string]struct{}     `json:"-"`      // dedup keys of event rows already counted; never reset
	ParentID            string                  `json:"parentId,omitempty"`
	Children            []string                `json:"children,omitempty"`
	CWD                 string                  `json:"cwd,omitempty"`
	GitBranch           string                  `json:"gitBranch,omitempty"`
	Model               string                  `json:"model,omitempty"`
	CostRate            float64                 `json:"costRate"` // dollars per minute (active sessions only)
	ErrorCount          int                     `json:"errorCount"`
	TaskDescription     string                  `json:"taskDescription"`
	Version             string                  `json:"version,omitempty"`
	Entrypoint          string                  `json:"entrypoint,omitempty"`
	WorkflowID          string                  `json:"workflowId,omitempty"` // wf_<id> grouping all agents of one Claude Code workflow run; empty for non-workflow sessions
	AgentID             string                  `json:"agentId,omitempty"`    // agent-<id> file stem for subagent/workflow rows (== ID); empty for normal sessions
	AgentKind           string                  `json:"agentKind,omitempty"`  // session | subagent | workflow_agent
	NeedsAttention      bool                    `json:"needsAttention"`       // Status is waiting, so Awaiting children counts until #320; the client adds unseen errors
	turn                string                  `json:"-"`                    // status the last turn left; input to Status
	SourceFile          string                  `json:"-"`                    // JSONL file path currently providing events (not serialized)
	repoSourceRank      int                     `json:"-"`                    // repo.Pin.Rank; see RepoPin
	repoToplevel        string                  `json:"-"`                    // repo.Pin.Toplevel
	repoInherited       bool                    `json:"-"`                    // repo.Pin.Inherited
	dedupPending        bool                    `json:"-"`                    // restored from the DB without dedup state; load it before applying a line
}

// SetPRLink records l, replacing any earlier link with the same number.
// It builds a new slice because Get/All snapshots share the old one.
func (s *Session) SetPRLink(l PRLink) {
	links := make([]PRLink, 0, len(s.PRLinks)+1)
	replaced := false
	for _, cur := range s.PRLinks {
		if cur.Number == l.Number {
			cur, replaced = l, true
		}
		links = append(links, cur)
	}
	if !replaced {
		links = append(links, l)
	}
	s.PRLinks = links
}

// RepoPin reports the Repo this session is attributed to and its provenance.
func (s *Session) RepoPin() repo.Pin {
	return repo.Pin{ID: s.RepoID, Rank: s.repoSourceRank, Toplevel: s.repoToplevel, Inherited: s.repoInherited}
}

// SetRepoPin sets RepoID and its provenance.
func (s *Session) SetRepoPin(p repo.Pin) {
	s.RepoID, s.repoSourceRank, s.repoToplevel, s.repoInherited = p.ID, p.Rank, p.Toplevel, p.Inherited
}

// GetMessageCosts returns the previously recorded cost entry for a message ID.
// If no entry exists, the zero value is returned.
func (s *Session) GetMessageCosts(msgID string) MessageCosts {
	if s.seenMessageCosts == nil {
		return MessageCosts{}
	}
	return s.seenMessageCosts[msgID]
}

// SetMessageCosts stores the cost/token snapshot for a message ID.
func (s *Session) SetMessageCosts(msgID string, mc MessageCosts) {
	if s.seenMessageCosts == nil {
		s.seenMessageCosts = make(map[string]MessageCosts)
	}
	s.seenMessageCosts[msgID] = mc
}

// MarkRowSeen records an event row's dedup key and reports whether it was new.
// The set is never evicted or cleared, so the startup replay of an
// already-persisted line does not count it again.
func (s *Session) MarkRowSeen(key string) bool {
	if _, ok := s.seenRows[key]; ok {
		return false
	}
	if s.seenRows == nil {
		s.seenRows = make(map[string]struct{})
	}
	s.seenRows[key] = struct{}{}
	return true
}

// SetSeenRows replaces the row-key set, used when restoring from the database.
func (s *Session) SetSeenRows(keys map[string]struct{}) {
	s.seenRows = keys
}

// DedupPending reports whether the session was restored from the database
// without its dedup state, which must be loaded before the next line is applied.
func (s *Session) DedupPending() bool { return s.dedupPending }

// SetDedupPending marks whether the session's dedup state still needs loading.
func (s *Session) SetDedupPending(v bool) { s.dedupPending = v }

// RestoreMessageCosts replaces the per-Message cost map, used when restoring from the database.
func (s *Session) RestoreMessageCosts(costs map[string]MessageCosts) {
	s.seenMessageCosts = costs
}

// Store is a thread-safe registry of sessions keyed by session ID.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	now      func() time.Time
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*Session),
		now:      time.Now,
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

	update(sess)

	s.settleLocked(sess, s.now())

	return sess
}

// All returns a snapshot slice of all sessions (unordered), with their state
// derived at the current time.
func (s *Store) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	out := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, s.snapshotLocked(sess, now))
	}
	return out
}

// snapshotLocked copies sess without its dedup state and derives the copy's
// state at now. Caller holds s.mu.
func (s *Store) snapshotLocked(sess *Session, now time.Time) *Session {
	cp := *sess
	cp.seenMessageCosts = nil
	cp.seenRows = nil
	s.deriveLocked(&cp, now)
	return &cp
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

// Get returns a snapshot of the session for the given ID, with its state
// derived at the current time, and whether it was found.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	return s.snapshotLocked(sess, s.now()), true
}
