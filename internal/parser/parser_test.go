package parser

import (
	"strings"
	"testing"
	"time"
)

func TestParseLine_ValidUserMessage(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"user","message":{"role":"user","content":"Hello, world!"},"sessionId":"sess-1","uuid":"uuid-1","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Type != "user" {
		t.Errorf("Type: got %q, want %q", msg.Type, "user")
	}
	if msg.Role != "user" {
		t.Errorf("Role: got %q, want %q", msg.Role, "user")
	}
	if msg.ContentText != "Hello, world!" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "Hello, world!")
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("SessionID: got %q, want %q", msg.SessionID, "sess-1")
	}
}

func TestParseLine_AssistantMessageWithTokenUsage(t *testing.T) {
	t.Parallel()
	line := []byte(`{
		"type":"assistant",
		"message":{"role":"assistant","content":"Here is the answer."},
		"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":20,"cache_write_input_tokens":10},
		"costUSD":0.005,
		"sessionId":"sess-2",
		"uuid":"uuid-2",
		"timestamp":"2024-01-01T00:00:00Z"
	}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", msg.InputTokens)
	}
	if msg.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", msg.OutputTokens)
	}
	// CacheTokens = cache_read + cache_write = 20 + 10 = 30
	if msg.CacheTokens != 30 {
		t.Errorf("CacheTokens: got %d, want 30", msg.CacheTokens)
	}
	if msg.CostUSD != 0.005 {
		t.Errorf("CostUSD: got %f, want 0.005", msg.CostUSD)
	}
}

func TestParseLine_AssistantMessageWithContentBlockArray(t *testing.T) {
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
	if msg.ContentText != "[tool_result]" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "[tool_result]")
	}
}

func TestParseLine_CostUSDAccumulated(t *testing.T) {
	t.Parallel()
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":"response"},"costUSD":1.23,"sessionId":"sess-6","uuid":"uuid-6","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.CostUSD != 1.23 {
		t.Errorf("CostUSD: got %f, want 1.23", msg.CostUSD)
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
	// message.content should be preferred over top-level content
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
	// Simulates a real Claude Code JSONL output line
	line := []byte(`{"type":"assistant","message":{"id":"msg_01XyZ","type":"message","role":"assistant","content":[{"type":"text","text":"I'll help you with that task."}],"model":"claude-opus-4-5","stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":512,"output_tokens":64,"cache_creation_input_tokens":0,"cache_read_input_tokens":128}},"isSidechain":false,"costUSD":0.00234,"durationMs":1200,"sessionId":"abc123def456","uuid":"550e8400-e29b-41d4-a716-446655440000","timestamp":"2024-06-15T10:30:00Z","usage":{"input_tokens":512,"output_tokens":64,"cache_read_input_tokens":128,"cache_write_input_tokens":0}}`)
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
	if msg.CostUSD != 0.00234 {
		t.Errorf("CostUSD: got %f, want 0.00234", msg.CostUSD)
	}
	if msg.InputTokens != 512 {
		t.Errorf("InputTokens: got %d, want 512", msg.InputTokens)
	}
	if msg.CacheTokens != 128 {
		t.Errorf("CacheTokens: got %d, want 128", msg.CacheTokens)
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

func TestParseLine_TopLevelContentFallback(t *testing.T) {
	t.Parallel()
	// When message.content is absent, top-level content should be used
	line := []byte(`{"type":"tool_result","content":"tool output here","sessionId":"sess-9","uuid":"uuid-9","timestamp":"2024-01-01T00:00:00Z"}`)
	msg, err := ParseLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.ContentText != "tool output here" {
		t.Errorf("ContentText: got %q, want %q", msg.ContentText, "tool output here")
	}
}
