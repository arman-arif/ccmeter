package ccmeter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AccountConfig describes one Claude Code account, identified by its config dir.
type AccountConfig struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// Optional limits, in equivalent API cost (USD). When zero, the meter
	// falls back to your own 30-day peak for the 5-hour window.
	BlockLimitUSD  float64 `json:"block_limit_usd,omitempty"`
	WeeklyLimitUSD float64 `json:"weekly_limit_usd,omitempty"`
	// Archived keeps the account and its history but leaves it out of the
	// table, the dashboard and background plan-limit checks.
	Archived bool `json:"archived,omitempty"`
}

// Config is the whole ccmeter config file.
type Config struct {
	Accounts []AccountConfig `json:"accounts"`
	// Pricing overrides, keyed by a substring of the model name, USD per million tokens.
	Pricing map[string]Price `json:"pricing,omitempty"`
}

func configPath() string {
	if p := os.Getenv("CCMETER_CONFIG"); p != "" {
		return expandHome(p)
	}
	return filepath.Join(userConfigDir(), "ccmeter", "config.json")
}

func userConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(homeDir(), ".config")
	}
	return dir
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}

// loadConfig reads the config file, or auto-detects accounts if there is none.
func loadConfig(path string) (Config, bool, error) {
	var cfg Config
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg.Accounts = detectAccounts()
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, true, fmt.Errorf("parse %s: %w", path, err)
	}
	for i := range cfg.Accounts {
		if cfg.Accounts[i].Name == "" {
			cfg.Accounts[i].Name = nameFromDir(cfg.Accounts[i].Dir)
		}
	}
	return cfg, true, nil
}

// detectAccounts finds config dirs: $CLAUDE_CONFIG_DIR (comma separated),
// ~/.claude, ~/.config/claude and any ~/.claude-* directory with a projects/ folder.
func detectAccounts() []AccountConfig {
	seen := map[string]bool{}
	var out []AccountConfig
	add := func(dir string) {
		dir = filepath.Clean(expandHome(dir))
		if dir == "" || seen[dir] {
			return
		}
		if st, err := os.Stat(filepath.Join(dir, "projects")); err != nil || !st.IsDir() {
			return
		}
		seen[dir] = true
		out = append(out, AccountConfig{Name: nameFromDir(dir), Dir: dir})
	}
	for _, d := range strings.Split(os.Getenv("CLAUDE_CONFIG_DIR"), ",") {
		add(d)
	}
	home := homeDir()
	add(filepath.Join(home, ".claude"))
	add(filepath.Join(home, ".config", "claude"))
	matches, _ := filepath.Glob(filepath.Join(home, ".claude-*"))
	sort.Strings(matches)
	for _, m := range matches {
		add(m)
	}
	return out
}

func nameFromDir(dir string) string {
	base := filepath.Base(filepath.Clean(expandHome(dir)))
	switch base {
	case ".claude", "claude":
		return "default"
	}
	base = strings.TrimPrefix(base, ".claude-")
	base = strings.TrimPrefix(base, ".claude_")
	base = strings.TrimPrefix(base, ".")
	if base == "" {
		return dir
	}
	return base
}

func writeSampleConfig(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	accts := detectAccounts()
	if len(accts) == 0 {
		accts = []AccountConfig{
			{Name: "personal", Dir: "~/.claude"},
			{Name: "work", Dir: "~/.claude-work"},
		}
	}
	cfg := Config{Accounts: accts}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
