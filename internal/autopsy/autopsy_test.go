package autopsy

import (
	"strings"
	"testing"

	"github.com/zxela-claude/claude-monitor/internal/store"
)

// testSession returns a realistic SessionRow for testing.
func testSession() *store.SessionRow {
	return &store.SessionRow{
		ID:                  "sess-abc12345-def6-7890-ghij-klmnopqrstuv",
		SessionName:         "implement-autopsy",
		TaskDescription:     "Add session autopsy feature with markdown export",
		CWD:                 "/home/user/claude-monitor",
		Branch:              "feat/autopsy",
		Model:               "claude-sonnet-4-20250514",
		StartedAt:           "2026-04-08T10:00:00Z",
		EndedAt:             "2026-04-08T10:35:22Z",
		TotalCost:           0.4523,
		InputTokens:         125000,
		OutputTokens:        18000,
		CacheReadTokens:     95000,
		CacheCreationTokens: 12000,
		MessageCount:        24,
		EventCount:          87,
		ErrorCount:          2,
	}
}

// testEvents returns a realistic set of EventRows for testing.
func testEvents() []store.EventRow {
	return []store.EventRow{
		{ToolName: "Read", ToolDetail: "internal/store/sqlite.go"},
		{ToolName: "Read", ToolDetail: "cmd/claude-monitor/main.go"},
		{ToolName: "Edit", ToolDetail: "internal/autopsy/autopsy.go"},
		{ToolName: "Write", ToolDetail: "internal/autopsy/autopsy_test.go"},
		{ToolName: "Bash", ToolDetail: "go build ./..."},
		{ToolName: "Bash", ToolDetail: "go test ./..."},
		{ToolName: "Bash", ToolDetail: "go build ./..."},
		{ToolName: "Grep", ToolDetail: "scanSessionRows"},
		{ToolName: "Glob", ToolDetail: "**/*_test.go"},
		{IsError: true, ContentPreview: "undefined: autopsy"},
		{IsError: true, ContentPreview: "test failed: expected 42 got 0"},
		{Type: "assistant", Role: "assistant"},
		{Type: "user", Role: "user"},
	}
}

// testChildren returns child session rows for subagent testing.
func testChildren() []store.SessionRow {
	return []store.SessionRow{
		{
			ID:          "child-001",
			SessionName: "lint-check",
			TotalCost:   0.0312,
		},
		{
			ID:          "child-002",
			SessionName: "test-runner",
			TotalCost:   0.0891,
		},
	}
}

func TestGenerate_BasicFields(t *testing.T) {
	sess := testSession()
	events := testEvents()
	children := testChildren()

	s := Generate(sess, events, children)

	if s.SessionID != sess.ID {
		t.Errorf("SessionID = %q, want %q", s.SessionID, sess.ID)
	}
	if s.SessionName != sess.SessionName {
		t.Errorf("SessionName = %q, want %q", s.SessionName, sess.SessionName)
	}
	if s.TaskDescription != sess.TaskDescription {
		t.Errorf("TaskDescription = %q, want %q", s.TaskDescription, sess.TaskDescription)
	}
	if s.Model != sess.Model {
		t.Errorf("Model = %q, want %q", s.Model, sess.Model)
	}
	if s.TotalCost != sess.TotalCost {
		t.Errorf("TotalCost = %f, want %f", s.TotalCost, sess.TotalCost)
	}
	if s.MessageCount != sess.MessageCount {
		t.Errorf("MessageCount = %d, want %d", s.MessageCount, sess.MessageCount)
	}
}

func TestGenerate_Duration(t *testing.T) {
	sess := testSession()
	s := Generate(sess, nil, nil)

	if s.Duration != "35m 22s" {
		t.Errorf("Duration = %q, want %q", s.Duration, "35m 22s")
	}
}

func TestGenerate_DurationEmpty(t *testing.T) {
	sess := testSession()
	sess.EndedAt = ""
	s := Generate(sess, nil, nil)

	if s.Duration != "" {
		t.Errorf("Duration = %q, want empty for missing EndedAt", s.Duration)
	}
}

func TestGenerate_ToolCounts(t *testing.T) {
	sess := testSession()
	events := testEvents()
	s := Generate(sess, events, nil)

	// Build map from tool uses for easier assertions.
	toolMap := make(map[string]int)
	for _, tu := range s.ToolUses {
		toolMap[tu.Name] = tu.Count
	}

	if toolMap["Read"] != 2 {
		t.Errorf("Read count = %d, want 2", toolMap["Read"])
	}
	if toolMap["Bash"] != 3 {
		t.Errorf("Bash count = %d, want 3", toolMap["Bash"])
	}
	if toolMap["Edit"] != 1 {
		t.Errorf("Edit count = %d, want 1", toolMap["Edit"])
	}
	if toolMap["Write"] != 1 {
		t.Errorf("Write count = %d, want 1", toolMap["Write"])
	}

	// ToolUses should be sorted by count descending.
	for i := 1; i < len(s.ToolUses); i++ {
		if s.ToolUses[i].Count > s.ToolUses[i-1].Count {
			t.Errorf("ToolUses not sorted descending: %v", s.ToolUses)
			break
		}
	}
}

func TestGenerate_FilesChanged(t *testing.T) {
	sess := testSession()
	events := testEvents()
	s := Generate(sess, events, nil)

	// Read, Edit, Write tools should have their tool_detail extracted as file paths.
	fileSet := make(map[string]bool)
	for _, f := range s.FilesChanged {
		fileSet[f] = true
	}

	expected := []string{
		"internal/store/sqlite.go",
		"cmd/claude-monitor/main.go",
		"internal/autopsy/autopsy.go",
		"internal/autopsy/autopsy_test.go",
	}
	for _, want := range expected {
		if !fileSet[want] {
			t.Errorf("FilesChanged missing %q", want)
		}
	}

	// Bash tool detail should NOT appear in files.
	if fileSet["go build ./..."] {
		t.Error("FilesChanged should not include Bash commands")
	}
}

func TestGenerate_CommandsRun(t *testing.T) {
	sess := testSession()
	events := testEvents()
	s := Generate(sess, events, nil)

	cmdSet := make(map[string]bool)
	for _, c := range s.CommandsRun {
		cmdSet[c] = true
	}

	if !cmdSet["go build ./..."] {
		t.Error("CommandsRun missing 'go build ./...'")
	}
	if !cmdSet["go test ./..."] {
		t.Error("CommandsRun missing 'go test ./...'")
	}

	// Duplicate "go build ./..." should be deduplicated.
	if len(s.CommandsRun) != 2 {
		t.Errorf("CommandsRun len = %d, want 2 (duplicates should be removed)", len(s.CommandsRun))
	}
}

func TestGenerate_Errors(t *testing.T) {
	sess := testSession()
	events := testEvents()
	s := Generate(sess, events, nil)

	if len(s.Errors) != 2 {
		t.Fatalf("Errors len = %d, want 2", len(s.Errors))
	}
	if s.Errors[0] != "undefined: autopsy" {
		t.Errorf("Errors[0] = %q, want %q", s.Errors[0], "undefined: autopsy")
	}
}

func TestGenerate_Subagents(t *testing.T) {
	sess := testSession()
	children := testChildren()
	s := Generate(sess, nil, children)

	if s.SubagentCount != 2 {
		t.Errorf("SubagentCount = %d, want 2", s.SubagentCount)
	}
	if len(s.SubagentCosts) != 2 {
		t.Fatalf("SubagentCosts len = %d, want 2", len(s.SubagentCosts))
	}
	if s.SubagentCosts[0].Name != "lint-check" {
		t.Errorf("SubagentCosts[0].Name = %q, want %q", s.SubagentCosts[0].Name, "lint-check")
	}
	if s.SubagentCosts[1].Cost != 0.0891 {
		t.Errorf("SubagentCosts[1].Cost = %f, want %f", s.SubagentCosts[1].Cost, 0.0891)
	}
}

func TestGenerate_SubagentNameFallback(t *testing.T) {
	sess := testSession()
	children := []store.SessionRow{
		{ID: "child-no-name", TotalCost: 0.01},
	}
	s := Generate(sess, nil, children)

	if s.SubagentCosts[0].Name != "child-no-name" {
		t.Errorf("SubagentCosts[0].Name = %q, want ID fallback %q", s.SubagentCosts[0].Name, "child-no-name")
	}
}

func TestGenerate_MaxLimits(t *testing.T) {
	sess := testSession()

	// Create more than maxFiles unique file events.
	var events []store.EventRow
	for i := 0; i < 100; i++ {
		events = append(events, store.EventRow{
			ToolName:   "Edit",
			ToolDetail: strings.Repeat("a", 10) + string(rune('A'+i%26)) + string(rune('0'+i/26)),
		})
	}
	// Also add more than maxCommands unique commands.
	for i := 0; i < 100; i++ {
		events = append(events, store.EventRow{
			ToolName:   "Bash",
			ToolDetail: strings.Repeat("x", 10) + string(rune('A'+i%26)) + string(rune('0'+i/26)),
		})
	}
	// Also add more than maxErrors.
	for i := 0; i < 100; i++ {
		events = append(events, store.EventRow{
			IsError:        true,
			ContentPreview: "error " + string(rune('A'+i%26)),
		})
	}

	s := Generate(sess, events, nil)

	if len(s.FilesChanged) > maxFiles {
		t.Errorf("FilesChanged len = %d, exceeds max %d", len(s.FilesChanged), maxFiles)
	}
	if len(s.CommandsRun) > maxCommands {
		t.Errorf("CommandsRun len = %d, exceeds max %d", len(s.CommandsRun), maxCommands)
	}
	if len(s.Errors) > maxErrors {
		t.Errorf("Errors len = %d, exceeds max %d", len(s.Errors), maxErrors)
	}
}

func TestGenerate_TruncateLong(t *testing.T) {
	sess := testSession()
	longCmd := strings.Repeat("x", 500)
	events := []store.EventRow{
		{ToolName: "Bash", ToolDetail: longCmd},
		{ToolName: "Edit", ToolDetail: strings.Repeat("f", 500)},
		{IsError: true, ContentPreview: strings.Repeat("e", 500)},
	}

	s := Generate(sess, events, nil)

	if len(s.CommandsRun[0]) > maxTruncLen+3 { // +3 for "..."
		t.Errorf("Command not truncated: len=%d", len(s.CommandsRun[0]))
	}
	if !strings.HasSuffix(s.CommandsRun[0], "...") {
		t.Error("Truncated command should end with ...")
	}
	if len(s.FilesChanged[0]) > maxTruncLen+3 {
		t.Errorf("File path not truncated: len=%d", len(s.FilesChanged[0]))
	}
	if len(s.Errors[0]) > maxTruncLen+3 {
		t.Errorf("Error not truncated: len=%d", len(s.Errors[0]))
	}
}

func TestRenderMarkdown_ContainsAllSections(t *testing.T) {
	sess := testSession()
	events := testEvents()
	children := testChildren()
	s := Generate(sess, events, children)

	md := RenderMarkdown(s)

	// Check for required sections.
	sections := []string{
		"# Session Autopsy: implement-autopsy",
		"**Task:** Add session autopsy feature",
		"**Model:** claude-sonnet-4-20250514",
		"**Branch:** feat/autopsy",
		"## Cost Summary",
		"$0.4523",
		"## Activity",
		"**Messages:** 24",
		"### Tools Used",
		"### Files Changed",
		"### Commands Run",
		"## Errors",
		"## Subagents",
		"lint-check",
		"test-runner",
	}
	for _, want := range sections {
		if !strings.Contains(md, want) {
			t.Errorf("RenderMarkdown missing %q", want)
		}
	}
}

func TestRenderMarkdown_NoSessionName(t *testing.T) {
	sess := testSession()
	sess.SessionName = ""
	s := Generate(sess, nil, nil)
	md := RenderMarkdown(s)

	// Should use shortID as fallback.
	if !strings.Contains(md, "# Session Autopsy: sess-abc") {
		t.Errorf("Expected shortID fallback in title, got:\n%s", md[:200])
	}
}

func TestRenderMarkdown_EmptySections(t *testing.T) {
	sess := testSession()
	sess.ErrorCount = 0
	s := Generate(sess, nil, nil)
	md := RenderMarkdown(s)

	// Should NOT contain sections for empty data.
	if strings.Contains(md, "### Tools Used") {
		t.Error("Should not render Tools Used section when no tools were used")
	}
	if strings.Contains(md, "### Files Changed") {
		t.Error("Should not render Files Changed section when no files changed")
	}
	if strings.Contains(md, "### Commands Run") {
		t.Error("Should not render Commands Run section when no commands run")
	}
	if strings.Contains(md, "## Errors") {
		t.Error("Should not render Errors section when no errors")
	}
	if strings.Contains(md, "## Subagents") {
		t.Error("Should not render Subagents section when no subagents")
	}
}

func TestRenderMarkdown_DurationDisplay(t *testing.T) {
	sess := testSession()
	s := Generate(sess, nil, nil)
	md := RenderMarkdown(s)

	if !strings.Contains(md, "(35m 22s)") {
		t.Errorf("Expected duration in markdown, got:\n%s", md[:300])
	}
}

func TestRenderMarkdown_OngoingSession(t *testing.T) {
	sess := testSession()
	sess.EndedAt = ""
	s := Generate(sess, nil, nil)
	md := RenderMarkdown(s)

	if !strings.Contains(md, "(ongoing)") {
		t.Errorf("Expected (ongoing) for session without EndedAt")
	}
}

func TestRenderMarkdown_CommandsFormatted(t *testing.T) {
	sess := testSession()
	events := []store.EventRow{
		{ToolName: "Bash", ToolDetail: "npm test"},
	}
	s := Generate(sess, events, nil)
	md := RenderMarkdown(s)

	if !strings.Contains(md, "- `npm test`") {
		t.Errorf("Commands should be formatted with backticks")
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{125000, "125,000"},
		{1234567, "1,234,567"},
		{-42, "-42"},
		{-1234, "-1,234"},
	}

	for _, tt := range tests {
		got := formatInt(tt.input)
		if got != tt.want {
			t.Errorf("formatInt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestComputeDuration(t *testing.T) {
	tests := []struct {
		start, end string
		want       string
	}{
		{"2026-04-08T10:00:00Z", "2026-04-08T10:00:30Z", "30s"},
		{"2026-04-08T10:00:00Z", "2026-04-08T10:05:30Z", "5m 30s"},
		{"2026-04-08T10:00:00Z", "2026-04-08T12:05:30Z", "2h 5m 30s"},
		{"", "2026-04-08T12:05:30Z", ""},
		{"2026-04-08T10:00:00Z", "", ""},
		{"invalid", "2026-04-08T12:05:30Z", ""},
	}

	for _, tt := range tests {
		got := computeDuration(tt.start, tt.end)
		if got != tt.want {
			t.Errorf("computeDuration(%q, %q) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short string = %q", got)
	}
	if got := truncate("exactly10!", 10); got != "exactly10!" {
		t.Errorf("truncate exact length = %q", got)
	}
	long := "this is a longer string"
	got := truncate(long, 10)
	if got != "this is a ..." {
		t.Errorf("truncate long string = %q, want %q", got, "this is a ...")
	}
}

func TestIsFileTool(t *testing.T) {
	fileTools := []string{"Write", "Edit", "Read", "NotebookEdit"}
	for _, name := range fileTools {
		if !isFileTool(name) {
			t.Errorf("isFileTool(%q) = false, want true", name)
		}
	}

	nonFileTools := []string{"Bash", "Grep", "Glob", "", "Search"}
	for _, name := range nonFileTools {
		if isFileTool(name) {
			t.Errorf("isFileTool(%q) = true, want false", name)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abcdefghijklmnop"); got != "abcdefgh" {
		t.Errorf("shortID long = %q, want %q", got, "abcdefgh")
	}
	if got := shortID("abc"); got != "abc" {
		t.Errorf("shortID short = %q, want %q", got, "abc")
	}
	if got := shortID("12345678"); got != "12345678" {
		t.Errorf("shortID exact = %q, want %q", got, "12345678")
	}
}
