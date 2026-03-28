package parser

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseLine_AssistantWithUsage(t *testing.T) {
	line := []byte(`{
		"type": "assistant",
		"message": {
			"id": "msg_abc123",
			"role": "assistant",
			"content": [{"type": "text", "text": "Hello, world!"}],
			"usage": {
				"input_tokens": 100,
				"output_tokens": 360,
				"cache_creation_input_tokens": 11376,
				"cache_read_input_tokens": 8945
			}
		},
		"timestamp": "2026-03-21T10:00:00Z",
		"sessionId": "sess-001",
		"uuid": "uuid-001"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != "assistant" {
		t.Errorf("Type = %q, want %q", msg.Type, "assistant")
	}
	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want %q", msg.Role, "assistant")
	}
	if msg.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want %d", msg.InputTokens, 100)
	}
	if msg.OutputTokens != 360 {
		t.Errorf("OutputTokens = %d, want %d", msg.OutputTokens, 360)
	}
	if msg.CacheReadTokens != 8945 {
		t.Errorf("CacheReadTokens = %d, want %d", msg.CacheReadTokens, 8945)
	}
	if msg.CacheCreationTokens != 11376 {
		t.Errorf("CacheCreationTokens = %d, want %d", msg.CacheCreationTokens, 11376)
	}

	expectedCost := float64(100)*3.0/1e6 + float64(360)*15.0/1e6 + float64(8945)*0.30/1e6 + float64(11376)*3.75/1e6
	if math.Abs(msg.CostUSD-expectedCost) > 1e-12 {
		t.Errorf("CostUSD = %g, want %g", msg.CostUSD, expectedCost)
	}

	if msg.ContentText != "Hello, world!" {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "Hello, world!")
	}
	if msg.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", msg.SessionID, "sess-001")
	}
	if msg.MessageID != "msg_abc123" {
		t.Errorf("MessageID = %q, want %q", msg.MessageID, "msg_abc123")
	}
}

func TestParseLine_UserMessage(t *testing.T) {
	line := []byte(`{
		"type": "human",
		"message": {
			"role": "user",
			"content": "What is Go?"
		},
		"timestamp": "2026-03-21T10:01:00Z",
		"sessionId": "sess-001",
		"uuid": "uuid-002"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if msg.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", msg.InputTokens)
	}
	if msg.CostUSD != 0 {
		t.Errorf("CostUSD = %g, want 0", msg.CostUSD)
	}
	if msg.ContentText != "What is Go?" {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "What is Go?")
	}
	if msg.MessageID != "" {
		t.Errorf("MessageID = %q, want empty", msg.MessageID)
	}
}

func TestParseLine_SystemTurnDuration(t *testing.T) {
	line := []byte(`{
		"type": "system",
		"subtype": "turn_duration",
		"message": {
			"role": "system",
			"content": "Turn completed in 5.2s"
		},
		"timestamp": "2026-03-21T10:02:00Z",
		"sessionId": "sess-001",
		"uuid": "uuid-003"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != "system" {
		t.Errorf("Type = %q, want %q", msg.Type, "system")
	}
	if msg.CostUSD != 0 {
		t.Errorf("CostUSD = %g, want 0", msg.CostUSD)
	}
}

func TestParseLine_ProgressMessage(t *testing.T) {
	line := []byte(`{
		"type": "progress",
		"message": {
			"role": "assistant",
			"content": "Thinking..."
		},
		"timestamp": "2026-03-21T10:03:00Z",
		"sessionId": "sess-001",
		"uuid": "uuid-004"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Type != "progress" {
		t.Errorf("Type = %q, want %q", msg.Type, "progress")
	}
	if msg.ContentText != "Thinking..." {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "Thinking...")
	}
}

func TestParseLine_ToolUse(t *testing.T) {
	line := []byte(`{
		"type": "assistant",
		"message": {
			"role": "assistant",
			"content": [
				{"type": "tool_use", "name": "Read", "id": "tu_1", "input": {}}
			],
			"usage": {
				"input_tokens": 50,
				"output_tokens": 20,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens": 0
			}
		},
		"timestamp": "2026-03-21T10:04:00Z",
		"sessionId": "sess-001",
		"uuid": "uuid-005"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.ToolName != "Read" {
		t.Errorf("ToolName = %q, want %q", msg.ToolName, "Read")
	}
	if msg.ContentText != "[tool: Read]" {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "[tool: Read]")
	}
	if msg.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", msg.InputTokens)
	}
}

func TestParseLine_ToolResult(t *testing.T) {
	line := []byte(`{
		"type": "human",
		"message": {
			"role": "user",
			"content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "file contents here"}
			]
		},
		"timestamp": "2026-03-21T10:05:00Z",
		"sessionId": "sess-001",
		"uuid": "uuid-006"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.ContentText != "file contents here" {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "file contents here")
	}
	if msg.CostUSD != 0 {
		t.Errorf("CostUSD = %g, want 0", msg.CostUSD)
	}
}

func TestIsConversationTurn(t *testing.T) {
	tests := []struct {
		name     string
		msgType  string
		expected bool
	}{
		{"assistant is conversation", "assistant", true},
		{"user is conversation", "user", true},
		{"human is conversation", "human", true},
		{"progress is not", "progress", false},
		{"system is not", "system", false},
		{"file-history-snapshot is not", "file-history-snapshot", false},
		{"agent-name is not", "agent-name", false},
		{"custom-title is not", "custom-title", false},
		{"last-prompt is not", "last-prompt", false},
		{"queue-operation is not", "queue-operation", false},
		{"empty is not", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := &Event{Type: tc.msgType}
			got := msg.IsConversationTurn()
			if got != tc.expected {
				t.Errorf("IsConversationTurn() for type %q = %v, want %v", tc.msgType, got, tc.expected)
			}
		})
	}
}

func TestMessageIDExtraction(t *testing.T) {
	tests := []struct {
		name     string
		jsonLine string
		wantID   string
	}{
		{
			name:     "assistant with message ID",
			jsonLine: `{"type":"assistant","message":{"id":"msg_123","role":"assistant","content":"hello","usage":{"input_tokens":10,"output_tokens":5}},"timestamp":"2025-01-01T00:00:00Z"}`,
			wantID:   "msg_123",
		},
		{
			name:     "user without message ID",
			jsonLine: `{"type":"user","message":{"role":"user","content":"hi"},"timestamp":"2025-01-01T00:00:00Z"}`,
			wantID:   "",
		},
		{
			name:     "progress has no message ID",
			jsonLine: `{"type":"progress","message":{},"timestamp":"2025-01-01T00:00:00Z"}`,
			wantID:   "",
		},
		{
			name:     "streaming chunks share same ID",
			jsonLine: `{"type":"assistant","message":{"id":"msg_456","role":"assistant","content":"chunk","usage":{"input_tokens":0,"output_tokens":2}},"timestamp":"2025-01-01T00:00:01Z"}`,
			wantID:   "msg_456",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := ParseLine([]byte(tc.jsonLine))
			if err != nil {
				t.Fatalf("ParseLine() error: %v", err)
			}
			if msg.MessageID != tc.wantID {
				t.Errorf("MessageID = %q, want %q", msg.MessageID, tc.wantID)
			}
		})
	}
}

func TestParseLine_ContentBlockArray(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"This is a text block."}]},"sessionId":"sess-3","uuid":"uuid-3","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "This is a text block." {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "This is a text block.")
	}
	if msg.ToolName != "" {
		t.Errorf("ToolName: got %q, want empty", msg.ToolName)
	}
}

func TestParseLine_ToolUseBlock(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tool-1"}]},"sessionId":"sess-4","uuid":"uuid-4","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ToolName != "Bash" {
		t.Errorf("ToolName: got %q, want %q", msg.ToolName, "Bash")
	}
	if msg.ContentText != "[tool: Bash]" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "[tool: Bash]")
	}
}

func TestParseLine_ToolResultBlock(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"tool_result","content":[{"type":"tool_result","text":"some output"}],"sessionId":"sess-5","uuid":"uuid-5","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "some output" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "some output")
	}
}

func TestParseLine_ToolResultStringContent(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"file contents here","tool_use_id":"tu_1"}]},"sessionId":"sess-tr","uuid":"uuid-tr","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "file contents here" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "file contents here")
	}
	if msg.ForToolUseID != "tu_1" {
		t.Errorf("ForToolUseID: got %q, want %q", msg.ForToolUseID, "tu_1")
	}
}

func TestParseLine_ToolResultArrayContent(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"}],"tool_use_id":"tu_2"}]},"sessionId":"sess-tra","uuid":"uuid-tra","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "line one line two" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "line one line two")
	}
}

func TestParseLine_ToolUseFullContent(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"echo hello world this is a long command that should be stored as full content for expansion in the UI dashboard when users click the expand button to see the full tool input parameters and details","description":"Run a long echo command for testing purposes"}}]},"sessionId":"sess-fc","uuid":"uuid-fc","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.FullContent == "" {
		t.Error("FullContent should be set for tool_use with large input")
	}
	if msg.ToolName != "Bash" {
		t.Errorf("ToolName: got %q, want %q", msg.ToolName, "Bash")
	}
}

func TestParseLine_AgentIsAgent(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_agent1","name":"Agent","input":{"description":"Find files","subagent_type":"Explore","prompt":"search for config files"}}]},"sessionId":"sess-ag","uuid":"uuid-ag","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !msg.IsAgent {
		t.Error("IsAgent should be true for Agent tool")
	}
	if msg.ToolName != "Agent" {
		t.Errorf("ToolName: got %q, want %q", msg.ToolName, "Agent")
	}
	if msg.ToolUseID != "toolu_agent1" {
		t.Errorf("ToolUseID: got %q, want %q", msg.ToolUseID, "toolu_agent1")
	}
}

func TestParseLine_MalformedJSON(t *testing.T) {
	t.Parallel()
	line := []byte(`{not valid json`)
	_, err := ParseLine(line)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestParseLine_EmptyLine(t *testing.T) {
	t.Parallel()
	_, err := ParseLine([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty line, got nil")
	}
}

func TestParseLine_PreferMessageContent(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"user","message":{"role":"user","content":"from message"},"content":"from top-level","sessionId":"sess-7","uuid":"uuid-7","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "from message" {
		t.Errorf("ContentText: got %q, want %q (should prefer message.content)", msg.ContentText, "from message")
	}
}

func TestParseLine_ContentTruncationAt200Runes(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 300)
	line := []byte(`{"type":"user","message":{"role":"user","content":"` + long + `"},"sessionId":"sess-8","uuid":"uuid-8","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len([]rune(msg.ContentText)) != 200 {
		t.Errorf("ContentText rune count: got %d, want 200", len([]rune(msg.ContentText)))
	}
}

func TestParseLine_RealWorldSample(t *testing.T) {
	t.Parallel()
	// Real Claude Code JSONL: usage is inside message, cost computed from tokens
	line := []byte(`{"type":"assistant","message":{"id":"msg_01XyZ","type":"message","role":"assistant","content":[{"type":"text","text":"I'll help you with that task."}],"model":"claude-opus-4-5","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":512,"output_tokens":64,"cache_creation_input_tokens":0,"cache_read_input_tokens":128}},"isSidechain":false,"sessionId":"abc123def456","uuid":"550e8400-e29b-41d4-a716-446655440000","timestamp":"2024-06-15T10:30:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Type != "assistant" {
		t.Errorf("Type: got %q, want assistant", msg.Type)
	}
	if msg.Role != "assistant" {
		t.Errorf("Role: got %q, want assistant", msg.Role)
	}
	if msg.SessionID != "abc123def456" {
		t.Errorf("SessionID: got %q, want abc123def456", msg.SessionID)
	}
	if msg.InputTokens != 512 {
		t.Errorf("InputTokens: got %d, want 512", msg.InputTokens)
	}
	if msg.CacheReadTokens != 128 {
		t.Errorf("CacheReadTokens: got %d, want 128", msg.CacheReadTokens)
	}
	// Cost computed from tokens: 512*3/1e6 + 64*15/1e6 + 128*0.30/1e6
	expectedCost := float64(512)*3.0/1e6 + float64(64)*15.0/1e6 + float64(128)*0.30/1e6
	if math.Abs(msg.CostUSD-expectedCost) > 1e-12 {
		t.Errorf("CostUSD: got %g, want %g", msg.CostUSD, expectedCost)
	}
	wantContent := "I'll help you with that task."
	if msg.ContentText != wantContent {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, wantContent)
	}
	wantTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	if !msg.Timestamp.Equal(wantTime) {
		t.Errorf("Timestamp: got %v, want %v", msg.Timestamp, wantTime)
	}
}

func TestComputeCost_ByModel(t *testing.T) {
	t.Parallel()
	usage := rawUsage{
		InputTokens:              1000000,
		OutputTokens:             1000000,
		CacheReadInputTokens:     1000000,
		CacheCreationInputTokens: 1000000,
	}

	tests := []struct {
		name     string
		model    string
		wantCost float64
	}{
		{"opus", "claude-opus-4-6", 5.0 + 25.0 + 0.50 + 6.25},
		{"sonnet", "claude-sonnet-4-6", 3.0 + 15.0 + 0.30 + 3.75},
		{"haiku", "claude-haiku-4-5", 1.0 + 5.0 + 0.10 + 1.25},
		{"haiku versioned", "claude-haiku-4-5-20251001", 1.0 + 5.0 + 0.10 + 1.25},
		{"unknown falls back to sonnet", "claude-unknown-99", 3.0 + 15.0 + 0.30 + 3.75},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeCost(tc.model, usage)
			if math.Abs(got-tc.wantCost) > 1e-9 {
				t.Errorf("computeCost(%q) = %g, want %g", tc.model, got, tc.wantCost)
			}
		})
	}
}

func TestParseLine_OpusPricing(t *testing.T) {
	t.Parallel()
	line := []byte(`{
		"type": "assistant",
		"message": {
			"id": "msg_opus1",
			"role": "assistant",
			"model": "claude-opus-4-6",
			"content": [{"type": "text", "text": "Opus response"}],
			"usage": {
				"input_tokens": 100,
				"output_tokens": 200,
				"cache_creation_input_tokens": 300,
				"cache_read_input_tokens": 400
			}
		},
		"timestamp": "2026-03-21T12:00:00Z",
		"sessionId": "sess-opus",
		"uuid": "uuid-opus"
	}`)

	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCost := float64(100)*5.0/1e6 +
		float64(200)*25.0/1e6 +
		float64(400)*0.50/1e6 +
		float64(300)*6.25/1e6
	if math.Abs(msg.CostUSD-expectedCost) > 1e-12 {
		t.Errorf("CostUSD = %g, want %g (should use Opus pricing)", msg.CostUSD, expectedCost)
	}
	if msg.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", msg.Model, "claude-opus-4-6")
	}
}

func TestParseLine_ToolResultIsError(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"human","message":{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"Error: command not found","tool_use_id":"tu_err"}]},"sessionId":"sess-err","uuid":"uuid-err","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !msg.IsError {
		t.Error("IsError should be true for tool_result with is_error:true")
	}
	if msg.ForToolUseID != "tu_err" {
		t.Errorf("ForToolUseID: got %q, want %q", msg.ForToolUseID, "tu_err")
	}
}

func TestParseLine_ToolResultErrorContent(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"human","message":{"role":"user","content":[{"type":"tool_result","content":"Error: file not found","tool_use_id":"tu_err2"}]},"sessionId":"sess-err2","uuid":"uuid-err2","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !msg.IsError {
		t.Error("IsError should be true for tool_result with error content prefix")
	}
}

func TestParseLine_ToolResultNotError(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"human","message":{"role":"user","content":[{"type":"tool_result","content":"file contents here","tool_use_id":"tu_ok"}]},"sessionId":"sess-ok","uuid":"uuid-ok","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.IsError {
		t.Error("IsError should be false for normal tool_result")
	}
}

func TestParseLine_TopLevelContentFallback(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"tool_result","content":"tool output here","sessionId":"sess-9","uuid":"uuid-9","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "tool output here" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "tool output here")
	}
}
