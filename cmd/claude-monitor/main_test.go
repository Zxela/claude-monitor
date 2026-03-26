package main

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestVersionEndpoint(t *testing.T) {
	// Build the binary with a known version string.
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version=v1.0.0-test", "-o", "/tmp/claude-monitor-ver-test", "./")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	// Start the server on port 17701.
	srv := exec.Command("/tmp/claude-monitor-ver-test", "--port", "17701")
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Process.Kill()

	// Wait for server to be ready.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://localhost:17701/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// GET /api/version.
	resp, err := http.Get("http://localhost:17701/api/version")
	if err != nil {
		t.Fatalf("GET /api/version failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if got := body["version"]; got != "v1.0.0-test" {
		t.Errorf("expected version %q, got %q", "v1.0.0-test", got)
	}
}

func TestGroupedSessionsEndpoint(t *testing.T) {
	// Build the binary.
	cmd := exec.Command("go", "build", "-o", "/tmp/claude-monitor-grouped-test", "./")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	// Start the server on port 17702.
	srv := exec.Command("/tmp/claude-monitor-grouped-test", "--port", "17702")
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Process.Kill()

	// Wait for server to be ready.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://localhost:17702/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// GET /api/sessions/grouped.
	resp, err := http.Get("http://localhost:17702/api/sessions/grouped")
	if err != nil {
		t.Fatalf("GET /api/sessions/grouped failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	for _, key := range []string{"active", "lastHour", "today", "yesterday", "thisWeek", "older"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response missing expected key %q", key)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	// Build the binary
	cmd := exec.Command("go", "build", "-o", "/tmp/claude-monitor-test", "./")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}

	// Run with --version
	out, err := exec.Command("/tmp/claude-monitor-test", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version failed: %s\n%s", err, out)
	}
	output := strings.TrimSpace(string(out))
	if !strings.HasPrefix(output, "claude-monitor ") {
		t.Errorf("expected output starting with 'claude-monitor ', got %q", output)
	}
}
