package main

import (
	"os/exec"
	"strings"
	"testing"
)

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
