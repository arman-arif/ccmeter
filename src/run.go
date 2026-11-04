package ccmeter

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// History kept in memory: 30 days for the dashboard plus slack for the 5h window.
const historyWindow = 31 * 24 * time.Hour

const usage = `ccmeter - Claude Code usage meter for multiple accounts

Usage:
  ccmeter [table] [-v]           usage table for every account (default);
                                 refreshes stale plan limits first (-no-refresh)
  ccmeter json                   same data as JSON
  ccmeter serve [-addr ADDR]     web dashboard (default 127.0.0.1:7878);
                                 -read-only turns off account editing;
                                 -limits-interval 5m sets how often plan
                                 limits are checked in the background (0 = off)
  ccmeter statusline             one-line meter for Claude Code's status line;
                                 also records the real plan limits it reports
  ccmeter statusline install [NAME...] [--force]
                                 set ccmeter as the status line of every (or
                                 the named) account, to collect plan limits
  ccmeter statusline uninstall [NAME...]
  ccmeter hook install [NAME...] add a Claude Code hook (works in the CLI, IDE
                                 extensions and desktop app) that refreshes the
                                 real plan limits after each response
  ccmeter hook uninstall [NAME...]
  ccmeter limits [NAME...]       fetch and print the real plan limits now
  ccmeter init [--force]         write a config file with detected accounts
  ccmeter accounts               list accounts and where the config lives
  ccmeter add NAME DIR [flags]   add an account (any path; --block-limit,
                                 --weekly-limit in USD, --force to replace)
  ccmeter remove NAME|DIR        remove an account

Background:
  ccmeter start [serve flags]    run the dashboard in the background
  ccmeter stop                   stop the background dashboard
  ccmeter restart [serve flags]  stop, then start again
  ccmeter status                 is it running, where, and is autostart on
  ccmeter autostart enable [serve flags]
                                 start the dashboard when you log in
                                 (systemd/launchd/Windows Startup) and now
  ccmeter autostart disable      remove autostart and stop it
  ccmeter autostart status
  ccmeter version

Global flags:
  -config PATH                   config file (default: $CCMETER_CONFIG or
                                 <user config dir>/ccmeter/config.json)
  --color, --no-color            force color on or off (default: on in a
                                 terminal, off when piped or with $NO_COLOR)

Accounts come from the config file. Without one, ccmeter looks at
$CLAUDE_CONFIG_DIR, ~/.claude, ~/.config/claude and ~/.claude-*.
`

// Main runs the ccmeter command line. It reads os.Args so the root main.go
// stays a thin entry point over this package.
func Main() {
	args := os.Args[1:]
	cfgPath := configPath()
	// Pull -config and the color flags out of args wherever they appear.
	var rest []string
	forceColor := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-config" || a == "--config":
			if i+1 < len(args) {
				cfgPath = expandHome(args[i+1])
				i++
			}
		case strings.HasPrefix(a, "-config=") || strings.HasPrefix(a, "--config="):
			cfgPath = expandHome(a[strings.Index(a, "=")+1:])
		case a == "--color":
			forceColor = 1
		case a == "--no-color":
			forceColor = -1
		default:
			rest = append(rest, a)
		}
	}
	stdoutTTY = isTerminal(os.Stdout)
	useColor = colorEnabled(forceColor, stdoutTTY, os.Getenv("NO_COLOR"), os.Getenv("TERM"), enableTerminalColor)
	cmd := "table"
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		cmd, rest = rest[0], rest[1:]
	}

	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	case "version":
		fmt.Println("ccmeter", version)
		return
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		force := fs.Bool("force", false, "overwrite an existing config")
		fs.Parse(rest)
		if err := writeSampleConfig(cfgPath, *force); err != nil {
			fail(err)
		}
		fmt.Println("wrote", cfgPath)
		return
	case "start":
		if err := cmdStart(cfgPath, rest, false); err != nil {
			fail(err)
		}
		return
	case "stop":
		if err := cmdStop(false); err != nil {
			fail(err)
		}
		return
	case "restart":
		if err := cmdStop(true); err != nil {
			fail(err)
		}
		if err := cmdStart(cfgPath, rest, false); err != nil {
			fail(err)
		}
		return
	case "status":
		cmdStatus(rest)
		return
	case "autostart":
		if err := cmdAutostart(cfgPath, rest); err != nil {
			fail(err)
		}
		return
	case "add":
		if err := cmdAdd(cfgPath, rest); err != nil {
			fail(err)
		}
		return
	case "remove", "rm":
		if err := cmdRemove(cfgPath, rest); err != nil {
			fail(err)
		}
		return
	}

	cfg, fromFile, err := loadConfig(cfgPath)
	if err != nil {
		fail(err)
	}
	pr := newPricer(cfg.Pricing)

	switch cmd {
	case "accounts":
		printAccounts(os.Stdout, cfg, cfgPath, fromFile)
	case "table":
		fs := flag.NewFlagSet("table", flag.ExitOnError)
		verbose := fs.Bool("v", false, "show per-model breakdown")
		noRefresh := fs.Bool("no-refresh", false, "don't refresh stale plan limits first")
		fs.Parse(rest)
		accts := scanAll(cfg)
		if !*noRefresh {
			refreshDueLimits(accts)
		}
		printTable(os.Stdout, buildReport(accts, pr, time.Now()), *verbose, len(cfg.Accounts) == 0)
	case "json":
		accts := scanAll(cfg)
		refreshDueLimits(accts)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(buildReport(accts, pr, time.Now()))
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", defaultAddr, "listen address")
		every := fs.Duration("interval", 10*time.Second, "rescan interval")
		readOnly := fs.Bool("read-only", false, "disable managing accounts from the dashboard")
		limitsEvery := fs.Duration("limits-interval", defaultLimitsInterval, "how often to check plan limits for active accounts (0 = off)")
		fs.Parse(rest)
		if err := serve(cfg, cfgPath, pr, *addr, *every, *readOnly, *limitsEvery); err != nil {
			fail(err)
		}
	case "hook":
		if len(rest) > 0 && (rest[0] == "install" || rest[0] == "uninstall") {
			if err := cmdHookSetup(cfgPath, rest[0], rest[1:]); err != nil {
				fail(err)
			}
			return
		}
		cmdHook() // never fails: a hook must not disturb Claude Code
		return
	case "hook-fetch":
		if len(rest) == 1 {
			cmdHookFetch(rest[0])
		}
		return
	case "limits":
		if err := cmdLimits(cfg, rest); err != nil {
			fail(err)
		}
		return
	case "statusline":
		if len(rest) > 0 && (rest[0] == "install" || rest[0] == "uninstall") {
			if err := cmdStatuslineSetup(cfgPath, rest[0], rest[1:]); err != nil {
				fail(err)
			}
			return
		}
		runStatusline(cfg, pr)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ccmeter:", err)
	os.Exit(1)
}

func scanAll(cfg Config) []*Account {
	cutoff := time.Now().Add(-historyWindow)
	accts := make([]*Account, len(cfg.Accounts))
	done := make(chan struct{})
	for i, c := range cfg.Accounts {
		accts[i] = newAccount(c)
		go func(a *Account) { a.Scan(cutoff); done <- struct{}{} }(accts[i])
	}
	for range accts {
		<-done
	}
	return accts
}

func fmtTok(n int64) string {
	f := float64(n)
	switch {
	case f >= 1e9:
		return fmt.Sprintf("%.2fB", f/1e9)
	case f >= 1e6:
		return fmt.Sprintf("%.1fM", f/1e6)
	case f >= 1e3:
		return fmt.Sprintf("%.1fk", f/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func fmtUSD(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func fmtDur(min int) string {
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	return fmt.Sprintf("%dh%02dm", min/60, min%60)
}

func blockCell(b Block) string {
	if !b.Active {
		return "idle"
	}
	s := fmtUSD(b.CostUSD)
	if b.LimitUSD > 0 {
		tag := ""
		if b.LimitSource == "peak" {
			tag = " of peak"
		}
		s += fmt.Sprintf(" (%.0f%%%s)", b.Pct, tag)
	}
	return s + ", " + fmtDur(b.RemainingMin) + " left"
}
