package ccmeter

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

// Where a price came from.
const (
	priceFromOverride = "override"
	priceFromBuiltin  = "builtin"
)

// priceMatch is the price a model resolved to, and why.
type priceMatch struct {
	Price  Price
	Long   *Price // for prompts over longPromptTokens, when the model charges more
	Source string // priceFromOverride or priceFromBuiltin
	Match  string // the override's key, or the built-in rule's label
}

// Pricer resolves a model name to a price. User overrides win (longest
// matching substring), then the built-in rules.
type Pricer struct {
	overrides map[string]Price
}

func newPricer(overrides map[string]Price) *Pricer {
	return &Pricer{overrides: overrides}
}

func (p *Pricer) resolve(model string) (priceMatch, bool) {
	m := strings.ToLower(model)
	best, bestKey, found := Price{}, "", false
	for k, v := range p.overrides {
		if !strings.Contains(m, strings.ToLower(k)) {
			continue
		}
		// Keys of equal length are settled by name, so the answer never depends
		// on map order.
		if !found || len(k) > len(bestKey) || (len(k) == len(bestKey) && k < bestKey) {
			best, bestKey, found = v, k, true
		}
	}
	if found {
		return priceMatch{Price: best.filled(), Source: priceFromOverride, Match: bestKey}, true
	}
	for _, r := range builtinPrices {
		if r.matches(m) {
			return priceMatch{Price: r.Price.filled(), Long: r.Long, Source: priceFromBuiltin, Match: r.Label}, true
		}
	}
	return priceMatch{}, false
}

func (p *Pricer) lookup(model string) (Price, bool) {
	match, ok := p.resolve(model)
	return match.Price, ok
}

// pricesCheckedOn is when builtinPrices was last compared with pricesSourceURL.
// Change it whenever the table is checked again.
const (
	pricesCheckedOn = "2026-11-02"
	pricesSourceURL = "https://platform.claude.com/docs/en/about-claude/pricing"
)

// longPromptTokens is the prompt length above which Haiku 5.5 charges its higher
// prices. A prompt counts all its input tokens, cache reads and writes included.
const longPromptTokens = 100_000

// priceRule prices the models it names, or with Family, any model of that family
// not named by an earlier rule.
type priceRule struct {
	Label  string
	Names  []string
	Family string
	Price  Price
	Long   *Price
}

func (r priceRule) matches(m string) bool {
	for _, n := range r.Names {
		if isModelVersion(m, n) {
			return true
		}
	}
	return r.Family != "" && strings.Contains(m, r.Family)
}

// isModelVersion reports whether m is exactly this version: "sonnet-5" matches
// claude-sonnet-5 and claude-sonnet-5-20260101, but not claude-sonnet-5-5.
func isModelVersion(m, name string) bool {
	i := strings.Index(m, name)
	if i < 0 {
		return false
	}
	rest := m[i+len(name):]
	if rest == "" {
		return true
	}
	if rest[0] != '-' {
		return rest[0] < '0' || rest[0] > '9' // "@date", ":0", "[1m]"
	}
	rest = rest[1:]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	// A date follows the name, or a word such as -latest or -v1. A short number
	// is a different version.
	return digits == 0 || digits >= 8
}

// builtinPrices are the public Claude API list prices at pricesCheckedOn. The
// first matching rule wins, so a family's fallback comes after its versions. A
// version too new to be named here takes its family's newest known price, and a
// model of no known family is reported as "unpriced"; both can be set under
// "pricing" in the config file.
var builtinPrices = []priceRule{
	{Label: "Fable 5.1, Mythos 5.1", Names: []string{"fable-5-1", "mythos-5-1"}, Price: Price{Input: 10, Output: 50, CacheRead: 0.25}},
	{Label: "Fable 5, Mythos 5", Names: []string{"fable-5", "mythos-5"}, Price: Price{Input: 10, Output: 50}},
	{Label: "Other Fable", Family: "fable", Price: Price{Input: 10, Output: 50, CacheRead: 0.25}},

	{Label: "Opus 5.5", Names: []string{"opus-5-5"}, Price: Price{Input: 4, Output: 20, CacheRead: 0.20}},
	{Label: "Opus 5, 4.8, 4.7, 4.6, 4.5", Names: []string{"opus-5", "opus-4-8", "opus-4-7", "opus-4-6", "opus-4-5"}, Price: Price{Input: 5, Output: 25}},
	{Label: "Opus 4.1, 4, Claude 3 Opus", Names: []string{"opus-4-1", "opus-4", "3-opus"}, Price: Price{Input: 15, Output: 75}},
	{Label: "Other Opus", Family: "opus", Price: Price{Input: 4, Output: 20, CacheRead: 0.20}},

	{Label: "Sonnet 5.5", Names: []string{"sonnet-5-5"}, Price: Price{Input: 2, Output: 10, CacheRead: 0.10}},
	{Label: "Sonnet 5", Names: []string{"sonnet-5"}, Price: Price{Input: 2, Output: 10}},
	{Label: "Sonnet 4.6, 4.5, 4, Claude 3", Names: []string{"sonnet-4-6", "sonnet-4-5", "sonnet-4", "3-7-sonnet", "3-5-sonnet", "3-sonnet"}, Price: Price{Input: 3, Output: 15}},
	{Label: "Other Sonnet", Family: "sonnet", Price: Price{Input: 2, Output: 10, CacheRead: 0.10}},

	{Label: "Haiku 5.5", Names: []string{"haiku-5-5"}, Price: Price{Input: 0.10, Output: 0.50}, Long: &Price{Input: 0.50, Output: 2.50}},
	{Label: "Haiku 4.5", Names: []string{"haiku-4-5"}, Price: Price{Input: 1, Output: 5}},
	{Label: "Haiku 3.5", Names: []string{"3-5-haiku", "haiku-3-5"}, Price: Price{Input: 0.8, Output: 4}},
	{Label: "Claude 3 Haiku", Names: []string{"3-haiku", "haiku-3"}, Price: Price{Input: 0.25, Output: 1.25}},
	{Label: "Other Haiku", Family: "haiku", Price: Price{Input: 0.10, Output: 0.50}, Long: &Price{Input: 0.50, Output: 2.50}},
}

func (p *Pricer) cost(e Entry) (float64, bool) {
	match, ok := p.resolve(e.Model)
	if !ok {
		return 0, false
	}
	pr := match.Price
	if match.Long != nil && e.In+e.CW5+e.CW1h+e.CR > longPromptTokens {
		pr = match.Long.filled()
	}
	c := float64(e.In)*pr.Input +
		float64(e.Out)*pr.Output +
		float64(e.CW5)*pr.CacheWrite +
		float64(e.CW1h)*pr.CacheWrite1h +
		float64(e.CR)*pr.CacheRead
	return c / 1e6, true
}
