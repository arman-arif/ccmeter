# ccmeter

A Claude Code usage meter for **several accounts at once**. ccmeter treats each config dir (the folder `CLAUDE_CONFIG_DIR` points at) as a separate account.

It's a single Go binary with nothing else to install. It reads the session logs in each account's `projects/**/*.jsonl` and estimates usage at API list prices. The web dashboard is built into the binary.

ccmeter is an unofficial, community project. It isn't made, endorsed or supported by Anthropic. "Claude" and "Claude Code" are Anthropic's trademarks.

## Install

```bash
go install github.com/arman-arif/ccmeter@latest   # installs to ~/go/bin
```

Or build from a clone:

```bash
make build            # builds build/bin/ccmeter (not under sudo: it usually can't find go)
sudo make install     # copies it to /usr/local/bin (or: make install PREFIX=~/.local)
```

`make` on its own cross-compiles for Linux, macOS and Windows (amd64 and arm64) into `build/bin/`, with a `SHA256SUMS` file. `make help` lists every target. Without make, `go build -o ccmeter .` works too.

None of this needs Node. The dashboard is a React app in `frontend/`, and its build is committed in `src/web/` and embedded in the binary. You only need Node to change the dashboard: run `make web` afterwards and commit `src/web/`. While working on it, `cd frontend && npm run dev` serves it with live reload against a dashboard running on the default address.

## Set up accounts

```bash
ccmeter init        # writes ~/.config/ccmeter/config.json with every config dir it finds
ccmeter accounts    # shows what it will read
```

Config dirs can live anywhere, not just your home folder. Add or remove them without editing JSON:

```bash
ccmeter add work /mnt/data/claude-work --block-limit 40 --weekly-limit 600
ccmeter add client ./clients/acme/.claude      # relative paths are made absolute
ccmeter add work /new/path --force             # replace an existing account
ccmeter remove client                          # by name or by dir
```

The first `add` or `remove` creates the config file and keeps any auto-detected accounts in it.

You can do the same from the dashboard: the **Accounts** button (or the pencil on a card) opens a panel to add, edit, rename or remove accounts and set their limits. Changes are saved to the same config file. A running dashboard also picks up edits made with the CLI or by hand within one rescan.

Editing in the dashboard only works from the machine running it (loopback requests from the dashboard's own page). Start it with `ccmeter serve -read-only` to turn editing off entirely. To block DNS-rebinding attacks from websites, the dashboard only answers requests addressed to an IP address, `localhost` or this machine's name. If you reach it under another name, start it with `-addr thatname:7878`.

Without a config file, ccmeter looks in `$CLAUDE_CONFIG_DIR` (comma separated), `~/.claude`, `~/.config/claude` and `~/.claude-*`.

```json
{
  "accounts": [
    { "name": "personal", "dir": "~/.claude" },
    { "name": "work",     "dir": "~/.claude-work", "block_limit_usd": 40, "weekly_limit_usd": 600 }
  ],
  "pricing": {
    "claude-example": { "input": 10, "output": 50 }
  }
}
```

The price above is an example. Use it for any model the meter reports as unpriced, or to change a built-in price.

- `block_limit_usd` / `weekly_limit_usd` (optional): your limits, measured in equivalent API cost. Find them by watching where Claude Code stops you. If you don't set `block_limit_usd`, the 5-hour % is measured against your own busiest window in the last 30 days.
- `pricing` (optional): USD per million tokens, matched against model names by substring (the longest match wins). Cache prices default to 1.25x input for 5-minute writes, 2x for 1-hour writes and 0.1x for reads. Built-in prices cover the Fable, Opus, Sonnet and Haiku families, and the dashboard's Prices page lists them with the date they were last checked.

To run Claude Code against a second account: `CLAUDE_CONFIG_DIR=~/.claude-work claude` (log in once there).

## Use

```bash
ccmeter              # table: 5-hour window, today, 7 days, 30 days per account
ccmeter -v           # plus a per-model breakdown
ccmeter --no-color   # plain text; color is also off when piped or with NO_COLOR
ccmeter json         # raw data
ccmeter serve        # dashboard on http://127.0.0.1:7878 (-addr, -interval, -read-only, -limits-interval)
```

The dashboard's overview shows every account side by side. Each account also has its own page at `/accounts/<name>`, with usage by day, by weekday and hour and by token type, past 5-hour windows, models, projects, recent sessions, its plan, email and organization, whether hooks and the status line are set up, and, at the bottom, its plan limits and current 5-hour window. Choose 7 or 30 days and cost, tokens or responses at the top of the page. The choice is kept in the link. The Prices page at `/prices` shows the price each of your models is charged at, your overrides and the built-in list.

Anyone who can reach the dashboard can read this, including your project folder paths and each account's email and organization. It only listens on `127.0.0.1` unless you change `-addr`.

### Run in the background

```bash
ccmeter start              # dashboard in the background (same flags as serve: -addr, -interval, -read-only)
ccmeter status             # running? pid, URL, config, log file, autostart state
ccmeter restart            # stop + start, e.g. after upgrading the binary
ccmeter stop
```

The process is detached from your terminal, so closing the terminal doesn't stop it. Its log goes to `<user cache dir>/ccmeter/ccmeter.log` (`~/.cache/ccmeter` on Linux, `~/Library/Caches/ccmeter` on macOS, `%LocalAppData%\ccmeter` on Windows). `stop` only signals the process after the dashboard confirms its pid, so a stale pid file can't kill something else.

### Start on login

```bash
ccmeter autostart enable   # install and start now (accepts -addr, -interval, -read-only)
ccmeter autostart status
ccmeter autostart disable  # remove and stop
```

| OS | What gets installed | Notes |
|----|---------------------|-------|
| Linux | systemd user service `~/.config/systemd/user/ccmeter.service` | Restarts on crash. Logs: `journalctl --user -u ccmeter -f`. To run at boot before you log in: `loginctl enable-linger $USER`. Without a systemd user session it installs `~/.config/autostart/ccmeter.desktop` instead. |
| macOS | LaunchAgent `~/Library/LaunchAgents/com.ccmeter.dashboard.plist` | Restarts on crash; logs to the cache dir. |
| Windows | `ccmeter.vbs` in your Startup folder | Starts hidden at logon, no admin rights needed. |

Autostart records the binary's absolute path, so install ccmeter where it will stay (re-run `autostart enable` after moving it). If you have no config file yet, `start` and `autostart enable` create one from the accounts detected at that moment, because a background process doesn't see your shell's `CLAUDE_CONFIG_DIR`.

### Keeping plan limits fresh

ccmeter checks your real 5-hour and weekly limits on its own, along with any separate weekly limit your plan has for one model, such as Fable on Max. It uses `claude -p /usage`, which is free (details below):

- **The dashboard** (`serve`, `start`, autostart) checks accounts used in the last 5 hours every 5 minutes and idle accounts every 30 minutes, one at a time. Change it with `-limits-interval 10m`, or turn it off with `-limits-interval 0`. The Refresh button checks every account right away.
- **`ccmeter`** (the table) and `ccmeter json` refresh any account whose last check is more than 5 minutes old before printing (`-no-refresh` to skip).
- **The hook or plugin** (below) also checks right after you use an account, from any Claude Code surface.

All three share one throttle and lock per account, so they never check the same account twice in a row.

### Real plan limits from any Claude Code surface (hook)

The status line only runs in the terminal version of Claude Code. Hooks also run in the VS Code/JetBrains extensions and the desktop app, so this is the way to get real limits there:

```bash
ccmeter hook install        # every account; or: ccmeter hook install work
ccmeter limits              # fetch and print now
ccmeter hook uninstall
```

This adds a `SessionStart` and a `Stop` hook to each account's `settings.json`. Other hooks are kept and a `.bak` copy is written. What happens on each turn:

- **Fast and silent:** the hook returns in milliseconds, prints nothing and never blocks the turn.
- **Finds the account itself:** from `CLAUDE_CONFIG_DIR` or the session's transcript path.
- **Throttled:** at most every 5 minutes per account, it starts a background check of that account's 5-hour and weekly usage. The check runs `claude -p /usage --output-format json` with that account's `CLAUDE_CONFIG_DIR`. Claude Code answers `/usage` locally (no model call, no usage cost) and refreshes the login itself if needed. The check uses `--no-session-persistence`, no MCP servers and hooks disabled, so it leaves no session behind and doesn't trigger your hooks.
- **Needs the `claude` CLI:** if it isn't on your PATH (common for background services and GUI-launched editors), set `CCMETER_CLAUDE_BIN` to its path. Without it, ccmeter shows estimates only.
- **Optional direct endpoint (off by default):** `CCMETER_LIMITS_SOURCE=auto` makes ccmeter fall back to the usage endpoint behind `/usage` when the CLI fails, and `=endpoint` uses only that endpoint. Either way, ccmeter reads the account's saved login token (`.credentials.json`, or the macOS Keychain) and sends it to that endpoint itself. Anthropic's [usage policy](https://code.claude.com/docs/en/legal-and-compliance#authentication-and-credential-use) reserves Claude subscription logins for Claude Code and Anthropic's own apps, so read it and decide for yourself before turning this on.
- **Shared output:** results land where the table, dashboard and status line already look.

Caveats:
- **Text format:** the `claude -p /usage` text (`Current session: 4% used · resets Nov 2, 10:30am (Asia/Dhaka)`) and the optional endpoint aren't formal APIs. If either changes, ccmeter keeps showing estimates and the error appears on the account's card.
- **Read-only login use by the endpoint:** when turned on, the endpoint check never refreshes or writes your login. An expired token is skipped until Claude Code refreshes it. If the endpoint returns "too many requests", ccmeter waits 15 minutes.
- **macOS Keychain (endpoint only):** for config dirs other than `~/.claude`, the Keychain lookup is best effort.
- **Debugging:** problems from background fetches are logged to `<user cache dir>/ccmeter/hook.log`.

#### As a Claude Code plugin

The repo is also a plugin marketplace. The plugin adds the same hooks plus a `/ccmeter:usage` command that shows the usage table in chat. It needs the `ccmeter` binary installed (on your PATH, in `~/go/bin`, `/usr/local/bin`, `/opt/homebrew/bin` or `~/.local/bin`, or set `CCMETER_BIN`).

```
/plugin marketplace add arman-arif/ccmeter
/plugin install ccmeter@ccmeter
```

Newer Claude Code versions also accept `/plugin install ccmeter --marketplace arman-arif/ccmeter`. Use either the plugin or `ccmeter hook install`, not both. The plugin applies to every account where it's enabled; `hook install` is per account and uses the binary's absolute path.

### Real plan limits via the status line (terminal only)

The dollar amounts are estimates. Your real plan usage (the percentages `/usage` shows) can't be read from a script with the `claude` command. But Claude Code sends those percentages to its status-line command on every repaint, and ccmeter records them:

```bash
ccmeter statusline install          # every account; or: ccmeter statusline install work
ccmeter statusline uninstall
```

This sets `statusLine` in each account's `settings.json` to `ccmeter statusline`. Other settings are kept, a `.bak` copy is written, and an existing custom status line is only replaced with `--force`. After the next Claude Code response in that account:

- the status line shows `5h 42% (2h10m left) · week 31% (resets Wed 21:20)`
- `ccmeter` and the dashboard show both plan bars with their reset times, marked with how long ago Claude Code reported them

Notes:
- **Plans:** only Pro, Max, Team and Enterprise logins get these numbers. API-key accounts keep the estimates.
- **Freshness:** values update only while a Claude Code session runs in that account. When a reset time passes, ccmeter shows the window as reset instead of the old percentage.
- **Storage:** reports are saved per config dir in `<user cache dir>/ccmeter/limits/`.

### Status line

Add this to each account's `settings.json` (for example `~/.claude-work/settings.json`):

```json
{ "statusLine": { "type": "command", "command": "ccmeter statusline" } }
```

It prints something like `work · Opus · 5h: $3.18 (8%), 3h16m left · today $3.18 · week 3%`. The account is picked from `CLAUDE_CONFIG_DIR`, then from the session's transcript path.

## How it works

- Only assistant responses with a `usage` block are counted. A response logged on several lines (same message id and request id) is counted once.
- Files are read incrementally. The server remembers the byte offset of each file and only parses new lines on each rescan (every 10s by default). Partially written lines are held until they're complete.
- 5-hour windows follow Claude Code's session limit: a window starts at the hour of the first message and ends 5 hours later, or after a 5-hour gap.
- History kept in memory is 31 days.

## Limits of the approach

The numbers are estimates calculated from local logs. They are not the plan percentage that Claude Code's `/usage` shows, which comes from Anthropic's servers and weights models differently. Usage from claude.ai or other machines on the same account doesn't appear here either.

## Platform support

- **Linux:** used day to day: the dashboard, background mode, desktop-entry autostart, hooks and `claude -p /usage`.
- **macOS and Windows:** builds and unit tests cover them, but these parts haven't run on a real machine yet: LaunchAgent and Startup-folder autostart, the Keychain lookup (opt-in endpoint only), and the plugin's hook on Windows, which needs `sh` (for example from Git for Windows). Treat them as experimental, and please open an issue if something doesn't work.

## License

MIT. See [LICENSE](LICENSE).
