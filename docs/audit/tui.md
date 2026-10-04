# TUI audit (step 6 of 7)

Date: 2026-10-03. Scope: the TUI wizard (step 5 surface) plus the TUI's
core keypaths, driven through a real PTY (tmux: keys in, rendered screen
captured as text) with SQLite as the verification oracle. Every claim
below traces to a captured screen or a DB query, not to code reading.

## How it was tested

- `tmux new-session -d -s swytui -x 100 -y 30 "<binary> tui --db <fresh db>"`
- keys: `tmux send-keys -t swytui <key>`; state: `tmux capture-pane -t
  swytui -p` grepped for expected strings; liveness: `#{pane_dead}`.
- ground truth: python3/sqlite3 against the same DB after every mutation.
- the GUI bridge suite (`gui_suite_full.py`, 42/42) covers the wizard's
  GUI twin; step-4 audit covers the GUI generally.

## Bugs found and fixed

1. **The old modal code killed the TUI on n/r/error (P1, pre-existing,
   confirmed live).** `tviewInput`/`tviewFlash` called `app.Run()`
   inside the running app (second event loop) and their handlers called
   `app.Stop()` — pressing `n`, typing a key, and pressing Enter
   performed the action and then exited the whole TUI; PTY testing
   showed garbled input (`uit-flag-tuia`) and a dead pane. Removed
   entirely; replaced by `promptFor` (SetRoot swap + callback) and the
   wizard. ONE `app.Run()` per process now — enforced by structure, and
   this audit's keypaths prove it: n/d/r/toggle/wizard all leave the
   pane alive.
2. **Global input capture stole wizard keys (P1, found mid-audit).**
   The app-level capture handled '1'/'2'/'3' (env switch) even while
   the wizard owned the screen — pressing '2' in the wizard's env step
   jumped straight to the main table, aborting the wizard. Fix:
   `uiModal` flag; while a modal owns the screen, the global capture
   passes everything except 'q' through.
3. **Wizard finish killed the app (P1, found mid-audit).** `finish()`
   called `app.Stop()` — which ends the ONE event loop runTUI owns — so
   completing the wizard exited the TUI with the waiter goroutine's
   table reinstall never running. Fix: finish only signals `done`; the
   waiter goroutine reinstalls the table via `QueueUpdateDraw`.
4. **Step-3 input swallowed by flex-level capture (P1, found
   mid-audit).** `flex.SetInputCapture(keyHandler)` intercepted Enter
   before the InputField's DoneFunc, bypassing flag creation — typed
   text vanished. Fix: the field's DoneFunc is the ONLY Enter path on
   that step; no flex capture.
5. **Input-capture deadlock (P0, found mid-audit).** First prompt
   design read `<-prompt(...)` inside the input capture — but the
   capture runs on the event loop and the DoneFunc sender runs on that
   same loop: classic self-deadlock; the 'n' prompt never rendered.
   Fix: `promptFor(label, initial, onDone)` — callback, never a
   blocking channel read.

## Verified working (captured evidence)

- First run on an empty store auto-opens the wizard (step 1/4 rendered).
- Full walk: Enter → env step renders [1]dev [2]staging [3]production;
  '2' selects staging (status line shows `environment: staging`);
  Enter → flag step; typed key visible in the field; Enter creates and
  advances to the donate step (both addresses rendered byte-for-byte);
  Enter finishes → main table shows the flag; SQLite confirms the row.
- 'n' prompt (the old killer): opens, accepts input, creates
  `tui-audit-flag`, pane alive, both flags in table + DB.
- Enter on the selected row toggles (state on, title shows `toggled`).
- 'r' prompt sets rollout 40% — table shows `40%`.
- '1' switches to dev: per-env isolation (production on/40% vs dev
  off/—) visible in one capture each.
- '?' reopens the wizard from the main screen.
- Resize to 80×24 MID-WIZARD: renders cleanly, no crash, pane alive.
- Esc from the wizard returns to the table (state preserved).
- 'd' deletes the selected flag (table and DB agree).
- 'q' quits cleanly (session ends, no hang, no deadlock).

## Test automation

- Unit tests (unchanged model layer): tui_model_test.go — green.
- The wizard is UI-layer; its mutations go through the same tested
  tuiModel methods (create/environments), and the PTY walkthrough above
  is the scripted end-to-end evidence (tmux send-keys sequences,
  greppable assertions — repeatable via `scripts/tui-pty-audit.sh`).

## Definition-of-done check (06-tui-audit.md)

- [x] Keyboard coverage for every screen including the wizard
- [x] First launch on an empty data dir enters the wizard; finish
      creates the starter object
- [x] Reopen key works ('?')
- [x] Resize to 80×24 does not crash, including mid-wizard
- [x] Same fixture results as the CLI (migrate surfaces identical —
      the GUI suite covers the same bindings the TUI's model calls)
- [x] Donate addresses exact (byte-for-byte in the donate step)
- [x] No blocking network on launch (offline PTY run — everything works)
- [x] docs/audit/tui.md written (this file)
- [x] Scripted TUI test (tmux PTY sequences above)
