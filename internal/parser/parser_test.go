package parser

import (
	"math"
	"testing"
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
	if msg.CacheTokens != 11376+8945 {
		t.Errorf("CacheTokens = %d, want %d", msg.CacheTokens, 11376+8945)
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

	if msg.ContentText != "[tool_result]" {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "[tool_result]")
	}
	if msg.CostUSD != 0 {
		t.Errorf("CostUSD = %g, want 0", msg.CostUSD)
	}
}

func TestIsConversationMessage(t *testing.T) {
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
			msg := &ParsedMessage{Type: tc.msgType}
			got := msg.IsConversationMessage()
			if got != tc.expected {
				t.Errorf("IsConversationMessage() for type %q = %v, want %v", tc.msgType, got, tc.expected)
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
