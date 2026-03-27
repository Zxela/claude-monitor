package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	testPort    = 17799
	testVersion = "v0.0.0-test"
	testBinary  = "/tmp/claude-monitor-api-test"
)

var baseURL = fmt.Sprintf("http://localhost:%d", testPort)

// TestMain builds the binary once, starts the server once, runs all tests,
// then kills the server.
func TestMain(m *testing.M) {
	// Build the binary with a known version string.
	build := exec.Command(
		"go", "build",
		"-ldflags", fmt.Sprintf("-X main.version=%s", testVersion),
		"-o", testBinary,
		"./",
	)
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	// Start the server.
	srv := exec.Command(testBinary, "--port", fmt.Sprintf("%d", testPort))
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
		os.Exit(1)
	}

	// Wait for health check to pass (up to 10 seconds).
	deadline := time.Now().Add(10 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		srv.Process.Kill()
		fmt.Fprintln(os.Stderr, "server did not become ready in time")
		os.Exit(1)
	}

	// Run all tests.
	code := m.Run()

	// Kill the server and clean up.
	srv.Process.Kill()
	os.Remove(testBinary)

	os.Exit(code)
}

// getJSON performs a GET request against the shared test server, asserts the
// status code is 200 and the Content-Type is application/json, then decodes
// the response body into v.
func getJSON(t *testing.T, path string, v interface{}) {
	t.Helper()

	resp, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected status 200, got %d", path, resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET %s: expected Content-Type application/json, got %q", path, ct)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: failed to decode JSON response: %v", path, err)
	}
}

// TestHealth verifies GET /health returns 200 {"ok":true}.
func TestHealth(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	getJSON(t, "/health", &body)

	ok, exists := body["ok"]
	if !exists {
		t.Fatal("response missing key \"ok\"")
	}
	if ok != true {
		t.Errorf("expected ok=true, got %v", ok)
	}
}

// TestVersion verifies GET /api/version returns 200 with a "version" key equal
// to the value injected at build time.
func TestVersion(t *testing.T) {
	t.Parallel()

	var body map[string]string
	getJSON(t, "/api/version", &body)

	got, exists := body["version"]
	if !exists {
		t.Fatal("response missing key \"version\"")
	}
	if got != testVersion {
		t.Errorf("expected version %q, got %q", testVersion, got)
	}
}

// TestSessions verifies GET /api/sessions returns 200 and a JSON array.
func TestSessions(t *testing.T) {
	t.Parallel()

	var body []json.RawMessage
	getJSON(t, "/api/sessions", &body)

	// A nil slice would indicate the server returned JSON null rather than [].
	if body == nil {
		t.Error("expected non-nil array, got null")
	}
}

// TestSessionsGrouped verifies GET /api/sessions/grouped returns 200 and a
// response containing all six expected bucket keys.
func TestSessionsGrouped(t *testing.T) {
	t.Parallel()

	var body map[string]json.RawMessage
	getJSON(t, "/api/sessions/grouped", &body)

	for _, key := range []string{"active", "lastHour", "today", "yesterday", "thisWeek", "older"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing expected bucket key %q", key)
		}
	}
}

// TestProjects verifies GET /api/projects returns 200 and a JSON array.
func TestProjects(t *testing.T) {
	t.Parallel()

	var body []json.RawMessage
	getJSON(t, "/api/projects", &body)

	if body == nil {
		t.Error("expected non-nil array, got null")
	}
}

// TestSearch verifies GET /api/search?q=test returns 200 and a JSON array.
func TestSearch(t *testing.T) {
	t.Parallel()

	var body []json.RawMessage
	getJSON(t, "/api/search?q=test", &body)

	if body == nil {
		t.Error("expected non-nil array, got null")
	}
}

// TestSearchEmpty verifies GET /api/search (no query) returns 200 and an empty
// JSON array.
func TestSearchEmpty(t *testing.T) {
	t.Parallel()

	resp, err := http.Get(baseURL + "/api/search")
	if err != nil {
		t.Fatalf("GET /api/search failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var body []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty array, got %d elements", len(body))
	}
}

// TestHistory verifies GET /api/history?limit=5 returns 200 and a JSON array.
func TestHistory(t *testing.T) {
	t.Parallel()

	var body []json.RawMessage
	getJSON(t, "/api/history?limit=5", &body)

	if body == nil {
		t.Error("expected non-nil array, got null")
	}
}

// TestVersionFlag runs the binary with --version and checks that stdout
// contains the expected output. This test builds its own binary so it can
// capture stdout directly; it does not depend on the shared server.
func TestVersionFlag(t *testing.T) {
	// Build a dedicated binary for flag capture (no version injected, just
	// verify the output prefix).
	flagBinary := "/tmp/claude-monitor-flag-test"
	build := exec.Command(
		"go", "build",
		"-ldflags", fmt.Sprintf("-X main.version=%s", testVersion),
		"-o", flagBinary,
		"./",
	)
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	defer os.Remove(flagBinary)

	out, err := exec.Command(flagBinary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version exited with error: %v\n%s", err, out)
	}

	output := strings.TrimSpace(string(out))
	expected := fmt.Sprintf("claude-monitor %s", testVersion)
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

// TestStats verifies GET /api/stats returns 200 and a valid stats response
// with all expected fields.
func TestStats(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	getJSON(t, "/api/stats", &body)

	for _, key := range []string{
		"totalCost", "inputTokens", "outputTokens",
		"cacheReadTokens", "cacheCreationTokens",
		"sessionCount", "activeSessions", "cacheHitPct",
		"costRate", "costByModel",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing expected key %q", key)
		}
	}

	// costByModel must be an object, not null
	if body["costByModel"] == nil {
		t.Error("costByModel is null, expected an object")
	}
}

// TestStatsWindows verifies each window parameter is accepted (no 400/500).
func TestStatsWindows(t *testing.T) {
	t.Parallel()

	for _, window := range []string{"all", "today", "week", "month", ""} {
		path := "/api/stats"
		if window != "" {
			path += "?window=" + window
		}
		var body map[string]interface{}
		getJSON(t, path, &body)

		if _, ok := body["totalCost"]; !ok {
			t.Errorf("window=%q: response missing totalCost", window)
		}
	}
}

// TestStatsNonNegative verifies stats values are never negative.
func TestStatsNonNegative(t *testing.T) {
	t.Parallel()

	var body map[string]interface{}
	getJSON(t, "/api/stats?window=all", &body)

	for _, key := range []string{"totalCost", "inputTokens", "outputTokens", "sessionCount", "activeSessions"} {
		val, ok := body[key].(float64) // JSON numbers decode as float64
		if !ok {
			continue
		}
		if val < 0 {
			t.Errorf("%s is negative: %f", key, val)
		}
	}
}

// TestSwaggerEndpoint verifies that /swagger is not served by default (404).
func TestSwaggerEndpoint(t *testing.T) {
	t.Parallel()

	resp, err := http.Get(baseURL + "/swagger")
	if err != nil {
		t.Fatalf("GET /swagger failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for /swagger, got %d", resp.StatusCode)
	}
}
