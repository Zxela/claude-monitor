package store

import (
	"testing"
	"time"
)

func TestWindowStart(t *testing.T) {
	t.Parallel()

	// Wednesday 2026-06-10 15:30 in a fixed non-UTC zone, so local-midnight
	// boundaries are distinguishable from UTC ones.
	loc := time.FixedZone("UTC-7", -7*3600)
	now := time.Date(2026, 6, 10, 15, 30, 0, 0, loc)

	cases := []struct {
		window string
		want   time.Time
		ok     bool
	}{
		{"today", time.Date(2026, 6, 10, 0, 0, 0, 0, loc), true},
		{"week", time.Date(2026, 6, 8, 0, 0, 0, 0, loc), true}, // Monday
		{"month", time.Date(2026, 6, 1, 0, 0, 0, 0, loc), true},
		{"24h", now.Add(-24 * time.Hour), true},
		{"7d", now.AddDate(0, 0, -7), true},
		{"30d", now.AddDate(0, 0, -30), true},
		{"", time.Time{}, true},
		{"all", time.Time{}, true},
		{"yesterday", time.Time{}, false},
		{"99d", time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := WindowStart(c.window, now)
		if ok != c.ok {
			t.Errorf("WindowStart(%q) ok = %v, want %v", c.window, ok, c.ok)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("WindowStart(%q) = %v, want %v", c.window, got, c.want)
		}
	}
}

func TestWeekStart_SundayBelongsToPrecedingMonday(t *testing.T) {
	t.Parallel()
	// Sunday 2026-06-14 → ISO week starts Monday 2026-06-08.
	sunday := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	want := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	if got := WeekStart(sunday); !got.Equal(want) {
		t.Errorf("WeekStart(Sunday) = %v, want %v", got, want)
	}
}
