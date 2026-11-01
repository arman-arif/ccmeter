// ccmeter: a Claude Code usage meter for several accounts, one per config dir.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

var version = "0.1.0"

// History kept in memory: 30 days for the dashboard plus slack for the 5h window.
const historyWindow = 31 * 24 * time.Hour

const usage = `ccmeter - Claude Code usage meter for multiple accounts

Usage:
  ccmeter [table] [-v]           usage table for every account (default)
  ccmeter json                   same data as JSON
  ccmeter serve [-addr ADDR]     web dashboard (default 127.0.0.1:7878)
  ccmeter statusline             one-line meter for Claude Code's status line
  ccmeter init [--force]         write a config file with detected accounts
  ccmeter accounts               list accounts and where the config lives
  ccmeter version

Global flag:
  -config PATH                   config file (default: $CCMETER_CONFIG or
                                 <user config dir>/ccmeter/config.json)

Accounts come from the config file. Without one, ccmeter looks at
$CLAUDE_CONFIG_DIR, ~/.claude, ~/.config/claude and ~/.claude-*.
`

func main() {
	args := os.Args[1:]
	cfgPath := configPath()
	// Pull -config out of args wherever it appears.
	var rest []string
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
		default:
			rest = append(rest, a)
		}
	}
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
	}

	cfg, fromFile, err := loadConfig(cfgPath)
	if err != nil {
		fail(err)
	}
	pr := newPricer(cfg.Pricing)

	switch cmd {
	case "accounts":
		if fromFile {
			fmt.Println("config:", cfgPath)
		} else {
			fmt.Println("config: none (auto-detected; run `ccmeter init` to create", cfgPath+")")
		}
		for _, a := range cfg.Accounts {
			fmt.Printf("  %-16s %s\n", a.Name, a.Dir)
		}
	case "table":
		fs := flag.NewFlagSet("table", flag.ExitOnError)
		verbose := fs.Bool("v", false, "show per-model breakdown")
		fs.Parse(rest)
		accts := scanAll(cfg)
		printTable(buildReport(accts, pr, time.Now()), *verbose, len(cfg.Accounts) == 0)
	case "json":
		accts := scanAll(cfg)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(buildReport(accts, pr, time.Now()))
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", "127.0.0.1:7878", "listen address")
		every := fs.Duration("interval", 10*time.Second, "rescan interval")
		fs.Parse(rest)
		if err := serve(cfg, cfgPath, pr, *addr, *every); err != nil {
			fail(err)
		}
	case "statusline":
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

func periodCell(p Period) string {
	s := fmtUSD(p.CostUSD) + " / " + fmtTok(p.Tokens)
	if p.LimitUSD > 0 {
		s += fmt.Sprintf(" (%.0f%%)", p.Pct)
	}
	return s
}

func printTable(r Report, verbose, none bool) {
	if none {
		fmt.Println("No Claude Code config dirs found. Run `ccmeter init` and edit the file, or set CLAUDE_CONFIG_DIR.")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\t5-HOUR WINDOW\tTODAY\t7 DAYS\t30 DAYS")
	for _, s := range r.Accounts {
		if !s.Found {
			fmt.Fprintf(w, "%s\tnot found: %s\t\t\t\n", s.Name, s.Dir)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.Name, blockCell(s.Block),
			periodCell(s.Today), periodCell(s.Week), periodCell(s.Month))
	}
	if len(r.Accounts) > 1 {
		fmt.Fprintf(w, "ALL\t\t%s\t%s\t%s\n",
			fmtUSD(r.Total.Today.CostUSD)+" / "+fmtTok(r.Total.Today.Tokens),
			fmtUSD(r.Total.Week.CostUSD)+" / "+fmtTok(r.Total.Week.Tokens),
			fmtUSD(r.Total.Month.CostUSD)+" / "+fmtTok(r.Total.Month.Tokens))
	}
	w.Flush()

	if verbose {
		for _, s := range r.Accounts {
			if !s.Found || len(s.Models) == 0 {
				continue
			}
			fmt.Printf("\n%s - by model, 30 days\n", s.Name)
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
			fmt.Fprintln(w, "MODEL\tINPUT\tOUTPUT\tCACHE WRITE\tCACHE READ\tCOST\t")
			for _, m := range s.Models {
				cost := fmtUSD(m.CostUSD)
				if !m.Priced {
					cost = "unpriced"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t\n", m.Model, fmtTok(m.Input), fmtTok(m.Output),
					fmtTok(m.CacheWrite), fmtTok(m.CacheRead), cost)
			}
			w.Flush()
		}
	}
	for _, s := range r.Accounts {
		if len(s.Unpriced) > 0 {
			fmt.Printf("\nnote: %s has unpriced models (%s); add them under \"pricing\" in the config.\n",
				s.Name, strings.Join(s.Unpriced, ", "))
		}
	}
	fmt.Println("\nCosts are estimates at API list prices; % is of your limit, or of your own 30-day peak window.")
}
