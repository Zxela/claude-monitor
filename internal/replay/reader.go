package replay

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
)

// Event is a single parsed JSONL line with its zero-based position in the file.
type Event struct {
	Index int `json:"index"`
	parser.ParsedMessage
}

// ReadFile reads all JSONL lines from path and returns them as Events in order.
// Malformed lines are silently skipped (same behaviour as the watcher).
func ReadFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	i := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		msg, err := parser.ParseLine(line)
		if err != nil {
			continue
		}
		events = append(events, Event{Index: i, ParsedMessage: *msg})
		i++
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "replay: scanner error reading %s: %v (some events may be missing)\n", path, err)
		// Return partial events with non-nil error so callers can skip caching incomplete results.
		return events, err
	}
	return events, nil
}

// IndexAt returns the index of the first event whose Timestamp is >= t.
// Returns len(events) if no such event exists.
func IndexAt(events []Event, t time.Time) int {
	return sort.Search(len(events), func(i int) bool {
		return !events[i].Timestamp.Before(t)
	})
}
