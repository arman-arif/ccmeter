package main

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Limits on the rows one detail response can carry. The totals still count
// everything; SessionCount says how many sessions there were in all.
const (
	maxDetailDays = 30
	maxRangeRows  = 100
)

// AccountDetail is everything the dashboard's account page shows: the same
// Summary as the overview, plus a breakdown of a chosen range of days.
type AccountDetail struct {
	Summary
	Setup AccountSetup `json:"setup"`
	Range RangeDetail  `json:"range"`
}

// RangeDetail covers the last Days calendar days, including today.
type RangeDetail struct {
	Days int    `json:"days"`
	From string `json:"from"` // first day, 2006-01-02
	To   string `json:"to"`   // today
	Totals
	Daily        []DayPoint   `json:"daily"` // oldest first, every day present
	Models       []ModelRow   `json:"models"`
	Projects     []ProjectRow `json:"projects"`
	Sessions     []SessionRow `json:"sessions"` // most recent first
	SessionCount int          `json:"session_count"`
	Blocks       []BlockRow   `json:"blocks"` // 5-hour windows, most recent first
	Hours        []HourCell   `json:"hours"`  // only the hours with usage
}

type ProjectRow struct {
	Path string `json:"path"`
	Totals
	Sessions   int       `json:"sessions"`
	LastActive time.Time `json:"last_active"`
}

type SessionRow struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Totals
	Models []string  `json:"models"`
	Start  time.Time `json:"start"`
	Last   time.Time `json:"last"`
}

type BlockRow struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Last   time.Time `json:"last"`
	Active bool      `json:"active"`
	Totals
}

// HourCell is usage in one hour of one weekday (0 is Sunday), in local time.
type HourCell struct {
	Weekday int `json:"weekday"`
	Hour    int `json:"hour"`
	Totals
}

// AccountSetup says how plan limits reach this account. It only reports
// yes or no from settings.json, never the settings themselves.
type AccountSetup struct {
	Hooks         bool      `json:"hooks"`
	Plugin        bool      `json:"plugin"`
	StatusLine    bool      `json:"status_line"`
	SettingsError string    `json:"settings_error,omitempty"`
	LastAttempt   time.Time `json:"last_attempt,omitzero"`
	LastOK        time.Time `json:"last_ok,omitzero"`
	NextAllowed   time.Time `json:"next_allowed,omitzero"`
	LastError     string    `json:"last_error,omitempty"`
}

func readSetup(dir string) AccountSetup {
	st := readFetchState(dir)
	out := AccountSetup{LastAttempt: st.LastAttempt, LastOK: st.LastOK, NextAllowed: st.NextAllowed, LastError: st.LastError}
	settings, _, err := readSettings(settingsPath(dir))
	if err != nil {
		out.SettingsError = err.Error()
		return out
	}
	if hooks, ok := settings["hooks"].(map[string]any); ok {
		out.Hooks = true
		for _, ev := range hookEvents {
			groups, _ := hooks[ev].([]any)
			if len(ccmeterCommands(groups)) == 0 {
				out.Hooks = false
			}
		}
	}
	if plugins, ok := settings["enabledPlugins"].(map[string]any); ok {
		for name, on := range plugins {
			if strings.HasPrefix(name, "ccmeter@") && on == true {
				out.Plugin = true
			}
		}
	}
	if sl, ok := settings["statusLine"].(map[string]any); ok {
		c, _ := sl["command"].(string)
		out.StatusLine = strings.Contains(c, "ccmeter")
	}
	return out
}

// rangeDays reads the ?days= value, defaulting to the longest range.
func rangeDays(s string) int {
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= maxDetailDays {
		return n
	}
	return maxDetailDays
}

func buildDetail(a *Account, pr *Pricer, now time.Time, days int) AccountDetail {
	s, es := summarizeEntries(a, pr, now)
	s.Account = readAccountInfo(s.Dir)
	return AccountDetail{Summary: s, Setup: readSetup(s.Dir), Range: buildRange(es, a.projectPaths(), pr, now, days)}
}

// buildRange breaks down entries, which must be sorted oldest first. paths maps
// an entry's project to its real path; projects without one show their folder name.
func buildRange(es []Entry, paths map[string]string, pr *Pricer, now time.Time, days int) RangeDetail {
	projectPath := func(project string) string {
		if p := paths[project]; p != "" {
			return p
		}
		return project
	}
	today := startOfDay(now.Local())
	from := today.AddDate(0, 0, -(days - 1))
	r := RangeDetail{Days: days, From: from.Format("2006-01-02"), To: today.Format("2006-01-02")}

	type projectAgg struct {
		row      ProjectRow
		sessions map[string]bool
	}
	type sessionAgg struct {
		row    SessionRow
		models map[string]bool
	}
	daily := map[string]*DayPoint{}
	models := map[string]*ModelRow{}
	projects := map[string]*projectAgg{}
	sessions := map[string]*sessionAgg{}
	hours := map[[2]int]*HourCell{}
	costs := make([]float64, len(es))

	for i, e := range es {
		c, priced := pr.cost(e)
		costs[i] = c
		lt := e.T.Local()
		if lt.Before(from) {
			continue
		}
		r.add(e, c)

		key := lt.Format("2006-01-02")
		if daily[key] == nil {
			daily[key] = &DayPoint{Date: key}
		}
		daily[key].add(e, c)

		if models[e.Model] == nil {
			models[e.Model] = &ModelRow{Model: e.Model, Priced: priced}
		}
		models[e.Model].add(e, c)

		p := projects[e.Project]
		if p == nil {
			p = &projectAgg{row: ProjectRow{Path: projectPath(e.Project)}, sessions: map[string]bool{}}
			projects[e.Project] = p
		}
		p.row.add(e, c)
		p.sessions[e.Session] = true
		p.row.LastActive = e.T

		ss := sessions[e.Session]
		if ss == nil {
			ss = &sessionAgg{row: SessionRow{ID: e.Session, Project: projectPath(e.Project), Start: e.T}, models: map[string]bool{}}
			sessions[e.Session] = ss
		}
		ss.row.add(e, c)
		ss.models[e.Model] = true
		ss.row.Last = e.T

		hk := [2]int{int(lt.Weekday()), lt.Hour()}
		if hours[hk] == nil {
			hours[hk] = &HourCell{Weekday: hk[0], Hour: hk[1]}
		}
		hours[hk].add(e, c)
	}

	r.Daily = []DayPoint{}
	for d := from; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		if dp, ok := daily[key]; ok {
			r.Daily = append(r.Daily, *dp)
		} else {
			r.Daily = append(r.Daily, DayPoint{Date: key})
		}
	}

	r.Models = []ModelRow{}
	for _, m := range models {
		r.Models = append(r.Models, *m)
	}
	sortModels(r.Models)

	r.Projects = []ProjectRow{}
	for _, p := range projects {
		p.row.Sessions = len(p.sessions)
		r.Projects = append(r.Projects, p.row)
	}
	sort.Slice(r.Projects, func(i, j int) bool {
		a, b := r.Projects[i], r.Projects[j]
		if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		return a.Path < b.Path
	})
	if len(r.Projects) > maxRangeRows {
		r.Projects = r.Projects[:maxRangeRows]
	}

	r.SessionCount = len(sessions)
	r.Sessions = []SessionRow{}
	for _, ss := range sessions {
		for m := range ss.models {
			ss.row.Models = append(ss.row.Models, m)
		}
		sort.Strings(ss.row.Models)
		r.Sessions = append(r.Sessions, ss.row)
	}
	sort.Slice(r.Sessions, func(i, j int) bool {
		a, b := r.Sessions[i], r.Sessions[j]
		if !a.Last.Equal(b.Last) {
			return a.Last.After(b.Last)
		}
		return a.ID < b.ID
	})
	if len(r.Sessions) > maxRangeRows {
		r.Sessions = r.Sessions[:maxRangeRows]
	}

	r.Hours = []HourCell{}
	for _, h := range hours {
		r.Hours = append(r.Hours, *h)
	}
	sort.Slice(r.Hours, func(i, j int) bool {
		a, b := r.Hours[i], r.Hours[j]
		if a.Weekday != b.Weekday {
			return a.Weekday < b.Weekday
		}
		return a.Hour < b.Hour
	})

	r.Blocks = []BlockRow{}
	spans := splitBlocks(es, costs)
	for i := len(spans) - 1; i >= 0 && len(r.Blocks) < maxRangeRows; i-- {
		b := spans[i]
		if b.last.Local().Before(from) {
			break
		}
		end := b.start.Add(blockLen)
		active := i == len(spans)-1 && now.Before(end) && now.Sub(b.last) < blockLen
		r.Blocks = append(r.Blocks, BlockRow{Start: b.start, End: end, Last: b.last, Active: active, Totals: b.tot})
	}
	return r
}
