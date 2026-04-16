// Package parser parses individual JSONL lines from Claude Code session files.
package parser

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const maxContentPreview = 200

// previewMaxLength is the runtime-configurable preview truncation limit.
// Default matches maxContentPreview. Updated via SetPreviewMaxLength.
var previewMaxLength atomic.Int64

func init() {
	previewMaxLength.Store(maxContentPreview)
}

// SetPreviewMaxLength updates the content preview truncation length at runtime.
// Safe to call concurrently; takes effect on the next parse.
func SetPreviewMaxLength(n int) {
	previewMaxLength.Store(int64(n))
}

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
	Operation   string          `json:"operation"`
	// Compact/summary event.
	Summary string `json:"summary"`
	// Additional fields previously dropped.
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Subtype       string          `json:"subtype"`
	IsMeta        bool            `json:"isMeta"`
	Version       string          `json:"version"`
	Entrypoint    string          `json:"entrypoint"`
	DurationMs    *int64          `json:"durationMs"`     // system.turn_duration
	MessageCount  *int            `json:"messageCount"`   // system.turn_duration
	HookCount     *int            `json:"hookCount"`      // system.stop_hook_summary
	HookInfos     json.RawMessage `json:"hookInfos"`      // system.stop_hook_summary
	Level         string          `json:"level"`           // system.stop_hook_summary
}

type rawProgressData struct {
	Type      string `json:"type"`
	HookEvent string `json:"hookEvent"`
	HookName  string `json:"hookName"`
	Command   string `json:"command"`
}

// rawToolResult extracts metadata from the toolUseResult field on user/tool_result lines.
type rawToolResult struct {
	DurationMs        *int64 `json:"durationMs"`
	TotalDurationMs   *int64 `json:"totalDurationMs"`
	TotalTokens       *int64 `json:"totalTokens"`
	TotalToolUseCount *int   `json:"totalToolUseCount"`
	AgentType         string `json:"agentType"`
	Success           *bool  `json:"success"`
	Interrupted       bool   `json:"interrupted"`
	Truncated         bool   `json:"truncated"`
	Stderr            string `json:"stderr"`
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
	TeamName        string    `json:"teamName,omitempty"`        // team name for team agents
	ThinkingContent string    `json:"thinkingContent,omitempty"` // full text from thinking blocks
	AgentName    string    `json:"agentName,omitempty"`    // agent name within a team
	// Tool result metadata (from toolUseResult on user lines)
	DurationMs      *int64  `json:"durationMs,omitempty"`
	Success         *bool   `json:"success,omitempty"`
	Stderr          string  `json:"stderr,omitempty"`
	Interrupted     bool    `json:"interrupted,omitempty"`
	Truncated       bool    `json:"truncated,omitempty"`
	// Agent result metadata (from toolUseResult on agent completions)
	AgentDurationMs   *int64 `json:"agentDurationMs,omitempty"`
	AgentTokens       *int64 `json:"agentTokens,omitempty"`
	AgentToolUseCount *int   `json:"agentToolUseCount,omitempty"`
	AgentType         string `json:"agentType,omitempty"`
	// System message metadata
	Subtype          string `json:"subtype,omitempty"`
	TurnMessageCount *int   `json:"turnMessageCount,omitempty"`
	HookCount        *int   `json:"hookCount,omitempty"`
	HookInfos        string `json:"hookInfos,omitempty"` // JSON string
	Level            string `json:"level,omitempty"`
	// Session-level metadata
	IsMeta     bool   `json:"isMeta,omitempty"`
	Version    string `json:"version,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
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

// pricingTable is the compile-time fallback used when the DB is empty or unavailable.
var pricingTable = map[string]modelPricing{
	"claude-opus-4-6":   {5.0, 25.0, 0.50, 6.25},
	"claude-sonnet-4-6": {3.0, 15.0, 0.30, 3.75},
	"claude-haiku-4-5":  {1.0, 5.0, 0.10, 1.25},
}

var defaultPricing = pricingTable["claude-sonnet-4-6"]

// activePricingTable holds the merged pricing map pointer (DB values preferred over compile-time
// fallback). Stored as an atomic.Value so concurrent reads from computeCost and writes from
// SetPricingTable (called from the HTTP pricing handler) are race-free.
var activePricingTableAtomic atomic.Value // stores map[string]modelPricing

// warnedModels tracks models for which an unknown-pricing warning has already been logged
// (once per process run).
var warnedModels sync.Map

// ExternalPricing holds the four per-million-token rates for a model.
// Used by SetPricingTable to accept DB-loaded pricing without a store import.
type ExternalPricing struct {
	InputPerMTok       float64
	OutputPerMTok      float64
	CacheReadPerMTok   float64
	CacheCreatePerMTok float64
}

// SetPricingTable merges DB-sourced pricing (preferred) with the compile-time fallback.
// Call this once at startup after loading from the database.
// dbPricing keys are model_prefix strings.
func SetPricingTable(dbPricing map[string]ExternalPricing) {
	merged := make(map[string]modelPricing, len(pricingTable)+len(dbPricing))
	// Start with compile-time fallback.
	for k, v := range pricingTable {
		merged[k] = v
	}
	// Overwrite / extend with DB values.
	for prefix, p := range dbPricing {
		merged[prefix] = modelPricing(p)
	}
	activePricingTableAtomic.Store(merged)
}

// lookupPricing resolves pricing for the given model string.
// It prefers the atomically-stored pricing table (set from DB) and falls back to pricingTable.
// Returns the pricing and whether it was found (false means default was used).
func lookupPricing(model string) (modelPricing, bool) {
	var table map[string]modelPricing
	if v := activePricingTableAtomic.Load(); v != nil {
		table = v.(map[string]modelPricing)
	}
	if table == nil {
		table = pricingTable
	}

	// Exact match.
	if p, ok := table[model]; ok {
		return p, true
	}
	// Prefix match for versioned model names (e.g. "claude-haiku-4-5-20251001").
	// Sort keys by length descending so the most-specific prefix wins when
	// multiple prefixes could match (e.g. "claude-haiku-4" vs "claude-haiku-4-5").
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if len(model) > len(key) && model[:len(key)] == key {
			return table[key], true
		}
	}
	return defaultPricing, false
}

func computeCost(model string, usage rawUsage) float64 {
	p, found := lookupPricing(model)
	if !found && model != "" {
		// Warn once per unknown model per process run.
		if _, alreadyWarned := warnedModels.LoadOrStore(model, true); !alreadyWarned {
			log.Printf("[WARN] unknown model pricing for %q, using Sonnet default", model)
		}
	}
	return float64(usage.InputTokens)*p.InputPerMTok/1e6 +
		float64(usage.OutputTokens)*p.OutputPerMTok/1e6 +
		float64(usage.CacheReadInputTokens)*p.CacheReadPerMTok/1e6 +
		float64(usage.CacheCreationInputTokens)*p.CacheCreatePerMTok/1e6
}

// isErrorContent is a last-resort heuristic to detect error messages in tool result content.
// This function should only be applied when b.IsError is not set (the canonical source).
// Prefer the explicit is_error field from tool_result blocks over this heuristic.
func isErrorContent(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "error:") ||
		strings.Contains(lower, "\nerror:") ||
		strings.Contains(lower, "command failed") ||
		strings.Contains(lower, "exited with error") ||
		strings.Contains(lower, "exit status 1")
}

// unmarshalLine decodes a JSONL line into the intermediate rawMessage struct.
func unmarshalLine(line []byte) (*rawMessage, error) {
	var raw rawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	return &raw, nil
}

// buildBaseEvent constructs the core Event fields from a rawMessage: identity,
// tokens, cost, timestamps, and session-level metadata.
func buildBaseEvent(raw *rawMessage) *Event {
	usage := raw.Message.Usage
	cost := computeCost(raw.Message.Model, usage)

	msg := &Event{
		Type:                raw.Type,
		MessageID:           raw.Message.ID,
		Role:                raw.Message.Role,
		CostUSD:             cost,
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheReadTokens:     usage.CacheReadInputTokens,
		CacheCreationTokens: usage.CacheCreationInputTokens,
		Timestamp:           raw.Timestamp,
		SessionID:           raw.SessionID,
		UUID:                raw.UUID,
		ParentUUID:          raw.ParentUUID,
		CWD:                 raw.CWD,
		GitBranch:           raw.GitBranch,
		Model:               raw.Message.Model,
		IsSidechain:         raw.IsSidechain,
		TeamName:            raw.TeamName,
		AgentName:           raw.AgentName,
		// Session-level metadata (carried on most JSONL lines).
		IsMeta:     raw.IsMeta,
		Version:    raw.Version,
		Entrypoint: raw.Entrypoint,
	}

	if raw.Message.StopReason != nil {
		msg.StopReason = *raw.Message.StopReason
	}

	// Treat queue-operation/enqueue as a user message (real-time user input).
	if raw.Type == "queue-operation" && raw.Operation == "enqueue" {
		msg.Type = "user"
		msg.Role = "user"
	}

	// Compact/summary events (context compaction in Claude Code).
	if raw.Type == "summary" {
		msg.Subtype = "compact"
		if raw.Summary != "" {
			limit := int(previewMaxLength.Load())
			msg.ContentText = truncate(raw.Summary, limit)
			if len([]rune(raw.Summary)) > limit {
				msg.FullContent = raw.Summary
			}
		} else {
			msg.ContentText = "[context compacted]"
		}
	}

	return msg
}

// applySystemMetadata populates system-specific fields (turn_duration,
// stop_hook_summary) when the raw line is a system message.
func applySystemMetadata(msg *Event, raw *rawMessage) {
	if raw.Type != "system" {
		return
	}
	msg.Subtype = raw.Subtype
	msg.DurationMs = raw.DurationMs
	msg.TurnMessageCount = raw.MessageCount
	msg.HookCount = raw.HookCount
	if len(raw.HookInfos) > 0 {
		msg.HookInfos = string(raw.HookInfos)
	}
	msg.Level = raw.Level
}

// applyToolResultMetadata extracts metadata from the toolUseResult JSON field
// found on user/tool_result lines.
func applyToolResultMetadata(msg *Event, raw *rawMessage) {
	if len(raw.ToolUseResult) == 0 {
		return
	}
	// toolUseResult is sometimes a plain string (e.g. tool output text) rather
	// than a structured metadata object. Detect and skip gracefully.
	if len(raw.ToolUseResult) > 0 && raw.ToolUseResult[0] == '"' {
		return
	}
	var tr rawToolResult
	if err := json.Unmarshal(raw.ToolUseResult, &tr); err != nil {
		log.Printf("debug: unmarshal toolUseResult (uuid=%s): %v", raw.UUID, err)
		return
	}
	msg.DurationMs = tr.DurationMs
	if tr.Success != nil {
		msg.Success = tr.Success
		if !*tr.Success {
			msg.IsError = true
		}
	}
	msg.Interrupted = tr.Interrupted
	msg.Truncated = tr.Truncated
	msg.Stderr = tr.Stderr
	msg.AgentDurationMs = tr.TotalDurationMs
	msg.AgentTokens = tr.TotalTokens
	msg.AgentToolUseCount = tr.TotalToolUseCount
	msg.AgentType = tr.AgentType
}

// applyHookData extracts hook progress data from progress messages. Returns
// true if hook data was applied (so content extraction can be skipped).
func applyHookData(msg *Event, raw *rawMessage) bool {
	if raw.Type != "progress" || raw.Data.Type != "hook_progress" {
		return false
	}
	msg.HookEvent = raw.Data.HookEvent
	msg.HookName = raw.Data.HookName

	// Extract tool name from hookName (e.g. "PostToolUse:Read" -> "Read")
	hookTool := raw.Data.HookName
	if idx := strings.IndexByte(raw.Data.HookName, ':'); idx >= 0 {
		hookTool = raw.Data.HookName[idx+1:]
	}

	msg.ContentText = fmt.Sprintf("[hook:%s] %s", raw.Data.HookEvent, hookTool)

	// Include the hook command when available (shell command or "callback")
	cmd := raw.Data.Command
	if cmd != "" && cmd != "callback" {
		msg.ToolDetail = cmd
		msg.FullContent = cmd
	}

	return true
}

// applyContentData extracts content, tool metadata, and error flags from the
// message or top-level content field.
func applyContentData(msg *Event, raw *rawMessage) {
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
	msg.ThinkingContent = ci.thinkingContent
	msg.IsAgent = msg.ToolName == "Agent"
	msg.IsError = msg.IsError || ci.isError

	// Detect error indicators in content text when not already flagged.
	if !msg.IsError && msg.ForToolUseID != "" && isErrorContent(ci.text) {
		msg.IsError = true
	}

	// Populate ToolUseIDs for batched tool calls.
	if len(ci.toolUseAll) > 1 {
		ids := make([]string, len(ci.toolUseAll))
		for i, tu := range ci.toolUseAll {
			ids[i] = tu.ID
		}
		msg.ToolUseIDs = ids
	}
}

// ParseLine unmarshals a single JSONL line and returns an Event.
func ParseLine(line []byte) (*Event, error) {
	raw, err := unmarshalLine(line)
	if err != nil {
		return nil, err
	}

	msg := buildBaseEvent(raw)
	applySystemMetadata(msg, raw)
	applyToolResultMetadata(msg, raw)

	// Hook messages set their own content; skip generic content extraction.
	if !applyHookData(msg, raw) {
		applyContentData(msg, raw)
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
	text            string // preview text (truncated)
	toolName        string
	fullText        string // untruncated content
	toolDetail      string
	toolUseID       string         // first tool_use block ID
	toolUseAll      []toolUseEntry // all tool_use blocks (for batched calls)
	forToolUseID    string         // tool_use_id from tool_result block
	isError         bool           // true if tool_result has is_error or error content
	thinkingContent string         // full text from thinking blocks
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
		limit := int(previewMaxLength.Load())
		if len([]rune(s)) <= limit {
			return contentInfo{text: s}
		}
		return contentInfo{text: truncate(s, limit), fullText: s}
	}

	// Try array of content blocks.
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		log.Printf("debug: unmarshal content blocks: %v (prefix: %.80s)", err, raw)
		return contentInfo{}
	}

	var info contentInfo
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" && info.text == "" {
				limit := int(previewMaxLength.Load())
				info.text = truncate(b.Text, limit)
				if len([]rune(b.Text)) > limit {
					info.fullText = b.Text
				}
			}
		case "thinking":
			if b.Text != "" {
				if info.thinkingContent == "" {
					info.thinkingContent = b.Text
				}
				if info.fullText == "" {
					info.fullText = b.Text
				}
			}
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
			// Store full input JSON as expandable content (always, so users can expand tool calls).
			if info.fullText == "" && len(b.Input) > 0 {
				prettyInput, err := json.MarshalIndent(json.RawMessage(b.Input), "", "  ")
				if err == nil {
					info.fullText = string(prettyInput)
				}
			}
			// Extract tool input detail.
			if len(b.Input) > 0 {
				var inp map[string]interface{}
				if err := json.Unmarshal(b.Input, &inp); err != nil {
					log.Printf("debug: unmarshal tool input (tool=%s): %v", b.Name, err)
				} else {
					switch b.Name {
					case "Agent":
						desc, _ := inp["description"].(string)
						st, _ := inp["subagent_type"].(string)
						name, _ := inp["name"].(string)
						// Build agent identity (type or name) separate from task description
						agentName := name
						if agentName == "" { agentName = st }
						switch {
						case agentName != "" && desc != "":
							info.toolDetail = agentName
							info.text = fmt.Sprintf("[agent: %s] %s", agentName, desc)
						case agentName != "":
							info.toolDetail = agentName
							info.text = fmt.Sprintf("[agent: %s]", agentName)
						case desc != "":
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
					limit := int(previewMaxLength.Load())
					info.text = truncate(resultText, limit)
					if len([]rune(resultText)) > limit {
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
	if err := json.Unmarshal(raw, &blocks); err != nil {
		log.Printf("debug: unmarshal tool result content: %v (prefix: %.80s)", err, raw)
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
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
