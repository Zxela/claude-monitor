// Package mcp implements an MCP (Model Context Protocol) server that exposes
// claude-monitor data over JSON-RPC 2.0 via stdio. Claude Code can query its
// own usage mid-session, e.g. "How much have I spent today?"
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/zxela-claude/claude-monitor/internal/store"
)

// JSON-RPC types

// Request is an incoming JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // int, string, or nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is an outgoing JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string   `json:"jsonrpc"`
	ID      any      `json:"id"`
	Result  any      `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP-specific types

// ServerInfo identifies the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the response payload for the "initialize" method.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    Capabilities `json:"capabilities"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
}

// Capabilities declares what the server supports.
type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

// ToolsCapability signals that the server provides tools.
type ToolsCapability struct{}

// Tool describes an MCP tool.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema describes the JSON schema for a tool's arguments.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes a single property in a tool's input schema.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolCallParams is the params object for a "tools/call" request.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the result payload for a "tools/call" response.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a piece of tool output.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolsListResult wraps the tools array for the tools/list response.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// Server is the MCP server. It reads JSON-RPC from an io.Reader and writes
// responses to an io.Writer. All diagnostic logging goes to stderr.
type Server struct {
	db      *store.DB
	version string
	tools   []Tool
	reader  io.Reader
	writer  io.Writer
}

// NewServer creates a new MCP server backed by the given database.
func NewServer(db *store.DB, version string, reader io.Reader, writer io.Writer) *Server {
	s := &Server{
		db:      db,
		version: version,
		reader:  reader,
		writer:  writer,
	}
	s.tools = s.defineTools()
	return s
}

// Run reads JSON-RPC requests from the reader and writes responses to the
// writer until EOF.
func (s *Server) Run() error {
	scanner := bufio.NewScanner(s.reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("mcp: parse error: %v", err)
			s.writeError(nil, -32700, "Parse error")
			continue
		}

		s.handleRequest(&req)
	}
	return scanner.Err()
}

func (s *Server) handleRequest(req *Request) {
	switch req.Method {
	case "initialize":
		s.writeResult(req.ID, InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities:    Capabilities{Tools: &ToolsCapability{}},
			ServerInfo:      ServerInfo{Name: "claude-monitor", Version: s.version},
		})

	case "notifications/initialized":
		// Notification — no response required.

	case "tools/list":
		s.writeResult(req.ID, ToolsListResult{Tools: s.tools})

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.writeError(req.ID, -32602, "Invalid params")
			return
		}
		result := s.dispatchTool(params.Name, params.Arguments)
		s.writeResult(req.ID, result)

	default:
		s.writeError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func (s *Server) writeResult(id any, result any) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("mcp: marshal error: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := s.writer.Write(data); err != nil {
		log.Printf("mcp: write error: %v", err)
	}
}

func (s *Server) writeError(id any, code int, message string) {
	resp := Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message}}
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("mcp: marshal error: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := s.writer.Write(data); err != nil {
		log.Printf("mcp: write error: %v", err)
	}
}

// --- Tool definitions ---

func (s *Server) defineTools() []Tool {
	return []Tool{
		{
			Name:        "get_spending",
			Description: "Get cost and token usage summary for a time window.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"window": {
						Type:        "string",
						Description: "Time window to summarize. Defaults to \"today\".",
						Enum:        []string{"today", "week", "month", "all"},
					},
				},
			},
		},
		{
			Name:        "list_sessions",
			Description: "List recent Claude Code sessions, optionally filtered by repository.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"limit": {
						Type:        "integer",
						Description: "Maximum number of sessions to return (default 10, max 100).",
					},
					"repo": {
						Type:        "string",
						Description: "Filter by repository ID.",
					},
				},
			},
		},
		{
			Name:        "get_session",
			Description: "Get full details for a specific session by ID.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"session_id": {
						Type:        "string",
						Description: "The session ID to look up.",
					},
				},
				Required: []string{"session_id"},
			},
		},
		{
			Name:        "get_most_expensive",
			Description: "Find the most expensive sessions by cost.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"limit": {
						Type:        "integer",
						Description: "Number of sessions to return (default 5, max 50).",
					},
					"window": {
						Type:        "string",
						Description: "Time window to search. Defaults to \"all\".",
						Enum:        []string{"today", "week", "month", "all"},
					},
				},
			},
		},
	}
}

// --- Tool dispatch ---

func (s *Server) dispatchTool(name string, args json.RawMessage) *ToolResult {
	switch name {
	case "get_spending":
		return s.handleGetSpending(args)
	case "list_sessions":
		return s.handleListSessions(args)
	case "get_session":
		return s.handleGetSession(args)
	case "get_most_expensive":
		return s.handleGetMostExpensive(args)
	default:
		return errorResult(fmt.Sprintf("Unknown tool: %s", name))
	}
}

// --- Tool handlers ---

func (s *Server) handleGetSpending(args json.RawMessage) *ToolResult {
	var params struct {
		Window string `json:"window"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}
	if params.Window == "" {
		params.Window = "today"
	}

	since := windowToTime(params.Window)

	agg, err := s.db.AggregateStats(since)
	if err != nil {
		return errorResult(fmt.Sprintf("Database error: %v", err))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Spending Summary (%s)\n\n", params.Window)
	fmt.Fprintf(&b, "Total Cost: $%.4f\n", agg.TotalCost)
	fmt.Fprintf(&b, "Sessions: %d\n", agg.SessionCount)
	fmt.Fprintf(&b, "Input Tokens: %s\n", formatTokens(agg.InputTokens))
	fmt.Fprintf(&b, "Output Tokens: %s\n", formatTokens(agg.OutputTokens))
	fmt.Fprintf(&b, "Cache Read: %s\n", formatTokens(agg.CacheReadTokens))
	fmt.Fprintf(&b, "Cache Creation: %s\n", formatTokens(agg.CacheCreationTokens))

	if len(agg.CostByModel) > 0 {
		b.WriteString("\nCost by Model:\n")
		for model, cost := range agg.CostByModel {
			fmt.Fprintf(&b, "  %s: $%.4f\n", model, cost)
		}
	}

	if len(agg.CostByRepo) > 0 {
		b.WriteString("\nCost by Repo:\n")
		for repoID, cost := range agg.CostByRepo {
			fmt.Fprintf(&b, "  %s: $%.4f\n", repoID, cost)
		}
	}

	return textResult(b.String())
}

func (s *Server) handleListSessions(args json.RawMessage) *ToolResult {
	var params struct {
		Limit int    `json:"limit"`
		Repo  string `json:"repo"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	var sessions []store.SessionRow
	var err error
	if params.Repo != "" {
		sessions, err = s.db.ListSessionsByRepo(params.Repo, params.Limit, 0)
	} else {
		sessions, err = s.db.ListSessions(params.Limit, 0)
	}
	if err != nil {
		return errorResult(fmt.Sprintf("Database error: %v", err))
	}

	if len(sessions) == 0 {
		return textResult("No sessions found.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Sessions (%d)\n\n", len(sessions))
	for _, sess := range sessions {
		fmt.Fprintf(&b, "ID: %s\n", sess.ID)
		if sess.SessionName != "" {
			fmt.Fprintf(&b, "  Name: %s\n", sess.SessionName)
		}
		if sess.TaskDescription != "" {
			desc := sess.TaskDescription
			if len(desc) > 100 {
				desc = desc[:100] + "..."
			}
			fmt.Fprintf(&b, "  Task: %s\n", desc)
		}
		fmt.Fprintf(&b, "  Cost: $%.4f\n", sess.TotalCost)
		if sess.Model != "" {
			fmt.Fprintf(&b, "  Model: %s\n", sess.Model)
		}
		if sess.RepoID != "" {
			fmt.Fprintf(&b, "  Repo: %s\n", sess.RepoID)
		}
		fmt.Fprintf(&b, "  Started: %s\n", sess.StartedAt)
		if sess.EndedAt != "" {
			fmt.Fprintf(&b, "  Ended: %s\n", sess.EndedAt)
		}
		if sess.ErrorCount > 0 {
			fmt.Fprintf(&b, "  Errors: %d\n", sess.ErrorCount)
		}
		b.WriteString("\n")
	}

	return textResult(b.String())
}

func (s *Server) handleGetSession(args json.RawMessage) *ToolResult {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}
	if params.SessionID == "" {
		return errorResult("session_id is required")
	}

	sess, err := s.db.GetSession(params.SessionID)
	if err != nil {
		return errorResult(fmt.Sprintf("Database error: %v", err))
	}
	if sess == nil {
		return errorResult(fmt.Sprintf("Session not found: %s", params.SessionID))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Session Details\n\n")
	fmt.Fprintf(&b, "ID: %s\n", sess.ID)
	if sess.SessionName != "" {
		fmt.Fprintf(&b, "Name: %s\n", sess.SessionName)
	}
	if sess.TaskDescription != "" {
		fmt.Fprintf(&b, "Task: %s\n", sess.TaskDescription)
	}
	fmt.Fprintf(&b, "Cost: $%.4f\n", sess.TotalCost)
	if sess.Model != "" {
		fmt.Fprintf(&b, "Model: %s\n", sess.Model)
	}
	if sess.RepoID != "" {
		fmt.Fprintf(&b, "Repo: %s\n", sess.RepoID)
	}
	if sess.Branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n", sess.Branch)
	}
	if sess.CWD != "" {
		fmt.Fprintf(&b, "CWD: %s\n", sess.CWD)
	}
	fmt.Fprintf(&b, "Started: %s\n", sess.StartedAt)
	if sess.EndedAt != "" {
		fmt.Fprintf(&b, "Ended: %s\n", sess.EndedAt)
	}
	if sess.ParentID != "" {
		fmt.Fprintf(&b, "Parent: %s\n", sess.ParentID)
	}
	fmt.Fprintf(&b, "\nTokens:\n")
	fmt.Fprintf(&b, "  Input: %s\n", formatTokens(sess.InputTokens))
	fmt.Fprintf(&b, "  Output: %s\n", formatTokens(sess.OutputTokens))
	fmt.Fprintf(&b, "  Cache Read: %s\n", formatTokens(sess.CacheReadTokens))
	fmt.Fprintf(&b, "  Cache Creation: %s\n", formatTokens(sess.CacheCreationTokens))
	fmt.Fprintf(&b, "\nCounts:\n")
	fmt.Fprintf(&b, "  Messages: %d\n", sess.MessageCount)
	fmt.Fprintf(&b, "  Events: %d\n", sess.EventCount)
	fmt.Fprintf(&b, "  Errors: %d\n", sess.ErrorCount)

	return textResult(b.String())
}

func (s *Server) handleGetMostExpensive(args json.RawMessage) *ToolResult {
	var params struct {
		Limit  int    `json:"limit"`
		Window string `json:"window"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &params)
	}
	if params.Limit <= 0 {
		params.Limit = 5
	}
	if params.Limit > 50 {
		params.Limit = 50
	}
	if params.Window == "" {
		params.Window = "all"
	}

	since := windowToTime(params.Window)

	sessions, err := s.db.ListMostExpensiveSessions(since, params.Limit)
	if err != nil {
		return errorResult(fmt.Sprintf("Database error: %v", err))
	}

	if len(sessions) == 0 {
		return textResult("No sessions found.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Most Expensive Sessions (%s, top %d)\n\n", params.Window, len(sessions))
	for i, sess := range sessions {
		fmt.Fprintf(&b, "%d. $%.4f — %s\n", i+1, sess.TotalCost, sess.ID)
		if sess.SessionName != "" {
			fmt.Fprintf(&b, "   Name: %s\n", sess.SessionName)
		}
		if sess.TaskDescription != "" {
			desc := sess.TaskDescription
			if len(desc) > 80 {
				desc = desc[:80] + "..."
			}
			fmt.Fprintf(&b, "   Task: %s\n", desc)
		}
		if sess.Model != "" {
			fmt.Fprintf(&b, "   Model: %s\n", sess.Model)
		}
		if sess.RepoID != "" {
			fmt.Fprintf(&b, "   Repo: %s\n", sess.RepoID)
		}
		fmt.Fprintf(&b, "   Started: %s\n", sess.StartedAt)
		b.WriteString("\n")
	}

	return textResult(b.String())
}

// --- Helpers ---

func windowToTime(window string) time.Time {
	now := time.Now()
	switch window {
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "week":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(weekday - 1))
	case "month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	case "all":
		return time.Time{} // zero value = no filter
	default:
		return time.Time{}
	}
}

func textResult(text string) *ToolResult {
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: text}}}
}

func errorResult(message string) *ToolResult {
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: message}},
		IsError: true,
	}
}

// formatTokens formats a token count for human readability.
func formatTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%s (%dk)", formatWithCommas(n), n/1000)
	}
	return fmt.Sprintf("%s (%.1fM)", formatWithCommas(n), float64(n)/1_000_000)
}

// formatWithCommas formats an integer with comma separators.
func formatWithCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	remainder := len(s) % 3
	if remainder > 0 {
		result.WriteString(s[:remainder])
	}
	for i := remainder; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}

// SortSessionsByCostDesc sorts sessions by TotalCost descending (in place).
func SortSessionsByCostDesc(sessions []store.SessionRow) {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].TotalCost > sessions[j].TotalCost
	})
}
