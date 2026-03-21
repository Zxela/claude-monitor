// Package parser parses individual JSONL lines from Claude Code session files.
package parser

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

const maxContentPreview = 200

// rawMessage mirrors the on-disk JSONL structure. Content can be a string or
// an array of content blocks, so we capture it as raw JSON for flexible decoding.
type rawMessage struct {
	Type      string          `json:"type"`
	Message   rawInner        `json:"message"`
	CostUSD   float64         `json:"costUSD"`
	Usage     rawUsage        `json:"usage"`
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	UUID      string          `json:"uuid"`
	// Top-level content for non-wrapped messages (e.g. tool_use lines).
	Content json.RawMessage `json:"content"`
}

type rawInner struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type rawUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
}

// contentBlock represents one element inside a content array.
type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Name  string `json:"name,omitempty"` // for tool_use blocks
}

// ParsedMessage is the normalised representation of one JSONL line.
type ParsedMessage struct {
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	ContentText  string    `json:"contentText"` // extracted plain-text preview
	ToolName     string    `json:"toolName,omitempty"`
	CostUSD      float64   `json:"costUSD"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheTokens  int64     `json:"cacheTokens"`
	Timestamp    time.Time `json:"timestamp"`
	SessionID    string    `json:"sessionId"`
	UUID         string    `json:"uuid"`
}

// ParseLine unmarshals a single JSONL line and returns a ParsedMessage.
func ParseLine(line []byte) (*ParsedMessage, error) {
	var raw rawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	msg := &ParsedMessage{
		Type:         raw.Type,
		Role:         raw.Message.Role,
		CostUSD:      raw.CostUSD,
		InputTokens:  raw.Usage.InputTokens,
		OutputTokens: raw.Usage.OutputTokens,
		CacheTokens:  raw.Usage.CacheReadInputTokens + raw.Usage.CacheWriteInputTokens,
		Timestamp:    raw.Timestamp,
		SessionID:    raw.SessionID,
		UUID:         raw.UUID,
	}

	// Prefer message.content, fall back to top-level content.
	contentRaw := raw.Message.Content
	if len(contentRaw) == 0 {
		contentRaw = raw.Content
	}

	msg.ContentText, msg.ToolName = extractContent(contentRaw)

	return msg, nil
}

// extractContent attempts to decode content as a string first, then as a
// content-block array. Returns (textPreview, toolName).
func extractContent(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}

	// Try plain string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return truncate(s, maxContentPreview), ""
	}

	// Try array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", ""
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				return truncate(b.Text, maxContentPreview), ""
			}
		case "tool_use":
			return fmt.Sprintf("[tool: %s]", b.Name), b.Name
		case "tool_result":
			return "[tool_result]", ""
		}
	}

	return "", ""
}

// truncate returns at most n runes from s (valid UTF-8 boundary aware).
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
