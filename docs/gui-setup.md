# Switchyard Desktop GUI — Setup Wizard (Wails v2)

Every step below was executed and verified on Linux (CachyOS, Go 1.27.1,
webkit2gtk-4.1 2.52.6) on 2026-10-03. Copy-paste in order; do not skip a
field. Total time from clean checkout on an Arch-family distro: ~5
minutes (plus one `go install` of the Wails CLI, ~2 minutes).

## 0. What you need first

| Field | Value | Check command |
|---|---|---|
| Go | 1.27+ | `go version` |
| GCC (cgo) | any recent | `gcc --version` |
| pkg-config | any | `pkg-config --version` |
| A display | X11 or Wayland session | `echo $DISPLAY$WAYLAND_DISPLAY` |
| Repo | this checkout | `git status` works |

## 1. Install system dependencies (root/sudo)

The GUI is a Wails v2 desktop app; on Linux it renders through
WebKitGTK. Distro commands:

```bash
# Arch / CachyOS / Manjaro
sudo pacman -S --needed gtk3 webkit2gtk-4.1 pkgconf

# Debian / Ubuntu (24.04+ ships webkit2gtk-4.1)
sudo apt-get update
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config

# Fedora
sudo dnf install -y gtk3-devel webkit2gtk4.1-devel pkgconf-pkg-config
```

Verify the exact pkg-config names the build needs:

```bash
pkg-config --modversion gtk+-3.0        # must print a version
pkg-config --modversion webkit2gtk-4.1  # must print a version
```

If your distro only has `webkit2gtk-4.0` (older Debian/Ubuntu), use
`webkit2_40` instead of `webkit2_41` in every `-tags` below, and drop
`webkit2gtk-4.1` from the install line in favor of `libwebkit2gtk-4.0-dev`.

## 2. Install the Wails CLI (one-time)

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
```

Verify:

```bash
wails version   # prints: v2.16.0
```

(If `wails: command not found`, add `$(go env GOPATH)/bin` to PATH:
`export PATH="$PATH:$(go env GOPATH)/bin"`.)

## 3. Build the desktop binary

From the repo root:

```bash
CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 \
  -o switchyard-desktop ./cmd/switchyard
```

Field-by-field, because each one is load-bearing:

| Field | Why |
|---|---|
| `CGO_ENABLED=1` | Wails links WebKitGTK through cgo; the default Switchyard binary is deliberately cgo-free |
| `-tags desktop` | compiles the GUI entry (`cmd/switchyard/desktop_*.go`) instead of the cgo-free stub |
| `-tags production` | Wails' own runtime tag — without it `wails.Run` returns "Wails applications will not build without the correct build tags" and exits |
| `-tags webkit2_41` | selects the webkit2gtk-4.1 pkg-config name (Debian 24+/Arch). Use `webkit2_40` on webkit2gtk-4.0 distros |
| `-o switchyard-desktop` | the desktop binary name (same as the releases page) |

## 4. Run it

```bash
./switchyard-desktop desktop
```

- Default database: `./switchyard.db` in the current directory (created
  and schema-migrated on first launch).
- Custom database:

```bash
./switchyard-desktop desktop --db /path/to/flags.db
```

What a healthy launch looks like (all verified 2026-10-03):

1. The window opens within ~2 seconds.
2. stderr prints one WebKit line — this is normal, not an error:
   `Overriding existing handler for signal 10. Set JSC_SIGNAL_FOR_GC ...`
3. The flag table shows `no flags — create one above` on a fresh DB.
4. Create one: type `welcome-banner` in the new-flag box, press Enter.
   It appears in the table, `off`, kind `boolean`.
5. Click `toggle` — state flips to `on`, the audit view records
   `actor=gui action=toggle`.
6. Switch the env dropdown to `dev`, toggle there, switch back to
   `production` — per-env state must differ (that is the product).
7. Migrate view → source `launchdarkly`, path
   `testdata/fixtures/launchdarkly/full-export.json` → `dry-run` →
   summary `8 added`, fidelity `100.0%`, `no unmapped constructs`.
   Then edge corpus (`edge-cases.json`): every gap type is listed and
   fidelity is refused below the 90% gate — by design.
8. Eval console: flag `welcome-banner`, user `alice`, `evaluate` →
   `variant: "on"`, and the same `bucket` number on every re-run
   (deterministic SHA-256 bucketing).
9. Donate view: the two addresses match README byte-for-byte; `copy`
   buttons write them to the clipboard.

If step 1 fails with `desktop: Wails applications will not build
without the correct build tags`, the binary was built without
`production` — rebuild with all three tags.

## 5. Development mode (hot reload)

`wails dev` requires the app's Go package at the project root. This
repo keeps the binary package at `cmd/switchyard/` (the CLI lives
there), so run dev mode from inside it:

```bash
cd cmd/switchyard
wails dev -tags "desktop,webkit2_41" -appargs "--db /tmp/dev.db" -m -s
```

| Field | Why |
|---|---|
| `-tags "desktop,webkit2_41"` | same GUI tags minus `production` (dev mode sets dev tags itself) |
| `-appargs "--db /tmp/dev.db"` | args passed to the app — a scratch DB so dev never touches real flags |
| `-m` | skip `go mod tidy` (the root module is already tidied) |
| `-s` | skip the frontend build step — the frontend is one static HTML file, nothing to build |

The dev window opens with live reload on Go/asset change; the dev
server also mirrors the UI at the printed `http://localhost:34115`
(bindings work in a normal browser tab too).

## 6. Tests

GUI binding layer (needs CGO but no display):

```bash
CGO_ENABLED=1 go test -tags desktop ./cmd/switchyard/ -count=1
```

Covers: toggle→eval variant change, deterministic rollout bucket,
migrate dry-run renders unmapped + gate refusal, donate addresses
byte-for-byte, every binding error path surfaces, segment editor round
trip, per-env state isolation, audit append order.

Frontend syntax gate (no browser needed):

```bash
node --check <(python3 -c "import re;print(re.search(r'<script>(.*)</script>', open('cmd/switchyard/frontend/index.html').read(), re.S).group(1))")
```

## 7. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `Package webkit2gtk-4.0 was not found` | missing `webkit2_41` tag on a 4.1-only distro | add `webkit2_41` to `-tags` |
| `Package webkit2gtk-4.1 was not found` | 4.0-only distro | install `libwebkit2gtk-4.0-dev` and use `webkit2_40` tag |
| "will not build without the correct build tags" | built without `production` | add `production` to `-tags` |
| `wails: command not found` | GOPATH/bin not on PATH | `export PATH="$PATH:$(go env GOPATH)/bin"` |
| Window blank/white a few seconds | WebKit first-launch shader cache | wait; subsequent launches are ~1s |
| `no Go files` from `wails dev` at repo root | CLI expects main at root | run from `cmd/switchyard/` (step 5) |
| `sqlite3: not found` when inspecting the DB | sqlite3 CLI absent | any SQLite browser works; the binary needs none |
