package main

import (
	"strings"
	"testing"
	"time"

	"github.com/zxela/claude-monitor/internal/session"
	"github.com/zxela/claude-monitor/internal/store"
)

func TestBuildSessionAutopsyMarkdown(t *testing.T) {
	start := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	end := start.Add(5*time.Minute + 12*time.Second)
	sess := &session.Session{
		ID:                  "sess-123",
		TaskDescription:     "Fix flaky test and update docs",
		Model:               "claude-sonnet-4-6",
		RepoID:              "github.com/example/repo",
		GitBranch:           "feature/autopsy",
		StartedAt:           start,
		LastActive:          end,
		TotalCost:           1.23,
		InputTokens:         1000,
		OutputTokens:        500,
		CacheReadTokens:     200,
		CacheCreationTokens: 30,
		MessageCount:        8,
		EventCount:          24,
		ErrorCount:          1,
	}
	events := []store.EventRow{
		{
			Type:           "tool_use",
			ToolName:       "Bash",
			ToolDetail:     "go test ./...",
			ContentPreview: "running tests",
		},
		{
			Type:           "error",
			IsError:        true,
			ContentPreview: "panic: flaky failure",
		},
	}

	md := buildSessionAutopsyMarkdown(sess, events)
	for _, needle := range []string{
		"# Session Autopsy",
		"**Session ID:** `sess-123`",
		"**Total cost:** $1.23",
		"`go test ./...`",
		"panic: flaky failure",
	} {
		if !strings.Contains(md, needle) {
			t.Fatalf("autopsy markdown missing %q\n\n%s", needle, md)
		}
	}
}
