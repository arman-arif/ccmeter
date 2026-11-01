package main

import "strings"

// Price is USD per million tokens. Cache prices default to the standard
// multipliers of the input price when left at zero.
type Price struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheWrite   float64 `json:"cache_write,omitempty"`    // 5-minute cache write (default 1.25x input)
	CacheWrite1h float64 `json:"cache_write_1h,omitempty"` // 1-hour cache write (default 2x input)
	CacheRead    float64 `json:"cache_read,omitempty"`     // default 0.1x input
}

func (p Price) filled() Price {
	if p.CacheWrite == 0 {
		p.CacheWrite = p.Input * 1.25
	}
	if p.CacheWrite1h == 0 {
		p.CacheWrite1h = p.Input * 2
	}
	if p.CacheRead == 0 {
		p.CacheRead = p.Input * 0.1
	}
	return p
}

// Pricer resolves a model name to a price. User overrides win (longest
// matching substring), then the built-in family rules.
type Pricer struct {
	overrides map[string]Price
}

func newPricer(overrides map[string]Price) *Pricer {
	return &Pricer{overrides: overrides}
}

func (p *Pricer) lookup(model string) (Price, bool) {
	m := strings.ToLower(model)
	best, bestLen := Price{}, -1
	for k, v := range p.overrides {
		if strings.Contains(m, strings.ToLower(k)) && len(k) > bestLen {
			best, bestLen = v, len(k)
		}
	}
	if bestLen >= 0 {
		return best.filled(), true
	}
	if pr, ok := builtinPrice(m); ok {
		return pr.filled(), true
	}
	return Price{}, false
}

// builtinPrice covers the public Claude API list prices known at build time.
// Anything not matched here is reported as "unpriced"; add it under
// "pricing" in the config file.
func builtinPrice(m string) (Price, bool) {
	switch {
	case strings.Contains(m, "opus"):
		// Opus 4 / 4.1 and Claude 3 Opus used the older, higher price.
		if strings.Contains(m, "opus-4-1") || strings.Contains(m, "opus-4-2025") ||
			strings.Contains(m, "opus-4@") || strings.HasSuffix(m, "opus-4") ||
			strings.Contains(m, "3-opus") {
			return Price{Input: 15, Output: 75}, true
		}
		return Price{Input: 5, Output: 25}, true
	case strings.Contains(m, "sonnet"):
		return Price{Input: 3, Output: 15}, true
	case strings.Contains(m, "haiku"):
		if strings.Contains(m, "3-5-haiku") || strings.Contains(m, "haiku-3-5") {
			return Price{Input: 0.8, Output: 4}, true
		}
		if strings.Contains(m, "3-haiku") {
			return Price{Input: 0.25, Output: 1.25}, true
		}
		return Price{Input: 1, Output: 5}, true
	}
	return Price{}, false
}

func (p *Pricer) cost(e Entry) (float64, bool) {
	pr, ok := p.lookup(e.Model)
	if !ok {
		return 0, false
	}
	c := float64(e.In)*pr.Input +
		float64(e.Out)*pr.Output +
		float64(e.CW5)*pr.CacheWrite +
		float64(e.CW1h)*pr.CacheWrite1h +
		float64(e.CR)*pr.CacheRead
	return c / 1e6, true
}
