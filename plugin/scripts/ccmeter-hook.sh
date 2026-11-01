#!/bin/sh
# Runs `ccmeter hook` if ccmeter is installed; otherwise does nothing.
# The hook never blocks or fails a Claude Code turn.
for c in "$CCMETER_BIN" "$(command -v ccmeter 2>/dev/null)" \
         "$HOME/go/bin/ccmeter" "$HOME/.local/bin/ccmeter" "$HOME/bin/ccmeter" \
         /usr/local/bin/ccmeter /opt/homebrew/bin/ccmeter \
         "$HOME/go/bin/ccmeter.exe" "$(command -v ccmeter.exe 2>/dev/null)"; do
  if [ -n "$c" ] && [ -x "$c" ]; then
    exec "$c" hook
  fi
done
cat >/dev/null 2>&1
exit 0
