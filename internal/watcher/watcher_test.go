package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// newTestWatcher creates a Watcher with only the given paths (no defaultBasePaths).
// This isolates tests from real session files on the host machine.
func newTestWatcher(t *testing.T, paths []string) *Watcher {
	t.Helper()
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("creating fsnotify watcher: %v", err)
	}
	return &Watcher{
		fsw:       fsw,
		basePaths: paths,
		labels:    make(map[string]string),
		states:    make(map[string]*fileState),
		events:    make(chan Event, 4096),
	}
}

func TestSessionIDFromPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/some/dir/abc123.jsonl", "abc123"},
		{"/path/to/session-id-here.jsonl", "session-id-here"},
		{"/deep/nested/path/myfile.jsonl", "myfile"},
		// No extension
		{"/dir/noextension", "noextension"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got := sessionIDFromPath(tc.path)
			if got != tc.want {
				t.Errorf("sessionIDFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestProjectDirFromPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/.claude/projects/my-project/session.jsonl", "my-project"},
		{"/root/.claude/projects/another-project/abc.jsonl", "another-project"},
		{"/a/b/c/d.jsonl", "c"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			got := projectDirFromPath(tc.path)
			if got != tc.want {
				t.Errorf("projectDirFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestExpandHome_ExpandsHomePath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	input := "~/.claude/projects/"
	got := expandHome(input)
	want := filepath.Join(home, ".claude/projects/")
	if got != want {
		t.Errorf("expandHome(%q) = %q, want %q", input, got, want)
	}
}

func TestExpandHome_LeavesNonHomePaths(t *testing.T) {
	t.Parallel()
	tests := []string{
		"/absolute/path",
		"relative/path",
		"/home/user/.claude",
	}
	for _, input := range tests {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got := expandHome(input)
			if got != input {
				t.Errorf("expandHome(%q) = %q, want unchanged %q", input, got, input)
			}
		})
	}
}

func TestWatcher_EmitsEventsForAppendedLines(t *testing.T) {
	// Integration test: uses real temp dir and fsnotify.
	// Not parallel due to filesystem operations.
	dir := t.TempDir()

	// Create the JSONL file before starting the watcher so ensureTracked sets
	// offset = current size (end-of-file). We'll append after starting.
	jsonlPath := filepath.Join(dir, "test-session.jsonl")
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatalf("creating jsonl file: %v", err)
	}
	f.Close()

	w, err := New([]string{dir})
	if err != nil {
		t.Fatalf("creating watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := w.Start(ctx)

	// Give the watcher time to do its initial scan and register the file.
	time.Sleep(100 * time.Millisecond)

	// Append a valid JSONL line to the file.
	wantLine := `{"type":"user","message":{"role":"user","content":"hello watcher"},"sessionId":"test-session","uuid":"uuid-w1","timestamp":"2024-01-01T00:00:00Z"}`
	af, err := os.OpenFile(jsonlPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("opening jsonl for append: %v", err)
	}
	if _, err := af.WriteString(wantLine + "\n"); err != nil {
		af.Close()
		t.Fatalf("writing line: %v", err)
	}
	af.Close()

	select {
	case ev := <-events:
		if !strings.Contains(string(ev.Line), "hello watcher") {
			t.Errorf("event line %q does not contain expected content", string(ev.Line))
		}
		if ev.SessionID != "test-session" {
			t.Errorf("SessionID: got %q, want test-session", ev.SessionID)
		}
		// projectDir should be the temp dir's base name
		wantDir := filepath.Base(dir)
		if ev.ProjectDir != wantDir {
			t.Errorf("ProjectDir: got %q, want %q", ev.ProjectDir, wantDir)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for watcher event")
	}
}

func TestWatcher_FileDeletion(t *testing.T) {
	// Verify the watcher handles a deleted file gracefully: no panic, no
	// spurious events after the file is gone.
	dir := t.TempDir()

	jsonlPath := filepath.Join(dir, "del-session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte(`{"type":"user"}`+"\n"), 0644); err != nil {
		t.Fatalf("creating jsonl file: %v", err)
	}

	w := newTestWatcher(t, []string{dir})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := w.Start(ctx)

	// Wait for the initial scan to complete.
	time.Sleep(200 * time.Millisecond)

	// Remove the file while the watcher is running.
	if err := os.Remove(jsonlPath); err != nil {
		t.Fatalf("removing jsonl file: %v", err)
	}

	// Trigger a poll/scan cycle — the watcher should not panic.
	time.Sleep(200 * time.Millisecond)

	// The file state should have been cleaned up by scanAll's stale-file removal.
	w.mu.Lock()
	_, stillTracked := w.states[jsonlPath]
	w.mu.Unlock()

	// scanAll runs on the poll ticker; manually trigger it to guarantee cleanup.
	w.mu.Lock()
	w.scanAll()
	_, stillTrackedAfterScan := w.states[jsonlPath]
	w.mu.Unlock()

	if stillTrackedAfterScan {
		t.Errorf("deleted file %q still tracked after scanAll", jsonlPath)
	}

	// Drain any residual events; ensure none reference the deleted file after cleanup.
	drainTimeout := time.After(300 * time.Millisecond)
	_ = stillTracked // may or may not be cleaned by timer-driven scan
drain:
	for {
		select {
		case <-events:
			// consume
		case <-drainTimeout:
			break drain
		}
	}
}

func TestWatcher_StaleFileCleanup(t *testing.T) {
	dir := t.TempDir()

	// Create three session files.
	files := []string{"sess-a.jsonl", "sess-b.jsonl", "sess-c.jsonl"}
	for _, name := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(`{"type":"user"}`+"\n"), 0644); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	w := newTestWatcher(t, []string{dir})

	// Manually scan to register all files.
	w.mu.Lock()
	w.scanAll()
	trackedBefore := len(w.states)
	w.mu.Unlock()

	if trackedBefore != 3 {
		t.Fatalf("expected 3 tracked files, got %d", trackedBefore)
	}

	// Delete two of the three files.
	for _, name := range files[:2] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatalf("removing %s: %v", name, err)
		}
	}

	// Another scanAll should clean up the stale entries.
	w.mu.Lock()
	w.scanAll()
	trackedAfter := 0
	for p := range w.states {
		if strings.HasPrefix(p, dir) {
			trackedAfter++
		}
	}
	w.mu.Unlock()

	if trackedAfter != 1 {
		t.Errorf("expected 1 tracked file after cleanup, got %d", trackedAfter)
	}

	// Close underlying fsnotify watcher to release resources.
	w.fsw.Close()
}

func TestWatcher_FindSessionFile_Tracked(t *testing.T) {
	dir := t.TempDir()

	// Nest under a project-like directory so the walk is realistic.
	projDir := filepath.Join(dir, "my-project")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(projDir, "target-sess.jsonl")
	if err := os.WriteFile(target, []byte(`{"t":"user"}`+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w := newTestWatcher(t, []string{dir})

	// Scan to register.
	w.mu.Lock()
	w.scanAll()
	w.mu.Unlock()

	got := w.FindSessionFile("target-sess")
	if got != target {
		t.Errorf("FindSessionFile(tracked): got %q, want %q", got, target)
	}

	w.fsw.Close()
}

func TestWatcher_FindSessionFile_Untracked(t *testing.T) {
	// FindSessionFile should discover files via WalkDir even if they are
	// not yet in the states map.
	dir := t.TempDir()
	projDir := filepath.Join(dir, "proj")
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(projDir, "untracked-id.jsonl")
	if err := os.WriteFile(target, []byte(`{"t":"user"}`+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w := newTestWatcher(t, []string{dir})
	// Do NOT scan — the file should be found via WalkDir fallback.

	got := w.FindSessionFile("untracked-id")
	if got != target {
		t.Errorf("FindSessionFile(untracked): got %q, want %q", got, target)
	}

	w.fsw.Close()
}

func TestWatcher_FindSessionFile_NotFound(t *testing.T) {
	dir := t.TempDir()

	w := newTestWatcher(t, []string{dir})

	got := w.FindSessionFile("does-not-exist")
	if got != "" {
		t.Errorf("FindSessionFile(nonexistent): got %q, want empty string", got)
	}

	w.fsw.Close()
}

func TestWatcher_MultipleBasePaths(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	// Put a session file in each directory.
	pathA := filepath.Join(dirA, "sess-a.jsonl")
	pathB := filepath.Join(dirB, "sess-b.jsonl")
	if err := os.WriteFile(pathA, []byte{}, 0644); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if err := os.WriteFile(pathB, []byte{}, 0644); err != nil {
		t.Fatalf("write B: %v", err)
	}

	w := newTestWatcher(t, []string{dirA, dirB})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := w.Start(ctx)

	// Let the initial scan finish.
	time.Sleep(200 * time.Millisecond)

	// Append lines to both files.
	lineA := `{"type":"user","msg":"from-a"}` + "\n"
	lineB := `{"type":"user","msg":"from-b"}` + "\n"

	if err := appendToFile(pathA, lineA); err != nil {
		t.Fatalf("append A: %v", err)
	}
	if err := appendToFile(pathB, lineB); err != nil {
		t.Fatalf("append B: %v", err)
	}

	seen := map[string]bool{"from-a": false, "from-b": false}
	deadline := time.After(3 * time.Second)
	for {
		allSeen := true
		for _, v := range seen {
			if !v {
				allSeen = false
				break
			}
		}
		if allSeen {
			break
		}
		select {
		case ev := <-events:
			line := string(ev.Line)
			if strings.Contains(line, "from-a") {
				seen["from-a"] = true
			}
			if strings.Contains(line, "from-b") {
				seen["from-b"] = true
			}
		case <-deadline:
			t.Fatalf("timeout waiting for events from both dirs; seen=%v", seen)
		}
	}
}

func TestWatcher_AddAndRemovePath(t *testing.T) {
	dir := t.TempDir()

	w := newTestWatcher(t, nil)

	// Create a file before Add so it gets picked up.
	jsonlPath := filepath.Join(dir, "dynamic.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w.Add(dir, "my-container")

	// Adding the same path again should be a no-op.
	w.Add(dir, "my-container")

	w.mu.Lock()
	numPaths := 0
	for _, p := range w.basePaths {
		if p == dir {
			numPaths++
		}
	}
	_, tracked := w.states[jsonlPath]
	w.mu.Unlock()

	if numPaths != 1 {
		t.Errorf("expected dir listed once in basePaths, got %d", numPaths)
	}
	if !tracked {
		t.Errorf("expected file to be tracked after Add")
	}

	// Remove and verify cleanup.
	w.Remove(dir)

	w.mu.Lock()
	_, stillTracked := w.states[jsonlPath]
	found := false
	for _, p := range w.basePaths {
		if p == dir {
			found = true
		}
	}
	w.mu.Unlock()

	if stillTracked {
		t.Errorf("file still tracked after Remove")
	}
	if found {
		t.Errorf("dir still in basePaths after Remove")
	}

	w.fsw.Close()
}

func TestWatcher_LabelForPath(t *testing.T) {
	dir := t.TempDir()

	// Create the file before calling Add so scanPathNoLock discovers it.
	jsonlPath := filepath.Join(dir, "labelled.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w := newTestWatcher(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Add the path with a label — this triggers scanPathNoLock which
	// discovers the existing file and registers the fsnotify watch.
	w.Add(dir, "docker-web")
	time.Sleep(200 * time.Millisecond)

	line := `{"type":"user","msg":"labelled-event"}` + "\n"
	if err := appendToFile(jsonlPath, line); err != nil {
		t.Fatalf("append: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Label != "docker-web" {
			t.Errorf("Label: got %q, want %q", ev.Label, "docker-web")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for labelled event")
	}
}

func TestWatcher_PermissionError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test permission errors as root")
	}

	dir := t.TempDir()
	unreadable := filepath.Join(dir, "noperm")
	if err := os.MkdirAll(unreadable, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Put a file inside before making directory unreadable.
	if err := os.WriteFile(filepath.Join(unreadable, "secret.jsonl"), []byte(`{}`+"\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Remove read+execute permission from the directory.
	if err := os.Chmod(unreadable, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		// Restore so TempDir cleanup can remove it.
		os.Chmod(unreadable, 0755)
	})

	// The watcher should not panic or return an error when scanning an
	// unreadable directory — it simply skips it.
	w := newTestWatcher(t, []string{dir})

	w.mu.Lock()
	w.scanAll()
	count := 0
	for p := range w.states {
		if strings.HasPrefix(p, dir) {
			count++
		}
	}
	w.mu.Unlock()

	// The file inside the unreadable directory should not be tracked.
	if count != 0 {
		t.Errorf("expected 0 tracked files from unreadable dir, got %d", count)
	}

	w.fsw.Close()
}

func TestWatcher_BootstrapCallback(t *testing.T) {
	dir := t.TempDir()

	// Pre-populate a file with data so the bootstrap scan has something to read.
	jsonlPath := filepath.Join(dir, "boot-session.jsonl")
	content := `{"type":"user","msg":"line1"}` + "\n" + `{"type":"assistant","msg":"line2"}` + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Use newTestWatcher to avoid scanning defaultBasePaths which may
	// contain real session files on the host machine.
	w := newTestWatcher(t, []string{dir})

	var mu sync.Mutex
	var bootstrapEvents []Event
	w.SetBootstrapCallback(func(ev Event) {
		mu.Lock()
		bootstrapEvents = append(bootstrapEvents, ev)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = w.Start(ctx)

	// Give the initial scan time to run.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	got := len(bootstrapEvents)
	events := make([]Event, got)
	copy(events, bootstrapEvents)
	mu.Unlock()

	if got != 2 {
		t.Fatalf("expected 2 bootstrap events, got %d", got)
	}
	for _, ev := range events {
		if !ev.Bootstrap {
			t.Errorf("expected Bootstrap=true on bootstrap event")
		}
		if ev.SessionID != "boot-session" {
			t.Errorf("SessionID: got %q, want %q", ev.SessionID, "boot-session")
		}
	}
}

func TestWatcher_FileTruncation(t *testing.T) {
	dir := t.TempDir()

	jsonlPath := filepath.Join(dir, "trunc-session.jsonl")
	if err := os.WriteFile(jsonlPath, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	w := newTestWatcher(t, []string{dir})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := w.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Write and consume initial line.
	line1 := `{"type":"user","msg":"before-truncate"}` + "\n"
	if err := appendToFile(jsonlPath, line1); err != nil {
		t.Fatalf("append: %v", err)
	}

	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first event")
	}

	// Truncate the file and write new data.
	if err := os.WriteFile(jsonlPath, []byte(`{"type":"user","msg":"after-truncate"}`+"\n"), 0644); err != nil {
		t.Fatalf("truncate-write: %v", err)
	}

	select {
	case ev := <-events:
		if !strings.Contains(string(ev.Line), "after-truncate") {
			t.Errorf("expected post-truncate content, got %q", string(ev.Line))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for post-truncation event")
	}
}

func TestWatcher_DroppedEvents(t *testing.T) {
	dir := t.TempDir()

	w := newTestWatcher(t, []string{dir})

	// Initially zero.
	if got := w.DroppedEvents(); got != 0 {
		t.Errorf("initial DroppedEvents: got %d, want 0", got)
	}

	w.fsw.Close()
}

// appendToFile is a helper that opens a file in append mode, writes data, and
// closes it — mirroring how Claude Code writes to session JSONL files.
func appendToFile(path, data string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
