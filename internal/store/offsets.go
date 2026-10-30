package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// FileOffset is how far a JSONL file has been read and persisted: Offset is
// the byte just past the last line whose events are stored, Inode identifies
// the file so a replaced one is not resumed mid-way.
type FileOffset struct {
	Path      string
	SessionID string
	Offset    int64
	Inode     uint64
}

// LoadFileOffsets returns every saved file offset.
func (d *DB) LoadFileOffsets() ([]FileOffset, error) {
	rows, err := d.rdb.Query(`SELECT path, session_id, byte_offset, inode FROM file_offsets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileOffset
	for rows.Next() {
		var o FileOffset
		var ino int64
		if err := rows.Scan(&o.Path, &o.SessionID, &o.Offset, &ino); err != nil {
			return nil, err
		}
		o.Inode = uint64(ino)
		out = append(out, o)
	}
	return out, rows.Err()
}

func upsertOffsets(tx *sql.Tx, offsets []FileOffset) error {
	if len(offsets) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(`INSERT INTO file_offsets (path, session_id, byte_offset, inode) VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET session_id=excluded.session_id,
			byte_offset=excluded.byte_offset, inode=excluded.inode`)
	if err != nil {
		return fmt.Errorf("prepare offset: %w", err)
	}
	defer stmt.Close()
	for _, o := range offsets {
		if _, err := stmt.Exec(o.Path, o.SessionID, o.Offset, int64(o.Inode)); err != nil {
			return fmt.Errorf("save offset %s: %w", o.Path, err)
		}
	}
	return nil
}
