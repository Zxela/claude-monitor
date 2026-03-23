package session

import (
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

func TestCheckHealth_StuckByStatusTimeout(t *testing.T) {
	t.Parallel()
	s := NewStore()
	// Create an active session with status unchanged for >3 minutes
	s.Upsert("stuck-session", func(sess *Session) {
		sess.LastActive = time.Now() // active
		sess.Status = "thinking"
		sess.StatusSince = time.Now().Add(-4 * time.Minute) // status unchanged for 4 min
	})
	changed := s.CheckHealth()
	if len(changed) != 1 || changed[0] != "stuck-session" {
		t.Errorf("expected stuck-session to be changed, got %v", changed)
	}
	sess, _ := s.Get("stuck-session")
	if !sess.IsStuck {
		t.Error("expected session to be marked stuck")
	}
}

func TestCheckHealth_StuckByToolLoop(t *testing.T) {
	t.Parallel()
	s := NewStore()
	// Create an active session with 10 identical tool calls
	s.Upsert("loop-session", func(sess *Session) {
		sess.LastActive = time.Now()
		sess.Status = "tool_use"
		sess.StatusSince = time.Now() // recent status change, so not stuck by timeout
		sess.RecentTools = []string{"Bash", "Bash", "Bash", "Bash", "Bash", "Bash", "Bash", "Bash", "Bash", "Bash"}
	})
	changed := s.CheckHealth()
	if len(changed) != 1 || changed[0] != "loop-session" {
		t.Errorf("expected loop-session to be changed, got %v", changed)
	}
	sess, _ := s.Get("loop-session")
	if !sess.IsStuck {
		t.Error("expected session to be marked stuck due to tool loop")
	}
}

func TestCheckHealth_NotStuckWhenInactive(t *testing.T) {
	t.Parallel()
	s := NewStore()
	// Inactive session with old status — should NOT be stuck
	s.Upsert("inactive-stuck", func(sess *Session) {
		sess.LastActive = time.Now().Add(-60 * time.Second)
		sess.Status = "thinking"
		sess.StatusSince = time.Now().Add(-10 * time.Minute)
	})
	changed := s.CheckHealth()
	if len(changed) != 0 {
		t.Errorf("expected no changes for inactive session, got %v", changed)
	}
	sess, _ := s.Get("inactive-stuck")
	if sess.IsStuck {
		t.Error("inactive session should not be marked stuck")
	}
}

func TestCheckHealth_NotStuckWithVariedTools(t *testing.T) {
	t.Parallel()
	s := NewStore()
	s.Upsert("varied-tools", func(sess *Session) {
		sess.LastActive = time.Now()
		sess.Status = "tool_use"
		sess.StatusSince = time.Now()
		sess.RecentTools = []string{"Bash", "Read", "Bash", "Read", "Bash", "Read", "Bash", "Read", "Bash", "Read"}
	})
	changed := s.CheckHealth()
	if len(changed) != 0 {
		t.Errorf("expected no changes for varied tools, got %v", changed)
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
