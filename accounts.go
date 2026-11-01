package main

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
		return "", fmt.Errorf("empty directory")
	}
	return filepath.Abs(dir)
}

func sameDir(a, b string) bool {
	x, err1 := absDir(a)
	y, err2 := absDir(b)
	return err1 == nil && err2 == nil && x == y
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
	name, dir := strings.TrimSpace(pos[0]), pos[1]
	if name == "" {
		return fmt.Errorf("account name can't be empty")
	}
	abs, err := absDir(dir)
	if err != nil {
		return err
	}

	cfg, fromFile, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	if !fromFile && len(cfg.Accounts) > 0 {
		// First write: keep the auto-detected accounts so they don't disappear.
		fmt.Printf("creating %s with %d auto-detected account(s)\n", cfgPath, len(cfg.Accounts))
	}

	replaced := false
	for i, a := range cfg.Accounts {
		switch {
		case a.Name == name:
			if !*force {
				return fmt.Errorf("account %q already exists (%s); use --force to replace it", name, a.Dir)
			}
			cfg.Accounts[i] = AccountConfig{Name: name, Dir: abs, BlockLimitUSD: *block, WeeklyLimitUSD: *weekly}
			replaced = true
		case sameDir(a.Dir, abs):
			return fmt.Errorf("%s is already configured as account %q", abs, a.Name)
		}
	}
	if !replaced {
		cfg.Accounts = append(cfg.Accounts, AccountConfig{Name: name, Dir: abs, BlockLimitUSD: *block, WeeklyLimitUSD: *weekly})
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		return err
	}

	verb := "added"
	if replaced {
		verb = "replaced"
	}
	fmt.Printf("%s %s -> %s\n", verb, name, abs)
	if st, err := os.Stat(filepath.Join(abs, "projects")); err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "warning: %s has no projects/ folder yet; it will show as not found until Claude Code has run with CLAUDE_CONFIG_DIR=%s\n", abs, abs)
	}
	fmt.Println("restart `ccmeter serve` if it is running so it picks up the change")
	return nil
}

// cmdRemove: ccmeter remove <name|dir>
func cmdRemove(cfgPath string, args []string) error {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: ccmeter remove <name|dir>")
		os.Exit(2)
	}
	key := args[0]
	cfg, fromFile, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
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
		return fmt.Errorf("no account named or located at %q (see `ccmeter accounts`)", key)
	}
	removed := cfg.Accounts[idx]
	cfg.Accounts = append(cfg.Accounts[:idx], cfg.Accounts[idx+1:]...)
	if err := saveConfig(cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("removed %s (%s)\n", removed.Name, removed.Dir)
	if !fromFile {
		fmt.Printf("created %s so the removal sticks; auto-detection is off while it exists\n", cfgPath)
	}
	return nil
}
