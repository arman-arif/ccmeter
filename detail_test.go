package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEntry(t time.Time, project, session string, in int64) Entry {
	return Entry{T: t, Model: "claude-sonnet-4-5", In: in, Project: project, Session: session}
}

func TestBuildRange(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.Local)
	today := startOfDay(now)
	pr := newPricer(nil)
	es := []Entry{
		testEntry(today.AddDate(0, 0, -20).Add(9*time.Hour), "/old", "s0", 1000), // outside a 7-day range
		testEntry(today.AddDate(0, 0, -6).Add(9*time.Hour), "/a", "s1", 1000),    // first day of the range
		testEntry(today.AddDate(0, 0, -6).Add(10*time.Hour), "/a", "s1", 3000),
		testEntry(today.Add(9*time.Hour), "/b", "s2", 500),
		testEntry(today.Add(14*time.Hour), "/a", "s3", 2000), // inside the 5-hour window at 15:00
	}

	r := buildRange(es, pr, now, 7)

	if len(r.Daily) != 7 || r.Daily[0].Date != r.From || r.Daily[6].Date != r.To {
		t.Fatalf("daily series: %d days, %s to %s", len(r.Daily), r.From, r.To)
	}
	if r.Input != 6500 || r.Messages != 4 {
		t.Errorf("totals: input %d, messages %d", r.Input, r.Messages)
	}
	if r.Daily[0].Input != 4000 || r.Daily[1].Messages != 0 {
		t.Errorf("daily: %+v", r.Daily[:2])
	}

	if len(r.Projects) != 2 || r.Projects[0].Path != "/a" || r.Projects[0].Sessions != 2 || r.Projects[0].Messages != 3 {
		t.Errorf("projects: %+v", r.Projects)
	}
	if r.SessionCount != 3 || r.Sessions[0].ID != "s3" || r.Sessions[2].ID != "s1" {
		t.Errorf("sessions: %d, %+v", r.SessionCount, r.Sessions)
	}
	if s := r.Sessions[2]; s.Messages != 2 || !s.Last.After(s.Start) {
		t.Errorf("session s1: %+v", s)
	}

	if len(r.Blocks) != 3 {
		t.Fatalf("blocks: %+v", r.Blocks)
	}
	if !r.Blocks[0].Active || r.Blocks[1].Active {
		t.Errorf("only the newest window is active: %+v", r.Blocks[:2])
	}
	if got := r.Blocks[2].End.Sub(r.Blocks[2].Start); got != blockLen {
		t.Errorf("window length %v", got)
	}

	want := map[[2]int]int64{
		{int(today.AddDate(0, 0, -6).Weekday()), 9}:  1000,
		{int(today.AddDate(0, 0, -6).Weekday()), 10}: 3000,
		{int(today.Weekday()), 9}:                    500,
		{int(today.Weekday()), 14}:                   2000,
	}
	if len(r.Hours) != len(want) {
		t.Fatalf("hours: %+v", r.Hours)
	}
	for _, h := range r.Hours {
		if v, ok := want[[2]int{h.Weekday, h.Hour}]; !ok || v != h.Input {
			t.Errorf("hour %d/%d: input %d, want %d", h.Weekday, h.Hour, h.Input, v)
		}
	}
}

func TestRangeDays(t *testing.T) {
	for in, want := range map[string]int{"": 30, "7": 7, "30": 30, "1": 1, "0": 30, "31": 30, "x": 30, "-7": 30} {
		if got := rangeDays(in); got != want {
			t.Errorf("rangeDays(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestReadSetup(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()

	if s := readSetup(dir); s.Hooks || s.Plugin || s.StatusLine || s.SettingsError != "" {
		t.Errorf("no settings.json: %+v", s)
	}

	settings := `{
	  "statusLine": {"type": "command", "command": "/x/ccmeter statusline"},
	  "enabledPlugins": {"ccmeter@ccmeter": true, "other@other": true},
	  "hooks": {
	    "SessionStart": [{"hooks": [{"type": "command", "command": "/x/ccmeter hook"}]}],
	    "Stop": [{"hooks": [{"type": "command", "command": "/x/ccmeter hook"}]}]
	  }
	}`
	if err := os.WriteFile(settingsPath(dir), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := readSetup(dir); !s.Hooks || !s.Plugin || !s.StatusLine {
		t.Errorf("all set: %+v", s)
	}

	// One event missing isn't installed, and someone else's status line isn't ours.
	settings = `{
	  "statusLine": {"type": "command", "command": "my-line"},
	  "enabledPlugins": {"ccmeter@ccmeter": false},
	  "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "/x/ccmeter hook"}]}]}
	}`
	os.WriteFile(settingsPath(dir), []byte(settings), 0o644)
	if s := readSetup(dir); s.Hooks || s.Plugin || s.StatusLine {
		t.Errorf("partly set: %+v", s)
	}

	os.WriteFile(settingsPath(dir), []byte("{not json"), 0o644)
	if s := readSetup(dir); s.SettingsError == "" {
		t.Errorf("bad JSON should be reported: %+v", s)
	}
}

func TestDetailJSONKeepsSummaryFields(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	ts := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	writeLog(t, filepath.Join(dir, "projects", "p", "s.jsonl"),
		`{"type":"assistant","timestamp":"`+ts+`","requestId":"r","cwd":"/w","sessionId":"s","message":{"id":"m","model":"claude-sonnet-4-5","usage":{"input_tokens":10}}}`+"\n")
	a := newAccount(AccountConfig{Name: "x", Dir: dir})
	a.Scan(time.Now().Add(-historyWindow))

	b, err := json.Marshal(buildDetail(a, newPricer(nil), time.Now(), 7))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	// Summary's fields sit at the top level, next to setup and range.
	for _, k := range []string{"name", "dir", "block", "today", "daily", "setup", "range"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in %s", k, b)
		}
	}
	if strings.Contains(string(b), "0001-01-01") {
		t.Errorf("unset times should be left out: %s", b)
	}
}
