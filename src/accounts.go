package ccmeter

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// parseInterleaved parses flags that may appear before, between or after
// positional arguments (Go's flag package stops at the first positional one).
func parseInterleaved(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		fs.Parse(args)
		if fs.NArg() == 0 {
			return pos
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func saveConfig(path string, cfg Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// absDir turns a user-supplied dir into a clean absolute path.
func absDir(dir string) (string, error) {
	dir = expandHome(dir)
	if dir == "" {
		return "", fmt.Errorf("directory can't be empty")
	}
	return filepath.Abs(dir)
}

func sameDir(a, b string) bool {
	x, err1 := absDir(a)
	y, err2 := absDir(b)
	return err1 == nil && err2 == nil && x == y
}

func hasProjects(dir string) bool {
	st, err := os.Stat(filepath.Join(expandHome(dir), "projects"))
	return err == nil && st.IsDir()
}

// upsertAccount adds acct to cfg, or updates the account currently named
// `original` when that is set (allowing a rename). With replace, an existing
// account of the same name is overwritten. Returns whether one was replaced.
func upsertAccount(cfg *Config, acct AccountConfig, original string, replace bool) (bool, error) {
	acct.Name = strings.TrimSpace(acct.Name)
	if acct.Name == "" {
		return false, fmt.Errorf("account name can't be empty")
	}
	if acct.BlockLimitUSD < 0 || acct.WeeklyLimitUSD < 0 {
		return false, fmt.Errorf("limits can't be negative")
	}
	abs, err := absDir(acct.Dir)
	if err != nil {
		return false, err
	}
	acct.Dir = abs

	target := -1
	if original != "" {
		for i, a := range cfg.Accounts {
			if a.Name == original {
				target = i
			}
		}
		if target < 0 {
			return false, fmt.Errorf("no account named %q", original)
		}
	}
	for i, a := range cfg.Accounts {
		if i == target {
			continue
		}
		if a.Name == acct.Name {
			if !replace || target >= 0 {
				return false, fmt.Errorf("account %q already exists (%s)", a.Name, a.Dir)
			}
			target = i
			continue
		}
		if sameDir(a.Dir, abs) {
			return false, fmt.Errorf("%s is already configured as account %q", abs, a.Name)
		}
	}
	if target >= 0 {
		cfg.Accounts[target] = acct
		return true, nil
	}
	cfg.Accounts = append(cfg.Accounts, acct)
	return false, nil
}

// removeAccount deletes the account matching key by name, then by dir.
func removeAccount(cfg *Config, key string) (AccountConfig, error) {
	idx := -1
	for i, a := range cfg.Accounts {
		if a.Name == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, a := range cfg.Accounts {
			if sameDir(a.Dir, key) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return AccountConfig{}, fmt.Errorf("no account named or located at %q", key)
	}
	removed := cfg.Accounts[idx]
	cfg.Accounts = append(cfg.Accounts[:idx], cfg.Accounts[idx+1:]...)
	return removed, nil
}

// cmdAdd: ccmeter add <name> <dir> [--block-limit USD] [--weekly-limit USD] [--force]
func cmdAdd(cfgPath string, args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	block := fs.Float64("block-limit", 0, "5-hour limit in equivalent API cost (USD)")
	weekly := fs.Float64("weekly-limit", 0, "weekly limit in equivalent API cost (USD)")
	force := fs.Bool("force", false, "replace an existing account with the same name")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ccmeter add <name> <dir> [--block-limit USD] [--weekly-limit USD] [--force]")
		fs.PrintDefaults()
	}
	pos := parseInterleaved(fs, args)
	if len(pos) != 2 {
		fs.Usage()
		os.Exit(2)
	}

	cfg, fromFile, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	if !fromFile && len(cfg.Accounts) > 0 {
		// First write: keep the auto-detected accounts so they don't disappear.
		fmt.Printf("creating %s with %d auto-detected account(s)\n", cfgPath, len(cfg.Accounts))
	}
	acct := AccountConfig{Name: pos[0], Dir: pos[1], BlockLimitUSD: *block, WeeklyLimitUSD: *weekly}
	replaced, err := upsertAccount(&cfg, acct, "", *force)
	if err != nil {
		if !*force && strings.Contains(err.Error(), "already exists") {
			err = fmt.Errorf("%v; use --force to replace it", err)
		}
		return err
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		return err
	}
	saved := cfg.Accounts[len(cfg.Accounts)-1]
	for _, a := range cfg.Accounts {
		if a.Name == strings.TrimSpace(pos[0]) {
			saved = a
		}
	}
	verb := "added"
	if replaced {
		verb = "replaced"
	}
	fmt.Printf("%s %s -> %s\n", verb, saved.Name, saved.Dir)
	if !hasProjects(saved.Dir) {
		fmt.Fprintf(os.Stderr, "warning: %s has no projects/ folder yet; it will show as not found until Claude Code has run with CLAUDE_CONFIG_DIR=%s\n", saved.Dir, saved.Dir)
	}
	fmt.Println("a running `ccmeter serve` picks this up within one rescan")
	return nil
}

// cmdRemove: ccmeter remove <name|dir>
func cmdRemove(cfgPath string, args []string) error {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: ccmeter remove <name|dir>")
		os.Exit(2)
	}
	cfg, fromFile, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	removed, err := removeAccount(&cfg, args[0])
	if err != nil {
		return fmt.Errorf("%v (see `ccmeter accounts`)", err)
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("removed %s (%s)\n", removed.Name, removed.Dir)
	if !fromFile {
		fmt.Printf("created %s so the removal sticks; auto-detection is off while it exists\n", cfgPath)
	}
	return nil
}
