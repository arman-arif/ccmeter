package main

import (
	"sort"
	"time"
)

const blockLen = 5 * time.Hour

type Totals struct {
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheWrite int64   `json:"cache_write"`
	CacheRead  int64   `json:"cache_read"`
	Tokens     int64   `json:"tokens"`
	CostUSD    float64 `json:"cost_usd"`
	Messages   int     `json:"messages"`
}

func (t *Totals) add(e Entry, cost float64) {
	t.Input += e.In
	t.Output += e.Out
	t.CacheWrite += e.CW5 + e.CW1h
	t.CacheRead += e.CR
	t.Tokens += e.Total()
	t.CostUSD += cost
	t.Messages++
}

type Block struct {
	Active       bool      `json:"active"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	RemainingMin int       `json:"remaining_min"`
	Totals
	BurnUSDPerHour float64 `json:"burn_usd_per_hour"`
	ProjectedUSD   float64 `json:"projected_usd"`
	LimitUSD       float64 `json:"limit_usd"`
	LimitSource    string  `json:"limit_source"` // "config", "peak" or ""
	Pct            float64 `json:"pct"`
}

type Period struct {
	Totals
	LimitUSD float64 `json:"limit_usd,omitempty"`
	Pct      float64 `json:"pct,omitempty"`
}

type DayPoint struct {
	Date string `json:"date"`
	Totals
}

type ModelRow struct {
	Model string `json:"model"`
	Totals
	Priced bool `json:"priced"`
}

type Summary struct {
	Name         string      `json:"name"`
	Dir          string      `json:"dir"`
	Found        bool        `json:"found"`
	Error        string      `json:"error,omitempty"`
	Files        int         `json:"files"`
	LastActivity *time.Time  `json:"last_activity,omitempty"`
	Block        Block       `json:"block"`
	Today        Period      `json:"today"`
	Week         Period      `json:"week"`  // rolling 7 days
	Month        Period      `json:"month"` // rolling 30 days
	Daily        []DayPoint  `json:"daily"` // last 30 days, oldest first
	Models       []ModelRow  `json:"models"`
	Unpriced     []string    `json:"unpriced,omitempty"`
	Plan         *PlanLimits `json:"plan,omitempty"`       // real plan limits
	PlanError    string      `json:"plan_error,omitempty"` // last fetch problem, if recent
}

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Accounts    []Summary `json:"accounts"`
	Total       struct {
		Today Totals `json:"today"`
		Week  Totals `json:"week"`
		Month Totals `json:"month"`
	} `json:"total"`
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

type blockSpan struct {
	start, last time.Time
	tot         Totals
}

// splitBlocks groups entries into 5-hour windows the way Claude Code's
// session limit works: a window starts at the hour of the first message and
// a new one begins after it expires or after a 5-hour gap.
func splitBlocks(es []Entry, costs []float64) []blockSpan {
	var out []blockSpan
	for i, e := range es {
		n := len(out)
		if n == 0 || !e.T.Before(out[n-1].start.Add(blockLen)) || e.T.Sub(out[n-1].last) >= blockLen {
			out = append(out, blockSpan{start: e.T.Truncate(time.Hour)})
			n++
		}
		b := &out[n-1]
		b.last = e.T
		b.tot.add(e, costs[i])
	}
	return out
}

// sortModels puts the most expensive first, with a fixed order for ties so
// rows don't swap places between refreshes.
func sortModels(rows []ModelRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CostUSD != rows[j].CostUSD {
			return rows[i].CostUSD > rows[j].CostUSD
		}
		return rows[i].Model < rows[j].Model
	})
}

func summarize(a *Account, pr *Pricer, now time.Time) Summary {
	s, _ := summarizeEntries(a, pr, now)
	return s
}

// summarizeEntries also returns the entries it used, oldest first.
func summarizeEntries(a *Account, pr *Pricer, now time.Time) (Summary, []Entry) {
	cfg, es, found, errStr, files := a.snapshot()
	s := Summary{Name: cfg.Name, Dir: cfg.Dir, Found: found, Error: errStr, Files: files}
	s.Plan = loadLimits(cfg.Dir, now)
	if st := readFetchState(cfg.Dir); st.LastError != "" && now.Sub(st.LastAttempt) < 24*time.Hour {
		s.PlanError = st.LastError
	}
	sort.Slice(es, func(i, j int) bool { return es[i].T.Before(es[j].T) })

	local := now.Local()
	today0 := startOfDay(local)
	week0 := now.Add(-7 * 24 * time.Hour)
	month0 := today0.AddDate(0, 0, -29)

	costs := make([]float64, len(es))
	daily := map[string]*DayPoint{}
	models := map[string]*ModelRow{}
	unpriced := map[string]bool{}

	for i, e := range es {
		c, ok := pr.cost(e)
		costs[i] = c
		if !ok {
			unpriced[e.Model] = true
		}
		lt := e.T.Local()
		if !lt.Before(today0) {
			s.Today.add(e, c)
		}
		if !e.T.Before(week0) {
			s.Week.add(e, c)
		}
		if !lt.Before(month0) {
			s.Month.add(e, c)
			key := lt.Format("2006-01-02")
			dp := daily[key]
			if dp == nil {
				dp = &DayPoint{Date: key}
				daily[key] = dp
			}
			dp.add(e, c)
			mr := models[e.Model]
			if mr == nil {
				mr = &ModelRow{Model: e.Model, Priced: ok}
				models[e.Model] = mr
			}
			mr.add(e, c)
		}
	}
	if len(es) > 0 {
		t := es[len(es)-1].T
		s.LastActivity = &t
	}

	for d := month0; !d.After(today0); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if dp, ok := daily[key]; ok {
			s.Daily = append(s.Daily, *dp)
		} else {
			s.Daily = append(s.Daily, DayPoint{Date: key})
		}
	}
	for _, m := range models {
		s.Models = append(s.Models, *m)
	}
	sortModels(s.Models)
	for m := range unpriced {
		s.Unpriced = append(s.Unpriced, m)
	}
	sort.Strings(s.Unpriced)

	// 5-hour window.
	blocks := splitBlocks(es, costs)
	var peak float64
	for i, b := range blocks {
		if i == len(blocks)-1 {
			continue
		}
		if b.tot.CostUSD > peak {
			peak = b.tot.CostUSD
		}
	}
	if n := len(blocks); n > 0 {
		b := blocks[n-1]
		end := b.start.Add(blockLen)
		if now.Before(end) && now.Sub(b.last) < blockLen {
			s.Block = Block{Active: true, Start: b.start, End: end, Totals: b.tot}
			s.Block.RemainingMin = int(end.Sub(now).Minutes())
			if el := now.Sub(b.start).Hours(); el > 0.05 {
				s.Block.BurnUSDPerHour = b.tot.CostUSD / el
				s.Block.ProjectedUSD = b.tot.CostUSD + s.Block.BurnUSDPerHour*end.Sub(now).Hours()
			} else {
				s.Block.ProjectedUSD = b.tot.CostUSD
			}
		} else if peak < b.tot.CostUSD {
			peak = b.tot.CostUSD // finished last block counts toward the peak
		}
	}
	switch {
	case cfg.BlockLimitUSD > 0:
		s.Block.LimitUSD, s.Block.LimitSource = cfg.BlockLimitUSD, "config"
	case peak > 0:
		s.Block.LimitUSD, s.Block.LimitSource = peak, "peak"
	}
	if s.Block.LimitUSD > 0 {
		s.Block.Pct = 100 * s.Block.CostUSD / s.Block.LimitUSD
	}
	if cfg.WeeklyLimitUSD > 0 {
		s.Week.LimitUSD = cfg.WeeklyLimitUSD
		s.Week.Pct = 100 * s.Week.CostUSD / cfg.WeeklyLimitUSD
	}
	return s, es
}

func addTotals(dst *Totals, src Totals) {
	dst.Input += src.Input
	dst.Output += src.Output
	dst.CacheWrite += src.CacheWrite
	dst.CacheRead += src.CacheRead
	dst.Tokens += src.Tokens
	dst.CostUSD += src.CostUSD
	dst.Messages += src.Messages
}

func buildReport(accts []*Account, pr *Pricer, now time.Time) Report {
	r := Report{GeneratedAt: now}
	for _, a := range accts {
		s := summarize(a, pr, now)
		addTotals(&r.Total.Today, s.Today.Totals)
		addTotals(&r.Total.Week, s.Week.Totals)
		addTotals(&r.Total.Month, s.Month.Totals)
		r.Accounts = append(r.Accounts, s)
	}
	return r
}
