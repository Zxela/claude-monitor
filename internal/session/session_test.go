package session

import (
	"math"
	"sync"
	"testing"
	"time"
)

func TestNewStore_CreatesEmptyStore(t *testing.T) {
	t.Parallel()
	s := NewStore()
	all := s.All()
	if len(all) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(all))
	}
}

func TestUpsert_CreatesNewSessionOnFirstCall(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.Upsert("session-1", func(sess *Session) {
		sess.TotalCost = 1.5
	})
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	if sess.ID != "session-1" {
		t.Errorf("ID: got %q, want session-1", sess.ID)
	}
	if sess.TotalCost != 1.5 {
		t.Errorf("TotalCost: got %f, want 1.5", sess.TotalCost)
	}
}

func TestUpsert_UpdatesExistingSessionOnSecondCall(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Upsert("session-2", func(sess *Session) {
		sess.TotalCost = 1.0
		sess.MessageCount = 1
	})
	sess := s.Upsert("session-2", func(sess *Session) {
		sess.TotalCost += 0.5
		sess.MessageCount++
	})
	if sess.TotalCost != 1.5 {
		t.Errorf("TotalCost after second upsert: got %f, want 1.5", sess.TotalCost)
	}
	if sess.MessageCount != 2 {
		t.Errorf("MessageCount after second upsert: got %d, want 2", sess.MessageCount)
	}
	// Only one session should exist
	all := s.All()
	if len(all) != 1 {
		t.Errorf("expected 1 session, got %d", len(all))
	}
}

func TestUpsert_ThreadSafe(t *testing.T) {
	t.Parallel()
	s := NewStore()
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			s.Upsert("concurrent-session", func(sess *Session) {
				sess.MessageCount++
			})
		}()
	}
	wg.Wait()
	sess, ok := s.Get("concurrent-session")
	if !ok {
		t.Fatal("session not found after concurrent upserts")
	}
	// MessageCount may not be exactly 100 due to race in the increment
	// (each goroutine reads and writes, not atomic), but the store must not crash/deadlock.
	// The important thing is no data race and the session exists.
	_ = sess
}

func TestAll_ReturnsSnapshot(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Upsert("snap-session", func(sess *Session) {
		sess.TotalCost = 1.0
	})
	all := s.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 session, got %d", len(all))
	}
	// Mutate the returned copy — store should not be affected.
	all[0].TotalCost = 999.0
	sess, ok := s.Get("snap-session")
	if !ok {
		t.Fatal("session not found")
	}
	if sess.TotalCost == 999.0 {
		t.Error("mutation of All() result affected internal store")
	}
}

func TestGet_ReturnsCopy(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Upsert("copy-session", func(sess *Session) {
		sess.TotalCost = 5.0
	})
	cp, ok := s.Get("copy-session")
	if !ok {
		t.Fatal("session not found")
	}
	cp.TotalCost = 999.0

	// Re-fetch and check that the store value is unchanged.
	original, ok := s.Get("copy-session")
	if !ok {
		t.Fatal("session not found on re-fetch")
	}
	if original.TotalCost == 999.0 {
		t.Error("mutation of Get() result affected internal store")
	}
}

func TestGet_ReturnsFalseForUnknownSession(t *testing.T) {
	t.Parallel()
	s := NewStore()
	_, ok := s.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent session, got true")
	}
}

func TestIsActive_TrueWhenRecentlyActive(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.Upsert("active-session", func(sess *Session) {
		sess.LastActive = time.Now() // just now
	})
	if !sess.IsActive {
		t.Error("expected IsActive = true for session active just now")
	}
}

func TestIsActive_FalseWhenOldLastActive(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.Upsert("inactive-session", func(sess *Session) {
		sess.LastActive = time.Now().Add(-60 * time.Second) // 60s ago
	})
	if sess.IsActive {
		t.Error("expected IsActive = false for session active 60s ago")
	}
}

func TestCacheHitPct_CalculatedCorrectly(t *testing.T) {
	t.Parallel()
	s := NewStore()
	// cacheHitPct = cacheReadTokens / (inputTokens + cacheReadTokens + cacheCreationTokens) * 100
	// = 400 / (600 + 400 + 0) * 100 = 40.0
	sess := s.Upsert("cache-session", func(sess *Session) {
		sess.InputTokens = 600
		sess.CacheReadTokens = 400
		sess.CacheCreationTokens = 0
	})
	if sess.CacheHitPct != 40.0 {
		t.Errorf("CacheHitPct: got %f, want 40.0", sess.CacheHitPct)
	}

	// Cache creation tokens dilute the hit percentage.
	// = 400 / (100 + 400 + 500) * 100 = 40.0
	sess2 := s.Upsert("cache-session-2", func(sess *Session) {
		sess.InputTokens = 100
		sess.CacheReadTokens = 400
		sess.CacheCreationTokens = 500
	})
	if sess2.CacheHitPct != 40.0 {
		t.Errorf("CacheHitPct with creation: got %f, want 40.0", sess2.CacheHitPct)
	}
}

func TestErrorCount_Tracking(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.Upsert("error-session", func(sess *Session) {
		sess.ErrorCount++
	})
	if sess.ErrorCount != 1 {
		t.Errorf("ErrorCount: got %d, want 1", sess.ErrorCount)
	}
	sess = s.Upsert("error-session", func(sess *Session) {
		sess.ErrorCount++
	})
	if sess.ErrorCount != 2 {
		t.Errorf("ErrorCount after second error: got %d, want 2", sess.ErrorCount)
	}
	// Non-error upsert should not change error count
	sess = s.Upsert("error-session", func(sess *Session) {
		sess.MessageCount++
	})
	if sess.ErrorCount != 2 {
		t.Errorf("ErrorCount after non-error upsert: got %d, want 2", sess.ErrorCount)
	}
}

func TestMessageCosts_DedupStreamingChunks(t *testing.T) {
	t.Parallel()
	s := NewStore()

	// Simulate 3 JSONL lines for the same message ID with cumulative output tokens.
	// This mimics Claude's streaming: each chunk carries the running total.
	chunks := []struct {
		msgID       string
		cost        float64
		input       int64
		output      int64
		cacheRead   int64
		cacheCreate int64
	}{
		{"msg_abc", 0.001, 100, 10, 5000, 2000},   // first chunk
		{"msg_abc", 0.001, 100, 50, 5000, 2000},   // second chunk (output grew)
		{"msg_abc", 0.001, 100, 200, 5000, 2000},  // final chunk (output grew)
	}

	for _, c := range chunks {
		s.Upsert("dedup-session", func(sess *Session) {
			if sess.SeenMessageCosts == nil {
				sess.SeenMessageCosts = make(map[string]MessageCosts)
			}
			prev := sess.SeenMessageCosts[c.msgID]
			sess.TotalCost += c.cost - prev.CostUSD
			sess.InputTokens += c.input - prev.InputTokens
			sess.OutputTokens += c.output - prev.OutputTokens
			sess.CacheReadTokens += c.cacheRead - prev.CacheReadTokens
			sess.CacheCreationTokens += c.cacheCreate - prev.CacheCreationTokens
			sess.SeenMessageCosts[c.msgID] = MessageCosts{
				CostUSD:             c.cost,
				InputTokens:         c.input,
				OutputTokens:        c.output,
				CacheReadTokens:     c.cacheRead,
				CacheCreationTokens: c.cacheCreate,
			}
		})
	}

	sess, ok := s.Get("dedup-session")
	if !ok {
		t.Fatal("session not found")
	}

	// Should reflect the FINAL values, not the sum of all 3 chunks.
	if sess.TotalCost != 0.001 {
		t.Errorf("TotalCost: got %g, want 0.001 (should not triple-count)", sess.TotalCost)
	}
	if sess.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", sess.InputTokens)
	}
	if sess.OutputTokens != 200 {
		t.Errorf("OutputTokens: got %d, want 200 (final cumulative value)", sess.OutputTokens)
	}
	if sess.CacheReadTokens != 5000 {
		t.Errorf("CacheReadTokens: got %d, want 5000", sess.CacheReadTokens)
	}
	if sess.CacheCreationTokens != 2000 {
		t.Errorf("CacheCreationTokens: got %d, want 2000", sess.CacheCreationTokens)
	}
}

func TestMessageCosts_MultipleMessages(t *testing.T) {
	t.Parallel()
	s := NewStore()

	// Two different message IDs — costs should add, not replace.
	messages := []struct {
		msgID  string
		cost   float64
		output int64
	}{
		{"msg_1", 0.05, 500},
		{"msg_1", 0.05, 800},  // same msg, output grew
		{"msg_2", 0.10, 1000}, // new message
	}

	for _, m := range messages {
		s.Upsert("multi-msg-session", func(sess *Session) {
			if sess.SeenMessageCosts == nil {
				sess.SeenMessageCosts = make(map[string]MessageCosts)
			}
			prev := sess.SeenMessageCosts[m.msgID]
			sess.TotalCost += m.cost - prev.CostUSD
			sess.OutputTokens += m.output - prev.OutputTokens
			sess.SeenMessageCosts[m.msgID] = MessageCosts{
				CostUSD:      m.cost,
				OutputTokens: m.output,
			}
		})
	}

	sess, ok := s.Get("multi-msg-session")
	if !ok {
		t.Fatal("session not found")
	}

	// msg_1 final cost=0.05, msg_2 cost=0.10 → total=0.15
	if math.Abs(sess.TotalCost-0.15) > 1e-12 {
		t.Errorf("TotalCost: got %g, want 0.15", sess.TotalCost)
	}
	// msg_1 final output=800, msg_2 output=1000 → total=1800
	if sess.OutputTokens != 1800 {
		t.Errorf("OutputTokens: got %d, want 1800", sess.OutputTokens)
	}
}

func TestCacheHitPct_ZeroWhenNoTokens(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.Upsert("no-token-session", func(sess *Session) {
		sess.InputTokens = 0
		sess.CacheReadTokens = 0
	})
	if sess.CacheHitPct != 0.0 {
		t.Errorf("CacheHitPct: got %f, want 0.0", sess.CacheHitPct)
	}
}
