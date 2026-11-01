package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// statusInput is the JSON Claude Code pipes to a status-line command.
type statusInput struct {
	TranscriptPath string `json:"transcript_path"`
	Model          struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	RateLimits *rawRateLimits `json:"rate_limits"`
}

// runStatusline prints one line for the account the current session belongs to.
// Wire it up in <config dir>/settings.json:
//
//	"statusLine": {"type": "command", "command": "ccmeter statusline"}
func runStatusline(cfg Config, pr *Pricer) {
	var in statusInput
	if st, _ := os.Stdin.Stat(); st != nil && st.Mode()&os.ModeCharDevice == 0 {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		_ = json.Unmarshal(b, &in)
	}

	acct := pickAccount(cfg, in.TranscriptPath)
	_ = saveLimits(acct.Dir, in.RateLimits, time.Now())
	a := newAccount(acct)
	cutoff := time.Now().Add(-8 * 24 * time.Hour) // enough for today, 5h and 7 days
	a.Scan(cutoff)
	s := summarize(a, pr, time.Now())

	parts := []string{acct.Name}
	if in.Model.DisplayName != "" {
		parts = append(parts, in.Model.DisplayName)
	}
	if w := planWindow(s.Plan, true); w != nil {
		parts = append(parts, "5h "+planCell(w))
	} else {
		parts = append(parts, "5h: "+blockCell(s.Block))
	}
	if w := planWindow(s.Plan, false); w != nil {
		parts = append(parts, "week "+planCell(w))
	} else if s.Week.LimitUSD > 0 {
		parts = append(parts, fmt.Sprintf("week %.0f%%", s.Week.Pct))
	}
	parts = append(parts, "today "+fmtUSD(s.Today.CostUSD))
	fmt.Println(strings.Join(parts, " · "))
}

func planWindow(p *PlanLimits, fiveHour bool) *LimitWindow {
	if p == nil {
		return nil
	}
	if fiveHour {
		return p.FiveHour
	}
	return p.SevenDay
}

// planCell formats a plan window: "42% (resets 14:00)".
func planCell(w *LimitWindow) string {
	if w.Reset {
		return "reset"
	}
	s := fmt.Sprintf("%.0f%%", w.UsedPct)
	if !w.ResetsAt.IsZero() {
		left := time.Until(w.ResetsAt)
		if left < 24*time.Hour {
			s += " (" + fmtDur(int(left.Minutes())) + " left)"
		} else {
			s += " (resets " + w.ResetsAt.Local().Format("Mon 15:04") + ")"
		}
	}
	return s
}

// pickAccount decides which account a session belongs to: CLAUDE_CONFIG_DIR
// (inherited from the Claude Code process), then the transcript path, then
// the first configured account.
func pickAccount(cfg Config, transcript string) AccountConfig {
	clean := func(p string) string { return filepath.Clean(expandHome(p)) }
	if env := strings.Split(os.Getenv("CLAUDE_CONFIG_DIR"), ",")[0]; strings.TrimSpace(env) != "" {
		for _, a := range cfg.Accounts {
			if clean(a.Dir) == clean(env) {
				return a
			}
		}
		return AccountConfig{Name: nameFromDir(env), Dir: env}
	}
	if transcript != "" {
		t := clean(transcript)
		best := -1
		var pick AccountConfig
		for _, a := range cfg.Accounts {
			d := clean(a.Dir) + string(filepath.Separator)
			if strings.HasPrefix(t, d) && len(d) > best {
				best, pick = len(d), a
			}
		}
		if best >= 0 {
			return pick
		}
	}
	if len(cfg.Accounts) > 0 {
		return cfg.Accounts[0]
	}
	return AccountConfig{Name: "default", Dir: filepath.Join(homeDir(), ".claude")}
}
