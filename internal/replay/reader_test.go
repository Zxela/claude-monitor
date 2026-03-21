// Note: msgWithTime is defined in helpers_test.go in the same package.
// parser is not imported here — msgWithTime returns parser.ParsedMessage but callers
// don't need the import since the embedded field name is used without package prefix.
package replay_test

import (
	"os"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/replay"
)

func TestReadFile_returnsEventsInOrder(t *testing.T) {
	// Two minimal JSONL lines with different timestamps.
	content := `{"type":"user","message":{"role":"user","content":"hello"},"timestamp":"2026-01-01T10:00:00Z","sessionId":"s1","uuid":"a"}
{"type":"assistant","message":{"role":"assistant","content":"world"},"costUSD":0.001,"usage":{"input_tokens":10,"output_tokens":5},"timestamp":"2026-01-01T10:00:05Z","sessionId":"s1","uuid":"b"}
`
	f, _ := os.CreateTemp(t.TempDir(), "*.jsonl")
	f.WriteString(content)
	f.Close()

	events, err := replay.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].Index != 0 {
		t.Errorf("want Index=0, got %d", events[0].Index)
	}
	if events[1].Index != 1 {
		t.Errorf("want Index=1, got %d", events[1].Index)
	}
	if events[0].Role != "user" {
		t.Errorf("want role=user, got %q", events[0].Role)
	}
}

func TestReadFile_skipsBlankLines(t *testing.T) {
	content := `{"type":"user","message":{"role":"user","content":"hi"},"timestamp":"2026-01-01T10:00:00Z","sessionId":"s1","uuid":"a"}

{"type":"assistant","message":{"role":"assistant","content":"there"},"timestamp":"2026-01-01T10:00:01Z","sessionId":"s1","uuid":"b"}
`
	f, _ := os.CreateTemp(t.TempDir(), "*.jsonl")
	f.WriteString(content)
	f.Close()

	events, err := replay.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
}

func TestIndexAt_returnsCorrectIndex(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Second)
	t2 := t0.Add(10 * time.Second)

	events := []replay.Event{
		{Index: 0, ParsedMessage: msgWithTime(t0)},
		{Index: 1, ParsedMessage: msgWithTime(t1)},
		{Index: 2, ParsedMessage: msgWithTime(t2)},
	}

	if i := replay.IndexAt(events, t1); i != 1 {
		t.Errorf("want 1, got %d", i)
	}
	// Seek before all events should return 0.
	if i := replay.IndexAt(events, t0.Add(-1*time.Second)); i != 0 {
		t.Errorf("want 0, got %d", i)
	}
	// Seek after all events should return len(events).
	if i := replay.IndexAt(events, t2.Add(time.Second)); i != 3 {
		t.Errorf("want 3, got %d", i)
	}
}
