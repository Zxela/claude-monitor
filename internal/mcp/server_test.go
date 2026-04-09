package mcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/repo"
	"github.com/zxela-claude/claude-monitor/internal/session"
	"github.com/zxela-claude/claude-monitor/internal/store"
)

// openTestDB creates a temporary SQLite database for testing.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedTestData inserts a repo and a set of sessions for testing.
func seedTestData(t *testing.T, db *store.DB) {
	t.Helper()

	// Insert repos.
	if err := db.UpsertRepo(&repo.Repo{ID: "repo-alpha", Name: "alpha", URL: "git@github.com:test/alpha.git"}); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}
	if err := db.UpsertRepo(&repo.Repo{ID: "repo-beta", Name: "beta", URL: "git@github.com:test/beta.git"}); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	now := time.Now()
	sessions := []*session.Session{
		{
			ID:              "sess-1",
			RepoID:          "repo-alpha",
			SessionName:     "fix-bug",
			TaskDescription: "Fix the login bug",
			Model:           "claude-sonnet-4-20250514",
			TotalCost:       1.25,
			InputTokens:     50000,
			OutputTokens:    20000,
			CacheReadTokens: 10000,
			MessageCount:    15,
			EventCount:      30,
			ErrorCount:      2,
			StartedAt:       now.Add(-2 * time.Hour),
			LastActive:      now.Add(-1 * time.Hour),
			CWD:             "/home/user/alpha",
			GitBranch:       "main",
		},
		{
			ID:              "sess-2",
			RepoID:          "repo-alpha",
			SessionName:     "add-feature",
			TaskDescription: "Add the new dashboard feature",
			Model:           "claude-sonnet-4-20250514",
			TotalCost:       3.50,
			InputTokens:     100000,
			OutputTokens:    50000,
			CacheReadTokens: 30000,
			MessageCount:    40,
			EventCount:      80,
			ErrorCount:      0,
			StartedAt:       now.Add(-4 * time.Hour),
			LastActive:      now.Add(-3 * time.Hour),
			CWD:             "/home/user/alpha",
			GitBranch:       "feature-branch",
		},
		{
			ID:              "sess-3",
			RepoID:          "repo-beta",
			SessionName:     "refactor",
			TaskDescription: "Refactor the store layer",
			Model:           "claude-opus-4-20250514",
			TotalCost:       0.75,
			InputTokens:     20000,
			OutputTokens:    10000,
			CacheReadTokens: 5000,
			MessageCount:    10,
			EventCount:      20,
			ErrorCount:      0,
			StartedAt:       now.Add(-1 * time.Hour),
			LastActive:      now,
			CWD:             "/home/user/beta",
			GitBranch:       "main",
		},
	}

	for _, s := range sessions {
		if err := db.SaveSession(s); err != nil {
			t.Fatalf("SaveSession(%s): %v", s.ID, err)
		}
	}
}

// runServer creates a Server with the given input, runs it, and returns the output.
func runServer(t *testing.T, db *store.DB, input string) string {
	t.Helper()
	reader := strings.NewReader(input)
	var output bytes.Buffer
	server := NewServer(db, "test-version", reader, &output)
	if err := server.Run(); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	return output.String()
}

// parseResponses splits newline-delimited JSON responses and decodes them.
func parseResponses(t *testing.T, output string) []Response {
	t.Helper()
	var responses []Response
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestInitialize(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	resp := responses[0]
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	// ID should be 1
	idFloat, ok := resp.ID.(float64)
	if !ok || idFloat != 1 {
		t.Errorf("expected id 1, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	// Check result fields.
	resultBytes, _ := json.Marshal(resp.Result)
	var result InitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal InitializeResult: %v", err)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected protocol 2024-11-05, got %s", result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "claude-monitor" {
		t.Errorf("expected name claude-monitor, got %s", result.ServerInfo.Name)
	}
	if result.ServerInfo.Version != "test-version" {
		t.Errorf("expected version test-version, got %s", result.ServerInfo.Version)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability to be non-nil")
	}
}

func TestNotificationNoResponse(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	output := runServer(t, db, input)

	if strings.TrimSpace(output) != "" {
		t.Errorf("expected no output for notification, got %q", output)
	}
}

func TestToolsList(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolsListResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal ToolsListResult: %v", err)
	}
	if len(result.Tools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(result.Tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}
	for _, expected := range []string{"get_spending", "list_sessions", "get_session", "get_most_expensive"} {
		if !toolNames[expected] {
			t.Errorf("expected tool %q in list", expected)
		}
	}
}

func TestGetSpending(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_spending","arguments":{"window":"all"}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal ToolResult: %v", err)
	}

	if result.IsError {
		t.Fatalf("tool returned error: %s", result.Content[0].Text)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Spending Summary (all)") {
		t.Errorf("expected 'Spending Summary (all)' in output, got:\n%s", text)
	}
	if !strings.Contains(text, "$5.5000") {
		t.Errorf("expected total cost $5.5000 in output, got:\n%s", text)
	}
	if !strings.Contains(text, "Sessions: 3") {
		t.Errorf("expected 'Sessions: 3' in output, got:\n%s", text)
	}
}

func TestGetSpendingDefault(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	// No arguments — should default to "today".
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_spending","arguments":{}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resp := responses[0]
	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	text := result.Content[0].Text
	if !strings.Contains(text, "Spending Summary (today)") {
		t.Errorf("expected 'Spending Summary (today)' in output, got:\n%s", text)
	}
}

func TestListSessions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_sessions","arguments":{"limit":10}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	text := result.Content[0].Text
	if !strings.Contains(text, "Sessions (3)") {
		t.Errorf("expected 'Sessions (3)' in output, got:\n%s", text)
	}
	if !strings.Contains(text, "sess-1") {
		t.Errorf("expected sess-1 in output")
	}
	if !strings.Contains(text, "sess-2") {
		t.Errorf("expected sess-2 in output")
	}
	if !strings.Contains(text, "sess-3") {
		t.Errorf("expected sess-3 in output")
	}
}

func TestListSessionsByRepo(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_sessions","arguments":{"repo":"repo-beta"}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	text := result.Content[0].Text
	if !strings.Contains(text, "Sessions (1)") {
		t.Errorf("expected 'Sessions (1)' in output, got:\n%s", text)
	}
	if !strings.Contains(text, "sess-3") {
		t.Errorf("expected sess-3 in output")
	}
	if strings.Contains(text, "sess-1") || strings.Contains(text, "sess-2") {
		t.Errorf("did not expect sess-1 or sess-2 in repo-beta output")
	}
}

func TestGetSession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_session","arguments":{"session_id":"sess-2"}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if result.IsError {
		t.Fatalf("tool returned error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Session Details") {
		t.Errorf("expected 'Session Details' in output")
	}
	if !strings.Contains(text, "sess-2") {
		t.Errorf("expected sess-2 in output")
	}
	if !strings.Contains(text, "$3.5000") {
		t.Errorf("expected cost $3.5000 in output, got:\n%s", text)
	}
	if !strings.Contains(text, "add-feature") {
		t.Errorf("expected session name in output")
	}
	if !strings.Contains(text, "feature-branch") {
		t.Errorf("expected branch in output")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_session","arguments":{"session_id":"nonexistent"}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if !result.IsError {
		t.Error("expected IsError to be true for missing session")
	}
	if !strings.Contains(result.Content[0].Text, "Session not found") {
		t.Errorf("expected 'Session not found' in error, got: %s", result.Content[0].Text)
	}
}

func TestGetSessionMissingID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_session","arguments":{}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if !result.IsError {
		t.Error("expected IsError for missing session_id")
	}
	if !strings.Contains(result.Content[0].Text, "session_id is required") {
		t.Errorf("expected 'session_id is required', got: %s", result.Content[0].Text)
	}
}

func TestGetMostExpensive(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"get_most_expensive","arguments":{"limit":2,"window":"all"}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if result.IsError {
		t.Fatalf("tool returned error: %s", result.Content[0].Text)
	}

	text := result.Content[0].Text
	if !strings.Contains(text, "Most Expensive Sessions") {
		t.Errorf("expected 'Most Expensive Sessions' in output")
	}
	// sess-2 ($3.50) should be first, sess-1 ($1.25) second
	idx2 := strings.Index(text, "sess-2")
	idx1 := strings.Index(text, "sess-1")
	if idx2 < 0 || idx1 < 0 {
		t.Fatalf("expected both sess-2 and sess-1 in output, got:\n%s", text)
	}
	if idx2 > idx1 {
		t.Errorf("expected sess-2 before sess-1 (sorted by cost desc)")
	}
	// sess-3 should NOT appear since limit=2
	if strings.Contains(text, "sess-3") {
		t.Errorf("did not expect sess-3 with limit=2")
	}
}

func TestUnknownMethod(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := `{"jsonrpc":"2.0","id":10,"method":"unknown/method","params":{}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "Method not found") {
		t.Errorf("expected 'Method not found' in message, got: %s", resp.Error.Message)
	}
}

func TestUnknownTool(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"nonexistent_tool","arguments":{}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if !result.IsError {
		t.Error("expected IsError for unknown tool")
	}
	if !strings.Contains(result.Content[0].Text, "Unknown tool") {
		t.Errorf("expected 'Unknown tool' in error, got: %s", result.Content[0].Text)
	}
}

func TestParseError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := "this is not json\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected parse error")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("expected code -32700, got %d", resp.Error.Code)
	}
}

func TestMultipleRequests(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_spending","arguments":{"window":"all"}}}`,
	}, "\n") + "\n"

	output := runServer(t, db, input)
	responses := parseResponses(t, output)

	// Should have 3 responses (notification produces none).
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d:\n%s", len(responses), output)
	}

	// Check IDs match.
	ids := make([]float64, len(responses))
	for i, r := range responses {
		ids[i] = r.ID.(float64)
	}
	if ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Errorf("expected IDs [1,2,3], got %v", ids)
	}
}

func TestEmptyDatabase(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
	if !strings.Contains(result.Content[0].Text, "No sessions found") {
		t.Errorf("expected 'No sessions found', got: %s", result.Content[0].Text)
	}
}

func TestFormatTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000 (1k)"},
		{12345, "12,345 (12k)"},
		{999999, "999,999 (999k)"},
		{1000000, "1,000,000 (1.0M)"},
		{1500000, "1,500,000 (1.5M)"},
		{12345678, "12,345,678 (12.3M)"},
	}
	for _, tt := range tests {
		got := formatTokens(tt.input)
		if got != tt.expected {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatWithCommas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{12, "12"},
		{123, "123"},
		{1234, "1,234"},
		{12345, "12,345"},
		{123456, "123,456"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		got := formatWithCommas(tt.input)
		if got != tt.expected {
			t.Errorf("formatWithCommas(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestInvalidToolCallParams(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not an object"}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resp := responses[0]
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected code -32602, got %d", resp.Error.Code)
	}
}

func TestListSessionsLimitClamping(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	// Limit > 100 should be clamped.
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":{"limit":500}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	// Should not error — just clamped limit.
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestGetMostExpensiveLimitClamping(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	seedTestData(t, db)

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_most_expensive","arguments":{"limit":200}}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resultBytes, _ := json.Marshal(responses[0].Result)
	var result ToolResult
	json.Unmarshal(resultBytes, &result)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].Text)
	}
}

func TestStringID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	input := `{"jsonrpc":"2.0","id":"abc-123","method":"initialize","params":{}}` + "\n"
	output := runServer(t, db, input)

	responses := parseResponses(t, output)
	resp := responses[0]
	idStr, ok := resp.ID.(string)
	if !ok || idStr != "abc-123" {
		t.Errorf("expected string id 'abc-123', got %v (%T)", resp.ID, resp.ID)
	}
}
