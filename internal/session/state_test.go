package session

import (
	"math"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time                { return c.t }
func (c *clock) advance(d time.Duration)       { c.t = c.t.Add(d) }
func (c *clock) ago(d time.Duration) time.Time { return c.t.Add(-d) }

func clockedStore() (*Store, *clock) {
	c := &clock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	s := NewStore()
	s.now = c.now
	return s, c
}

// event upserts one Event for id, stamped at the clock's current time.
func event(s *Store, c *clock, id string, t Turn, edit ...func(*Session)) *Session {
	return s.Upsert(id, func(sess *Session) {
		sess.LastActive = c.now()
		sess.RecordTurn(t)
		for _, e := range edit {
			e(sess)
		}
	})
}

func get(t *testing.T, s *Store, id string) *Session {
	t.Helper()
	sess, ok := s.Get(id)
	if !ok {
		t.Fatalf("session %q not found", id)
	}
	return sess
}

func allByID(s *Store) map[string]*Session {
	out := map[string]*Session{}
	for _, sess := range s.All() {
		out[sess.ID] = sess
	}
	return out
}

type state struct {
	active, attention bool
	status            string
}

func stateOf(sess *Session) state {
	return state{sess.IsActive, sess.NeedsAttention, sess.Status}
}

func TestState_LastTurnSetsStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		turn Turn
		want state
	}{
		{"end_turn is awaiting user", Turn{Role: "assistant", StopReason: "end_turn"}, state{true, true, StatusWaiting}},
		{"tool_use stop", Turn{Role: "assistant", StopReason: "tool_use"}, state{true, false, StatusToolUse}},
		{"tool call", Turn{ToolName: "Bash"}, state{true, false, StatusToolUse}},
		{"assistant text", Turn{Role: "assistant"}, state{true, false, StatusThinking}},
		{"user message", Turn{Role: "user"}, state{true, false, StatusThinking}},
		{"compact summary", Turn{Summary: true, StopReason: "end_turn"}, state{true, false, StatusThinking}},
		{"not a turn", Turn{}, state{true, false, ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, c := clockedStore()
			if got := stateOf(event(s, c, "a", tc.turn)); got != tc.want {
				t.Errorf("Upsert = %+v, want %+v", got, tc.want)
			}
			if got := stateOf(get(t, s, "a")); got != tc.want {
				t.Errorf("Get = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestState_EventThatIsNotATurnKeepsStatus(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	event(s, c, "a", Turn{Role: "assistant", StopReason: "end_turn"})
	c.advance(time.Second)
	if got := event(s, c, "a", Turn{}); got.Status != StatusWaiting {
		t.Errorf("Status = %q, want waiting", got.Status)
	}
}

func TestState_GetAndAllDecayWithoutAnEvent(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	event(s, c, "a", Turn{Role: "assistant", StopReason: "end_turn"}, func(sess *Session) {
		sess.StartedAt = c.ago(2 * time.Minute)
		sess.TotalCost = 2
	})
	if got := get(t, s, "a"); got.CostRate != 1 {
		t.Errorf("CostRate = %v, want 1 $/min", got.CostRate)
	}

	c.advance(29 * time.Second)
	if got := stateOf(get(t, s, "a")); got != (state{true, true, StatusWaiting}) {
		t.Errorf("Get at 29s = %+v, want active waiting", got)
	}

	c.advance(time.Second)
	idle := state{false, false, StatusIdle}
	g := get(t, s, "a")
	if got := stateOf(g); got != idle {
		t.Errorf("Get at 30s = %+v, want idle", got)
	}
	if g.CostRate != 0 {
		t.Errorf("Get CostRate at 30s = %v, want 0", g.CostRate)
	}
	if got := stateOf(allByID(s)["a"]); got != idle {
		t.Errorf("All at 30s = %+v, want idle", got)
	}
}

func TestState_SubagentAwaitingIdlesAfterFiveSeconds(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	child := func(sess *Session) { sess.ParentID = "p" }
	event(s, c, "done", Turn{Role: "assistant", StopReason: "end_turn"}, child)
	event(s, c, "busy", Turn{ToolName: "Bash"}, child)

	c.advance(5 * time.Second)
	if got := get(t, s, "done"); got.IsActive || got.Status != StatusIdle {
		t.Errorf("finished subagent at 5s = %+v, want idle", stateOf(got))
	}
	if got := get(t, s, "busy"); !got.IsActive || got.Status != StatusToolUse {
		t.Errorf("working subagent at 5s = %+v, want active tool_use", stateOf(got))
	}
}

func TestState_ParentAwaitingChildren(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	event(s, c, "p", Turn{Role: "assistant", StopReason: "tool_use"})
	s.Upsert("p", func(sess *Session) { sess.Children = []string{"kid"} })

	c.advance(time.Minute)
	event(s, c, "kid", Turn{Role: "assistant", StopReason: "end_turn"}, func(sess *Session) { sess.ParentID = "p" })
	c.advance(10 * time.Second)

	// The Child session is itself idle after 5s, but keeps its Parent active for 30s.
	if got := get(t, s, "kid"); got.IsActive {
		t.Errorf("child = %+v, want idle", stateOf(got))
	}
	want := state{true, true, StatusWaiting}
	if got := stateOf(get(t, s, "p")); got != want {
		t.Errorf("Get parent = %+v, want %+v", got, want)
	}
	if got := stateOf(allByID(s)["p"]); got != want {
		t.Errorf("All parent = %+v, want %+v", got, want)
	}
	if got := s.Upsert("p", func(*Session) {}); stateOf(got) != want || got.CostRate != 0 {
		t.Errorf("Upsert parent = %+v cost rate %v, want %+v at 0", stateOf(got), got.CostRate, want)
	}

	c.advance(20 * time.Second)
	if got := stateOf(get(t, s, "p")); got != (state{false, false, StatusIdle}) {
		t.Errorf("parent after child went quiet = %+v, want idle", got)
	}
}

func TestState_QuietTurnIsForgottenOnTheNextUpsert(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	event(s, c, "a", Turn{Role: "assistant", StopReason: "end_turn"})
	c.advance(time.Minute)

	// A metadata line: no turn, no new LastActive.
	s.Upsert("a", func(*Session) {})
	if got := get(t, s, "a").LastTurn(); got != StatusIdle {
		t.Errorf("LastTurn = %q, want idle", got)
	}
	if got := stateOf(event(s, c, "a", Turn{})); got != (state{true, false, StatusIdle}) {
		t.Errorf("fresh Event that is not a turn = %+v, want active idle", got)
	}
}

func TestState_RestoredLastTurn(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	saved := &Session{ID: "a", LastActive: c.ago(10 * time.Second)}
	saved.SetLastTurn(StatusToolUse)
	s.Upsert("a", func(sess *Session) { *sess = *saved })
	if got := stateOf(get(t, s, "a")); got != (state{true, false, StatusToolUse}) {
		t.Errorf("restored = %+v, want active tool_use", got)
	}
}

func TestState_CostRateWaitsForAMinute(t *testing.T) {
	t.Parallel()
	s, c := clockedStore()
	got := event(s, c, "a", Turn{Role: "assistant"}, func(sess *Session) {
		sess.StartedAt = c.ago(30 * time.Second)
		sess.TotalCost = 1
	})
	if got.CostRate != 0 {
		t.Errorf("CostRate before a minute = %v, want 0", got.CostRate)
	}
	c.advance(90 * time.Second)
	got = event(s, c, "a", Turn{Role: "assistant"})
	if math.Abs(got.CostRate-0.5) > 1e-9 {
		t.Errorf("CostRate after two minutes = %v, want 0.5", got.CostRate)
	}
}
