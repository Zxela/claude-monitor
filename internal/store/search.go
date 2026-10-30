package store

import (
	"strings"

	_ "modernc.org/sqlite"
)

// buildFTSMatch turns a free-text query into a safe FTS5 MATCH expression by
// quoting each whitespace-separated term and joining them with spaces (implicit
// AND). Each term is a double-quoted literal with internal quotes doubled, so
// FTS5 operators and unbalanced quotes are neutralized and cannot trigger
// syntax errors. Returns "" when the query has no usable terms.
func buildFTSMatch(query string) string {
	tokens := strings.Fields(query)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " ")
}

// SearchFTS searches the FTS5 index for matching events.
func (d *DB) SearchFTS(query string, limit int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 50
	}
	// Tokenize on whitespace and quote each term individually, then join the
	// quoted terms with a space. FTS5 implicitly ANDs space-separated phrases,
	// so `session not` becomes `"session" "not"` (matches docs containing both
	// words anywhere) rather than `"session not"` (adjacent-only). Per-token
	// quoting preserves the safety property: every term is a quoted literal, so
	// bare operators and unbalanced quotes cannot cause FTS5 syntax errors.
	safe := buildFTSMatch(query)
	if safe == "" {
		// No usable terms (empty/whitespace-only query) — return no results
		// rather than issuing an invalid empty MATCH.
		return []EventRow{}, nil
	}
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		'', NULL, '', NULL
		FROM events_fts fts
		JOIN events e ON e.id = fts.rowid
		WHERE events_fts MATCH ?
		ORDER BY fts.rank * (1.0 + 10.0 / (1.0 + (julianday('now') - julianday(e.timestamp)) * 24.0))
		LIMIT ?`, safe, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

// SearchFullContent scans event_content for a substring match (slower, for key leak detection etc).
func (d *DB) SearchFullContent(query string, limit int) ([]EventRow, error) {
	if limit <= 0 {
		limit = 50
	}
	// Escape LIKE wildcards to prevent injection of % and _ characters.
	escaped := strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(query)
	rows, err := d.rdb.Query(`SELECT `+eventSelectCols+`,
		`+contentSelectCols+`
		FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE ec.content LIKE ? ESCAPE '\'
		ORDER BY e.timestamp DESC
		LIMIT ?`, "%"+escaped+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}
