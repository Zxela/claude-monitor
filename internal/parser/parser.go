// Package parser parses individual JSONL lines from Claude Code session files.
package parser

import (
	"encoding/json"
	"fmt"
	"strings"
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
	TeamName    string          `json:"teamName"`
	AgentName   string          `json:"agentName"`
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
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`       // for tool_use blocks
	ID        string          `json:"id,omitempty"`         // tool_use block ID
	Content   json.RawMessage `json:"content,omitempty"`    // for tool_result blocks (string or array)
	Input     json.RawMessage `json:"input,omitempty"`      // for tool_use blocks (raw input params)
	ToolUseID string          `json:"tool_use_id,omitempty"` // for tool_result blocks: links to tool_use
	IsError   bool            `json:"is_error,omitempty"`    // for tool_result blocks: true when tool errored
}

// Event is the normalised representation of one JSONL line.
type Event struct {
	Type         string    `json:"type"`
	MessageID    string    `json:"messageId,omitempty"`
	Role         string    `json:"role"`
	ContentText  string    `json:"contentPreview"`         // extracted plain-text preview (truncated)
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
	IsAgent      bool      `json:"isAgent,omitempty"`      // true when toolName is "Agent"
	ToolUseID    string    `json:"toolUseId,omitempty"`    // first tool_use block ID
	ToolUseIDs   []string  `json:"toolUseIds,omitempty"`   // all tool_use block IDs (for batched calls)
	ForToolUseID string    `json:"forToolUseId,omitempty"` // on tool_result: which tool_use this responds to
	IsError      bool      `json:"isError,omitempty"`      // true for tool_result with is_error or error content
	TeamName     string    `json:"teamName,omitempty"`     // team name for team agents
	AgentName    string    `json:"agentName,omitempty"`    // agent name within a team
}

// IsConversationTurn returns true if this message represents a real
// conversation event (user or assistant turn), as opposed to metadata lines
// like progress, system, file-history-snapshot, agent-name, etc.
func (m *Event) IsConversationTurn() bool {
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
	"claude-opus-4-6":   {5.0, 25.0, 0.50, 6.25},
	"claude-sonnet-4-6": {3.0, 15.0, 0.30, 3.75},
	"claude-haiku-4-5":  {1.0, 5.0, 0.10, 1.25},
}

var defaultPricing = pricingTable["claude-sonnet-4-6"]

func computeCost(model string, usage rawUsage) float64 {
	p, ok := pricingTable[model]
	if !ok {
		// Try prefix match for versioned model names (e.g. "claude-haiku-4-5-20251001")
		for key, pricing := range pricingTable {
			if len(model) > len(key) && model[:len(key)] == key {
				p = pricing
				ok = true
				break
			}
		}
		if !ok {
			p = defaultPricing
		}
	}
	return float64(usage.InputTokens)*p.InputPerMTok/1e6 +
		float64(usage.OutputTokens)*p.OutputPerMTok/1e6 +
		float64(usage.CacheReadInputTokens)*p.CacheReadPerMTok/1e6 +
		float64(usage.CacheCreationInputTokens)*p.CacheCreatePerMTok/1e6
}

// ParseLine unmarshals a single JSONL line and returns an Event.
func ParseLine(line []byte) (*Event, error) {
	var raw rawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	usage := raw.Message.Usage
	cacheReadTokens := usage.CacheReadInputTokens
	cacheCreationTokens := usage.CacheCreationInputTokens

	cost := computeCost(raw.Message.Model, usage)

	msg := &Event{
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
	msg.TeamName = raw.TeamName
	msg.AgentName = raw.AgentName
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
		ci := extractContent(contentRaw)
		msg.ContentText = ci.text
		msg.ToolName = ci.toolName
		msg.FullContent = ci.fullText
		msg.ToolDetail = ci.toolDetail
		msg.ToolUseID = ci.toolUseID
		msg.ForToolUseID = ci.forToolUseID
		msg.IsAgent = msg.ToolName == "Agent"
		msg.IsError = ci.isError
		// Also detect error indicators in content text when not already flagged.
		if !msg.IsError && msg.ForToolUseID != "" {
			lower := strings.ToLower(ci.text)
			if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "error ") ||
				strings.Contains(lower, "command failed") || strings.Contains(lower, "exited with error") {
				msg.IsError = true
			}
		}
		// Populate ToolUseIDs for batched tool calls
		if len(ci.toolUseAll) > 1 {
			ids := make([]string, len(ci.toolUseAll))
			for i, tu := range ci.toolUseAll {
				ids[i] = tu.ID
			}
			msg.ToolUseIDs = ids
		}
	}

	return msg, nil
}

// toolUseEntry records metadata for a single tool_use block in a content array.
type toolUseEntry struct {
	ID      string // tool_use block ID
	Name    string // tool name (e.g. "Agent", "Bash")
	IsAgent bool
}

// contentInfo bundles everything extracted from a content field.
type contentInfo struct {
	text         string // preview text (truncated)
	toolName     string
	fullText     string // untruncated content
	toolDetail   string
	toolUseID    string         // first tool_use block ID
	toolUseAll   []toolUseEntry // all tool_use blocks (for batched calls)
	forToolUseID string         // tool_use_id from tool_result block
	isError      bool           // true if tool_result has is_error or error content
}

// extractContent attempts to decode content as a string first, then as a
// content-block array.
func extractContent(raw json.RawMessage) contentInfo {
	if len(raw) == 0 {
		return contentInfo{}
	}

	// Try plain string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if len([]rune(s)) <= maxContentPreview {
			return contentInfo{text: s}
		}
		return contentInfo{text: truncate(s, maxContentPreview), fullText: s}
	}

	// Try array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return contentInfo{}
	}

	var info contentInfo
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" && info.text == "" {
				info.text = truncate(b.Text, maxContentPreview)
				if len([]rune(b.Text)) > maxContentPreview {
					info.fullText = b.Text
				}
			}
		case "thinking":
			if info.text == "" {
				info.text = "[thinking...]"
			}
		case "tool_use":
			info.toolName = b.Name
			if info.toolUseID == "" {
				info.toolUseID = b.ID // first tool_use ID
			}
			info.toolUseAll = append(info.toolUseAll, toolUseEntry{
				ID: b.ID, Name: b.Name, IsAgent: b.Name == "Agent",
			})
			if info.text == "" {
				info.text = fmt.Sprintf("[tool: %s]", b.Name)
			}
			// Store full input JSON as expandable content (only if text block didn't set it).
			if info.fullText == "" && len(b.Input) > 0 {
				prettyInput, err := json.MarshalIndent(json.RawMessage(b.Input), "", "  ")
				if err == nil && len(prettyInput) > maxContentPreview {
					info.fullText = string(prettyInput)
				}
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
						// Build agent identity (type or name) separate from task description
						agentName := name
						if agentName == "" { agentName = st }
						if agentName != "" && desc != "" {
							info.toolDetail = agentName
							info.text = fmt.Sprintf("[agent: %s] %s", agentName, desc)
						} else if agentName != "" {
							info.toolDetail = agentName
							info.text = fmt.Sprintf("[agent: %s]", agentName)
						} else if desc != "" {
							info.toolDetail = desc
							info.text = fmt.Sprintf("[agent] %s", desc)
						}
					case "Skill":
						skill, _ := inp["skill"].(string)
						if skill != "" {
							info.toolDetail = skill
							info.text = fmt.Sprintf("[skill: %s]", skill)
						}
					case "Bash":
						cmd, _ := inp["command"].(string)
						desc, _ := inp["description"].(string)
						if desc != "" { info.toolDetail = desc } else { info.toolDetail = truncate(cmd, 120) }
					case "Read", "Write", "Edit":
						fp, _ := inp["file_path"].(string)
						info.toolDetail = fp
					case "Grep", "Glob":
						pat, _ := inp["pattern"].(string)
						info.toolDetail = pat
					default:
						// Generic: stringify first key-value pair
						summary := truncate(string(b.Input), 100)
						info.toolDetail = summary
					}
				}
			}
		case "tool_result":
			info.forToolUseID = b.ToolUseID
			if b.IsError {
				info.isError = true
			}
			if info.text == "" {
				resultText := extractToolResultContent(b.Content)
				// Fallback to Text field if Content didn't produce anything.
				if resultText == "" && b.Text != "" {
					resultText = b.Text
				}
				if resultText != "" {
					info.text = truncate(resultText, maxContentPreview)
					if len([]rune(resultText)) > maxContentPreview {
						info.fullText = resultText
					}
				} else {
					info.text = "[tool_result]"
				}
			}
		}
	}
	if info.toolName != "" && info.text == "" {
		info.text = fmt.Sprintf("[tool: %s]", info.toolName)
	}
	return info
}

// extractToolResultContent handles tool_result content that can be either a
// plain string or an array of content blocks like [{"type":"text","text":"..."}].
func extractToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try plain string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Try array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
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
