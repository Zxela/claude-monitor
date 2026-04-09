// Package autopsy generates markdown summaries of completed Claude Code sessions.
package autopsy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/store"
)

const (
	maxFiles    = 50
	maxCommands = 50
	maxErrors   = 50
	maxTruncLen = 200
)

// Summary holds the structured data for a session autopsy.
type Summary struct {
	SessionID       string `json:"sessionId"`
	SessionName     string `json:"sessionName"`
	TaskDescription string `json:"taskDescription"`
	StartedAt       string `json:"startedAt"`
	EndedAt         string `json:"endedAt"`
	Duration        string `json:"duration"`
	Model           string `json:"model"`
	CWD             string `json:"cwd"`
	GitBranch       string `json:"gitBranch"`

	// Cost
	TotalCost           float64 `json:"totalCost"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`

	// Activity
	MessageCount int              `json:"messageCount"`
	EventCount   int              `json:"eventCount"`
	ErrorCount   int              `json:"errorCount"`
	ToolUses     []ToolUseSummary `json:"toolUses"`
	FilesChanged []string         `json:"filesChanged"`
	CommandsRun  []string         `json:"commandsRun"`
	Errors       []string         `json:"errors"`

	// Subagents
	SubagentCount int               `json:"subagentCount"`
	SubagentCosts []SubagentSummary `json:"subagentCosts"`
}

// ToolUseSummary counts how many times a particular tool was used.
type ToolUseSummary struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// SubagentSummary records cost information for a child session.
type SubagentSummary struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Cost float64 `json:"cost"`
}

// Generate builds a Summary from a session row, its events, and any child sessions.
func Generate(session *store.SessionRow, events []store.EventRow, children []store.SessionRow) Summary {
	s := Summary{
		SessionID:           session.ID,
		SessionName:         session.SessionName,
		TaskDescription:     session.TaskDescription,
		StartedAt:           session.StartedAt,
		EndedAt:             session.EndedAt,
		Model:               session.Model,
		CWD:                 session.CWD,
		GitBranch:           session.Branch,
		TotalCost:           session.TotalCost,
		InputTokens:         session.InputTokens,
		OutputTokens:        session.OutputTokens,
		CacheReadTokens:     session.CacheReadTokens,
		CacheCreationTokens: session.CacheCreationTokens,
		MessageCount:        session.MessageCount,
		EventCount:          session.EventCount,
		ErrorCount:          session.ErrorCount,
	}

	// Compute duration.
	s.Duration = computeDuration(session.StartedAt, session.EndedAt)

	// Process events.
	toolCounts := make(map[string]int)
	filesSet := make(map[string]bool)
	commandsSet := make(map[string]bool)
	var commandsList []string
	var filesList []string

	for _, ev := range events {
		// Count tool uses.
		if ev.ToolName != "" {
			toolCounts[ev.ToolName]++
		}

		// Extract file paths from file-oriented tools.
		if isFileTool(ev.ToolName) && ev.ToolDetail != "" {
			path := truncate(ev.ToolDetail, maxTruncLen)
			if !filesSet[path] && len(filesList) < maxFiles {
				filesSet[path] = true
				filesList = append(filesList, path)
			}
		}

		// Extract commands from Bash events.
		if ev.ToolName == "Bash" && ev.ToolDetail != "" {
			cmd := truncate(ev.ToolDetail, maxTruncLen)
			if !commandsSet[cmd] && len(commandsList) < maxCommands {
				commandsSet[cmd] = true
				commandsList = append(commandsList, cmd)
			}
		}

		// Collect errors.
		if ev.IsError && ev.ContentPreview != "" && len(s.Errors) < maxErrors {
			s.Errors = append(s.Errors, truncate(ev.ContentPreview, maxTruncLen))
		}
	}

	// Build sorted tool-use summary (descending by count).
	for name, count := range toolCounts {
		s.ToolUses = append(s.ToolUses, ToolUseSummary{Name: name, Count: count})
	}
	sort.Slice(s.ToolUses, func(i, j int) bool {
		return s.ToolUses[i].Count > s.ToolUses[j].Count
	})

	s.FilesChanged = filesList
	s.CommandsRun = commandsList

	// Subagents.
	s.SubagentCount = len(children)
	for _, child := range children {
		name := child.SessionName
		if name == "" {
			name = child.ID
		}
		s.SubagentCosts = append(s.SubagentCosts, SubagentSummary{
			ID:   child.ID,
			Name: name,
			Cost: child.TotalCost,
		})
	}

	return s
}

// RenderMarkdown produces a clean markdown report from a Summary.
func RenderMarkdown(s Summary) string {
	var b strings.Builder

	// Title.
	title := s.SessionName
	if title == "" {
		title = shortID(s.SessionID)
	}
	fmt.Fprintf(&b, "# Session Autopsy: %s\n\n", title)

	// Metadata.
	if s.TaskDescription != "" {
		fmt.Fprintf(&b, "**Task:** %s\n", s.TaskDescription)
	}
	if s.StartedAt != "" {
		endLabel := s.EndedAt
		if endLabel == "" {
			endLabel = "(ongoing)"
		}
		durLabel := ""
		if s.Duration != "" {
			durLabel = fmt.Sprintf(" (%s)", s.Duration)
		}
		fmt.Fprintf(&b, "**Date:** %s — %s%s\n", s.StartedAt, endLabel, durLabel)
	}
	if s.Model != "" {
		fmt.Fprintf(&b, "**Model:** %s\n", s.Model)
	}
	if s.CWD != "" {
		fmt.Fprintf(&b, "**Working Directory:** %s\n", s.CWD)
	}
	if s.GitBranch != "" {
		fmt.Fprintf(&b, "**Branch:** %s\n", s.GitBranch)
	}
	b.WriteString("\n")

	// Cost Summary.
	b.WriteString("## Cost Summary\n\n")
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	fmt.Fprintf(&b, "| Total Cost | $%.4f |\n", s.TotalCost)
	fmt.Fprintf(&b, "| Input Tokens | %s |\n", formatInt(s.InputTokens))
	fmt.Fprintf(&b, "| Output Tokens | %s |\n", formatInt(s.OutputTokens))
	fmt.Fprintf(&b, "| Cache Read | %s |\n", formatInt(s.CacheReadTokens))
	fmt.Fprintf(&b, "| Cache Creation | %s |\n", formatInt(s.CacheCreationTokens))
	b.WriteString("\n")

	// Activity.
	b.WriteString("## Activity\n\n")
	fmt.Fprintf(&b, "- **Messages:** %d\n", s.MessageCount)
	fmt.Fprintf(&b, "- **Events:** %d\n", s.EventCount)
	fmt.Fprintf(&b, "- **Errors:** %d\n", s.ErrorCount)
	b.WriteString("\n")

	// Tools Used.
	if len(s.ToolUses) > 0 {
		b.WriteString("### Tools Used\n\n")
		b.WriteString("| Tool | Uses |\n")
		b.WriteString("|------|------|\n")
		for _, t := range s.ToolUses {
			fmt.Fprintf(&b, "| %s | %d |\n", t.Name, t.Count)
		}
		b.WriteString("\n")
	}

	// Files Changed.
	if len(s.FilesChanged) > 0 {
		b.WriteString("### Files Changed\n\n")
		for _, f := range s.FilesChanged {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	// Commands Run.
	if len(s.CommandsRun) > 0 {
		b.WriteString("### Commands Run\n\n")
		for _, c := range s.CommandsRun {
			fmt.Fprintf(&b, "- `%s`\n", c)
		}
		b.WriteString("\n")
	}

	// Errors.
	if len(s.Errors) > 0 {
		b.WriteString("## Errors\n\n")
		for i, e := range s.Errors {
			fmt.Fprintf(&b, "%d. %s\n", i+1, e)
		}
		b.WriteString("\n")
	}

	// Subagents.
	if s.SubagentCount > 0 {
		b.WriteString("## Subagents\n\n")
		b.WriteString("| Agent | Cost |\n")
		b.WriteString("|-------|------|\n")
		for _, a := range s.SubagentCosts {
			fmt.Fprintf(&b, "| %s | $%.4f |\n", a.Name, a.Cost)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// isFileTool returns true for tools that operate on files.
func isFileTool(name string) bool {
	switch name {
	case "Write", "Edit", "Read", "NotebookEdit":
		return true
	}
	return false
}

// computeDuration parses two RFC3339 timestamps and returns a human-readable duration.
func computeDuration(start, end string) string {
	if start == "" {
		return ""
	}
	t1, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return ""
	}
	endStr := end
	if endStr == "" {
		return ""
	}
	t2, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return ""
	}
	d := t2.Sub(t1)
	if d < 0 {
		d = 0
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// shortID returns the first 8 characters of an ID, or the full ID if shorter.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// formatInt formats an int64 with comma separators.
func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}
