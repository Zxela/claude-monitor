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

	// Verify cost calculation:
	// input: 100 * 3.0 / 1e6 = 0.0003
	// output: 360 * 15.0 / 1e6 = 0.0054
	// cache_read: 8945 * 0.30 / 1e6 = 0.0026835
	// cache_creation: 11376 * 3.75 / 1e6 = 0.042660
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
	if msg.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0", msg.OutputTokens)
	}
	if msg.CostUSD != 0 {
		t.Errorf("CostUSD = %g, want 0", msg.CostUSD)
	}
	if msg.ContentText != "What is Go?" {
		t.Errorf("ContentText = %q, want %q", msg.ContentText, "What is Go?")
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
