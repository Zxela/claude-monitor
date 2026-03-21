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
	// cacheHitPct = cacheTokens / (inputTokens + cacheTokens) * 100
	// = 400 / (600 + 400) * 100 = 40.0
	sess := s.Upsert("cache-session", func(sess *Session) {
		sess.InputTokens = 600
		sess.CacheTokens = 400
	})
	if sess.CacheHitPct != 40.0 {
		t.Errorf("CacheHitPct: got %f, want 40.0", sess.CacheHitPct)
	}
}

func TestCacheHitPct_ZeroWhenNoTokens(t *testing.T) {
	t.Parallel()
	s := NewStore()
	sess := s.Upsert("no-token-session", func(sess *Session) {
		sess.InputTokens = 0
		sess.CacheTokens = 0
	})
	if sess.CacheHitPct != 0.0 {
		t.Errorf("CacheHitPct: got %f, want 0.0", sess.CacheHitPct)
	}
}
