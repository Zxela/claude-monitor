package replay_test

import (
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
)

// msgWithTime constructs a minimal Event for use in tests.
func msgWithTime(t time.Time) parser.Event {
	return parser.Event{Timestamp: t}
}
