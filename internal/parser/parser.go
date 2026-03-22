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
	Timestamp time.Time       `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	UUID      string          `json:"uuid"`
	// Top-level content for non-wrapped messages (e.g. tool_use lines).
	Content    json.RawMessage `json:"content"`
	ParentUUID string          `json:"parentUuid"`
	CWD        string          `json:"cwd"`
	GitBranch  string          `json:"gitBranch"`
	IsSidechain bool            `json:"isSidechain"`
	Data        rawProgressData `json:"data"`
}

type rawProgressData struct {
	Type      string `json:"type"`
	HookEvent string `json:"hookEvent"`
	HookName  string `json:"hookName"`
	Command   string `json:"command"`
}

type rawInner struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage      rawUsage        `json:"usage"`
	Model      string          `json:"model"`
	StopReason *string         `json:"stop_reason"`
}

type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// contentBlock represents one element inside a content array.
type contentBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`    // for tool_use blocks
	Content string          `json:"content,omitempty"` // for tool_result blocks (string form)
	Input   json.RawMessage `json:"input,omitempty"`   // for tool_use blocks (raw input params)
}

// ParsedMessage is the normalised representation of one JSONL line.
type ParsedMessage struct {
	Type         string    `json:"type"`
	MessageID    string    `json:"messageId,omitempty"`
	Role         string    `json:"role"`
	ContentText  string    `json:"contentText"`            // extracted plain-text preview (truncated)
	FullContent  string    `json:"fullContent,omitempty"`  // full untruncated content (for expand)
	ToolName     string    `json:"toolName,omitempty"`
	CostUSD      float64   `json:"costUSD"`
	InputTokens  int64     `json:"inputTokens"`
	OutputTokens int64     `json:"outputTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	Timestamp    time.Time `json:"timestamp"`
	SessionID    string    `json:"sessionId"`
	UUID         string    `json:"uuid"`
	ParentUUID   string    `json:"parentUuid,omitempty"`
	CWD          string    `json:"cwd,omitempty"`
	GitBranch    string    `json:"gitBranch,omitempty"`
	Model        string    `json:"model,omitempty"`
	IsSidechain  bool      `json:"isSidechain,omitempty"`
	StopReason   string    `json:"stopReason,omitempty"`
	HookEvent    string    `json:"hookEvent,omitempty"`
	HookName     string    `json:"hookName,omitempty"`
	ToolDetail   string    `json:"toolDetail,omitempty"` // extra context for Agent/Skill calls
}

// IsConversationMessage returns true if this message represents a real
// conversation event (user or assistant turn), as opposed to metadata lines
// like progress, system, file-history-snapshot, agent-name, etc.
func (m *ParsedMessage) IsConversationMessage() bool {
	switch m.Type {
	case "assistant", "user", "human":
		return true
	default:
		return false
	}
}

type modelPricing struct {
	InputPerMTok       float64
	OutputPerMTok      float64
	CacheReadPerMTok   float64
	CacheCreatePerMTok float64
}

var pricingTable = map[string]modelPricing{
	"claude-opus-4-6":   {15.0, 75.0, 1.50, 18.75},
	"claude-sonnet-4-6": {3.0, 15.0, 0.30, 3.75},
	"claude-haiku-4-5":  {0.80, 4.0, 0.08, 1.0},
}

var defaultPricing = pricingTable["claude-sonnet-4-6"]

func computeCost(model string, usage rawUsage) float64 {
	p, ok := pricingTable[model]
	if !ok {
		p = defaultPricing
	}
	return float64(usage.InputTokens)*p.InputPerMTok/1e6 +
		float64(usage.OutputTokens)*p.OutputPerMTok/1e6 +
		float64(usage.CacheReadInputTokens)*p.CacheReadPerMTok/1e6 +
		float64(usage.CacheCreationInputTokens)*p.CacheCreatePerMTok/1e6
}

// ParseLine unmarshals a single JSONL line and returns a ParsedMessage.
func ParseLine(line []byte) (*ParsedMessage, error) {
	var raw rawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	usage := raw.Message.Usage
	cacheReadTokens := usage.CacheReadInputTokens
	cacheCreationTokens := usage.CacheCreationInputTokens

	cost := computeCost(raw.Message.Model, usage)

	msg := &ParsedMessage{
		Type:         raw.Type,
		MessageID:    raw.Message.ID,
		Role:         raw.Message.Role,
		CostUSD:      cost,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheReadTokens:     cacheReadTokens,
		CacheCreationTokens: cacheCreationTokens,
		Timestamp:    raw.Timestamp,
		SessionID:    raw.SessionID,
		UUID:         raw.UUID,
	}

	msg.ParentUUID = raw.ParentUUID
	msg.CWD = raw.CWD
	msg.GitBranch = raw.GitBranch
	msg.Model = raw.Message.Model
	msg.IsSidechain = raw.IsSidechain
	if raw.Message.StopReason != nil {
		msg.StopReason = *raw.Message.StopReason
	}
	// Extract hook data from progress messages.
	if raw.Type == "progress" && raw.Data.Type == "hook_progress" {
		msg.HookEvent = raw.Data.HookEvent
		msg.HookName = raw.Data.HookName
		// Extract tool name from hookName (e.g. "PostToolUse:Read" → "Read")
		hookTool := raw.Data.HookName
		if i := len(raw.Data.HookEvent); i < len(raw.Data.HookName) && raw.Data.HookName[i] == ':' {
			hookTool = raw.Data.HookName[i+1:]
		}
		msg.ContentText = fmt.Sprintf("[hook:%s] %s", raw.Data.HookEvent, hookTool)
	}

	// Skip content extraction for hook messages (contentText already set above).
	if msg.HookEvent == "" {
		contentRaw := raw.Message.Content
		if len(contentRaw) == 0 {
			contentRaw = raw.Content
		}
		msg.ContentText, msg.ToolName, msg.FullContent, msg.ToolDetail = extractContent(contentRaw)
	}

	return msg, nil
}

// extractContent attempts to decode content as a string first, then as a
// content-block array. Returns (textPreview, toolName, fullText, toolDetail).
func extractContent(raw json.RawMessage) (string, string, string, string) {
	if len(raw) == 0 {
		return "", "", "", ""
	}

	// Try plain string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if len([]rune(s)) <= maxContentPreview {
			return s, "", "", ""
		}
		return truncate(s, maxContentPreview), "", s, ""
	}

	// Try array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", "", "", ""
	}

	var firstText string
	var fullText string
	var toolName string
	var toolDetail string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" && firstText == "" {
				firstText = truncate(b.Text, maxContentPreview)
				if len([]rune(b.Text)) > maxContentPreview {
					fullText = b.Text
				}
			}
		case "thinking":
			if firstText == "" {
				firstText = "[thinking...]"
			}
		case "tool_use":
			toolName = b.Name
			if firstText == "" {
				firstText = fmt.Sprintf("[tool: %s]", b.Name)
			}
			// Extract tool input detail.
			if len(b.Input) > 0 {
				var inp map[string]interface{}
				if json.Unmarshal(b.Input, &inp) == nil {
					switch b.Name {
					case "Agent":
						desc, _ := inp["description"].(string)
						st, _ := inp["subagent_type"].(string)
						name, _ := inp["name"].(string)
						parts := []string{}
						if st != "" { parts = append(parts, st) }
						if name != "" { parts = append(parts, name) }
						if desc != "" { parts = append(parts, desc) }
						if len(parts) > 0 {
							toolDetail = joinParts(parts)
							firstText = fmt.Sprintf("[agent: %s]", toolDetail)
						}
					case "Skill":
						skill, _ := inp["skill"].(string)
						if skill != "" {
							toolDetail = skill
							firstText = fmt.Sprintf("[skill: %s]", skill)
						}
					case "Bash":
						cmd, _ := inp["command"].(string)
						desc, _ := inp["description"].(string)
						if desc != "" { toolDetail = desc } else { toolDetail = truncate(cmd, 120) }
					case "Read":
						fp, _ := inp["file_path"].(string)
						toolDetail = fp
					case "Write":
						fp, _ := inp["file_path"].(string)
						toolDetail = fp
					case "Edit":
						fp, _ := inp["file_path"].(string)
						toolDetail = fp
					case "Grep":
						pat, _ := inp["pattern"].(string)
						toolDetail = pat
					case "Glob":
						pat, _ := inp["pattern"].(string)
						toolDetail = pat
					default:
						// Generic: stringify first key-value pair
						summary := truncate(string(b.Input), 100)
						toolDetail = summary
					}
				}
			}
		case "tool_result":
			if firstText == "" {
				if b.Content != "" {
					firstText = truncate(b.Content, maxContentPreview)
					if len([]rune(b.Content)) > maxContentPreview {
						fullText = b.Content
					}
				} else {
					firstText = "[tool_result]"
				}
			}
		}
	}
	if toolName != "" {
		if firstText == "" {
			firstText = fmt.Sprintf("[tool: %s]", toolName)
		}
		return firstText, toolName, fullText, toolDetail
	}
	return firstText, "", fullText, toolDetail
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " / "
		}
		result += p
	}
	return result
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
