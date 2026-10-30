package pricing

import (
	"math"
	"sort"
	"testing"
)

func TestBookCost_ByModel(t *testing.T) {
	t.Parallel()
	b := NewBook()
	usage := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheCreationTokens: 1_000_000}
	tests := []struct {
		name  string
		model string
		want  float64
	}{
		{"fable", "claude-fable-5", 10.0 + 50.0 + 1.0 + 12.50},
		{"fable 1m suffix", "claude-fable-5[1m]", 10.0 + 50.0 + 1.0 + 12.50},
		{"fable 5.1", "claude-fable-5-1", 10.0 + 50.0 + 0.25 + 12.50},
		{"opus 5.5", "claude-opus-5-5", 4.0 + 20.0 + 0.20 + 5.0},
		{"opus 5.5 versioned takes the longest prefix", "claude-opus-5-5-20261001", 4.0 + 20.0 + 0.20 + 5.0},
		{"opus 5", "claude-opus-5", 5.0 + 25.0 + 0.50 + 6.25},
		{"sonnet 5.5", "claude-sonnet-5-5", 2.0 + 10.0 + 0.20 + 2.50},
		{"haiku 5.5 (3M prompt, long card)", "claude-haiku-5-5", 0.50 + 2.50 + 0.05 + 0.625},
		{"opus 4.8 versioned", "claude-opus-4-8-20260101", 5.0 + 25.0 + 0.50 + 6.25},
		{"sonnet", "claude-sonnet-4-6", 3.0 + 15.0 + 0.30 + 3.75},
		{"haiku versioned", "claude-haiku-4-5-20251001", 1.0 + 5.0 + 0.10 + 1.25},
		{"unknown falls back to sonnet 4.6", "claude-unknown-99", 3.0 + 15.0 + 0.30 + 3.75},
		{"synthetic falls back to sonnet 4.6", "<synthetic>", 3.0 + 15.0 + 0.30 + 3.75},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.Cost(tc.model, usage); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("Cost(%q) = %g, want %g", tc.model, got, tc.want)
			}
		})
	}
}

func TestBookCost_Haiku55LongContextBoundary(t *testing.T) {
	t.Parallel()
	b := NewBook()
	tests := []struct {
		name  string
		model string
		usage Usage
		want  float64
	}{
		{"100K prompt is base", "claude-haiku-5-5",
			Usage{InputTokens: 100_000, OutputTokens: 1_000},
			100_000*0.10/1e6 + 1_000*0.50/1e6},
		{"100K+1 prompt is long", "claude-haiku-5-5",
			Usage{InputTokens: 100_001, OutputTokens: 1_000},
			100_001*0.50/1e6 + 1_000*2.50/1e6},
		{"cache tokens count toward the prompt", "claude-haiku-5-5",
			Usage{InputTokens: 1, CacheReadTokens: 60_000, CacheCreationTokens: 40_000, OutputTokens: 1_000},
			1*0.50/1e6 + 60_000*0.05/1e6 + 40_000*0.625/1e6 + 1_000*2.50/1e6},
		{"cache tokens at the boundary stay base", "claude-haiku-5-5",
			Usage{CacheReadTokens: 60_000, CacheCreationTokens: 40_000, OutputTokens: 1_000},
			60_000*0.01/1e6 + 40_000*0.125/1e6 + 1_000*0.50/1e6},
		{"versioned model name", "claude-haiku-5-5-20261001",
			Usage{InputTokens: 200_000},
			200_000 * 0.50 / 1e6},
		{"other models have one card", "claude-sonnet-5-5",
			Usage{InputTokens: 200_000},
			200_000 * 2.0 / 1e6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.Cost(tc.model, tc.usage); math.Abs(got-tc.want) > 1e-12 {
				t.Errorf("Cost(%q) = %g, want %g", tc.model, got, tc.want)
			}
		})
	}
}

func TestBookLoad_DBRowReplacesFallbackRow(t *testing.T) {
	t.Parallel()
	b := NewBook()
	long := Usage{InputTokens: 200_000}

	b.Load(Table{"claude-haiku-5-5": {
		InputPerMTok: 1, OutputPerMTok: 1, CacheReadPerMTok: 1, CacheCreatePerMTok: 1,
		LongContextThreshold: 150_000, LongInputPerMTok: 3,
	}})
	if got, want := b.Cost("claude-haiku-5-5", long), 200_000*3.0/1e6; math.Abs(got-want) > 1e-12 {
		t.Errorf("DB tier: cost = %g, want %g", got, want)
	}
	if got, want := b.Cost("claude-haiku-5-5", Usage{InputTokens: 150_000}), 150_000*1.0/1e6; math.Abs(got-want) > 1e-12 {
		t.Errorf("DB base at DB threshold: cost = %g, want %g", got, want)
	}

	b.Load(Table{"claude-haiku-5-5": {InputPerMTok: 1, OutputPerMTok: 1, CacheReadPerMTok: 1, CacheCreatePerMTok: 1}})
	if got, want := b.Cost("claude-haiku-5-5", long), 200_000*1.0/1e6; math.Abs(got-want) > 1e-12 {
		t.Errorf("DB row without tier: cost = %g, want %g (no fallback tier)", got, want)
	}

	b.Load(nil)
	if got, want := b.Cost("claude-haiku-5-5", long), 200_000*0.50/1e6; math.Abs(got-want) > 1e-12 {
		t.Errorf("after Load(nil): cost = %g, want %g (fallback tier)", got, want)
	}
}

func TestBookLoad_LongestPrefixWins(t *testing.T) {
	t.Parallel()
	b := NewBook()
	b.Load(Table{
		"claude-opus":   {InputPerMTok: 99},
		"claude-zeta-1": {InputPerMTok: 7},
	})
	u := Usage{InputTokens: 1_000_000}
	if got := b.Cost("claude-opus-4-6-20260101", u); got != 5.0 {
		t.Errorf("opus-4-6 with a shorter DB prefix: cost = %g, want 5", got)
	}
	if got := b.Cost("claude-opus-9", u); got != 99 {
		t.Errorf("claude-opus-9: cost = %g, want 99 (DB prefix)", got)
	}
	if got := b.Cost("claude-zeta-1-20270101", u); got != 7 {
		t.Errorf("DB-only model: cost = %g, want 7", got)
	}
	if got := b.Cost("claude-sonnet-4-6", u); got != 3.0 {
		t.Errorf("model the DB has no row for: cost = %g, want 3 (fallback)", got)
	}
}

func TestBookCost_UnknownModelIgnoresDBSonnetRow(t *testing.T) {
	t.Parallel()
	b := NewBook()
	b.Load(Table{"claude-sonnet-4-6": {InputPerMTok: 42}})
	if got := b.Cost("gpt-unknown", Usage{InputTokens: 1_000_000}); got != 3.0 {
		t.Errorf("unknown model: cost = %g, want 3 (compiled Sonnet 4.6)", got)
	}
}

// legacyPricing is the parser's table, tier table and formula as they were
// before this package, kept verbatim to pin cost results.
type legacyPricing struct{ in, out, cr, cc float64 }

type legacyTier struct {
	threshold int64
	rates     legacyPricing
}

var legacyTable = map[string]legacyPricing{
	"claude-fable-5-1":  {10.0, 50.0, 0.25, 12.50},
	"claude-mythos-5-1": {10.0, 50.0, 0.25, 12.50},
	"claude-fable-5":    {10.0, 50.0, 1.0, 12.50},
	"claude-mythos-5":   {10.0, 50.0, 1.0, 12.50},
	"claude-opus-5-5":   {4.0, 20.0, 0.20, 5.0},
	"claude-opus-5":     {5.0, 25.0, 0.50, 6.25},
	"claude-opus-4-8":   {5.0, 25.0, 0.50, 6.25},
	"claude-opus-4-7":   {5.0, 25.0, 0.50, 6.25},
	"claude-opus-4-6":   {5.0, 25.0, 0.50, 6.25},
	"claude-opus-4-5":   {5.0, 25.0, 0.50, 6.25},
	"claude-sonnet-5-5": {2.0, 10.0, 0.20, 2.50},
	"claude-sonnet-5":   {2.0, 10.0, 0.20, 2.50},
	"claude-sonnet-4-6": {3.0, 15.0, 0.30, 3.75},
	"claude-haiku-5-5":  {0.10, 0.50, 0.01, 0.125},
	"claude-haiku-4-5":  {1.0, 5.0, 0.10, 1.25},
}

var legacyLongTable = map[string]legacyTier{
	"claude-haiku-5-5": {100_000, legacyPricing{0.50, 2.50, 0.05, 0.625}},
}

func legacyMerge(db Table) (map[string]legacyPricing, map[string]legacyTier) {
	base := map[string]legacyPricing{}
	long := map[string]legacyTier{}
	for k, v := range legacyTable {
		base[k] = v
	}
	for k, v := range legacyLongTable {
		long[k] = v
	}
	for prefix, p := range db {
		base[prefix] = legacyPricing{p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheCreatePerMTok}
		if p.LongContextThreshold > 0 {
			long[prefix] = legacyTier{p.LongContextThreshold, legacyPricing{
				p.LongInputPerMTok, p.LongOutputPerMTok, p.LongCacheReadPerMTok, p.LongCacheCreatePerMTok,
			}}
		} else {
			delete(long, prefix)
		}
	}
	return base, long
}

func legacyCost(base map[string]legacyPricing, long map[string]legacyTier, model string, u Usage) float64 {
	prompt := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens
	p := legacyTable["claude-sonnet-4-6"]
	key, found := "", false
	if _, ok := base[model]; ok {
		key, found = model, true
	} else {
		keys := make([]string, 0, len(base))
		for k := range base {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		for _, k := range keys {
			if len(model) > len(k) && model[:len(k)] == k {
				key, found = k, true
				break
			}
		}
	}
	if found {
		p = base[key]
		if tier, ok := long[key]; ok && prompt > tier.threshold {
			p = tier.rates
		}
	}
	return float64(u.InputTokens)*p.in/1e6 +
		float64(u.OutputTokens)*p.out/1e6 +
		float64(u.CacheReadTokens)*p.cr/1e6 +
		float64(u.CacheCreationTokens)*p.cc/1e6
}

func TestBookCost_IdenticalToLegacy(t *testing.T) {
	t.Parallel()
	usages := []Usage{
		{},
		{InputTokens: 1},
		{InputTokens: 512, OutputTokens: 64, CacheReadTokens: 128},
		{InputTokens: 100, OutputTokens: 200, CacheReadTokens: 400, CacheCreationTokens: 300},
		{InputTokens: 100_000, OutputTokens: 1_000},
		{InputTokens: 100_001, OutputTokens: 1_000},
		{InputTokens: 3, CacheReadTokens: 60_000, CacheCreationTokens: 39_997, OutputTokens: 7},
		{InputTokens: 3, CacheReadTokens: 60_000, CacheCreationTokens: 39_998, OutputTokens: 7},
		{InputTokens: 123_457, OutputTokens: 98_765, CacheReadTokens: 4_321_987, CacheCreationTokens: 54_321},
		{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheCreationTokens: 1_000_000},
	}
	models := []string{"", "<synthetic>", "claude-unknown-99", "claude", "gpt-4o"}
	for k := range legacyTable {
		models = append(models, k, k+"-20261001", k+"[1m]", k[:len(k)-1])
	}
	dbs := map[string]Table{
		"fallback only": nil,
		"seeded rows":   {"claude-opus-4-6": {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.50, CacheCreatePerMTok: 6.25}},
		"edited haiku 5.5 keeps tier": {"claude-haiku-5-5": {
			InputPerMTok: 0.10, OutputPerMTok: 0.60, CacheReadPerMTok: 0.01, CacheCreatePerMTok: 0.125,
			LongContextThreshold: 100_000, LongInputPerMTok: 0.50, LongOutputPerMTok: 2.50, LongCacheReadPerMTok: 0.05, LongCacheCreatePerMTok: 0.625,
		}},
		"haiku 5.5 row without tier": {"claude-haiku-5-5": {InputPerMTok: 0.10, OutputPerMTok: 0.50, CacheReadPerMTok: 0.01, CacheCreatePerMTok: 0.125}},
		"tier on another model":      {"claude-sonnet-5": {InputPerMTok: 2, OutputPerMTok: 10, CacheReadPerMTok: 0.2, CacheCreatePerMTok: 2.5, LongContextThreshold: 200_000, LongInputPerMTok: 4, LongOutputPerMTok: 15, LongCacheReadPerMTok: 0.4, LongCacheCreatePerMTok: 5}},
		"short and new prefixes":     {"claude-opus": {InputPerMTok: 99, OutputPerMTok: 1}, "claude-": {InputPerMTok: 0.3}, "gpt": {OutputPerMTok: 0.7}},
	}
	for name, db := range dbs {
		b := NewBook()
		b.Load(db)
		base, long := legacyMerge(db)
		for _, m := range models {
			for _, u := range usages {
				got, want := b.Cost(m, u), legacyCost(base, long, m, u)
				if got != want {
					t.Errorf("%s: Cost(%q, %+v) = %v, legacy %v", name, m, u, got, want)
				}
			}
		}
	}
}
