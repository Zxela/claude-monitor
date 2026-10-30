// Package pricing prices a Message's token usage in USD.
package pricing

import (
	"log"
	"sort"
	"sync"
	"sync/atomic"
)

// Price is a model's per-million-token rates. The Long* rates apply to
// requests whose prompt exceeds LongContextThreshold tokens; a zero threshold
// means the model has one rate card.
type Price struct {
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

// Usage is the token counts of one Message.
type Usage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
}

// cost prices u, switching to the long-context card when the prompt (input +
// cache read + cache write tokens) exceeds the threshold.
func (p Price) cost(u Usage) float64 {
	in, out, cacheRead, cacheCreate := p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheCreatePerMTok
	if p.LongContextThreshold > 0 && u.InputTokens+u.CacheReadTokens+u.CacheCreationTokens > p.LongContextThreshold {
		in, out, cacheRead, cacheCreate = p.LongInputPerMTok, p.LongOutputPerMTok, p.LongCacheReadPerMTok, p.LongCacheCreatePerMTok
	}
	return float64(u.InputTokens)*in/1e6 +
		float64(u.OutputTokens)*out/1e6 +
		float64(u.CacheReadTokens)*cacheRead/1e6 +
		float64(u.CacheCreationTokens)*cacheCreate/1e6
}

// Table maps model prefixes to prices.
type Table map[string]Price

// fallback is the compiled table, used for any prefix the DB has no row for.
var fallback = Table{
	"claude-fable-5-1":  {InputPerMTok: 10.0, OutputPerMTok: 50.0, CacheReadPerMTok: 0.25, CacheCreatePerMTok: 12.50},
	"claude-mythos-5-1": {InputPerMTok: 10.0, OutputPerMTok: 50.0, CacheReadPerMTok: 0.25, CacheCreatePerMTok: 12.50},
	"claude-fable-5":    {InputPerMTok: 10.0, OutputPerMTok: 50.0, CacheReadPerMTok: 1.0, CacheCreatePerMTok: 12.50},
	"claude-mythos-5":   {InputPerMTok: 10.0, OutputPerMTok: 50.0, CacheReadPerMTok: 1.0, CacheCreatePerMTok: 12.50},
	"claude-opus-5-5":   {InputPerMTok: 4.0, OutputPerMTok: 20.0, CacheReadPerMTok: 0.20, CacheCreatePerMTok: 5.0},
	"claude-opus-5":     {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheCreatePerMTok: 6.25},
	"claude-opus-4-8":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheCreatePerMTok: 6.25},
	"claude-opus-4-7":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheCreatePerMTok: 6.25},
	"claude-opus-4-6":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheCreatePerMTok: 6.25},
	"claude-opus-4-5":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheCreatePerMTok: 6.25},
	"claude-sonnet-5-5": {InputPerMTok: 2.0, OutputPerMTok: 10.0, CacheReadPerMTok: 0.20, CacheCreatePerMTok: 2.50},
	"claude-sonnet-5":   {InputPerMTok: 2.0, OutputPerMTok: 10.0, CacheReadPerMTok: 0.20, CacheCreatePerMTok: 2.50},
	"claude-sonnet-4-6": {InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.30, CacheCreatePerMTok: 3.75},
	"claude-haiku-5-5": {
		InputPerMTok: 0.10, OutputPerMTok: 0.50, CacheReadPerMTok: 0.01, CacheCreatePerMTok: 0.125,
		LongContextThreshold: 100_000,
		LongInputPerMTok:     0.50, LongOutputPerMTok: 2.50, LongCacheReadPerMTok: 0.05, LongCacheCreatePerMTok: 0.625,
	},
	"claude-haiku-4-5": {InputPerMTok: 1.0, OutputPerMTok: 5.0, CacheReadPerMTok: 0.10, CacheCreatePerMTok: 1.25},
}

// unknownModelPrice is the compiled Sonnet 4.6 card, even when the DB overrides that row.
var unknownModelPrice = fallback["claude-sonnet-4-6"]

// match returns the price for model: an exact key, else the longest key that
// prefixes it (so "claude-opus-5-5-…" never takes "claude-opus-5").
func (t Table) match(model string) (Price, bool) {
	if p, ok := t[model]; ok {
		return p, true
	}
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if len(model) > len(key) && model[:len(key)] == key {
			return t[key], true
		}
	}
	return Price{}, false
}

// Book is the live price table: the compiled fallback overlaid with DB rows.
// Safe for concurrent use.
type Book struct {
	table  atomic.Pointer[Table]
	warned sync.Map
}

// NewBook returns a Book holding only the compiled fallback.
func NewBook() *Book {
	b := &Book{}
	b.Load(nil)
	return b
}

// Load replaces the DB rows. A DB row replaces the fallback row for its prefix
// whole, so a DB row without a long-context tier has none.
func (b *Book) Load(rows Table) {
	t := make(Table, len(fallback)+len(rows))
	for k, v := range fallback {
		t[k] = v
	}
	for k, v := range rows {
		t[k] = v
	}
	b.table.Store(&t)
}

// Cost prices u for model. An unknown model is priced at Sonnet 4.6 and
// logged once per process.
func (b *Book) Cost(model string, u Usage) float64 {
	p, ok := b.table.Load().match(model)
	if !ok {
		p = unknownModelPrice
		// "<synthetic>" is Claude Code's own model string for injected Messages.
		if model != "" && model != "<synthetic>" {
			if _, warned := b.warned.LoadOrStore(model, true); !warned {
				log.Printf("[WARN] unknown model pricing for %q, using Sonnet default", model)
			}
		}
	}
	return p.cost(u)
}
