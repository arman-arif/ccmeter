package ccmeter

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Real output of `claude -p /usage` (Claude Code, November 2026).
const realUsageText = "You are currently using your subscription to power your Claude Code usage\n\n" +
	"Current session: 4% used · resets Nov 2, 10:30am (Asia/Dhaka)\n" +
	"Current week (all models): 8% used · resets Nov 2, 9am (Asia/Dhaka)"

func TestParseRealUsageText(t *testing.T) {
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	now := time.Date(2026, 11, 2, 5, 47, 0, 0, dhaka)
	five, seven, models := parseUsageText(realUsageText, now.UTC())
	if five == nil || seven == nil {
		t.Fatalf("missing windows: %+v %+v", five, seven)
	}
	if five.UsedPct != 4 || !five.ResetsAt.Equal(time.Date(2026, 11, 2, 10, 30, 0, 0, dhaka)) {
		t.Errorf("session = %v%% resets %v", five.UsedPct, five.ResetsAt)
	}
	if seven.UsedPct != 8 || !seven.ResetsAt.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, dhaka)) {
		t.Errorf("week = %v%% resets %v", seven.UsedPct, seven.ResetsAt)
	}
	if models == nil || len(models) != 0 {
		t.Errorf("no model limits should be an empty list, not nil: %#v", models)
	}
}

// Real output on a Max plan, with its Fable limit and the usage tips below it.
func TestParseRealMaxPlanUsageText(t *testing.T) {
	text := "You are currently using your subscription to power your Claude Code usage\n\n" +
		"Current session: 25% used · resets Nov 2, 8:59pm (Asia/Dhaka)\n" +
		"Current week (all models): 45% used · resets Nov 5, 7:59am (Asia/Dhaka)\n" +
		"Current week (Fable): 3% used · resets Nov 5, 8am (Asia/Dhaka)\n\n" +
		"What's contributing to your usage?\n" +
		"Approximate · sessions on this machine only · shares overlap\n\n" +
		"Last 24h · 314 requests · 5 sessions\n" +
		"  84%  Start fresh when you switch tasks · run /clear\n" +
		"  Top skills: /some-skill 14%\n"
	dhaka, _ := time.LoadLocation("Asia/Dhaka")
	five, seven, models := parseUsageText(text, time.Date(2026, 11, 2, 18, 0, 0, 0, dhaka))
	if five == nil || five.UsedPct != 25 || seven == nil || seven.UsedPct != 45 {
		t.Fatalf("all-models windows: %+v %+v", five, seven)
	}
	if len(models) != 1 || models[0].Name != "Fable" || models[0].UsedPct != 3 ||
		!models[0].ResetsAt.Equal(time.Date(2026, 11, 5, 8, 0, 0, 0, dhaka)) {
		t.Errorf("fable: %+v", models)
	}
}

func TestSavedModelLimits(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	now := time.Now()
	week := &LimitWindow{UsedPct: 40, ResetsAt: now.Add(time.Hour)}
	fable := []ModelLimit{
		{Name: "Fable", LimitWindow: LimitWindow{UsedPct: 7, ResetsAt: now.Add(time.Hour)}},
		{Name: "Sonnet", LimitWindow: LimitWindow{UsedPct: 9, ResetsAt: now.Add(-time.Hour)}},
	}
	if err := saveLimitWindows(dir, "claude-cli", nil, week, fable, now); err != nil {
		t.Fatal(err)
	}
	pl := loadLimits(dir, now)
	if len(pl.Models) != 2 || pl.Models[0].UsedPct != 7 || pl.Models[0].Reset || !pl.Models[1].Reset {
		t.Fatalf("saved models, the second past its reset: %+v", pl.Models)
	}

	// A source that can't report them, like the status line, keeps the last ones.
	if err := saveLimitWindows(dir, "statusline", nil, week, nil, now); err != nil {
		t.Fatal(err)
	}
	if pl := loadLimits(dir, now); pl.Source != "statusline" || len(pl.Models) != 2 {
		t.Fatalf("should keep the last known models: %+v", pl)
	}

	// /usage saying there are none clears them.
	if err := saveLimitWindows(dir, "claude-cli", nil, week, []ModelLimit{}, now); err != nil {
		t.Fatal(err)
	}
	if pl := loadLimits(dir, now); len(pl.Models) != 0 {
		t.Fatalf("should be cleared: %+v", pl.Models)
	}
}

func TestParseUsageVariants(t *testing.T) {
	now := time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)
	text := "Current session: 0% used\n" +
		"Current week (Sonnet only): 50% used · resets Jan 2, 9am (UTC)\n" +
		"Current week (all models): 12.5% used · resets Jan 2, 9am (UTC)\n" +
		"Extra usage: not enabled"
	five, seven, models := parseUsageText(text, now)
	if five == nil || five.UsedPct != 0 || !five.ResetsAt.IsZero() {
		t.Errorf("session = %+v", five)
	}
	if seven == nil || seven.UsedPct != 12.5 || !seven.ResetsAt.Equal(time.Date(2027, 1, 2, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("week should use the all-models line and roll the year: %+v", seven)
	}
	if len(models) != 1 || models[0].Name != "Sonnet" || models[0].UsedPct != 50 {
		t.Errorf("model limit: %+v", models)
	}
	if tm, ok := parseResetTime("3pm (UTC)", now); !ok || !tm.Equal(time.Date(2026, 12, 30, 15, 0, 0, 0, time.UTC)) {
		t.Errorf("time-only = %v", tm)
	}
	if tm, ok := parseResetTime("10am (UTC)", now); !ok || !tm.Equal(time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("time-only in the past should be tomorrow: %v", tm)
	}
	if tm, ok := parseResetTime("12am (UTC)", now); !ok || tm.Hour() != 0 {
		t.Errorf("12am = %v", tm)
	}
}

func TestClaudeUsageWithFakeCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	log := filepath.Join(t.TempDir(), "args.log")
	script := filepath.Join(t.TempDir(), "claude")
	// Rejects the extra flags (like an older CLI) to exercise the fallback.
	body := `#!/bin/sh
echo "$CLAUDE_CONFIG_DIR|$CCMETER_IN_FETCH|$*" >> "` + log + `"
case "$*" in *--no-session-persistence*) echo "error: unknown option" >&2; exit 1;; esac
printf '%s' '{"is_error":false,"num_turns":0,"local_command":"usage","result":"Current session: 61% used · resets 11pm (UTC)\nCurrent week (all models): 20% used · resets Nov 5, 9am (UTC)"}'
`
	os.WriteFile(script, []byte(body), 0o755)
	t.Setenv("CCMETER_CLAUDE_BIN", script)

	work := filepath.Join(home, ".claude-work")
	five, seven, _, err := claudeUsage(work, time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC))
	if err != nil || five.UsedPct != 61 || seven.UsedPct != 20 {
		t.Fatalf("got %+v %+v %v", five, seven, err)
	}
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], work+"|1|") || strings.Contains(lines[1], "--no-session") {
		t.Fatalf("unexpected invocations:\n%s", b)
	}

	// Default account: CLAUDE_CONFIG_DIR must not be set.
	t.Setenv("CLAUDE_CONFIG_DIR", "/should/be/removed")
	os.Remove(log)
	if _, _, _, err := claudeUsage(filepath.Join(home, ".claude"), time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(log)
	if !strings.HasPrefix(string(b), "|1|") {
		t.Errorf("default account should run without CLAUDE_CONFIG_DIR: %s", b)
	}
}
