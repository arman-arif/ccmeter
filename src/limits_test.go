package ccmeter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRateLimitParsing(t *testing.T) {
	in := `{"rate_limits":{"five_hour":{"used_percentage":42.5,"resets_at":1738425600},
	        "seven_day":{"used_percentage":31,"resets_at":"2026-11-04T15:00:00Z"}}}`
	var s statusInput
	if err := json.Unmarshal([]byte(in), &s); err != nil {
		t.Fatal(err)
	}
	f, w := s.RateLimits.FiveHour.toWindow(), s.RateLimits.SevenDay.toWindow()
	if f.UsedPct != 42.5 || f.ResetsAt.Unix() != 1738425600 {
		t.Errorf("five_hour = %+v", f)
	}
	if w.UsedPct != 31 || !w.ResetsAt.Equal(time.Date(2026, 11, 4, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("seven_day = %+v", w)
	}
	if tm, ok := parseLooseTime(json.RawMessage(`1738425600000`)); !ok || tm.Unix() != 1738425600 {
		t.Errorf("milliseconds not handled: %v", tm)
	}
	if (&rawWindow{}).toWindow() != nil {
		t.Error("window without used_percentage should be nil")
	}
}

func TestSaveLoadLimitsAndReset(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	p := 50.0
	q := 20.0
	rl := &rawRateLimits{
		FiveHour: &rawWindow{UsedPercentage: &p, ResetsAt: json.RawMessage(`1`)}, // long past
		SevenDay: &rawWindow{UsedPercentage: &q},
	}
	if err := saveLimits("/x/acct", rl, now); err != nil {
		t.Fatal(err)
	}
	got := loadLimits("/x/acct", now)
	if got == nil || !got.FiveHour.Reset || got.SevenDay.Reset || got.SevenDay.UsedPct != 20 {
		t.Fatalf("unexpected %+v", got)
	}
	if loadLimits("/x/other", now) != nil {
		t.Error("limits leaked across accounts")
	}
	if loadLimits("/x/acct", now.Add(9*24*time.Hour)) != nil {
		t.Error("stale report should be ignored")
	}
}

func TestInstallStatuslineKeepsSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"model":"opus","statusLine":{"type":"command","command":"mine.sh"}}`), 0o644)
	a := AccountConfig{Name: "w", Dir: dir}
	if _, err := installStatusline(a, "ccmeter statusline", false); err == nil {
		t.Fatal("should refuse to replace a custom status line")
	}
	if res, err := installStatusline(a, "ccmeter statusline", true); err != nil || res != "installed" {
		t.Fatal(res, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"model": "opus"`) || !strings.Contains(string(b), `"command": "ccmeter statusline"`) {
		t.Fatalf("settings not merged:\n%s", b)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("no backup written")
	}
	if res, _ := uninstallStatusline(a); res != "removed" {
		t.Errorf("uninstall: %s", res)
	}
}
