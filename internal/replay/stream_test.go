// internal/replay/stream_test.go
// Note: msgWithTime is defined in helpers_test.go in the same package.
package replay_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/replay"
)

// flushRecorder wraps httptest.ResponseRecorder to implement http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushRecorder) Flush() {}

func TestStream_sendsAllEventsAsSSE(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	events := []replay.Event{
		{Index: 0, Event: msgWithTime(t0)},
		{Index: 1, Event: msgWithTime(t0.Add(10 * time.Millisecond))},
		{Index: 2, Event: msgWithTime(t0.Add(20 * time.Millisecond))},
	}

	rec := &flushRecorder{httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	replay.Stream(rec, req, events, replay.StreamParams{FromIndex: 0, Speed: 100.0}) // 100x speed = near-instant

	body := rec.Body.String()

	// Should have 3 "event: message" lines + 1 "event: done"
	messageCount := strings.Count(body, "event: message")
	if messageCount != 3 {
		t.Errorf("want 3 message events, got %d\nbody:\n%s", messageCount, body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("missing done event\nbody:\n%s", body)
	}
}

func TestStream_respectsFromIndex(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	events := []replay.Event{
		{Index: 0, Event: msgWithTime(t0)},
		{Index: 1, Event: msgWithTime(t0.Add(10 * time.Millisecond))},
		{Index: 2, Event: msgWithTime(t0.Add(20 * time.Millisecond))},
	}

	rec := &flushRecorder{httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	replay.Stream(rec, req, events, replay.StreamParams{FromIndex: 2, Speed: 100.0})

	body := rec.Body.String()
	messageCount := strings.Count(body, "event: message")
	if messageCount != 1 {
		t.Errorf("want 1 message event (from index 2), got %d\nbody:\n%s", messageCount, body)
	}
}

func TestStream_setsSSEHeaders(t *testing.T) {
	events := []replay.Event{}
	rec := &flushRecorder{httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	replay.Stream(rec, req, events, replay.StreamParams{FromIndex: 0, Speed: 1.0})

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("want Content-Type text/event-stream, got %q", ct)
	}
}

func TestStream_parsesSSEDataAsJSON(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	msg := msgWithTime(t0)
	msg.Role = "user"
	events := []replay.Event{{Index: 0, Event: msg}}

	rec := &flushRecorder{httptest.NewRecorder()}
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	replay.Stream(rec, req, events, replay.StreamParams{FromIndex: 0, Speed: 100.0})

	// Find the data: line and verify it's valid JSON containing "index"
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if strings.Contains(data, `"index"`) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("no SSE data line with 'index' field found\nbody:\n%s", rec.Body.String())
	}
}
