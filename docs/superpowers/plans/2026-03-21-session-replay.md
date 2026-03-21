# Session Replay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add session replay to claude-monitor — click a session card, then scrub/play/pause through its full message history with speed control.

**Architecture:** A new `internal/replay` package reads historical JSONL files and streams events over Server-Sent Events (SSE) with inter-event timing scaled by speed. A separate manifest endpoint returns all event metadata for the scrubber. The frontend adds a replay panel that replaces the live feed panel when active.

**Tech Stack:** Go 1.22 stdlib (`bufio`, `net/http`, `encoding/json`), SSE (`http.Flusher`), vanilla JS `EventSource`, `input[type=range]` scrubber.

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/replay/reader.go` | Create | Read all JSONL lines from a session file; binary-search by timestamp |
| `internal/replay/helpers_test.go` | Create | Shared test helpers (`msgWithTime`) for the `replay_test` package |
| `internal/replay/reader_test.go` | Create | Tests for ReadFile and IndexAt |
| `internal/replay/stream.go` | Create | SSE streaming with inter-event timing, speed control, disconnect handling |
| `internal/replay/stream_test.go` | Create | Tests for Stream output format and event count |
| `cmd/claude-monitor/main.go` | Modify | Add two new routes: manifest `GET /api/sessions/{id}/replay` and SSE `GET /api/sessions/{id}/replay/stream` |
| `static/index.html` | Modify | Add replay panel HTML/CSS; replay button on session cards; JS player logic |

---

## Task 1: replay.ReadFile — read JSONL into events

**Files:**
- Create: `internal/replay/reader.go`
- Create: `internal/replay/reader_test.go`

The parser package already parses individual lines. This package just reads all of them from a file.

- [ ] **Step 1: Create the shared test helper file**

```go
// internal/replay/helpers_test.go
package replay_test

import (
	"time"

	"github.com/zxela-claude/claude-monitor/internal/parser"
)

// msgWithTime constructs a minimal ParsedMessage for use in tests.
func msgWithTime(t time.Time) parser.ParsedMessage {
	return parser.ParsedMessage{Timestamp: t}
}
```

- [ ] **Step 2: Write the failing test**

```go
// internal/replay/reader_test.go
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

func msgWithTime(t time.Time) parser.ParsedMessage {
	return parser.ParsedMessage{Timestamp: t}
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
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd /workspace/group/claude-monitor
go test ./internal/replay/... -v
```

Expected: compile error — package `replay` doesn't exist yet.

- [ ] **Step 4: Write the implementation**

```go
// internal/replay/reader.go
package replay

import (
	"bufio"
	"bytes"
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
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
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
	return events, scanner.Err()
}

// IndexAt returns the index of the first event whose Timestamp is >= t.
// Returns len(events) if no such event exists.
func IndexAt(events []Event, t time.Time) int {
	return sort.Search(len(events), func(i int) bool {
		return !events[i].Timestamp.Before(t)
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /workspace/group/claude-monitor
go test ./internal/replay/... -v -run TestReadFile
go test ./internal/replay/... -v -run TestIndexAt
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
cd /workspace/group/claude-monitor
git add internal/replay/helpers_test.go internal/replay/reader.go internal/replay/reader_test.go
git commit -m "feat(replay): add ReadFile and IndexAt helpers"
```

---

## Task 2: replay.Stream — SSE streaming with timing

**Files:**
- Create: `internal/replay/stream.go`
- Create: `internal/replay/stream_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
		{Index: 0, ParsedMessage: msgWithTime(t0)},
		{Index: 1, ParsedMessage: msgWithTime(t0.Add(10 * time.Millisecond))},
		{Index: 2, ParsedMessage: msgWithTime(t0.Add(20 * time.Millisecond))},
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
		{Index: 0, ParsedMessage: msgWithTime(t0)},
		{Index: 1, ParsedMessage: msgWithTime(t0.Add(10 * time.Millisecond))},
		{Index: 2, ParsedMessage: msgWithTime(t0.Add(20 * time.Millisecond))},
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
	events := []replay.Event{{Index: 0, ParsedMessage: msg}}

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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /workspace/group/claude-monitor
go test ./internal/replay/... -v -run TestStream
```

Expected: compile error — `Stream` and `StreamParams` not defined.

- [ ] **Step 3: Write the implementation**

```go
// internal/replay/stream.go
package replay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// StreamParams controls playback behaviour.
type StreamParams struct {
	FromIndex int
	Speed     float64 // 1.0 = real-time; 2.0 = double speed; 0 defaults to 1.0
}

// maxDelay is the longest we'll sleep between events regardless of actual gap.
const maxDelay = 5 * time.Second

// Stream writes events[params.FromIndex:] as SSE to w with inter-event timing
// scaled by params.Speed. It returns when all events are sent, the client
// disconnects (r.Context() cancelled), or an error occurs.
func Stream(w http.ResponseWriter, r *http.Request, events []Event, params StreamParams) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	speed := params.Speed
	if speed <= 0 {
		speed = 1.0
	}

	from := params.FromIndex
	if from < 0 {
		from = 0
	}
	if from > len(events) {
		from = len(events)
	}

	slice := events[from:]
	ctx := r.Context()

	for i, ev := range slice {
		// Respect client disconnect.
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Sleep for scaled gap between this event and the previous one.
		if i > 0 {
			prev := slice[i-1]
			if !prev.Timestamp.IsZero() && !ev.Timestamp.IsZero() {
				gap := ev.Timestamp.Sub(prev.Timestamp)
				if gap > 0 {
					delay := time.Duration(float64(gap) / speed)
					if delay > maxDelay {
						delay = maxDelay
					}
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}
		}

		data, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: {\"total\":%d}\n\n", len(events))
	flusher.Flush()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /workspace/group/claude-monitor
go test ./internal/replay/... -v
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /workspace/group/claude-monitor
git add internal/replay/stream.go internal/replay/stream_test.go
git commit -m "feat(replay): add SSE Stream with speed control and disconnect handling"
```

> Note: `helpers_test.go` was already staged in the Task 1 commit.

---

## Task 3: Wire replay routes into main.go

**Files:**
- Modify: `cmd/claude-monitor/main.go`

Two new routes:
- `GET /api/sessions/{id}/replay` — manifest (all events with index/timestamp/type for the scrubber)
- `GET /api/sessions/{id}/replay/stream` — SSE stream

Note: register the `/stream` sub-route **before** the parent `/replay` route. Go 1.22 mux picks the most specific match, but explicit ordering avoids confusion.

- [ ] **Step 1: Add the replay import and two route handlers**

In `cmd/claude-monitor/main.go`, add `"strconv"` and `"github.com/zxela-claude/claude-monitor/internal/replay"` to the import block. Then add these two handlers immediately after the `/api/sessions` handler:

```go
// Replay manifest — returns all events with timestamps for the scrubber.
mux.HandleFunc("/api/sessions/{id}/replay/stream", func(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    sess, ok := store.Get(id)
    if !ok {
        http.NotFound(w, r)
        return
    }
    if sess.FilePath == "" {
        http.Error(w, "session file not available", http.StatusBadRequest)
        return
    }
    events, err := replay.ReadFile(sess.FilePath)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    from, _ := strconv.Atoi(r.URL.Query().Get("from"))
    speed, _ := strconv.ParseFloat(r.URL.Query().Get("speed"), 64)
    replay.Stream(w, r, events, replay.StreamParams{FromIndex: from, Speed: speed})
})

mux.HandleFunc("/api/sessions/{id}/replay", func(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    sess, ok := store.Get(id)
    if !ok {
        http.NotFound(w, r)
        return
    }
    if sess.FilePath == "" {
        http.Error(w, "session file not available", http.StatusBadRequest)
        return
    }
    events, err := replay.ReadFile(sess.FilePath)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    type manifestEvent struct {
        Index       int       `json:"index"`
        Timestamp   time.Time `json:"timestamp"`
        Type        string    `json:"type"`
        Role        string    `json:"role"`
        ContentText string    `json:"contentText"`
        ToolName    string    `json:"toolName,omitempty"`
        CostUSD     float64   `json:"costUSD"`
    }
    out := make([]manifestEvent, len(events))
    for i, e := range events {
        out[i] = manifestEvent{
            Index:       e.Index,
            Timestamp:   e.Timestamp,
            Type:        e.Type,
            Role:        e.Role,
            ContentText: e.ContentText,
            ToolName:    e.ToolName,
            CostUSD:     e.CostUSD,
        }
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "sessionId": id,
        "events":    out,
    })
})
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /workspace/group/claude-monitor
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Smoke-test the routes manually**

Start the server pointing at a directory with JSONL files. If you don't have a live Claude session, create a fixture:

```bash
mkdir -p /tmp/test-sessions/test-project
cat > /tmp/test-sessions/test-project/abc123.jsonl << 'EOF'
{"type":"user","message":{"role":"user","content":"hello"},"timestamp":"2026-01-01T10:00:00Z","sessionId":"abc123","uuid":"a"}
{"type":"assistant","message":{"role":"assistant","content":"world"},"costUSD":0.001,"usage":{"input_tokens":10,"output_tokens":5},"timestamp":"2026-01-01T10:00:05Z","sessionId":"abc123","uuid":"b"}
EOF

cd /workspace/group/claude-monitor
go run ./cmd/claude-monitor --watch /tmp/test-sessions &
sleep 1

# Manifest
curl -s http://localhost:7700/api/sessions/abc123/replay | jq .

# SSE stream (exits after both events + done)
curl -N http://localhost:7700/api/sessions/abc123/replay/stream?speed=100
```

Expected: manifest returns `{"sessionId":"abc123","events":[...]}` with 2 events. SSE stream prints two `event: message` blocks then `event: done`.

- [ ] **Step 4: Kill the test server and commit**

```bash
kill %1  # or pkill claude-monitor

cd /workspace/group/claude-monitor
git add cmd/claude-monitor/main.go
git commit -m "feat(replay): add manifest and SSE stream routes to HTTP server"
```

---

## Task 4: Frontend — replay panel and player

**Files:**
- Modify: `static/index.html`

This is the largest task. Work in three sub-steps: HTML/CSS, then session card button, then JS logic.

### 4a — HTML structure and CSS

- [ ] **Step 1: Add replay panel HTML** — insert immediately before `</div> <!-- end #main -->` (line 546):

```html
    <!-- Replay Panel (hidden by default, replaces feed panel when active) -->
    <div id="replay-panel" style="display:none; flex-direction:column; flex:1; overflow:hidden; border-left:1px solid var(--border);">
      <div class="panel-header" style="display:flex; justify-content:space-between; align-items:center;">
        <span>⏵ REPLAY — <span id="replay-session-label" style="color:var(--cyan)">—</span></span>
        <button id="replay-close-btn" onclick="closeReplay()" style="background:none;border:1px solid var(--border2);color:var(--text-dim);font-family:var(--font);font-size:10px;padding:2px 8px;cursor:pointer;letter-spacing:1px;">✕ CLOSE</button>
      </div>
      <div id="replay-feed" style="flex:1;overflow-y:auto;padding:6px 10px;font-size:12px;line-height:1.5;">
        <div id="replay-empty" style="color:var(--text-dim);padding:20px;text-align:center;">PRESS PLAY TO BEGIN</div>
      </div>
      <div id="replay-controls" style="display:flex;align-items:center;gap:8px;padding:8px 12px;border-top:1px solid var(--border);background:var(--bg2);flex-shrink:0;">
        <button id="replay-restart-btn" onclick="replayRestart()" title="Restart" style="background:none;border:1px solid var(--border2);color:var(--text-dim);font-family:var(--font);font-size:11px;padding:3px 8px;cursor:pointer;">⏮</button>
        <button id="replay-play-btn" onclick="replayToggle()" style="background:rgba(0,255,136,0.1);border:1px solid var(--green);color:var(--green);font-family:var(--font);font-size:11px;padding:3px 12px;cursor:pointer;letter-spacing:1px;">▶ PLAY</button>
        <select id="replay-speed" style="background:var(--bg3);border:1px solid var(--border2);color:var(--text);font-family:var(--font);font-size:11px;padding:2px 4px;">
          <option value="0.5">0.5×</option>
          <option value="1" selected>1×</option>
          <option value="2">2×</option>
          <option value="10">10×</option>
        </select>
        <input type="range" id="replay-scrubber" min="0" max="0" value="0" style="flex:1;accent-color:var(--blue);">
        <span id="replay-progress" style="color:var(--text-dim);font-size:11px;white-space:nowrap;min-width:60px;text-align:right;">0 / 0</span>
      </div>
    </div>
```

- [ ] **Step 2: Verify it compiles and the panel is invisible**

```bash
cd /workspace/group/claude-monitor
go build ./...
```

Open http://localhost:7700 — no visible change (panel is `display:none`).

### 4b — Add replay button to session cards

- [ ] **Step 3: Add replay button to session card HTML** — find the card `html` template string inside `renderSessions()` (~line 695). Add a replay button inside the card:

Find this closing line in the card template (it's a button or the last `</div>` of the card):
```js
      <div class="session-meta">
```

Add a replay button immediately after the `session-name` div inside the card template. Look for where `sess.id` is rendered for the card and add:
```js
      <button class="replay-btn" onclick="event.stopPropagation(); openReplay('${escHtml(sess.id)}')" title="Replay session">⏵</button>
```

Also add the CSS for `.replay-btn` in the `<style>` block:
```css
  .replay-btn {
    background: none;
    border: 1px solid var(--border2);
    color: var(--text-dim);
    font-family: var(--font);
    font-size: 10px;
    padding: 2px 6px;
    cursor: pointer;
    letter-spacing: 1px;
    margin-top: 4px;
  }
  .replay-btn:hover {
    border-color: var(--blue);
    color: var(--blue);
  }
```

### 4c — JavaScript player logic

- [ ] **Step 4: Add replay state and functions to the `<script>` block**

Add this block to the `const state = { ... }` object (after `ws: null,`):

```js
  // Replay state
  replay: {
    sessionId: null,
    events: [],      // manifest events (all, for scrubber)
    currentIndex: 0, // index of last-received event + 1
    es: null,        // EventSource for active stream
    playing: false,
  },
```

Then add these functions before the `connect()` function:

```js
// ── Replay Player ──────────────────────────────────────────────

async function openReplay(sessionId) {
  closeReplayStream();
  state.replay.sessionId = sessionId;
  state.replay.currentIndex = 0;
  state.replay.events = [];

  // Show replay panel, hide live feed.
  document.getElementById('replay-panel').style.display = 'flex';
  document.getElementById('feed-panel').style.display = 'none';

  // Label
  const sess = state.sessions[sessionId];
  const label = sess ? (sess.projectName || sessionId.slice(0, 8)) : sessionId.slice(0, 8);
  document.getElementById('replay-session-label').textContent = label;

  // Fetch manifest.
  try {
    const resp = await fetch(`/api/sessions/${sessionId}/replay`);
    if (!resp.ok) throw new Error(resp.statusText);
    const data = await resp.json();
    state.replay.events = data.events || [];
  } catch (e) {
    document.getElementById('replay-empty').textContent = 'Failed to load session: ' + e.message;
    return;
  }

  const total = state.replay.events.length;
  const scrubber = document.getElementById('replay-scrubber');
  scrubber.max = Math.max(0, total - 1);
  scrubber.value = 0;
  document.getElementById('replay-progress').textContent = `0 / ${total}`;
  document.getElementById('replay-empty').textContent = 'PRESS PLAY TO BEGIN';
}

function closeReplay() {
  closeReplayStream();
  document.getElementById('replay-panel').style.display = 'none';
  document.getElementById('feed-panel').style.display = '';
  state.replay.sessionId = null;
}

function closeReplayStream() {
  if (state.replay.es) {
    state.replay.es.close();
    state.replay.es = null;
  }
  state.replay.playing = false;
  updateReplayPlayBtn();
}

function replayToggle() {
  if (state.replay.playing) {
    closeReplayStream();
  } else {
    startReplayStream(state.replay.currentIndex);
  }
}

function replayRestart() {
  closeReplayStream();
  state.replay.currentIndex = 0;
  document.getElementById('replay-scrubber').value = 0;
  document.getElementById('replay-progress').textContent = `0 / ${state.replay.events.length}`;
  // Clear feed
  const feed = document.getElementById('replay-feed');
  feed.innerHTML = '';
  startReplayStream(0);
}

function startReplayStream(fromIndex) {
  if (!state.replay.sessionId) return;
  closeReplayStream();

  const speed = parseFloat(document.getElementById('replay-speed').value) || 1;
  state.replay.playing = true;
  updateReplayPlayBtn();

  const url = `/api/sessions/${state.replay.sessionId}/replay/stream?from=${fromIndex}&speed=${speed}`;
  const es = new EventSource(url);
  state.replay.es = es;

  es.addEventListener('message', (e) => {
    let msg;
    try { msg = JSON.parse(e.data); } catch { return; }

    state.replay.currentIndex = msg.index + 1;
    document.getElementById('replay-scrubber').value = msg.index;
    document.getElementById('replay-progress').textContent = `${msg.index + 1} / ${state.replay.events.length}`;
    addReplayEntry(msg);
  });

  es.addEventListener('done', () => {
    closeReplayStream();
  });

  es.onerror = () => {
    closeReplayStream();
  };
}

function updateReplayPlayBtn() {
  const btn = document.getElementById('replay-play-btn');
  if (btn) btn.textContent = state.replay.playing ? '⏸ PAUSE' : '▶ PLAY';
}

function addReplayEntry(msg) {
  const feed = document.getElementById('replay-feed');
  const empty = feed.querySelector('#replay-empty');
  if (empty) empty.remove();

  const type = msg.role || msg.type || 'system';
  const ts   = msg.timestamp ? new Date(msg.timestamp).toTimeString().slice(0,8) : '—';
  const toolName = msg.toolName;

  let typeColor = 'var(--text-dim)';
  let typeLabel = type.toUpperCase();
  if (type === 'user')      { typeColor = 'var(--blue)';   typeLabel = 'USER'; }
  if (type === 'assistant') { typeColor = 'var(--green)';  typeLabel = 'ASST'; }
  if (toolName)             { typeColor = 'var(--yellow)'; typeLabel = 'TOOL'; }

  const cost = msg.costUSD ? ` <span style="color:var(--text-dim)">${'$' + msg.costUSD.toFixed(4)}</span>` : '';
  const preview = escHtml(msg.contentText || (toolName ? `[${toolName}]` : ''));

  const el = document.createElement('div');
  el.className = 'feed-entry';
  el.style.borderLeft = `2px solid ${typeColor}`;
  el.innerHTML = `<span style="color:var(--text-dim)">${ts}</span> <span style="color:${typeColor}">${typeLabel}</span>${cost} <span style="color:var(--text)">${preview}</span>`;
  feed.appendChild(el);

  // Auto-scroll replay feed.
  feed.scrollTop = feed.scrollHeight;
}

// Scrubber seek — fires on mouseup/touchend (change event).
document.getElementById('replay-scrubber').addEventListener('change', (e) => {
  if (!state.replay.sessionId) return;
  const idx = parseInt(e.target.value);
  closeReplayStream();  // always close before starting a new stream
  state.replay.currentIndex = idx;
  // Clear feed and restart from seek point.
  document.getElementById('replay-feed').innerHTML = '';
  startReplayStream(idx);
});
```

- [ ] **Step 5: Build and manual end-to-end test**

```bash
cd /workspace/group/claude-monitor
go build ./...

# Start with fixture session from Task 3
go run ./cmd/claude-monitor --watch /tmp/test-sessions &
sleep 1
```

Open http://localhost:7700:
1. Session card for `abc123` should appear with a `⏵` button
2. Click `⏵` — replay panel appears, live feed disappears
3. Click `▶ PLAY` — two events stream in, `PAUSE` button appears, progress shows `2 / 2`
4. Click `✕ CLOSE` — live feed returns

- [ ] **Step 6: Kill server and commit**

```bash
kill %1

cd /workspace/group/claude-monitor
git add static/index.html
git commit -m "feat(replay): add session replay player UI with scrubber, play/pause, speed control"
```

---

## Task 5: Update roadmap

**Files:**
- Modify: `knowledge/projects/claude-monitor/ROADMAP.md` (in `/workspace/group/knowledge/`)

- [ ] **Step 1: Mark session replay done in roadmap**

Change `- [ ] **Session replay**` to `- [x] **Session replay**` and add a "done" note.

- [ ] **Step 2: Commit the knowledge update**

```bash
cd /workspace/group/knowledge
git add projects/claude-monitor/ROADMAP.md
git commit -m "Update claude-monitor roadmap: session replay complete"
```

---

## Quick Reference

| Endpoint | Description |
|----------|-------------|
| `GET /api/sessions/{id}/replay` | JSON manifest of all events (for scrubber) |
| `GET /api/sessions/{id}/replay/stream?from=N&speed=F` | SSE stream from event index N at speed F |

**Speed examples:** `speed=1` = real-time, `speed=2` = 2× faster, `speed=10` = 10× faster, `speed=100` = near-instant (good for tests).

**Test commands:**
```bash
go test ./internal/replay/... -v          # all replay tests
go build ./...                            # full compile check
```
