package store

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// StorageInfo holds database storage statistics.
type StorageInfo struct {
	TotalSizeBytes   int64 `json:"totalSizeBytes"`
	HotContentBytes  int64 `json:"hotContentBytes"`
	WarmContentBytes int64 `json:"warmContentBytes"`
	EventCount       int64 `json:"eventCount"`
}

// decompressContent inflates gzip-compressed event content from a warm-tier BLOB.
func decompressContent(data []byte) (string, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decompressContent: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("decompressContent: %w", err)
	}
	return string(out), nil
}

// compressContent gzips s, returning nil for a NULL value.
func compressContent(s sql.NullString) ([]byte, error) {
	if !s.Valid {
		return nil, nil
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := io.WriteString(w, s.String); err != nil {
		w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CompactHotToWarm compresses event_content older than hotDays into gzip BLOBs.
func (d *DB) CompactHotToWarm(hotDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -hotDays).Format(time.RFC3339)

	// Earlier versions let re-ingest write plaintext back into warm rows next to
	// their gzip copy. Drop that duplicate.
	if _, err := d.db.Exec(`UPDATE event_content SET content = NULL
		WHERE tier = 'warm' AND content IS NOT NULL AND compressed IS NOT NULL`); err != nil {
		return 0, err
	}

	rows, err := d.rdb.Query(`SELECT ec.event_id, ec.content, ec.thinking FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE ec.tier = 'hot' AND (ec.content IS NOT NULL OR ec.thinking IS NOT NULL)
		AND e.timestamp < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type compactEntry struct {
		eventID  int64
		data     []byte
		thinking []byte
	}
	var entries []compactEntry

	for rows.Next() {
		var eventID int64
		var content, thinking sql.NullString
		if err := rows.Scan(&eventID, &content, &thinking); err != nil {
			return 0, err
		}
		data, err := compressContent(content)
		if err != nil {
			continue
		}
		thinkingData, err := compressContent(thinking)
		if err != nil {
			continue
		}
		entries = append(entries, compactEntry{eventID: eventID, data: data, thinking: thinkingData})
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if len(entries) == 0 {
		return 0, nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			log.Printf("CompactHotToWarm: rollback error: %v", rbErr)
		}
	}()

	stmt, err := tx.Prepare(`UPDATE event_content SET tier='warm', content=NULL, compressed=?,
		thinking=NULL, thinking_compressed=? WHERE event_id=?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.Exec(e.data, e.thinking, e.eventID); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(entries)), nil
}

// CompactWarmToCold drops event_content older than warmDays, leaving a 'cold'
// row with no content so a later re-ingest cannot bring it back as hot.
func (d *DB) CompactWarmToCold(warmDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -warmDays).Format(time.RFC3339)
	result, err := d.db.Exec(`UPDATE event_content SET tier = 'cold', content = NULL, compressed = NULL,
		thinking = NULL, thinking_compressed = NULL
		WHERE tier != 'cold' AND event_id IN (
		SELECT ec.event_id FROM event_content ec
		JOIN events e ON e.id = ec.event_id
		WHERE e.timestamp < ?)`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// StorageInfo returns storage statistics.
func (d *DB) StorageInfo() (*StorageInfo, error) {
	info := &StorageInfo{}

	// Total DB size via page_count * page_size
	if err := d.rdb.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&info.TotalSizeBytes); err != nil {
		// Fallback: just count events
		info.TotalSizeBytes = 0
	}

	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&info.EventCount); err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}
	if err := d.rdb.QueryRow(`SELECT COALESCE(SUM(LENGTH(content)),0) + COALESCE(SUM(LENGTH(thinking)),0) FROM event_content WHERE tier='hot'`).Scan(&info.HotContentBytes); err != nil {
		return nil, fmt.Errorf("hot content size: %w", err)
	}
	if err := d.rdb.QueryRow(`SELECT COALESCE(SUM(LENGTH(compressed)),0) + COALESCE(SUM(LENGTH(thinking_compressed)),0) FROM event_content WHERE tier='warm'`).Scan(&info.WarmContentBytes); err != nil {
		return nil, fmt.Errorf("warm content size: %w", err)
	}

	return info, nil
}
