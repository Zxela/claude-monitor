package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestClient creates a Client wired to a local httptest.Server via Unix socket.
// The caller supplies a mux to define response handlers.
func newTestClient(t *testing.T, mux *http.ServeMux) (*Client, func()) {
	t.Helper()

	// Use a temp Unix socket so we don't collide with other tests.
	sockPath := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	client := NewClient(sockPath)

	cleanup := func() {
		srv.Close()
		ln.Close()
	}
	return client, cleanup
}

// ---------- FindClaudePaths tests ----------

func TestFindClaudePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		responseCode int
		responseBody string
		wantPaths    []MountedPath
		wantErr      bool
	}{
		{
			name:         "empty container list",
			responseCode: http.StatusOK,
			responseBody: `[]`,
			wantPaths:    nil,
			wantErr:      false,
		},
		{
			name:         "container with .claude/projects mount",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": ["/agent-1"],
				"Mounts": [{
					"Type": "bind",
					"Source": "/var/lib/docker/volumes/xyz/workspace/.claude/projects",
					"Destination": "/home/user/.claude/projects"
				}]
			}]`,
			wantPaths: []MountedPath{
				{ContainerName: "agent-1", HostPath: "/var/lib/docker/volumes/xyz/workspace/.claude/projects"},
			},
			wantErr: false,
		},
		{
			name:         "container with .claude mount adds projects subdir",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": ["/agent-2"],
				"Mounts": [{
					"Type": "bind",
					"Source": "/data/.claude",
					"Destination": "/home/user/.claude"
				}]
			}]`,
			wantPaths: []MountedPath{
				{ContainerName: "agent-2", HostPath: "/data/.claude/projects"},
			},
			wantErr: false,
		},
		{
			name:         "container with source ending in .claude also matches",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": ["/agent-src"],
				"Mounts": [{
					"Type": "volume",
					"Source": "/host/path/.claude",
					"Destination": "/some/other/dest"
				}]
			}]`,
			wantPaths: []MountedPath{
				{ContainerName: "agent-src", HostPath: "/host/path/.claude/projects"},
			},
			wantErr: false,
		},
		{
			name:         "container without relevant mounts is skipped",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": ["/web-server"],
				"Mounts": [{
					"Type": "bind",
					"Source": "/var/www",
					"Destination": "/usr/share/nginx/html"
				}]
			}]`,
			wantPaths: nil,
			wantErr:  false,
		},
		{
			name:         "container with no mounts at all",
			responseCode: http.StatusOK,
			responseBody: `[{"Names": ["/bare"], "Mounts": []}]`,
			wantPaths:    nil,
			wantErr:      false,
		},
		{
			name:         "container with no names uses unknown",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": [],
				"Mounts": [{
					"Type": "bind",
					"Source": "/x/.claude/projects",
					"Destination": "/home/.claude/projects"
				}]
			}]`,
			wantPaths: []MountedPath{
				{ContainerName: "unknown", HostPath: "/x/.claude/projects"},
			},
			wantErr: false,
		},
		{
			name:         "multiple containers with mixed mounts",
			responseCode: http.StatusOK,
			responseBody: `[
				{
					"Names": ["/agent-a"],
					"Mounts": [{
						"Type": "bind",
						"Source": "/vol/a/.claude/projects",
						"Destination": "/home/.claude/projects"
					}]
				},
				{
					"Names": ["/web"],
					"Mounts": [{
						"Type": "bind",
						"Source": "/var/www",
						"Destination": "/html"
					}]
				},
				{
					"Names": ["/agent-b"],
					"Mounts": [{
						"Type": "bind",
						"Source": "/vol/b/.claude",
						"Destination": "/home/.claude"
					}]
				}
			]`,
			wantPaths: []MountedPath{
				{ContainerName: "agent-a", HostPath: "/vol/a/.claude/projects"},
				{ContainerName: "agent-b", HostPath: "/vol/b/.claude/projects"},
			},
			wantErr: false,
		},
		{
			name:         "malformed JSON response",
			responseCode: http.StatusOK,
			responseBody: `{not-valid-json`,
			wantPaths:    nil,
			wantErr:      true,
		},
		{
			name:         "container with multiple mounts matches only relevant ones",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": ["/multi-mount"],
				"Mounts": [
					{"Type": "bind", "Source": "/var/www", "Destination": "/html"},
					{"Type": "bind", "Source": "/data/.claude/projects", "Destination": "/home/.claude/projects"},
					{"Type": "bind", "Source": "/tmp/logs", "Destination": "/var/log"}
				]
			}]`,
			wantPaths: []MountedPath{
				{ContainerName: "multi-mount", HostPath: "/data/.claude/projects"},
			},
			wantErr: false,
		},
		{
			name:         "leading slash stripped from container name",
			responseCode: http.StatusOK,
			responseBody: `[{
				"Names": ["/my-container"],
				"Mounts": [{
					"Type": "bind",
					"Source": "/a/.claude/projects",
					"Destination": "/b/.claude/projects"
				}]
			}]`,
			wantPaths: []MountedPath{
				{ContainerName: "my-container", HostPath: "/a/.claude/projects"},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.responseCode)
				fmt.Fprint(w, tc.responseBody)
			})

			client, cleanup := newTestClient(t, mux)
			defer cleanup()

			got, err := client.FindClaudePaths(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantPaths) {
				t.Fatalf("got %d paths, want %d:\n  got:  %+v\n  want: %+v", len(got), len(tc.wantPaths), got, tc.wantPaths)
			}
			for i := range tc.wantPaths {
				if got[i].ContainerName != tc.wantPaths[i].ContainerName {
					t.Errorf("path[%d].ContainerName = %q, want %q", i, got[i].ContainerName, tc.wantPaths[i].ContainerName)
				}
				if got[i].HostPath != tc.wantPaths[i].HostPath {
					t.Errorf("path[%d].HostPath = %q, want %q", i, got[i].HostPath, tc.wantPaths[i].HostPath)
				}
			}
		})
	}
}

// ---------- StopContainer tests ----------

func TestStopContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		responseCode  int
		wantErr       bool
	}{
		{
			name:          "stop returns 204 No Content",
			containerName: "agent-1",
			responseCode:  http.StatusNoContent,
			wantErr:       false,
		},
		{
			name:          "stop returns 304 Not Modified (already stopped)",
			containerName: "agent-2",
			responseCode:  http.StatusNotModified,
			wantErr:       false,
		},
		{
			name:          "stop returns 404 Not Found",
			containerName: "nonexistent",
			responseCode:  http.StatusNotFound,
			wantErr:       true,
		},
		{
			name:          "stop returns 500 Internal Server Error",
			containerName: "broken",
			responseCode:  http.StatusInternalServerError,
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
				// Verify the request path includes the container name and /stop
				expectedPath := "/containers/" + tc.containerName + "/stop"
				if r.URL.Path != expectedPath {
					t.Errorf("unexpected path: got %q, want %q", r.URL.Path, expectedPath)
				}
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: got %q, want POST", r.Method)
				}
				w.WriteHeader(tc.responseCode)
			})

			client, cleanup := newTestClient(t, mux)
			defer cleanup()

			err := client.StopContainer(context.Background(), tc.containerName)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestStopContainer_ErrorMessage(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict) // 409
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	err := client.StopContainer(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 409 response")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error should contain status code 409, got: %v", err)
	}
}

// ---------- StopContainer context cancellation ----------

func TestStopContainer_CancelledContext(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		// Delay response to ensure context cancellation triggers first
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusNoContent)
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.StopContainer(ctx, "test-container")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// ---------- FindClaudePaths with connection error ----------

func TestFindClaudePaths_ConnectionError(t *testing.T) {
	t.Parallel()

	// Point client at a non-existent socket
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")
	client := NewClient(sockPath)

	_, err := client.FindClaudePaths(context.Background())
	if err == nil {
		t.Fatal("expected error when socket does not exist")
	}
	if !strings.Contains(err.Error(), "docker socket") {
		t.Errorf("error should mention docker socket, got: %v", err)
	}
}

// ---------- Watch tests ----------

func TestWatch_InitialPoll(t *testing.T) {
	t.Parallel()

	body := `[{
		"Names": ["/watcher-a"],
		"Mounts": [{"Type": "bind", "Source": "/vol/a/.claude/projects", "Destination": "/home/.claude/projects"}]
	}]`

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := Watch(ctx, client, time.Hour) // Long interval; we only care about initial poll
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	select {
	case ev := <-ch:
		if !ev.Added {
			t.Error("expected Added=true for initial event")
		}
		if ev.ContainerName != "watcher-a" {
			t.Errorf("ContainerName = %q, want %q", ev.ContainerName, "watcher-a")
		}
		if ev.HostPath != "/vol/a/.claude/projects" {
			t.Errorf("HostPath = %q, want %q", ev.HostPath, "/vol/a/.claude/projects")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for initial event")
	}
}

func TestWatch_InitialPollError(t *testing.T) {
	t.Parallel()

	sockPath := filepath.Join(t.TempDir(), "gone.sock")
	client := NewClient(sockPath)

	_, err := Watch(context.Background(), client, time.Second)
	if err == nil {
		t.Fatal("expected error when initial poll fails")
	}
	if !strings.Contains(err.Error(), "docker initial poll") {
		t.Errorf("error should mention initial poll, got: %v", err)
	}
}

func TestWatch_EmptyInitialPoll(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := Watch(ctx, client, time.Hour)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Cancel to close the channel, then verify no events were sent.
	cancel()

	// Drain channel; should get zero events before close.
	var events []PathEvent
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d: %+v", len(events), events)
	}
}

func TestWatch_DetectsAddedAndRemovedPaths(t *testing.T) {
	t.Parallel()

	pollCount := 0
	responses := []string{
		// Initial poll: one container
		`[{
			"Names": ["/agent-init"],
			"Mounts": [{"Type":"bind","Source":"/vol/init/.claude/projects","Destination":"/home/.claude/projects"}]
		}]`,
		// Second poll: previous container gone, new container appears
		`[{
			"Names": ["/agent-new"],
			"Mounts": [{"Type":"bind","Source":"/vol/new/.claude/projects","Destination":"/home/.claude/projects"}]
		}]`,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idx := pollCount
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		fmt.Fprint(w, responses[idx])
		pollCount++
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := Watch(ctx, client, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Collect events until we have the initial add, the removal, and the new add.
	var events []PathEvent
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed prematurely")
			}
			events = append(events, ev)
			// We expect: initial add, then remove of init, then add of new
			if len(events) >= 3 {
				goto done
			}
		case <-deadline:
			t.Fatalf("timeout: only received %d events: %+v", len(events), events)
		}
	}
done:

	// Event 0: initial add
	if !events[0].Added || events[0].ContainerName != "agent-init" {
		t.Errorf("event[0]: expected Added=true for agent-init, got %+v", events[0])
	}
	// Event 1 or 2: removal of agent-init
	foundRemoval := false
	foundNewAdd := false
	for _, ev := range events[1:] {
		if !ev.Added && ev.ContainerName == "agent-init" && ev.HostPath == "/vol/init/.claude/projects" {
			foundRemoval = true
		}
		if ev.Added && ev.ContainerName == "agent-new" && ev.HostPath == "/vol/new/.claude/projects" {
			foundNewAdd = true
		}
	}
	if !foundRemoval {
		t.Error("did not receive removal event for agent-init")
	}
	if !foundNewAdd {
		t.Error("did not receive add event for agent-new")
	}
}

func TestWatch_CancelStopsGoroutine(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := Watch(ctx, client, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	cancel()

	// Channel should close promptly.
	select {
	case _, ok := <-ch:
		if ok {
			// It's fine if we get a straggling event, but the channel must eventually close.
			for range ch {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel was not closed after context cancel")
	}
}

// ---------- JSON parsing edge cases ----------

func TestFindClaudePaths_JSONStructureVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "null Mounts field",
			body:      `[{"Names": ["/c1"], "Mounts": null}]`,
			wantCount: 0,
		},
		{
			name:      "missing Mounts field entirely",
			body:      `[{"Names": ["/c1"]}]`,
			wantCount: 0,
		},
		{
			name:      "null Names field with matching mount",
			body:      `[{"Names": null, "Mounts": [{"Type": "bind", "Source": "/x/.claude/projects", "Destination": "/y/.claude/projects"}]}]`,
			wantCount: 1,
		},
		{
			name:    "non-array top-level JSON",
			body:    `{"message": "error"}`,
			wantErr: true,
		},
		{
			name:    "truncated JSON",
			body:    `[{"Names": ["/c1"]`,
			wantErr: true,
		},
		{
			name:      "extra fields are ignored",
			body:      `[{"Names": ["/c1"], "Id": "abc123", "State": "running", "Mounts": [{"Type": "bind", "Source": "/z/.claude/projects", "Destination": "/w/.claude/projects", "RW": true}]}]`,
			wantCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			})

			client, cleanup := newTestClient(t, mux)
			defer cleanup()

			got, err := client.FindClaudePaths(context.Background())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantCount {
				t.Errorf("got %d paths, want %d: %+v", len(got), tc.wantCount, got)
			}
		})
	}
}

// ---------- NewClient / httptest integration ----------

func TestNewClient_UsesUnixSocket(t *testing.T) {
	t.Parallel()

	// Verify that NewClient returns a non-nil client.
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	// Create a real socket so the client can connect.
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	c := NewClient(sockPath)
	if c == nil {
		t.Fatal("expected non-nil Client")
	}
	if c.http == nil {
		t.Fatal("expected non-nil http.Client")
	}
}

// ---------- Request validation ----------

func TestFindClaudePaths_SendsCorrectFilterQuery(t *testing.T) {
	t.Parallel()

	var capturedQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	_, err := client.FindClaudePaths(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The query should contain the filters parameter requesting running containers.
	if !strings.Contains(capturedQuery, "filters=") {
		t.Errorf("expected filters in query, got %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "running") {
		t.Errorf("expected 'running' in filter query, got %q", capturedQuery)
	}
}

// ---------- MountedPath struct ----------

func TestMountedPath_Fields(t *testing.T) {
	t.Parallel()

	mp := MountedPath{
		ContainerName: "test-container",
		HostPath:      "/data/.claude/projects",
	}

	// Verify JSON round-trip to ensure struct tags work.
	data, err := json.Marshal(mp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got MountedPath
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != mp {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, mp)
	}
}

// ---------- PathEvent struct ----------

func TestPathEvent_Fields(t *testing.T) {
	t.Parallel()

	ev := PathEvent{
		ContainerName: "agent-1",
		HostPath:      "/vol/a/.claude/projects",
		Added:         true,
	}

	if ev.ContainerName != "agent-1" {
		t.Errorf("ContainerName = %q, want %q", ev.ContainerName, "agent-1")
	}
	if ev.HostPath != "/vol/a/.claude/projects" {
		t.Errorf("HostPath = %q, want %q", ev.HostPath, "/vol/a/.claude/projects")
	}
	if !ev.Added {
		t.Error("expected Added=true")
	}
}

// ---------- Concurrency safety with httptest ----------

func TestFindClaudePaths_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	body := `[{
		"Names": ["/concurrent-agent"],
		"Mounts": [{"Type": "bind", "Source": "/vol/.claude/projects", "Destination": "/home/.claude/projects"}]
	}]`

	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	})

	client, cleanup := newTestClient(t, mux)
	defer cleanup()

	// Fire multiple concurrent requests.
	const n = 10
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			paths, err := client.FindClaudePaths(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if len(paths) != 1 {
				errs <- fmt.Errorf("expected 1 path, got %d", len(paths))
				return
			}
			errs <- nil
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request %d: %v", i, err)
		}
	}
}

// Suppress noisy log output from Watch when Docker poll fails mid-watch.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
