# ccmeter

A Claude Code usage meter for **several accounts at once**. ccmeter treats each config dir (the folder `CLAUDE_CONFIG_DIR` points at) as a separate account.

It's a single Go binary with no dependencies. It reads the session logs in each account's `projects/**/*.jsonl` and estimates usage at API list prices. The web dashboard is built into the binary.

## Install

```bash
go build -o ccmeter .          # or use a prebuilt binary from dist/
sudo mv ccmeter /usr/local/bin/
```

Cross-compile: `GOOS=darwin GOARCH=arm64 go build -o ccmeter-macos .`

## Set up accounts

```bash
ccmeter init        # writes ~/.config/ccmeter/config.json with every config dir it finds
ccmeter accounts    # shows what it will read
```

Without a config file, ccmeter looks in `$CLAUDE_CONFIG_DIR` (comma separated), `~/.claude`, `~/.config/claude` and `~/.claude-*`.

```json
{
  "accounts": [
    { "name": "personal", "dir": "~/.claude" },
    { "name": "work",     "dir": "~/.claude-work", "block_limit_usd": 40, "weekly_limit_usd": 600 }
  ],
  "pricing": {
    "claude-fable": { "input": 10, "output": 50 }
  }
}
```

The fable price above is a placeholder. Put in the real price for any model the meter reports as unpriced.

- `block_limit_usd` / `weekly_limit_usd` (optional): your limits, measured in equivalent API cost. Find them by watching where Claude Code stops you. If you don't set `block_limit_usd`, the 5-hour % is measured against your own busiest window in the last 30 days.
- `pricing` (optional): USD per million tokens, matched against model names by substring (the longest match wins). Cache prices default to 1.25x input for 5-minute writes, 2x for 1-hour writes and 0.1x for reads. Built-in prices cover the Opus, Sonnet and Haiku families.

To run Claude Code against a second account: `CLAUDE_CONFIG_DIR=~/.claude-work claude` (log in once there).

## Use

```bash
ccmeter              # table: 5-hour window, today, 7 days, 30 days per account
ccmeter -v           # plus a per-model breakdown
ccmeter json         # raw data
ccmeter serve        # dashboard on http://127.0.0.1:7878 (-addr, -interval)
```

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
