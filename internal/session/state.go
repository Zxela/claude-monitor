package session

import "time"

// Session status values. StatusWaiting covers both Awaiting user and a Parent
// session kept active by its Child sessions.
const (
	StatusIdle     = "idle"
	StatusThinking = "thinking"
	StatusToolUse  = "tool_use"
	StatusWaiting  = "waiting"
)

const (
	activeThreshold = 30 * time.Second
	// Subagents emit end_turn and then stop writing.
	subagentWaitingThreshold = 5 * time.Second
)

// Turn is what one Event says about its Session's turn.
type Turn struct {
	Summary    bool
	StopReason string
	ToolName   string
	Role       string
}

// RecordTurn records the Session's last turn. An Event that is not a turn
// leaves it unchanged.
func (s *Session) RecordTurn(t Turn) {
	switch {
	case t.Summary:
		s.turn = StatusThinking // a compact keeps the Session active
	case t.StopReason == "end_turn":
		s.turn = StatusWaiting
	case t.StopReason == "tool_use", t.ToolName != "":
		s.turn = StatusToolUse
	case t.Role == "assistant", t.Role == "user":
		s.turn = StatusThinking
	}
}

// LastTurn reports the status the Session's last turn left it in, as persisted.
func (s *Session) LastTurn() string { return s.turn }

// SetLastTurn restores a status saved from LastTurn.
func (s *Session) SetLastTurn(status string) { s.turn = status }

// derive sets Status, IsActive and NeedsAttention from the last turn, the
// Child sessions' activity and now, and zeroes CostRate once the Session's
// own Events go quiet. It reports whether the Session's own Events are recent.
func (s *Session) derive(now time.Time, childActive func() bool) (self bool) {
	threshold := activeThreshold
	if s.ParentID != "" && s.turn == StatusWaiting {
		threshold = subagentWaitingThreshold
	}
	self = now.Sub(s.LastActive) < threshold
	s.IsActive = self
	s.Status = s.turn
	if !self {
		s.Status = StatusIdle
		s.CostRate = 0
	}
	if s.Status == StatusIdle && childActive() {
		s.IsActive = true
		s.Status = StatusWaiting
	}
	s.NeedsAttention = s.Status == StatusWaiting
	return self
}

// deriveLocked derives sess's state at now. Caller holds s.mu.
func (s *Store) deriveLocked(sess *Session, now time.Time) (self bool) {
	return sess.derive(now, func() bool {
		for _, id := range sess.Children {
			if child, ok := s.sessions[id]; ok && now.Sub(child.LastActive) < activeThreshold {
				return true
			}
		}
		return false
	})
}

// settleLocked derives the stored sess in place. A last turn that has gone
// quiet is forgotten, so a later Event that is not a turn leaves the Session
// idle. Caller holds s.mu for writing.
func (s *Store) settleLocked(sess *Session, now time.Time) {
	if !s.deriveLocked(sess, now) {
		sess.turn = StatusIdle
		return
	}
	if sess.TotalCost > 0 && !sess.StartedAt.IsZero() {
		if mins := now.Sub(sess.StartedAt).Minutes(); mins >= 1 {
			sess.CostRate = sess.TotalCost / mins
		}
	}
}
