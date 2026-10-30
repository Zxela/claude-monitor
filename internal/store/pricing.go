package store

import (
	"fmt"

	_ "modernc.org/sqlite"
)

// ModelPricing holds per-million-token pricing for a model prefix. The Long*
// rates apply to requests whose prompt exceeds LongContextThreshold tokens;
// a zero threshold means the model has one rate card.
type ModelPricing struct {
	InputPerMTok           float64 `json:"input_per_mtok"`
	OutputPerMTok          float64 `json:"output_per_mtok"`
	CacheReadPerMTok       float64 `json:"cache_read_per_mtok"`
	CacheCreatePerMTok     float64 `json:"cache_create_per_mtok"`
	LongContextThreshold   int64   `json:"long_context_threshold,omitempty"`
	LongInputPerMTok       float64 `json:"long_input_per_mtok,omitempty"`
	LongOutputPerMTok      float64 `json:"long_output_per_mtok,omitempty"`
	LongCacheReadPerMTok   float64 `json:"long_cache_read_per_mtok,omitempty"`
	LongCacheCreatePerMTok float64 `json:"long_cache_create_per_mtok,omitempty"`
}

// AllModelPricing returns all rows from the model_pricing table as a map keyed by model_prefix.
func (d *DB) AllModelPricing() (map[string]ModelPricing, error) {
	rows, err := d.rdb.Query(`SELECT model_prefix, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_create_per_mtok,
		long_context_threshold, long_input_per_mtok, long_output_per_mtok, long_cache_read_per_mtok, long_cache_create_per_mtok
		FROM model_pricing`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]ModelPricing)
	for rows.Next() {
		var prefix string
		var p ModelPricing
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
func (d *DB) UpsertModelPricing(prefix string, p ModelPricing) error {
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
