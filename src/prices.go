package ccmeter

import "sort"

// The report behind the dashboard's Prices page.

// PriceView is a Price with its cache prices filled in, all of them always sent.
type PriceView struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheWrite   float64 `json:"cache_write"`
	CacheWrite1h float64 `json:"cache_write_1h"`
	CacheRead    float64 `json:"cache_read"`
}

// ListedPrice is one row of a price list.
type ListedPrice struct {
	Name  string    `json:"name"`
	Price PriceView `json:"price"`
}

// UsedPrice is a model found in the logs and the price ccmeter charges it at.
type UsedPrice struct {
	Model    string     `json:"model"`
	Source   string     `json:"source"` // "override", "builtin", or "" when unpriced
	Match    string     `json:"match,omitempty"`
	Price    *PriceView `json:"price,omitempty"`
	Accounts []string   `json:"accounts"`
	Messages int        `json:"messages"`
	Tokens   int64      `json:"tokens"`
	CostUSD  float64    `json:"cost_usd"`
}

// PricesReport is what the dashboard's Prices page shows.
type PricesReport struct {
	CheckedOn  string        `json:"checked_on"`
	SourceURL  string        `json:"source_url"`
	ConfigPath string        `json:"config_path"`
	Used       []UsedPrice   `json:"used"`
	Overrides  []ListedPrice `json:"overrides"`
	Builtin    []ListedPrice `json:"builtin"`
}

func viewOf(p Price) PriceView { return PriceView(p.filled()) }

// buildPrices lists the models in the given summaries' last 30 days with their
// prices (unpriced ones first, as they need attention, then by cost), the user's
// overrides and the built-in rules.
func buildPrices(summaries []Summary, pr *Pricer, configPath string) PricesReport {
	rep := PricesReport{
		CheckedOn: pricesCheckedOn, SourceURL: pricesSourceURL, ConfigPath: configPath,
		Used: []UsedPrice{}, Overrides: []ListedPrice{}, Builtin: []ListedPrice{},
	}

	used := map[string]*UsedPrice{}
	for _, s := range summaries {
		for _, m := range s.Models {
			u := used[m.Model]
			if u == nil {
				u = &UsedPrice{Model: m.Model, Accounts: []string{}}
				if match, ok := pr.resolve(m.Model); ok {
					view := viewOf(match.Price)
					u.Source, u.Match, u.Price = match.Source, match.Match, &view
				}
				used[m.Model] = u
			}
			u.Accounts = append(u.Accounts, s.Name)
			u.Messages += m.Messages
			u.Tokens += m.Tokens
			u.CostUSD += m.CostUSD
		}
	}
	for _, u := range used {
		rep.Used = append(rep.Used, *u)
	}
	sort.Slice(rep.Used, func(i, j int) bool {
		a, b := rep.Used[i], rep.Used[j]
		if (a.Price == nil) != (b.Price == nil) {
			return a.Price == nil
		}
		if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		return a.Model < b.Model
	})

	for k, v := range pr.overrides {
		rep.Overrides = append(rep.Overrides, ListedPrice{Name: k, Price: viewOf(v)})
	}
	sort.Slice(rep.Overrides, func(i, j int) bool { return rep.Overrides[i].Name < rep.Overrides[j].Name })

	for _, r := range builtinPrices {
		if r.Family != "" {
			continue // a fallback repeats a price already listed under its version
		}
		if r.Long == nil {
			rep.Builtin = append(rep.Builtin, ListedPrice{Name: r.Label, Price: viewOf(r.Price)})
			continue
		}
		rep.Builtin = append(rep.Builtin,
			ListedPrice{Name: r.Label + ", prompts up to 100K tokens", Price: viewOf(r.Price)},
			ListedPrice{Name: r.Label + ", prompts over 100K tokens", Price: viewOf(*r.Long)})
	}
	return rep
}
