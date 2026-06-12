package store

import "time"

// WindowStart resolves a window token to its inclusive start time. Every
// stats endpoint accepts the union of two vocabularies with identical
// definitions everywhere, so the same token always means the same boundary:
//
//   - calendar tokens — "today" (local midnight), "week" (ISO-week Monday
//     00:00 local), "month" (1st of the month 00:00 local)
//   - rolling tokens — "24h", "7d", "30d" (relative to now)
//   - "" / "all" — lifetime (zero time)
//
// ok is false for unknown tokens. Callers compare against TEXT-stored UTC
// RFC3339 timestamps, so format the returned time with
// since.UTC().Format(time.RFC3339) (see AggregateStats).
func WindowStart(window string, now time.Time) (time.Time, bool) {
	switch window {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), true
	case "week":
		return WeekStart(now), true
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), true
	case "24h":
		return now.Add(-24 * time.Hour), true
	case "7d":
		return now.AddDate(0, 0, -7), true
	case "30d":
		return now.AddDate(0, 0, -30), true
	case "", "all":
		return time.Time{}, true
	}
	return time.Time{}, false
}

// WeekStart returns the start of the ISO week (Monday 00:00) in now's
// location (Issue 37).
func WeekStart(now time.Time) time.Time {
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return todayStart.AddDate(0, 0, -(weekday - 1))
}
