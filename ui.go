package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Terminal styling. Color is only ever decoration: anything it signals is also
// said in words, so piped output and NO_COLOR lose no information.
const (
	styleBold   = "1"
	styleDim    = "2"
	styleRed    = "31"
	styleGreen  = "32"
	styleYellow = "33"
)

// Same thresholds as the dashboard.
const (
	nearLimitPct = 80
	overLimitPct = 100
)

const meterWidth = 8

var (
	useColor  bool // set once in main
	stdoutTTY bool
)

// colorEnabled decides whether to style stdout. force is +1 for --color and -1
// for --no-color; flags beat NO_COLOR (no-color.org), which beats detection.
func colorEnabled(force int, tty bool, noColor, term string, enableVT func() bool) bool {
	switch {
	case force > 0:
		enableVT()
		return true
	case force < 0, noColor != "", !tty, term == "dumb":
		return false
	}
	return enableVT()
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func paint(style, s string) string {
	if !useColor || s == "" {
		return s
	}
	return "\x1b[" + style + "m" + s + "\x1b[0m"
}

// visibleLen is the number of terminal cells s takes, ignoring color codes.
func visibleLen(s string) int {
	n := 0
	for len(s) > 0 {
		if strings.HasPrefix(s, "\x1b[") {
			if end := strings.IndexByte(s, 'm'); end >= 0 {
				s = s[end+1:]
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s)
		s = s[size:]
		n++
	}
	return n
}

// table is a grid of cells that may contain color codes, which text/tabwriter
// would count as width.
type table struct {
	header []string
	rows   [][]string
	right  map[int]bool // right-aligned columns
	rule   int          // draw a rule above this row index; 0 means none
}

func (t *table) add(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) render(w io.Writer) {
	widths := make([]int, len(t.header))
	for _, row := range append([][]string{t.header}, t.rows...) {
		for i, c := range row {
			if n := visibleLen(c); n > widths[i] {
				widths[i] = n
			}
		}
	}
	line := func(row []string, style string) {
		var b strings.Builder
		for i := range widths {
			c := ""
			if i < len(row) {
				c = row[i]
			}
			pad := strings.Repeat(" ", widths[i]-visibleLen(c))
			if style != "" {
				c = paint(style, c)
			}
			if t.right[i] {
				b.WriteString(pad + c)
			} else {
				b.WriteString(c + pad)
			}
			b.WriteString("  ")
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	line(t.header, styleBold)
	total := 0
	for _, n := range widths {
		total += n + 2
	}
	for i, row := range t.rows {
		if t.rule > 0 && i == t.rule {
			fmt.Fprintln(w, paint(styleDim, strings.Repeat("─", total-2)))
		}
		line(row, "")
	}
}

func meterStyle(pct float64) string {
	switch {
	case pct >= overLimitPct:
		return styleRed
	case pct >= nearLimitPct:
		return styleYellow
	}
	return styleGreen
}

// meterBar draws pct as a fixed-width bar. Any usage shows at least one cell,
// and only 100% or more fills the bar.
func meterBar(pct float64) string {
	filled := int(math.Round(pct / 100 * meterWidth))
	switch {
	case pct > 0 && filled == 0:
		filled = 1
	case pct < overLimitPct && filled >= meterWidth:
		filled = meterWidth - 1
	case filled > meterWidth:
		filled = meterWidth
	}
	return paint(meterStyle(pct), strings.Repeat("█", filled)) +
		paint(styleDim, strings.Repeat("░", meterWidth-filled))
}

// meter is one usage cell: "███░░░░░░░  33%  2h43m left". over names what
// reaching 100% means for this measure.
func meter(pct float64, detail, over string) string {
	s := meterBar(pct) + " " + paint(meterStyle(pct), fmt.Sprintf("%3.0f%%", pct))
	alert := ""
	switch {
	case pct >= overLimitPct:
		alert = over
	case pct >= nearLimitPct:
		alert = "near limit"
	}
	if detail != "" {
		s += "  " + detail
	}
	if alert != "" {
		if detail != "" {
			s += ","
		} else {
			s += " "
		}
		s += " " + paint(meterStyle(pct), alert)
	}
	return s
}

func dash() string { return paint(styleDim, "-") }

func planMeter(w *LimitWindow, now time.Time) string {
	if w.Reset {
		return paint(styleDim, "reset")
	}
	return meter(w.UsedPct, resetDetail(w.ResetsAt, now), "limit reached")
}

// resetDetail says when a window ends: a countdown today, a day and time after.
func resetDetail(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	left := t.Sub(now)
	if left < 24*time.Hour {
		return fmtDur(int(left.Minutes())) + " left"
	}
	return "resets " + t.Local().Format("Mon 15:04")
}

// fiveHourCell shows the real plan window, or else the estimate from local logs.
func fiveHourCell(s Summary, now time.Time) string {
	if w := planWindow(s.Plan, true); w != nil {
		return planMeter(w, now)
	}
	b := s.Block
	switch {
	case !b.Active:
		return paint(styleDim, "idle")
	case b.LimitSource == "peak":
		return meter(b.Pct, fmtDur(b.RemainingMin)+" left, of peak", "new peak")
	case b.LimitUSD > 0:
		return meter(b.Pct, fmtDur(b.RemainingMin)+" left, of "+fmtUSD(b.LimitUSD), "over limit")
	}
	return fmtDur(b.RemainingMin) + " left"
}

func weekCell(s Summary, now time.Time) string {
	if w := planWindow(s.Plan, false); w != nil {
		return planMeter(w, now)
	}
	if s.Week.LimitUSD > 0 {
		return meter(s.Week.Pct, "of "+fmtUSD(s.Week.LimitUSD), "over limit")
	}
	return dash()
}

func costCell(v float64) string {
	if v == 0 {
		return paint(styleDim, fmtUSD(v))
	}
	return fmtUSD(v)
}

func tokCell(n int64) string { return paint(styleDim, fmtTok(n)) }

func planSource(src string) string {
	switch src {
	case "usage-endpoint":
		return "usage endpoint"
	case "claude-cli":
		return "claude /usage"
	}
	return "status line"
}

func warnLine(w io.Writer, name, msg string) {
	fmt.Fprintln(w, paint(styleYellow, "warning:"), name+": "+firstLine(msg))
}

func printTable(w io.Writer, r Report, verbose, none bool) {
	if none {
		fmt.Fprintln(w, "No Claude Code config dirs found. Run `ccmeter init` and edit the file, or set CLAUDE_CONFIG_DIR.")
		return
	}
	now := r.GeneratedAt

	limits := table{header: []string{"LIMITS", "5-HOUR WINDOW", "WEEK"}}
	costs := table{
		header: []string{"EST. COST", "5-HOUR", "TODAY", "TOKENS", "7 DAYS", "TOKENS", "30 DAYS", "TOKENS"},
		right:  map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true},
	}
	var blockCost float64
	for _, s := range r.Accounts {
		if !s.Found {
			limits.add(s.Name, paint(styleYellow, "not found"), dash())
			continue
		}
		limits.add(s.Name, fiveHourCell(s, now), weekCell(s, now))
		block := dash()
		if s.Block.Active {
			block = costCell(s.Block.CostUSD)
			blockCost += s.Block.CostUSD
		}
		costs.add(s.Name, block, costCell(s.Today.CostUSD), tokCell(s.Today.Tokens),
			costCell(s.Week.CostUSD), tokCell(s.Week.Tokens),
			costCell(s.Month.CostUSD), tokCell(s.Month.Tokens))
	}
	if len(costs.rows) > 1 {
		costs.rule = len(costs.rows)
		costs.add(paint(styleBold, "all"), costCell(blockCost),
			costCell(r.Total.Today.CostUSD), tokCell(r.Total.Today.Tokens),
			costCell(r.Total.Week.CostUSD), tokCell(r.Total.Week.Tokens),
			costCell(r.Total.Month.CostUSD), tokCell(r.Total.Month.Tokens))
	}
	limits.render(w)
	var names []string
	var plans []*PlanLimits
	for _, s := range r.Accounts {
		names, plans = append(names, s.Name), append(plans, s.Plan)
	}
	printModelLimits(w, names, plans, now)
	if len(costs.rows) > 0 {
		fmt.Fprintln(w)
		costs.render(w)
	}

	if verbose {
		for _, s := range r.Accounts {
			if !s.Found || len(s.Models) == 0 {
				continue
			}
			fmt.Fprintf(w, "\n%s\n", paint(styleBold, s.Name+": by model, 30 days"))
			t := table{
				header: []string{"MODEL", "INPUT", "OUTPUT", "CACHE WRITE", "CACHE READ", "COST"},
				right:  map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true},
			}
			for _, m := range s.Models {
				cost := fmtUSD(m.CostUSD)
				if !m.Priced {
					cost = paint(styleYellow, "unpriced")
				}
				t.add(m.Model, fmtTok(m.Input), fmtTok(m.Output), fmtTok(m.CacheWrite), fmtTok(m.CacheRead), cost)
			}
			t.render(w)
		}
	}

	var warnings []func()
	warn := func(name, msg string) {
		warnings = append(warnings, func() { warnLine(w, name, msg) })
	}
	var seen []string
	for _, s := range r.Accounts {
		if !s.Found {
			warn(s.Name, "not found: "+s.Dir)
		}
		if len(s.Unpriced) > 0 {
			warn(s.Name, "unpriced models ("+strings.Join(s.Unpriced, ", ")+`); add them under "pricing" in the config`)
		}
		if s.PlanError != "" {
			warn(s.Name, "last plan limit check failed: "+s.PlanError)
		}
		if s.Plan != nil {
			seen = append(seen, fmt.Sprintf("%s %s ago (%s)", s.Name, fmtDur(s.Plan.AgeMin), planSource(s.Plan.Source)))
		}
	}
	if len(warnings) > 0 {
		fmt.Fprintln(w)
		for _, f := range warnings {
			f()
		}
	}
	fmt.Fprintln(w)
	if len(seen) > 0 {
		fmt.Fprintln(w, paint(styleDim, "Plan limits updated: "+strings.Join(seen, ", ")))
	}
	fmt.Fprintln(w, paint(styleDim, "Costs are estimates at API list prices; % is of your limit, or of your own 30-day peak window."))
	if len(seen) == 0 {
		fmt.Fprintln(w, "Run `ccmeter hook install` (or `ccmeter statusline install`) to also show your real plan limits.")
	}
}

// printLimits renders fetched plan limits. plans and errs are parallel to
// names; errors are listed under the table.
func printLimits(w io.Writer, names []string, plans []*PlanLimits, errs []error, now time.Time) {
	t := table{header: []string{"ACCOUNT", "5-HOUR WINDOW", "WEEK", "UPDATED"}}
	for i, name := range names {
		pl := plans[i]
		if pl == nil {
			t.add(name, paint(styleDim, "no limits"), dash(), dash())
			continue
		}
		five, week := dash(), dash()
		if pl.FiveHour != nil {
			five = planMeter(pl.FiveHour, now)
		}
		if pl.SevenDay != nil {
			week = planMeter(pl.SevenDay, now)
		}
		t.add(name, five, week, paint(styleDim, fmtDur(pl.AgeMin)+" ago, "+planSource(pl.Source)))
	}
	t.render(w)
	printModelLimits(w, names, plans, now)
	printed := false
	for i, err := range errs {
		if err == nil {
			continue
		}
		if !printed {
			fmt.Fprintln(w)
			printed = true
		}
		warnLine(w, names[i], err.Error())
	}
}

// printModelLimits lists the weekly limits for one model, such as Fable, on the
// accounts that have one, and prints nothing when none do.
func printModelLimits(w io.Writer, names []string, plans []*PlanLimits, now time.Time) {
	t := table{header: []string{"ACCOUNT", "MODEL", "WEEK"}}
	for i, pl := range plans {
		if pl == nil {
			continue
		}
		for j := range pl.Models {
			t.add(names[i], pl.Models[j].Name, planMeter(&pl.Models[j].LimitWindow, now))
		}
	}
	if len(t.rows) > 0 {
		fmt.Fprintln(w)
		t.render(w)
	}
}

func printAccounts(w io.Writer, cfg Config, cfgPath string, fromFile bool) {
	if fromFile {
		fmt.Fprintln(w, paint(styleBold, "config:"), cfgPath)
	} else {
		fmt.Fprintln(w, paint(styleBold, "config:"), "none (auto-detected; run `ccmeter init` to create "+cfgPath+")")
	}
	if len(cfg.Accounts) == 0 {
		return
	}
	fmt.Fprintln(w)
	t := table{header: []string{"ACCOUNT", "DIR", "NOTES"}}
	for _, a := range cfg.Accounts {
		var notes []string
		if st, err := os.Stat(filepath.Join(expandHome(a.Dir), "projects")); err != nil || !st.IsDir() {
			notes = append(notes, paint(styleYellow, "no projects/ folder"))
		}
		if a.BlockLimitUSD > 0 {
			notes = append(notes, "5h limit "+fmtUSD(a.BlockLimitUSD))
		}
		if a.WeeklyLimitUSD > 0 {
			notes = append(notes, "weekly limit "+fmtUSD(a.WeeklyLimitUSD))
		}
		t.add(a.Name, a.Dir, strings.Join(notes, ", "))
	}
	t.render(w)
}
