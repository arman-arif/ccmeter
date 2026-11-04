package ccmeter

import "testing"

func TestBuildPrices(t *testing.T) {
	pr := newPricer(map[string]Price{"fable": {Input: 1, Output: 2}})
	rep := buildPrices([]Summary{
		{Name: "work", Models: []ModelRow{
			{Model: "claude-sonnet-5-5", Totals: Totals{Messages: 2, Tokens: 20, CostUSD: 1}},
			{Model: "claude-opus-5-5", Totals: Totals{Messages: 1, Tokens: 10, CostUSD: 5}},
			{Model: "mystery", Totals: Totals{Messages: 1, Tokens: 5}},
		}},
		{Name: "home", Models: []ModelRow{
			{Model: "claude-sonnet-5-5", Totals: Totals{Messages: 3, Tokens: 30, CostUSD: 1}},
			{Model: "claude-fable-5-1", Totals: Totals{Messages: 1, Tokens: 1, CostUSD: 0.5}},
		}},
		{Name: "empty"},
	}, pr, "/cfg.json")

	var order []string
	for _, u := range rep.Used {
		order = append(order, u.Model)
	}
	want := []string{"mystery", "claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got %v, want %v", order, want)
		}
	}

	if u := rep.Used[0]; u.Price != nil || u.Source != "" {
		t.Errorf("mystery should be unpriced: %+v", u)
	}
	if u := rep.Used[1]; u.Source != priceFromBuiltin || u.Match != "Opus 5.5" || u.Price.CacheRead != 0.20 {
		t.Errorf("opus: %+v", u)
	}
	s := rep.Used[2]
	if s.Messages != 5 || s.Tokens != 50 || s.CostUSD != 2 || len(s.Accounts) != 2 || s.Accounts[0] != "work" || s.Accounts[1] != "home" {
		t.Errorf("sonnet should add up across accounts: %+v", s)
	}
	if u := rep.Used[3]; u.Source != priceFromOverride || u.Match != "fable" || u.Price.Input != 1 {
		t.Errorf("fable: %+v", u)
	}

	if len(rep.Overrides) != 1 || rep.Overrides[0].Name != "fable" || rep.Overrides[0].Price.CacheRead != 0.1 {
		t.Errorf("overrides: %+v", rep.Overrides)
	}
	if rep.ConfigPath != "/cfg.json" || rep.CheckedOn != pricesCheckedOn || rep.SourceURL != pricesSourceURL {
		t.Errorf("header: %+v", rep)
	}

	listed := map[string]PriceView{}
	for _, b := range rep.Builtin {
		listed[b.Name] = b.Price
	}
	if p, ok := listed["Haiku 5.5, prompts over 100K tokens"]; !ok || p.Input != 0.50 || p.Output != 2.50 {
		t.Errorf("Haiku 5.5's higher rate should be listed: %+v", listed)
	}
	if _, ok := listed["Other Opus"]; ok {
		t.Errorf("fallbacks repeat a listed price and should be left out: %+v", listed)
	}
}

func TestBuildPricesNeverSendsNullLists(t *testing.T) {
	rep := buildPrices(nil, newPricer(nil), "")
	if rep.Used == nil || rep.Overrides == nil || rep.Builtin == nil {
		t.Errorf("lists should be empty, not nil: %+v", rep)
	}
}
