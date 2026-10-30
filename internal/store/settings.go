package store

import (
	"fmt"

	_ "modernc.org/sqlite"
)

// GetSetting returns a setting value by key.
func (d *DB) GetSetting(key string) (string, error) {
	var value string
	err := d.rdb.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return value, fmt.Errorf("GetSetting(%s): %w", key, err)
	}
	return value, nil
}

// SetSetting upserts a setting.
func (d *DB) SetSetting(key, value string) error {
	_, err := d.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("SetSetting: %w", err)
	}
	return nil
}

// AllSettings returns all settings as a map.
func (d *DB) AllSettings() (map[string]string, error) {
	rows, err := d.rdb.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		result[k] = v
	}
	return result, rows.Err()
}
