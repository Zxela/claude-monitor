package search

import (
	"testing"

	"github.com/zxela-claude/claude-monitor/internal/parser"
)

func TestAddAndSearchBasic(t *testing.T) {
	idx := New()
	idx.Add("s1", "Session 1", "project-a", parser.ParsedMessage{
		ContentText: "hello world",
		Role:        "user",
	})
	idx.Add("s2", "Session 2", "project-b", parser.ParsedMessage{
		ContentText: "goodbye world",
		Role:        "assistant",
	})

	results := idx.Search("hello", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SessionID != "s1" {
		t.Errorf("expected session s1, got %s", results[0].SessionID)
	}

	results = idx.Search("world", 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestCaseInsensitiveSearch(t *testing.T) {
	idx := New()
	idx.Add("s1", "S1", "proj", parser.ParsedMessage{
		ContentText: "Hello World",
		Role:        "user",
	})

	for _, query := range []string{"hello", "HELLO", "Hello", "hElLo"} {
		results := idx.Search(query, 10)
		if len(results) != 1 {
			t.Errorf("query %q: expected 1 result, got %d", query, len(results))
		}
	}
}

func TestLimitRespected(t *testing.T) {
	idx := New()
	for i := 0; i < 20; i++ {
		idx.Add("s1", "S1", "proj", parser.ParsedMessage{
			ContentText: "matching text",
			Role:        "user",
		})
	}

	results := idx.Search("matching", 5)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
}

func TestEmptyQueryReturnsEmpty(t *testing.T) {
	idx := New()
	idx.Add("s1", "S1", "proj", parser.ParsedMessage{
		ContentText: "some content",
		Role:        "user",
	})

	results := idx.Search("", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestEmptyContentNotIndexed(t *testing.T) {
	idx := New()
	// Message with no searchable content should not be indexed.
	idx.Add("s1", "S1", "proj", parser.ParsedMessage{
		Role: "assistant",
	})
	// Message with only whitespace should not be indexed.
	idx.Add("s2", "S2", "proj", parser.ParsedMessage{
		ContentText: "   ",
		Role:        "assistant",
	})

	// The index should have no entries, so any search returns nothing.
	results := idx.Search("assistant", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 results (empty content not indexed), got %d", len(results))
	}
}

func TestSearchToolNameAndToolDetail(t *testing.T) {
	idx := New()
	idx.Add("s1", "S1", "proj", parser.ParsedMessage{
		ToolName:   "Bash",
		ToolDetail: "npm install",
		Role:       "assistant",
	})

	results := idx.Search("Bash", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for ToolName search, got %d", len(results))
	}

	results = idx.Search("npm install", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result for ToolDetail search, got %d", len(results))
	}
}

func TestUpdateSession(t *testing.T) {
	idx := New()
	idx.Add("s1", "Old Name", "old-project", parser.ParsedMessage{
		ContentText: "some text",
		Role:        "user",
	})
	idx.Add("s2", "Other Session", "other-project", parser.ParsedMessage{
		ContentText: "other text",
		Role:        "user",
	})

	idx.UpdateSession("s1", "New Name", "new-project")

	results := idx.Search("some", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SessionName != "New Name" {
		t.Errorf("expected session name 'New Name', got %q", results[0].SessionName)
	}
	if results[0].ProjectName != "new-project" {
		t.Errorf("expected project name 'new-project', got %q", results[0].ProjectName)
	}

	// s2 should be unchanged.
	results = idx.Search("other", 10)
	if len(results) != 1 || results[0].SessionName != "Other Session" {
		t.Errorf("s2 should be unchanged")
	}
}

func TestNewestFirst(t *testing.T) {
	idx := New()
	idx.Add("s1", "First", "proj", parser.ParsedMessage{
		ContentText: "match first",
		Role:        "user",
	})
	idx.Add("s2", "Second", "proj", parser.ParsedMessage{
		ContentText: "match second",
		Role:        "user",
	})

	results := idx.Search("match", 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Most recently added should come first.
	if results[0].SessionID != "s2" {
		t.Errorf("expected newest (s2) first, got %s", results[0].SessionID)
	}
	if results[1].SessionID != "s1" {
		t.Errorf("expected oldest (s1) second, got %s", results[1].SessionID)
	}
}
