// Package search provides an in-memory search index for session messages,
// eliminating the need to read JSONL files from disk on every search query.
package search

import (
	"strings"
	"sync"

	"github.com/zxela-claude/claude-monitor/internal/parser"
)

// Entry represents a single indexed message with its session context.
type Entry struct {
	SessionID   string `json:"sessionId"`
	SessionName string `json:"sessionName"`
	ProjectName string `json:"projectName"`
	parser.ParsedMessage
}

// indexedEntry pairs an Entry with pre-lowercased searchable text.
type indexedEntry struct {
	entry     Entry
	searchStr string // pre-lowercased concatenation of ContentText + ToolDetail + ToolName
}

// Index is a concurrency-safe in-memory search index for session messages.
type Index struct {
	mu      sync.RWMutex
	entries []indexedEntry // chronological order (oldest first), searched in reverse
}

// New creates a new empty search index.
func New() *Index {
	return &Index{}
}

// Add indexes a message for a session. Only messages with searchable content
// (non-empty ContentText, ToolDetail, or ToolName) are stored. O(1) amortized.
func (idx *Index) Add(sessionID, sessionName, projectName string, msg parser.ParsedMessage) {
	searchable := msg.ContentText + " " + msg.ToolDetail + " " + msg.ToolName
	// Skip messages with no meaningful searchable content.
	if strings.TrimSpace(searchable) == "" {
		return
	}

	ie := indexedEntry{
		entry: Entry{
			SessionID:     sessionID,
			SessionName:   sessionName,
			ProjectName:   projectName,
			ParsedMessage: msg,
		},
		searchStr: strings.ToLower(searchable),
	}

	idx.mu.Lock()
	idx.entries = append(idx.entries, ie) // O(1) amortized append
	idx.mu.Unlock()
}

// Search returns up to limit entries matching the query (case-insensitive substring).
// Results are returned newest-first by iterating in reverse.
func (idx *Index) Search(query string, limit int) []Entry {
	if query == "" || limit <= 0 {
		return nil
	}
	queryLower := strings.ToLower(query)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	results := make([]Entry, 0, limit)
	// Iterate in reverse for newest-first results.
	for i := len(idx.entries) - 1; i >= 0; i-- {
		if strings.Contains(idx.entries[i].searchStr, queryLower) {
			results = append(results, idx.entries[i].entry)
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

// UpdateSession updates the session name and project name for all entries
// belonging to the given session ID. This is called when session metadata
// changes after initial indexing (e.g. custom title set).
func (idx *Index) UpdateSession(sessionID, sessionName, projectName string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	for i := range idx.entries {
		if idx.entries[i].entry.SessionID == sessionID {
			idx.entries[i].entry.SessionName = sessionName
			idx.entries[i].entry.ProjectName = projectName
		}
	}
}
