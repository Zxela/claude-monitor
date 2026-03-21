package watcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
