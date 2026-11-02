package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestColorEnabled(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	cases := []struct {
		name    string
		force   int
		tty     bool
		noColor string
		term    string
		vt      func() bool
		want    bool
	}{
		{"terminal", 0, true, "", "xterm", yes, true},
		{"piped", 0, false, "", "xterm", yes, false},
		{"NO_COLOR", 0, true, "1", "xterm", yes, false},
		{"dumb terminal", 0, true, "", "dumb", yes, false},
		{"old Windows console", 0, true, "", "", no, false},
		{"--color when piped", 1, false, "1", "dumb", yes, true},
		{"--no-color on a terminal", -1, true, "", "xterm", yes, false},
	}
	for _, c := range cases {
		if got := colorEnabled(c.force, c.tty, c.noColor, c.term, c.vt); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMeterBar(t *testing.T) {
	cases := map[float64]int{0: 0, 0.4: 1, 24: 2, 50: 4, 99.6: 7, 100: 8, 250: 8}
	for pct, want := range cases {
		bar := meterBar(pct)
		if got := strings.Count(bar, "█"); got != want {
			t.Errorf("%v%%: %d filled, want %d", pct, got, want)
		}
		if n := utf8.RuneCountInString(bar); n != meterWidth {
			t.Errorf("%v%%: width %d", pct, n)
		}
	}
}

// Warnings must survive without color, for pipes and NO_COLOR.
func TestMeterAlertsInWords(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{50, ""},
		{80, "near limit"},
		{100, "limit reached"},
	}
	for _, c := range cases {
		got := meter(c.pct, "1h left", "limit reached")
		if c.want == "" && strings.Contains(got, "limit") {
			t.Errorf("%v%%: unexpected alert in %q", c.pct, got)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%v%%: %q lacks %q", c.pct, got, c.want)
		}
	}
}

func TestResetDetail(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	if got := resetDetail(now.Add(90*time.Minute), now); got != "1h30m left" {
		t.Errorf("soon: %q", got)
	}
	if got := resetDetail(now.Add(72*time.Hour), now); got != "resets Thu 10:00" {
		t.Errorf("later: %q", got)
	}
	if got := resetDetail(time.Time{}, now); got != "" {
		t.Errorf("zero: %q", got)
	}
}

func TestTableAlignsAroundColor(t *testing.T) {
	useColor = true
	defer func() { useColor = false }()
	tb := table{header: []string{"A", "B", "C"}, right: map[int]bool{1: true}}
	tb.add("x", paint(styleRed, "12"), "end")
	tb.add(paint(styleBold, "longer"), "1", "end")
	var b bytes.Buffer
	tb.render(&b)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	var at []int
	for _, l := range lines[1:] {
		plain := l
		for _, code := range []string{"\x1b[31m", "\x1b[1m", "\x1b[0m"} {
			plain = strings.ReplaceAll(plain, code, "")
		}
		at = append(at, strings.Index(plain, "end"))
	}
	if at[0] != at[1] {
		t.Errorf("columns differ: %v\n%s", at, b.String())
	}
}

func TestVisibleLen(t *testing.T) {
	useColor = true
	defer func() { useColor = false }()
	if n := visibleLen(paint(styleGreen, "██ 5%")); n != 5 {
		t.Errorf("got %d", n)
	}
}

func testReport(now time.Time) Report {
	r := Report{GeneratedAt: now}
	plan := Summary{Name: "personal", Found: true, Plan: &PlanLimits{
		Source:   "claude-cli",
		AgeMin:   2,
		FiveHour: &LimitWindow{UsedPct: 85, ResetsAt: now.Add(2 * time.Hour)},
		SevenDay: &LimitWindow{UsedPct: 12, ResetsAt: now.Add(96 * time.Hour)},
	}}
	plan.Block.Active = true
	plan.Block.CostUSD = 3.85
	plan.Today.CostUSD = 3.85
	est := Summary{Name: "work", Found: true, PlanError: "no plan limits\nsecond line"}
	est.Block = Block{Active: true, RemainingMin: 60, LimitUSD: 10, LimitSource: "peak", Pct: 40}
	gone := Summary{Name: "old", Dir: "/nowhere"}
	r.Accounts = []Summary{plan, est, gone}
	return r
}

func TestPrintTable(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	var b bytes.Buffer
	printTable(&b, testReport(now), false, false)
	out := b.String()
	for _, want := range []string{
		"near limit",
		"of peak",
		"not found: /nowhere",
		"last plan limit check failed: no plan limits",
		"Plan limits updated: personal 2m ago (claude /usage)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "second line") {
		t.Errorf("multi-line error leaked:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("color without a terminal:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "personal") || strings.HasPrefix(l, "all") {
			if n := visibleLen(l); n > 80 {
				t.Errorf("wider than 80 columns (%d): %s", n, l)
			}
		}
	}
}

func TestPrintLimits(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.Local)
	pl := &PlanLimits{Source: "claude-cli", FiveHour: &LimitWindow{UsedPct: 24, ResetsAt: now.Add(time.Hour)},
		Models: []ModelLimit{{Name: "Fable", LimitWindow: LimitWindow{UsedPct: 62, ResetsAt: now.Add(time.Hour)}}}}
	var b bytes.Buffer
	printLimits(&b, []string{"default", "work"}, []*PlanLimits{nil, pl},
		[]error{errors.New("boom"), nil}, now)
	out := b.String()
	for _, want := range []string{"no limits", "24%", "claude /usage", "warning: default: boom", "MODEL", "Fable", "62%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
