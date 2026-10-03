# GUI audit (step 4 of 7) — Wails desktop

Date: 2026-10-03. Scope: the Wails v2 GUI added in step 3, its binding
layer, embedded frontend, and the wizard doc. Full battery re-run after
every fix: `gofmt -l .`, `go vet ./...`, `go vet -tags desktop`,
`go test ./... -count=1`, `go test -tags desktop ./... -count=1`,
`go build ./...`, `CGO_ENABLED=1 go build -tags
desktop,production,webkit2_41`. All green at the end.

## Bugs found and fixed

1. **Wails runtime stub mode (P0).** The first desktop build compiled
   but exited immediately at launch with "Wails applications will not
   build without the correct build tags": Wails' runtime gates on its
   own `production`/`dev`/`bindings` tags, and none were set. `go
   build` succeeding was not evidence the window path worked — caught
   only by launching the real binary against a display. Fix: build
   with `-tags desktop,production,webkit2_41`. Documented in
   docs/gui-setup.md with the field-by-field reason for each tag.
2. **webkit2gtk ABI mismatch (P0, environment).** `production` pulls
   in the webview, which defaults to `webkit2gtk-4.0`; modern distros
   ship 4.1 only, so the build failed at pkg-config. Fix: Wails'
   `webkit2_41` build tag selects the 4.1 pkg-config name. Verified:
   window opens, WebKitWebProcess children spawn, store opens.
3. **Syntax error in the default binary (P0).** A doc-string edit put
   unescaped quotes inside an `Fprintln` in desktop_default.go:28 —
   the whole package failed to compile. Caught by `wails dev`'s build;
   `go build ./...` then confirmed. Fix: single-quoted string with no
   inner quotes.
4. **Frontend race on startup (P1).** `boot()` called bindings at
   DOMContentLoaded; the Wails runtime injects `window.go` after the
   page loads on slow starts, so every view could render
   "window.go is undefined". Fix: `boot()` polls for
   `window.go.main.App` (50 ms) before calling anything.
5. **Broken segment-edit handler (P1).** The segment list's inline
   `onclick='editSegment(${JSON.stringify(JSON.stringify(s))...})'
   template produced invalid JS — the whole `<script>` block failed
   `node --check` (missing `)` after argument list), meaning the
   entire frontend was dead, not just the segments view. Fix: store
   the JSON in a `data-seg` attribute and read
   `this.dataset.seg`. `node --check` now passes.
6. **Binding-layer test bugs (test-only, found by the suite).**
   Segment fixture used `operator:"eq"`, which the evaluator does not
   support (unknown operators fail closed by design); changed to
   `in`, which is the real semantics. Also removed leftover stub
   references (`Rule`/`Clause` live in `internal/model`, not the
   main package).

## Noted, not fixed here (out of step-4 scope)

- **TUI modal bug (P1, step-6 scope).** `tviewInput`/`tviewFlash` in
  tui.go call `app.Run()` inside an already-running tview app — a
  second event loop — and the modal's OK handler calls `app.Stop()`,
  killing the whole TUI. Effect: pressing `n` (new flag) or `r`
  (rollout) performs the action and then exits the TUI. The step-6
  TUI audit owns the fix; recorded here so it is not lost.
- The audit() helper swallows append failures after the mutation
  commits (best-effort, mirrors the HTTP path's semantics). If audit
  guarantees ever need to be hard, that is a store-level change, not
  a GUI one.

## Verified working (evidence, not claims)

- Launch on real display: binary alive 8s and 38s checks, store file
  created, all 9 schema tables present, WebKitWebProcess spawned.
- Representative corpora: LD `full-export.json` and Unleash
  `unleash-representative.json` dry-run at 100% fidelity, no
  refusal. Edge corpora: every gap type listed; below-gate refusal
  rendered as a result, not an error.
- Donate addresses: byte-for-byte equal to README (test-pinned).
- Rollout bucketing: 25 consecutive evaluations of a fixed
  (flag, user) pair return the identical bucket and enabled state.
- Error surfaces: all 10 binding failure paths return errors the
  frontend renders (test-pinned).
- Wizard: docs/gui-setup.md is command-verified — every field in it
  was executed on this machine before being written down.

## Definition-of-done check (04-gui-audit.md)

- [x] GUI test suite green (`go test -tags desktop`) + frontend
      `node --check` gate
- [x] Bugs fixed, not ticketed
- [x] docs/audit/gui.md written (this file)
- [x] No TUI work (TUI bug recorded for step 6)
- [x] No new product surface
- [x] Still Wails — bound Go methods, embedded frontend, no side
      HTTP admin
