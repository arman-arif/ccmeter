package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Real plan limits come from Claude Code itself: on every status-line repaint
// it pipes JSON with rate_limits.five_hour / rate_limits.seven_day (used
// percentage and reset time) to the configured command. `ccmeter statusline`
// saves the latest values per account so the table and dashboard can show them.
// Only Pro/Max/Team/Enterprise logins get these fields, and only after the
// session's first response.

// LimitWindow is one plan window as last reported by Claude Code.
type LimitWindow struct {
	UsedPct  float64   `json:"used_pct"`
	ResetsAt time.Time `json:"resets_at"`
	// Reset is set when resets_at has passed since the last report, so the
	// saved percentage no longer applies.
	Reset bool `json:"reset,omitempty"`
}

// PlanLimits is what the dashboard and table read.
type PlanLimits struct {
	Source    string       `json:"source"` // "claude-code-statusline"
	UpdatedAt time.Time    `json:"updated_at"`
	AgeMin    int          `json:"age_min"`
	FiveHour  *LimitWindow `json:"five_hour,omitempty"`
	SevenDay  *LimitWindow `json:"seven_day,omitempty"`
}

// rawRateLimits mirrors the status-line JSON. resets_at has been sent both as
// epoch seconds and as an ISO 8601 string, so it is decoded loosely.
type rawRateLimits struct {
	FiveHour *rawWindow `json:"five_hour"`
	SevenDay *rawWindow `json:"seven_day"`
}

type rawWindow struct {
	UsedPercentage *float64        `json:"used_percentage"`
	ResetsAt       json.RawMessage `json:"resets_at"`
	ResetAt        json.RawMessage `json:"reset_at"`
}

func parseLooseTime(raw json.RawMessage) (time.Time, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return time.Time{}, false
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return time.Time{}, false
		}
		if t, err := time.Parse(time.RFC3339Nano, str); err == nil {
			return t, true
		}
		s = str // maybe a quoted number
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return time.Time{}, false
	}
	if f > 1e12 { // milliseconds
		f /= 1000
	}
	return time.Unix(int64(f), 0), true
}

func (w *rawWindow) toWindow() *LimitWindow {
	if w == nil || w.UsedPercentage == nil {
		return nil
	}
	lw := &LimitWindow{UsedPct: *w.UsedPercentage}
	if t, ok := parseLooseTime(w.ResetsAt); ok {
		lw.ResetsAt = t
	} else if t, ok := parseLooseTime(w.ResetAt); ok {
		lw.ResetsAt = t
	}
	return lw
}

func limitsFile(dir string) string {
	abs, err := absDir(dir)
	if err != nil {
		abs = dir
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(stateDir(), "limits", hex.EncodeToString(sum[:])[:16]+".json")
}

type savedLimits struct {
	Dir       string       `json:"dir"`
	UpdatedAt time.Time    `json:"updated_at"`
	FiveHour  *LimitWindow `json:"five_hour,omitempty"`
	SevenDay  *LimitWindow `json:"seven_day,omitempty"`
}

// saveLimits stores what Claude Code reported for the account in dir.
func saveLimits(dir string, rl *rawRateLimits, now time.Time) error {
	if rl == nil {
		return nil
	}
	s := savedLimits{Dir: dir, UpdatedAt: now, FiveHour: rl.FiveHour.toWindow(), SevenDay: rl.SevenDay.toWindow()}
	if s.FiveHour == nil && s.SevenDay == nil {
		return nil
	}
	path := limitsFile(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(s)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadLimits returns the last report for dir, or nil if there is none newer
// than 8 days (older than a weekly window, so nothing in it still applies).
func loadLimits(dir string, now time.Time) *PlanLimits {
	b, err := os.ReadFile(limitsFile(dir))
	if err != nil {
		return nil
	}
	var s savedLimits
	if json.Unmarshal(b, &s) != nil || now.Sub(s.UpdatedAt) > 8*24*time.Hour {
		return nil
	}
	pl := &PlanLimits{Source: "claude-code-statusline", UpdatedAt: s.UpdatedAt,
		AgeMin: int(now.Sub(s.UpdatedAt).Minutes()), FiveHour: s.FiveHour, SevenDay: s.SevenDay}
	for _, w := range []*LimitWindow{pl.FiveHour, pl.SevenDay} {
		if w != nil && !w.ResetsAt.IsZero() && now.After(w.ResetsAt) {
			w.Reset = true
		}
	}
	return pl
}

// ---- status line setup ---------------------------------------------------

func settingsPath(dir string) string { return filepath.Join(expandHome(dir), "settings.json") }

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'$`\\&|;<>()*?!#~") {
		return s
	}
	return `"` + strings.NewReplacer(`"`, `\"`, `$`, `\$`, "`", "\\`").Replace(s) + `"`
}

func statuslineCommand(exe, cfgPath string) string {
	return shellQuote(exe) + " -config " + shellQuote(cfgPath) + " statusline"
}

// installStatusline points the account's Claude Code status line at ccmeter.
// It keeps every other setting, refuses to replace a different status line
// unless force is set, and writes settings.json.bak first.
func installStatusline(acct AccountConfig, command string, force bool) (string, error) {
	path := settingsPath(acct.Dir)
	settings := map[string]any{}
	orig, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(orig))) > 0 {
			if err := json.Unmarshal(orig, &settings); err != nil {
				return "", fmt.Errorf("%s isn't valid JSON (%v); fix it or add the status line by hand", path, err)
			}
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return "", err
	}
	if cur, ok := settings["statusLine"].(map[string]any); ok {
		c, _ := cur["command"].(string)
		if c == command {
			return "already set", nil
		}
		if !force && !strings.Contains(c, "ccmeter") {
			return "", fmt.Errorf("%s already has a status line (%q); use --force to replace it", acct.Name, c)
		}
	}
	settings["statusLine"] = map[string]any{"type": "command", "command": command}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if orig != nil {
		if err := os.WriteFile(path+".bak", orig, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return "", err
	}
	return "installed", nil
}

func uninstallStatusline(acct AccountConfig) (string, error) {
	path := settingsPath(acct.Dir)
	orig, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "not set", nil
	}
	if err != nil {
		return "", err
	}
	settings := map[string]any{}
	if err := json.Unmarshal(orig, &settings); err != nil {
		return "", fmt.Errorf("%s isn't valid JSON: %v", path, err)
	}
	cur, ok := settings["statusLine"].(map[string]any)
	if !ok {
		return "not set", nil
	}
	if c, _ := cur["command"].(string); !strings.Contains(c, "ccmeter") {
		return "left alone (not ccmeter's)", nil
	}
	delete(settings, "statusLine")
	out, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(path+".bak", orig, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return "", err
	}
	return "removed", nil
}

// cmdStatuslineSetup: ccmeter statusline install|uninstall [NAME...] [--force]
func cmdStatuslineSetup(cfgPath string, action string, args []string) error {
	force := false
	var names []string
	for _, a := range args {
		if a == "--force" || a == "-force" {
			force = true
		} else {
			names = append(names, a)
		}
	}
	cfg, _, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	var targets []AccountConfig
	if len(names) == 0 {
		targets = cfg.Accounts
	} else {
		for _, n := range names {
			found := false
			for _, a := range cfg.Accounts {
				if a.Name == n || sameDir(a.Dir, n) {
					targets, found = append(targets, a), true
				}
			}
			if !found {
				return fmt.Errorf("no account %q (see `ccmeter accounts`)", n)
			}
		}
	}
	if len(targets) == 0 {
		return errors.New("no accounts configured")
	}
	var command string
	if action == "install" {
		exe, err := executablePath()
		if err != nil {
			return err
		}
		cfgPath = absConfigPath(cfgPath)
		if err := ensureConfigFile(cfgPath); err != nil {
			return err
		}
		command = statuslineCommand(exe, cfgPath)
	}
	failed := 0
	for _, a := range targets {
		var res string
		var err error
		if action == "install" {
			res, err = installStatusline(a, command, force)
		} else {
			res, err = uninstallStatusline(a)
		}
		if err != nil {
			fmt.Printf("  %-16s error: %v\n", a.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %-16s %s (%s)\n", a.Name, res, settingsPath(a.Dir))
	}
	if action == "install" {
		fmt.Println("Plan limits show up after the next Claude Code response in each account (Pro/Max/Team plans only).")
	}
	if failed > 0 {
		return fmt.Errorf("%d account(s) not updated", failed)
	}
	return nil
}
