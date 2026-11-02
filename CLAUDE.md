# ccmeter: project context

ccmeter is a Claude Code usage meter for **several accounts at once**. Each account is a Claude Code config dir (the folder `CLAUDE_CONFIG_DIR` points at, e.g. `~/.claude`, `~/.claude-work`). It is a single Go binary with no third-party dependencies. It has a CLI table, a status-line command, a web dashboard built into the binary, a background service with start-on-login, and Claude Code hook/plugin integration for real plan limits.

- Repo: https://github.com/arman-arif/ccmeter (branch `main`)
- Owner: Ariful Islam. Commit as `Ariful Islam <hello@ariful.dev>`.
- The user mostly uses the **Claude Code VS Code extension**, not the terminal CLI. That drove most of the plan-limit design below.

## Working conventions (follow these)

- **One commit per logical step**, with descriptive messages (subject plus a short body). Push to `main` after each piece of work, unless asked otherwise.
- **No emoji anywhere in the web UI.** All icons are inline SVG `<symbol>`s in the sprite at the top of `web/index.html`, used via `icon('name')` in JS or `<svg class="icon"><use href="#i-name"/></svg>`.
- **Go standard library only** (`time/tzdata` is embedded for Windows). Keep it a single self-contained binary; the dashboard is `//go:embed web`.
- Before committing, run `gofmt -l .`, `go vet ./...`, `GOOS=darwin go vet ./...`, `GOOS=windows go vet ./...` and `go test ./...`.
- The plugin must pass `claude plugin validate .` and `claude plugin validate ./plugin`.
- Never print or log access tokens. ccmeter only ever **reads** Claude logins and never refreshes or writes them.
- Hooks must never slow down or fail a Claude Code turn: return immediately, print nothing, exit 0.
- Charts follow the dataviz guidance already applied: one series color (`--series-1`), thin bars with 4px rounded tops and 2px gaps, hover tooltips, light and dark themes via CSS vars, and a table view for data.

## Build, run, test

```bash
make                      # cross-compiled bundle for all platforms in build/bin/, with SHA256SUMS
make build                # build/bin/ccmeter for this machine
make check                # gofmt, vet x3, tests, plugin validation (run before committing)
make serve                # build, then dashboard on http://127.0.0.1:7878
make install              # copies build/bin/ccmeter to /usr/local/bin, never builds (sudo has no go on PATH); PREFIX=~/.local for no sudo
./build/bin/ccmeter       # table (refreshes stale plan limits first)
```
`build/`, `dist/` and a root `ccmeter` binary are gitignored. `gofmt` may not be on PATH; the Makefile uses `$(go env GOROOT)/bin/gofmt`.

## Commands (see `ccmeter help`)

| Command | What it does |
|---|---|
| `ccmeter [table] [-v] [-no-refresh]` | Per account: 5-hour window, today, 7 days, 30 days. `-v` adds a per-model breakdown. Refreshes stale plan limits first. |
| `ccmeter json` | Same data as JSON (`Report` in stats.go). |
| `ccmeter serve [-addr] [-interval] [-read-only] [-limits-interval]` | Dashboard plus API. It rescans logs every 10s, reloads the config file when it changes on disk, and checks plan limits on a schedule. |
| `ccmeter start / stop / restart / status` | Runs `serve` detached, with a pid file and log in the user cache dir. |
| `ccmeter autostart enable / disable / status` | Linux: systemd user service, falling back to an XDG `.desktop` entry. macOS: LaunchAgent `com.ccmeter.dashboard`. Windows: hidden `ccmeter.vbs` in the Startup folder. |
| `ccmeter statusline` | Status-line command. It also saves `rate_limits` from Claude Code's stdin JSON. |
| `ccmeter statusline install / uninstall [NAME...] [--force]` | Edits each account's `settings.json` `statusLine`, writing a `.bak`. A custom status line is only replaced with `--force`. |
| `ccmeter hook` | Hook entry point (SessionStart and Stop). It triggers a throttled, detached plan-limit check. |
| `ccmeter hook install / uninstall [NAME...]` | Adds or removes ccmeter's hooks in each account's `settings.json`, keeping other hooks and writing a `.bak`. |
| `ccmeter hook-fetch DIR` | Internal: the detached child that does the check. |
| `ccmeter limits [NAME...]` | Checks plan limits now and prints them. |
| `ccmeter init / accounts / add NAME DIR / remove NAME\|DIR` | Account management. `add` accepts `--block-limit`, `--weekly-limit` (USD) and `--force`. |

Global flags: `-config PATH` (default `$CCMETER_CONFIG`, else `<user config dir>/ccmeter/config.json`); `--color` / `--no-color` (default: color on a terminal, off when piped, with `NO_COLOR` or `TERM=dumb`).

## Architecture (file map)

| File | Responsibility |
|---|---|
| `main.go` | Argument parsing and dispatch, formatting helpers (`fmtTok`, `fmtUSD`, `fmtDur`, `blockCell`, used by the status line). `version` constant. |
| `ui.go` | Terminal output: color detection, `table` (aligns cells that contain color codes, which text/tabwriter can't), usage meters, `printTable`, `printLimits`, `printAccounts`. Tables stay within 80 columns. Color is decoration only: alerts are also in words, so piped output and `NO_COLOR` lose nothing. Near-limit thresholds match the dashboard (80%, 100%). |
| `term_unix.go` / `term_windows.go` | `enableTerminalColor` (turns on ANSI in the Windows console, plain text where unsupported). |
| `config.go` | `Config`/`AccountConfig` (name, dir, `block_limit_usd`, `weekly_limit_usd`, `pricing` overrides), config path, auto-detection, `init`. |
| `accounts.go` | Shared rules for adding, updating and removing accounts (`upsertAccount`, `removeAccount`, used by the CLI and the web API). `saveConfig` writes atomically. |
| `pricing.go` | Built-in API list prices by model family (Opus 4.5+ $5/$25, Opus 4/4.1 $15/$75, Sonnet $3/$15, Haiku 4.5 $1/$5, ...). Cache write is 1.25x input (5-minute) or 2x (1-hour); cache read is 0.1x. Unknown models are reported as "unpriced" (e.g. Fable/Mythos: prices unknown, so the user adds them under `pricing`). |
| `scan.go` | Incremental JSONL scanner over `<dir>/projects/**/*.jsonl`. It keeps a byte offset per file, resumes after partial lines, dedupes by `message.id:requestId`, skips `<synthetic>`, and keeps 31 days in memory. |
| `stats.go` | `summarize` builds the `Summary` for each account: the 5-hour block (starts at the hour of the first message, ends 5h later or after a 5h gap), burn rate, projection, % of the configured limit or of the 30-day peak block, today / rolling 7d / 30d, daily series, models, `Plan` (real limits) and `PlanError`. |
| `statusline.go` | `ccmeter statusline`: picks the account from `CLAUDE_CONFIG_DIR`, then the transcript path, then the first account. Saves `rate_limits` and prints one line. |
| `limits.go` | Plan limit storage: `LimitWindow`, `PlanLimits`, `saveLimitWindows` / `loadLimits` (one file per config dir, keyed by a hash of the dir). Marks a window `Reset` once its reset time passes. Status-line install/uninstall. |
| `claudecli.go` | **Primary limits source:** runs `claude -p /usage --output-format json` per account and parses the text. |
| `usageapi.go` | **Opt-in source:** the undocumented endpoint behind `/usage`. Also the read-only credential reader, the fetch state file (throttle and backoff), the per-account lock, `refreshLimits` and `fetchPlanLimits` (CLI only by default; `auto` adds the endpoint as a fallback). |
| `limitsched.go` | Scheduler inside `serve` (active accounts every `-limits-interval`, default 5m; idle accounts every 30m; one at a time). Manual refresh trigger. `refreshDueLimits` is used by table/json. |
| `hook.go` | `ccmeter hook` (finds the account from `CLAUDE_CONFIG_DIR` or `<dir>/projects/...` in the transcript path, then spawns a detached `hook-fetch` when one is due), `limits`, and hook install/uninstall with settings.json merging. |
| `server.go` | HTTP server. Hot-reloads the config while keeping scan state for unchanged dirs. API routes are listed below. Every request must name an IP, `localhost`, the machine's hostname or the `-addr` host in `Host` (`localOnly`, which blocks DNS rebinding). Write routes only accept loopback requests from the dashboard's own origin (`sameMachine`); `-read-only` disables account edits. |
| `daemon.go` | start/stop/status, pid file, `/api/health` probing (stop only signals a pid the dashboard confirms), `ensureConfigFile`, `serveOpts`. |
| `detach_unix.go` / `detach_windows.go` | Detaching the child process (setsid / DETACHED_PROCESS + CREATE_NO_WINDOW) and terminating it. |
| `autostart.go` | Per-OS autostart. Renderers for the systemd unit, desktop entry, launchd plist and VBS, with careful quoting (unit-tested). |
| `web/index.html` | The whole dashboard: vanilla JS and CSS, SVG icon sprite, light/dark theme toggle, totals tiles, account cards (plan bars or estimate block, period stats, 30-day bar chart, daily and model tables), Accounts dialog (collapsible add/edit form, inline delete confirmation). Polls `/api/summary` every 15s. |
| `plugin/` and `.claude-plugin/marketplace.json` | Claude Code plugin (see below). |
| `*_test.go` | Tests for autostart renderers, /usage parsing (real output fixture), the fake-CLI flow, hook routing, settings merge, endpoint fetch with throttle and expiry, the endpoint being opt-in, the dashboard's Host check, and the scheduler. |

### HTTP API (`server.go`)
- `GET /api/summary`: the `Report` (all accounts and totals).
- `GET /api/health`: `{pid, version, addr, config, started}`, used by start/stop/status.
- `GET /api/accounts`, `POST /api/accounts` (add, or edit via `original`), `DELETE /api/accounts?name=`.
- `POST /api/limits/refresh` (triggers a pass), `GET /api/limits/status`.

## Plan limits: how ccmeter gets the real 5-hour and weekly %

Dollar figures are estimates from local logs. The real plan percentages come from Claude Code. Facts established in this project:

1. **`claude -p /usage` (primary).** In print mode, Claude Code handles `/usage` locally (`"local_command":"usage"`, `num_turns: 0`, `total_cost_usd: 0`), so there is no model call and no usage cost. Real output captured from the user's machine (Nov 2026), in the JSON `result` field:
   ```
   You are currently using your subscription to power your Claude Code usage

   Current session: 4% used · resets Nov 2, 10:30am (Asia/Dhaka)
   Current week (all models): 8% used · resets Nov 2, 9am (Asia/Dhaka)
   ```
   - The parser (`parseUsageText`) maps "session" to the 5-hour window and "week (all models)" to weekly. It ignores other lines, such as Sonnet-only. Reset times are parsed with the named IANA zone; the year is inferred and a time-only value rolls to the next day.
   - The check runs with `--no-session-persistence --strict-mcp-config --settings {"disableAllHooks":true}`, and retries with plain flags if the CLI rejects those.
   - Working dir is the ccmeter state dir. The child gets `CCMETER_IN_FETCH=1` so ccmeter's own hook doesn't recurse.
   - For the default `~/.claude` account, **`CLAUDE_CONFIG_DIR` is left unset**. Setting it, even to the default path, can change the macOS Keychain item name.
   - Claude Code refreshes the login itself in this path.
2. **Usage endpoint (opt-in, off by default).** Only with `CCMETER_LIMITS_SOURCE=auto` (CLI, then endpoint) or `=endpoint`. It sends the account's saved subscription login token to Anthropic directly, and Anthropic's usage policy (code.claude.com/docs/en/legal-and-compliance, "Authentication and credential use") reserves those logins for Claude Code and Anthropic's own apps, so it must never be on by default. `GET https://api.anthropic.com/api/oauth/usage` with `Authorization: Bearer <token>` and `anthropic-beta: oauth-2025-04-20`. It returns `five_hour` / `seven_day` with `utilization` (0-100) and `resets_at`. This is undocumented.
   - Token sources: `CLAUDE_CODE_OAUTH_TOKEN`, the macOS Keychain (`Claude Code-credentials`, plus `-<first 8 hex of sha256(dir)>` for non-default dirs; this naming is a best guess), or `<dir>/.credentials.json` (`claudeAiOauth.accessToken`, `expiresAt` in ms).
   - Expired tokens are skipped. On HTTP 429 it backs off 15 minutes.
3. **Status line.** Claude Code pipes `rate_limits.five_hour/seven_day` (`used_percentage`, `resets_at` as epoch seconds or ISO) to the status-line command, for Pro/Max only and after the first response. **The VS Code extension does not run status-line commands**, which is why sources 1 and 2 exist. Some CLI versions have shipped without `rate_limits`.

**Hooks do fire in the VS Code extension** (recent versions) and receive `session_id`, `transcript_path`, `cwd` and `hook_event_name`, but **no `rate_limits`**. So the hook only triggers a check.

**Throttling:** state files live in `<cache>/ccmeter/limits/<hash>.state.json` (`last_attempt`, `next_allowed`, `last_error`, `last_ok`) with a `<hash>.lock` (stale after 60s). There is a minimum 5-minute gap per account for hook and table checks. The scheduler uses its own intervals but the same state and lock. `CCMETER_LIMITS_SOURCE` picks the source: unset or `cli` (default), `auto`, or `endpoint`.

**Triggers:** `serve` scheduler, the dashboard Refresh button, table/json (stale only), hooks (SessionStart and Stop) and `ccmeter limits`.

## Claude Code plugin

- `.claude-plugin/marketplace.json` (marketplace `ccmeter`) points to `./plugin`.
- `plugin/.claude-plugin/plugin.json` is version 0.2.0, with `icon.png` (512px gauge).
- `plugin/hooks/hooks.json` runs `sh "${CLAUDE_PLUGIN_ROOT}/scripts/ccmeter-hook.sh"` on SessionStart and Stop. The script finds the binary (`$CCMETER_BIN`, PATH, `~/go/bin`, `~/.local/bin`, `/usr/local/bin`, `/opt/homebrew/bin`) or exits 0 silently.
- `plugin/commands/usage.md` provides `/ccmeter:usage`, which runs `!ccmeter` with `allowed-tools: Bash(ccmeter:*)`.
- Install: `/plugin marketplace add arman-arif/ccmeter`, then `/plugin install ccmeter@ccmeter`. Verified installable with Claude Code 2.1.316. Use either the plugin or `ccmeter hook install`, not both.

## Files on the user's machine

- Config: `<user config dir>/ccmeter/config.json` (Linux: `~/.config/ccmeter/config.json`; macOS: `~/Library/Application Support/ccmeter/config.json`).
- State (user cache dir `/ccmeter`): `ccmeter.pid`, `ccmeter.log` (background service), `hook.log` (detached fetch errors), `limits/*.json` (plan limits, per config dir), `limits/*.state.json`, `limits/*.lock`.
- Per account: `settings.json` (status line and hooks) with a `settings.json.bak` written on every edit.

## Environment variables

| Variable | Purpose |
|---|---|
| `CCMETER_CONFIG` | Config file path |
| `NO_COLOR` | Turns off terminal color (no-color.org) |
| `CLAUDE_CONFIG_DIR` | Account auto-detection (comma separated) and account selection in statusline/hook |
| `CCMETER_CLAUDE_BIN` | Path to the `claude` CLI if it isn't on PATH (common for background services and GUI-launched VS Code) |
| `CCMETER_BIN` | Path to `ccmeter` for the plugin's hook script |
| `CCMETER_LIMITS_SOURCE` | Plan-limit source: `cli` (default), `auto` (CLI, then the opt-in endpoint) or `endpoint` |
| `CCMETER_USAGE_URL` | Override the usage endpoint (tests only) |
| `CCMETER_IN_FETCH` | Set on ccmeter's own `claude` child so its hooks are ignored |

## Testing tricks used in this project

- **Fake data:** generate JSONL under a temp `HOME` (e.g. `/tmp/fakehome/.claude/projects/x/s.jsonl`) with assistant lines carrying `message.usage`, and set `HOME`, `XDG_CACHE_HOME` and `XDG_CONFIG_HOME` to the temp dir.
- **Fake `claude`:** a shell script printing `{"is_error":false,"num_turns":0,"local_command":"usage","result":"Current session: ..."}`, pointed to by `CCMETER_CLAUDE_BIN`.
- **Fake endpoint:** a local HTTP server, plus `CCMETER_USAGE_URL`.
- **UI checks:** Playwright screenshots of `ccmeter serve` in light and dark mode. Check for page errors, and grep `web/` for emoji code points.
- **Safety:** don't `pkill -f` with a pattern that matches your own shell command. Use `pgrep -x ccmeter` or the pid file.

## Session history (what was built, in order)

1. Core: config and auto-detect, pricing, incremental scanner, stats and 5-hour blocks, status line, embedded dashboard, CLI, README. Go was chosen over Node for the single binary, low memory use and fast status-line startup.
2. `add` / `remove` commands, so config dirs can live anywhere.
3. Dashboard account management (Accounts dialog, API, live config reload, same-machine-only edits, `-read-only`).
4. Background mode (`start` / `stop` / `restart` / `status`) and autostart on all three OSes.
5. Real plan limits via the status line, plus `statusline install`; dashboard plan bars; collapsible add-account form.
6. VS Code reality check: the extension doesn't run status lines, but hooks fire. Added the usage-endpoint fetch, `ccmeter hook`, `hook install`, `ccmeter limits`, and the Claude Code plugin and marketplace.
7. The user found `claude -p /usage` works in print mode. Made it the primary source, with real output as a test fixture.
8. ccmeter now refreshes limits by itself: the scheduler in `serve`, the Refresh button, and table/json refreshing stale data.

## Open items and ideas

- `version` in main.go and the plugin are both `0.2.0`; bump them together. Consider GitHub Releases with cross-compiled binaries (`make` output in `build/bin/`).
- **Not yet verified on real machines:** macOS Keychain naming for non-default dirs in the opt-in endpoint, the Windows plugin hook (runs via `sh`), systemd/launchd autostart (only the renderers are unit-tested; the Linux desktop-entry path was run for real), and real `claude -p /usage` from the background service's PATH.
- **Idea:** a small VS Code extension showing `5h % · wk %` per account in the VS Code status bar, reading `/api/summary`.
- **Idea:** surface the "Current week (Sonnet only)" line, which is currently ignored.
- **Idea:** a dashboard button to run `hook install` / `statusline install` per account.
