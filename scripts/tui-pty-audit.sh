#!/bin/bash
# scripts/tui-pty-audit.sh — scripted TUI audit through a real PTY (tmux).
# Keys in, rendered screen captured as text, SQLite as the oracle.
# Usage: bash scripts/tui-pty-audit.sh   (from the repo root; needs tmux)
set -euo pipefail

BIN="${1:-./switchyard}"
DB="$(mktemp -d)/tui-pty.db"
SESSION="swy-audit-$$"
PASS=0; FAIL=0
say()  { printf '%s\n' "$*"; }
ok()   { say "PASS $1"; PASS=$((PASS+1)); }
bad()  { say "FAIL $1"; FAIL=$((FAIL+1)); }
screen()  { tmux capture-pane -t "$SESSION" -p; }
alive()   { [ "$(tmux list-panes -t "$SESSION" -F '#{pane_dead}' 2>/dev/null)" = "0" ]; }
expect()  { if screen | grep -q "$2"; then ok "$1"; else bad "$1 — screen lacks: $2"; fi; }

command -v tmux >/dev/null || { say "tmux required"; exit 2; }

tmux new-session -d -s "$SESSION" -x 100 -y 30 "$BIN tui --db $DB"
sleep 2
trap 'tmux kill-session -t "$SESSION" 2>/dev/null || true' EXIT

# 1. first-run wizard opens on the empty store
expect "wizard opens on first run" "switchyard setup — step 1/4"

# 2. walk: env step, pick staging
tmux send-keys -t "$SESSION" Enter; sleep 0.5
expect "env step lists environments" "\[2\] staging"
tmux send-keys -t "$SESSION" 2; sleep 0.5
expect "env selection reflected" "environment: staging"

# 3. flag step: create one
tmux send-keys -t "$SESSION" Enter; sleep 0.5
tmux send-keys -t "$SESSION" "pty-audit-flag"; sleep 0.5
tmux send-keys -t "$SESSION" Enter; sleep 0.8
expect "donate step renders ETH address" "0x85ee7E71f762d772599cbF1EC20E651B30657521"
expect "donate step renders BTC address" "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"

# 4. finish → main table, flag created, pane alive
tmux send-keys -t "$SESSION" Enter; sleep 1
alive && ok "app alive after wizard" || bad "app died after wizard"
expect "table shows created flag" "pty-audit-flag"
[ "$(python3 -c "import sqlite3;print(sqlite3.connect('$DB').execute('SELECT COUNT(*) FROM flags WHERE key=?',('pty-audit-flag',)).fetchone()[0])")" = "1" ] \
  && ok "flag persisted to SQLite" || bad "flag not in SQLite"

# 5. the old killer path: 'n' prompt creates without dying
tmux send-keys -t "$SESSION" n; sleep 0.5
expect "n-prompt opens" "new flag key:"
tmux send-keys -t "$SESSION" "pty-second-flag"; sleep 0.4
tmux send-keys -t "$SESSION" Enter; sleep 0.8
alive && ok "app alive after n-prompt" || bad "app died on n-prompt"
expect "second flag in table" "pty-second-flag"

# 6. resize mid-wizard: reopen with '?', resize, must survive
tmux send-keys -t "$SESSION" ?; sleep 0.5
tmux resize-window -t "$SESSION" -x 80 -y 24; sleep 0.5
alive && ok "resize 80x24 mid-wizard survives" || bad "resize killed the app"
tmux send-keys -t "$SESSION" Escape; sleep 0.5
alive && ok "Esc returns to table" || bad "Esc killed the app"

say ""
say "TUI PTY AUDIT: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
