package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // reset times name an IANA zone; embed the database for Windows
)

// Plan limits from the official CLI: `claude -p /usage` is handled locally by
// Claude Code (no model call, no usage cost) and prints lines such as
//
//	Current session: 4% used · resets Nov 2, 10:30am (Asia/Dhaka)
//	Current week (all models): 8% used · resets Nov 2, 9am (Asia/Dhaka)
//
// Claude Code refreshes the account's login itself, so this works even when
// the saved token has expired. It is ccmeter's first choice; the direct
// usage endpoint is the fallback.

const fetchGuardEnv = "CCMETER_IN_FETCH" // set on the child so its hooks don't recurse

// findClaude locates the claude CLI.
func findClaude() (string, error) {
	if p := os.Getenv("CCMETER_CLAUDE_BIN"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	home := homeDir()
	for _, p := range []string{
		filepath.Join(home, ".claude", "local", "claude"),
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".npm-global", "bin", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
		filepath.Join(home, "AppData", "Roaming", "npm", "claude.cmd"),
		filepath.Join(home, ".local", "bin", "claude.exe"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("claude CLI not found (set CCMETER_CLAUDE_BIN to its path)")
}

// claudeEnv returns the environment for running claude against dir. For the
// default ~/.claude, CLAUDE_CONFIG_DIR is left unset: setting it, even to the
// default path, can make Claude Code look for its login under another name.
func claudeEnv(dir string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			env = append(env, kv)
		}
	}
	if !isDefaultConfigDir(dir) {
		abs, _ := absDir(dir)
		env = append(env, "CLAUDE_CONFIG_DIR="+abs)
	}
	return append(env, fetchGuardEnv+"=1")
}

type cliResult struct {
	IsError      bool   `json:"is_error"`
	Result       string `json:"result"`
	LocalCommand string `json:"local_command"`
	NumTurns     int    `json:"num_turns"`
}

func runClaudeUsage(bin, dir string, args []string) (cliResult, error) {
	var r cliResult
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = claudeEnv(dir)
	cmd.Dir = stateDir() // neutral folder: no project CLAUDE.md or settings
	_ = os.MkdirAll(cmd.Dir, 0o755)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr, cmd.Stdin = &stdout, &stderr, nil
	err := cmd.Run()
	if ctx.Err() != nil {
		return r, errors.New("claude -p /usage timed out")
	}
	if jerr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &r); jerr != nil {
		msg := strings.TrimSpace(stderr.String() + " " + stdout.String())
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return r, fmt.Errorf("claude -p /usage: %s", msg)
	}
	return r, nil
}

// claudeUsage runs `claude -p /usage` for the account in dir and parses it.
func claudeUsage(dir string, now time.Time) (*LimitWindow, *LimitWindow, error) {
	bin, err := findClaude()
	if err != nil {
		return nil, nil, err
	}
	base := []string{"-p", "/usage", "--output-format", "json"}
	// Extra flags keep the check side-effect free: no saved session, no MCP
	// servers, and none of your hooks (sounds, notifications) firing.
	extra := []string{"--no-session-persistence", "--strict-mcp-config", "--settings", `{"disableAllHooks":true}`}
	r, err := runClaudeUsage(bin, dir, append(base, extra...))
	if err != nil {
		// Older CLIs may not know the extra flags; retry with the plain command.
		r, err = runClaudeUsage(bin, dir, base)
	}
	if err != nil {
		return nil, nil, err
	}
	if r.IsError {
		return nil, nil, fmt.Errorf("claude -p /usage reported an error: %s", firstLine(r.Result))
	}
	if r.NumTurns > 0 || (r.LocalCommand != "" && r.LocalCommand != "usage") {
		return nil, nil, errors.New("this claude version sent /usage to the model instead of handling it locally")
	}
	five, seven := parseUsageText(r.Result, now)
	if five == nil && seven == nil {
		return nil, nil, fmt.Errorf("no plan limits in claude /usage output (%s)", firstLine(r.Result))
	}
	return five, seven, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}

var (
	usageLineRe = regexp.MustCompile(`(?i)^\s*([^:]+?)\s*:\s*(\d+(?:\.\d+)?)\s*%\s*used(?:\s*[·•|,-]\s*resets?\s+(.+?))?\s*$`)
	resetRe     = regexp.MustCompile(`(?i)^(?:(?:on\s+)?([a-z]{3,9})\.?\s+(\d{1,2}),?\s*(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:\(([^)]+)\))?\s*$`)
	months      = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
)

// parseUsageText extracts the session (5-hour) and weekly (all models)
// windows from /usage output. Other lines (e.g. Sonnet-only weeks) are ignored.
func parseUsageText(text string, now time.Time) (five, seven *LimitWindow) {
	for _, line := range strings.Split(text, "\n") {
		m := usageLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		label := strings.ToLower(m[1])
		pct, _ := strconv.ParseFloat(m[2], 64)
		w := &LimitWindow{UsedPct: pct}
		if m[3] != "" {
			if t, ok := parseResetTime(m[3], now); ok {
				w.ResetsAt = t
			}
		}
		switch {
		case strings.Contains(label, "session") && five == nil:
			five = w
		case strings.Contains(label, "week") && seven == nil &&
			(strings.Contains(label, "all") || !strings.Contains(label, "(")):
			seven = w
		}
	}
	return five, seven
}

// parseResetTime understands "Nov 2, 10:30am (Asia/Dhaka)", "9am (UTC)" and
// "Nov 2 at 9am". The year is inferred as the next occurrence after now.
func parseResetTime(s string, now time.Time) (time.Time, bool) {
	m := resetRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return time.Time{}, false
	}
	loc := now.Location()
	if m[6] != "" {
		if l, err := time.LoadLocation(strings.TrimSpace(m[6])); err == nil {
			loc = l
		}
	}
	ref := now.In(loc)
	hour, _ := strconv.Atoi(m[3])
	min := 0
	if m[4] != "" {
		min, _ = strconv.Atoi(m[4])
	}
	switch strings.ToLower(m[5]) {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour != 12 {
			hour += 12
		}
	}
	if hour > 23 || min > 59 {
		return time.Time{}, false
	}
	if m[1] == "" { // time only: today, or tomorrow if already past
		t := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, min, 0, 0, loc)
		if t.Before(ref.Add(-time.Minute)) {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	mon, ok := months[strings.ToLower(m[1])[:3]]
	if !ok {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[2])
	t := time.Date(ref.Year(), mon, day, hour, min, 0, 0, loc)
	if t.Before(ref.AddDate(0, 0, -2)) { // e.g. "Jan 2" seen in late December
		t = t.AddDate(1, 0, 0)
	}
	return t, true
}
