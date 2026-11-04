package ccmeter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// `ccmeter hook` runs as a Claude Code hook (SessionStart and Stop). Hooks fire
// in the CLI, the IDE extensions and the desktop app, unlike the status line.
// It returns at once and, at most every few minutes per account, starts a
// detached fetch of the real plan limits for the account the session uses.

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	HookEventName  string `json:"hook_event_name"`
}

// hookConfigDir works out which config dir the session belongs to.
func hookConfigDir(in hookInput) string {
	if env := strings.TrimSpace(strings.Split(os.Getenv("CLAUDE_CONFIG_DIR"), ",")[0]); env != "" {
		return env
	}
	// Transcripts live at <config dir>/projects/<project>/<session>.jsonl.
	if in.TranscriptPath != "" {
		p := filepath.Clean(expandHome(in.TranscriptPath))
		for d := filepath.Dir(p); d != filepath.Dir(d); d = filepath.Dir(d) {
			if filepath.Base(d) == "projects" {
				return filepath.Dir(d)
			}
		}
	}
	return filepath.Join(homeDir(), ".claude")
}

func cmdHook() {
	if os.Getenv(fetchGuardEnv) != "" {
		return // fired by ccmeter's own `claude -p /usage` check
	}
	var in hookInput
	if st, _ := os.Stdin.Stat(); st != nil && st.Mode()&os.ModeCharDevice == 0 {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		_ = json.Unmarshal(b, &in)
	}
	dir := hookConfigDir(in)
	if !fetchDue(dir, time.Now()) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	// Detach so the hook (and Claude Code) never waits on the network.
	cmd := exec.Command(exe, "hook-fetch", dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := os.MkdirAll(stateDir(), 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(stateDir(), "hook.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			defer f.Close()
			cmd.Stdout, cmd.Stderr = f, f
		}
	}
	detach(cmd)
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

func cmdHookFetch(dir string) {
	if _, err := refreshLimits(dir, false); err != nil {
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", time.Now().Format(time.RFC3339), dir, err)
	}
}

// cmdLimits: ccmeter limits [NAME...] fetches plan limits now and prints them.
func cmdLimits(cfg Config, names []string) error {
	targets, err := pickTargets(cfg, names)
	if err != nil {
		return err
	}
	labels := make([]string, len(targets))
	plans := make([]*PlanLimits, len(targets))
	errs := make([]error, len(targets))
	failed := 0
	for i, a := range targets {
		if stdoutTTY {
			fmt.Printf("\rchecking %s...\x1b[K", a.Name) // each check takes seconds
		}
		labels[i] = a.Name
		plans[i], errs[i] = refreshLimits(a.Dir, true)
		if errs[i] != nil {
			failed++
		}
	}
	if stdoutTTY {
		fmt.Print("\r\x1b[K")
	}
	printLimits(os.Stdout, labels, plans, errs, time.Now())
	if failed > 0 {
		return fmt.Errorf("%d account(s) without fresh limits", failed)
	}
	return nil
}

func pickTargets(cfg Config, names []string) ([]AccountConfig, error) {
	if len(names) == 0 {
		if len(cfg.Accounts) == 0 {
			return nil, errors.New("no accounts configured")
		}
		return cfg.Accounts, nil
	}
	var out []AccountConfig
	for _, n := range names {
		found := false
		for _, a := range cfg.Accounts {
			if a.Name == n || sameDir(a.Dir, n) {
				out, found = append(out, a), true
			}
		}
		if !found {
			return nil, fmt.Errorf("no account %q (see `ccmeter accounts`)", n)
		}
	}
	return out, nil
}

// ---- hook install / uninstall --------------------------------------------

var hookEvents = []string{"SessionStart", "Stop"}

func isCcmeterHook(cmd string) bool {
	return strings.Contains(cmd, "ccmeter") && strings.Contains(cmd, "hook")
}

func readSettings(path string) (map[string]any, []byte, error) {
	settings := map[string]any{}
	orig, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if len(strings.TrimSpace(string(orig))) > 0 {
		if err := json.Unmarshal(orig, &settings); err != nil {
			return nil, nil, fmt.Errorf("%s isn't valid JSON (%v); fix it or edit hooks by hand", path, err)
		}
	}
	return settings, orig, nil
}

func writeSettings(path string, settings map[string]any, orig []byte) error {
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if orig != nil {
		if err := os.WriteFile(path+".bak", orig, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// stripCcmeterHooks removes ccmeter's hook entries from one event's groups,
// leaving everything else untouched. Returns the new list and how many it removed.
func stripCcmeterHooks(groups []any) ([]any, int) {
	var out []any
	removed := 0
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			out = append(out, g)
			continue
		}
		inner, _ := gm["hooks"].([]any)
		var keep []any
		for _, h := range inner {
			if hm, ok := h.(map[string]any); ok {
				if c, _ := hm["command"].(string); isCcmeterHook(c) {
					removed++
					continue
				}
			}
			keep = append(keep, h)
		}
		if len(inner) > 0 && len(keep) == 0 {
			continue // group held only ccmeter's hook
		}
		gm["hooks"] = keep
		out = append(out, gm)
	}
	return out, removed
}

func ccmeterCommands(groups []any) []string {
	var out []string
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		inner, _ := gm["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if c, _ := hm["command"].(string); isCcmeterHook(c) {
				out = append(out, c)
			}
		}
	}
	return out
}

func installHook(acct AccountConfig, command string) (string, error) {
	path := settingsPath(acct.Dir)
	settings, orig, err := readSettings(path)
	if err != nil {
		return "", err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// Unchanged if every event already has exactly this one ccmeter hook.
	same := true
	for _, ev := range hookEvents {
		groups, _ := hooks[ev].([]any)
		if cmds := ccmeterCommands(groups); len(cmds) != 1 || cmds[0] != command {
			same = false
		}
	}
	for _, ev := range hookEvents {
		groups, _ := hooks[ev].([]any)
		stripped, _ := stripCcmeterHooks(groups)
		hooks[ev] = append(stripped, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": command}},
		})
	}
	if same {
		return "already set", nil
	}
	settings["hooks"] = hooks
	if err := writeSettings(path, settings, orig); err != nil {
		return "", err
	}
	return "installed", nil
}

func uninstallHook(acct AccountConfig) (string, error) {
	path := settingsPath(acct.Dir)
	settings, orig, err := readSettings(path)
	if err != nil {
		return "", err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	total := 0
	for ev, v := range hooks {
		groups, _ := v.([]any)
		stripped, n := stripCcmeterHooks(groups)
		total += n
		if len(stripped) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = stripped
		}
	}
	if total == 0 {
		return "not set", nil
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	if err := writeSettings(path, settings, orig); err != nil {
		return "", err
	}
	return "removed", nil
}

// cmdHookSetup: ccmeter hook install|uninstall [NAME...]
func cmdHookSetup(cfgPath, action string, names []string) error {
	cfg, _, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	targets, err := pickTargets(cfg, names)
	if err != nil {
		return err
	}
	var command string
	if action == "install" {
		exe, err := executablePath()
		if err != nil {
			return err
		}
		command = shellQuote(exe) + " hook"
	}
	failed := 0
	for _, a := range targets {
		var res string
		if action == "install" {
			res, err = installHook(a, command)
		} else {
			res, err = uninstallHook(a)
		}
		if err != nil {
			fmt.Printf("  %-16s error: %v\n", a.Name, err)
			failed++
			continue
		}
		fmt.Printf("  %-16s %s (%s)\n", a.Name, res, settingsPath(a.Dir))
	}
	if action == "install" {
		fmt.Println("Plan limits refresh when a session starts and after each response, at most every 5 minutes per account.")
		fmt.Println("Check now with: ccmeter limits")
	}
	if failed > 0 {
		return fmt.Errorf("%d account(s) not updated", failed)
	}
	return nil
}
