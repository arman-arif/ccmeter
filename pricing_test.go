package main

import (
	"math"
	"testing"
)

// Prices from pricesSourceURL at pricesCheckedOn: input, 5-minute cache write,
// 1-hour cache write, cache read, output.
func TestBuiltinPricesMatchPublishedTable(t *testing.T) {
	tests := []struct {
		model                  string
		in, cw5, cw1h, cr, out float64
	}{
		{"claude-fable-5-1", 10, 12.5, 20, 0.25, 50},
		{"claude-mythos-5-1", 10, 12.5, 20, 0.25, 50},
		{"claude-fable-5", 10, 12.5, 20, 1, 50},
		{"claude-mythos-5", 10, 12.5, 20, 1, 50},
		{"claude-opus-5-5", 4, 5, 8, 0.20, 20},
		{"claude-opus-5", 5, 6.25, 10, 0.50, 25},
		{"claude-opus-4-8", 5, 6.25, 10, 0.50, 25},
		{"claude-opus-4-7", 5, 6.25, 10, 0.50, 25},
		{"claude-opus-4-6", 5, 6.25, 10, 0.50, 25},
		{"claude-opus-4-5", 5, 6.25, 10, 0.50, 25},
		{"claude-opus-4-5-20251101", 5, 6.25, 10, 0.50, 25},
		{"claude-opus-4-1-20250805", 15, 18.75, 30, 1.50, 75},
		{"claude-opus-4-20250514", 15, 18.75, 30, 1.50, 75},
		{"claude-sonnet-5-5", 2, 2.50, 4, 0.10, 10},
		{"claude-sonnet-5", 2, 2.50, 4, 0.20, 10},
		{"claude-sonnet-4-6", 3, 3.75, 6, 0.30, 15},
		{"claude-sonnet-4-5-20250929", 3, 3.75, 6, 0.30, 15},
		{"claude-sonnet-4-20250514", 3, 3.75, 6, 0.30, 15},
		{"claude-haiku-5-5", 0.10, 0.125, 0.20, 0.01, 0.50},
		{"claude-haiku-4-5", 1, 1.25, 2, 0.10, 5},
		{"claude-3-5-haiku-20241022", 0.80, 1, 1.60, 0.08, 4},
	}
	pr := newPricer(nil)
	for _, tt := range tests {
		p, ok := pr.lookup(tt.model)
		if !ok {
			t.Errorf("%s: unpriced", tt.model)
			continue
		}
		got := []float64{p.Input, p.CacheWrite, p.CacheWrite1h, p.CacheRead, p.Output}
		want := []float64{tt.in, tt.cw5, tt.cw1h, tt.cr, tt.out}
		for i := range got {
			if math.Abs(got[i]-want[i]) > 1e-9 {
				t.Errorf("%s: got %v, want %v", tt.model, got, want)
				break
			}
		}
	}
}

func TestPriceRuleForEachModelName(t *testing.T) {
	tests := []struct{ model, rule string }{
		{"claude-sonnet-5-5", "Sonnet 5.5"},
		{"claude-sonnet-5-20260101", "Sonnet 5"},
		{"anthropic.claude-sonnet-5-5", "Sonnet 5.5"},
		{"claude-opus-4-5@20251101", "Opus 5, 4.8, 4.7, 4.6, 4.5"},
		{"anthropic.claude-opus-4-1-20250805-v1:0", "Opus 4.1, 4, Claude 3 Opus"},
		{"claude-opus-4-latest", "Opus 4.1, 4, Claude 3 Opus"},
		{"claude-opus-5-5[1m]", "Opus 5.5"},
		{"claude-3-opus-20240229", "Opus 4.1, 4, Claude 3 Opus"},
		{"claude-3-5-sonnet-20241022", "Sonnet 4.6, 4.5, 4, Claude 3"},
		{"claude-3-haiku-20240307", "Claude 3 Haiku"},
		// Versions newer than the table, and bare aliases, take the family's newest price.
		{"claude-opus-6", "Other Opus"},
		{"claude-sonnet-5-7", "Other Sonnet"},
		{"claude-fable-6", "Other Fable"},
		{"CLAUDE-HAIKU-9", "Other Haiku"},
		{"haiku", "Other Haiku"},
	}
	pr := newPricer(nil)
	for _, tt := range tests {
		m, ok := pr.resolve(tt.model)
		if !ok || m.Source != priceFromBuiltin || m.Match != tt.rule {
			t.Errorf("%s: got %q (%q, %v), want %q", tt.model, m.Match, m.Source, ok, tt.rule)
		}
	}
	// A preview has no published price, so it must not borrow one.
	for _, model := range []string{"claude-mythos-preview", "gpt-5", ""} {
		if _, ok := pr.resolve(model); ok {
			t.Errorf("%s: should be unpriced", model)
		}
	}
}

func TestOverridesWinAndLongestKeyMatches(t *testing.T) {
	pr := newPricer(map[string]Price{
		"sonnet":     {Input: 7, Output: 8},
		"sonnet-5-5": {Input: 9, Output: 10},
		"FABLE":      {Input: 11, Output: 12},
		"aaa-5-5":    {Input: 1, Output: 1},
		"bbb-5-5":    {Input: 2, Output: 2},
	})
	m, _ := pr.resolve("claude-sonnet-5-5")
	if m.Source != priceFromOverride || m.Match != "sonnet-5-5" || m.Price.Input != 9 {
		t.Errorf("longest key should win: %+v", m)
	}
	if m.Price.CacheRead != 0.9 {
		t.Errorf("cache read should default to 0.1x input: %v", m.Price.CacheRead)
	}
	if m, _ := pr.resolve("claude-sonnet-4-6"); m.Match != "sonnet" {
		t.Errorf("shorter key should still apply: %+v", m)
	}
	if m, _ := pr.resolve("claude-fable-5-1"); m.Match != "FABLE" {
		t.Errorf("keys should match in any case: %+v", m)
	}
	if m, _ := pr.resolve("aaa-5-5-bbb-5-5"); m.Match != "aaa-5-5" {
		t.Errorf("a tie should settle by name: %+v", m)
	}
	if m, _ := pr.resolve("claude-opus-5-5"); m.Source != priceFromBuiltin {
		t.Errorf("no override applies: %+v", m)
	}
}

func TestCost(t *testing.T) {
	pr := newPricer(nil)
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

	// 1M of each kind at Opus 5.5's prices.
	e := Entry{Model: "claude-opus-5-5", In: 1e6, Out: 1e6, CW5: 1e6, CW1h: 1e6, CR: 1e6}
	if c, ok := pr.cost(e); !ok || !near(c, 4+20+5+8+0.2) {
		t.Errorf("opus: %v %v", c, ok)
	}

	// Haiku 5.5 charges a whole request more once its prompt passes 100K tokens,
	// counting cache reads and writes.
	short := Entry{Model: "claude-haiku-5-5", In: 1000, Out: 1000, CR: 99_000}
	if c, _ := pr.cost(short); !near(c, (1000*0.10+1000*0.50+99_000*0.01)/1e6) {
		t.Errorf("at the limit: %v", c)
	}
	long := Entry{Model: "claude-haiku-5-5", In: 1001, Out: 1000, CR: 99_000}
	if c, _ := pr.cost(long); !near(c, (1001*0.50+1000*2.50+99_000*0.05)/1e6) {
		t.Errorf("over the limit: %v", c)
	}
	// Only Haiku 5.5 has a second rate.
	big := Entry{Model: "claude-sonnet-5-5", In: 200_000}
	if c, _ := pr.cost(big); !near(c, 0.4) {
		t.Errorf("sonnet: %v", c)
	}
	// An override replaces both of Haiku 5.5's rates.
	flat := newPricer(map[string]Price{"haiku": {Input: 1, Output: 1}})
	if c, _ := flat.cost(Entry{Model: "claude-haiku-5-5", In: 200_000}); !near(c, 0.2) {
		t.Errorf("override: %v", c)
	}

	if c, ok := pr.cost(Entry{Model: "mystery", In: 1e6}); ok || c != 0 {
		t.Errorf("unpriced: %v %v", c, ok)
	}
}
