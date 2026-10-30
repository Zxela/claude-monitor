package store

import (
	"fmt"

	"github.com/zxela/claude-monitor/internal/pricing"
)

// AllModelPricing returns all rows from the model_pricing table as a map keyed by model_prefix.
func (d *DB) AllModelPricing() (pricing.Table, error) {
	rows, err := d.rdb.Query(`SELECT model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok,
		long_context_threshold, long_input_per_mtok, long_output_per_mtok, long_cache_read_per_mtok, long_cache_create_per_mtok
		FROM model_pricing`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(pricing.Table)
	for rows.Next() {
		var prefix string
		var p pricing.Price
		if err := rows.Scan(&prefix, &p.InputPerMTok, &p.OutputPerMTok, &p.CacheReadPerMTok, &p.CacheCreatePerMTok,
			&p.LongContextThreshold, &p.LongInputPerMTok, &p.LongOutputPerMTok, &p.LongCacheReadPerMTok, &p.LongCacheCreatePerMTok); err != nil {
			return nil, err
		}
		result[prefix] = p
	}
	return result, rows.Err()
}

// UpsertModelPricing inserts or replaces the base rates for the given model
// prefix. An existing long-context tier is kept.
func (d *DB) UpsertModelPricing(prefix string, p pricing.Price) error {
	_, err := d.db.Exec(
		`INSERT INTO model_pricing (model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(model_prefix) DO UPDATE SET
		 input_per_mtok=excluded.input_per_mtok,
		 output_per_mtok=excluded.output_per_mtok,
		 cache_read_per_mtok=excluded.cache_read_per_mtok,
		 cache_create_per_mtok=excluded.cache_create_per_mtok`,
		prefix, p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheCreatePerMTok,
	)
	if err != nil {
		return fmt.Errorf("UpsertModelPricing: %w", err)
	}
	return nil
}
