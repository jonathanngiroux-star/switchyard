# Switchyard

**Self-hosted feature-flag control plane. One Go binary, one SQLite file, one port.**

Switchyard replaces two things platform teams at Series A–B companies (50–500 people)
pay for today:

- **LaunchDarkly, at $12,000–$100,000/year** — priced for enterprise, with a
  migration off it that is a two-week consulting engagement.
- **Unleash self-hosted on Kubernetes** — which wants a cluster, Postgres, Redis,
  and your on-call rotation before it evaluates a single boolean.

The weekly job it kills: platform engineers recreating flag configurations
across dev/staging/production on every release, auditing rollout percentages
by hand, and coordinating SDK updates across 5+ services. Switchyard's answer
is a migration CLI that shows a machine-readable diff of exactly what an
import would change — and exactly what it *cannot* map — before you touch a
single service.

The wedge is not another flag UI. **The wedge is a dry-run importer you can
trust.**

---

## What it does

### 1. Flag control plane (single binary)

One static binary. No daemon tree, no message bus, no sidecars. SQLite is the
only state — a single file, WAL mode, foreign keys enforced. Postgres is a
documented upgrade path, not a requirement to self-host.

**Three control surfaces, one binary:**

- **TUI** — run bare `switchyard` (or `switchyard tui`) in a terminal:
  full-screen flag table, toggle/create/delete, rollout editing,
  environment switching. Pure Go, ships in every binary including Docker.
  First launch on an empty store runs a 4-step setup wizard (welcome →
  environment → first flag → donate); `?` reopens it from the main
  screen; `Esc` skips it. Scripted PTY audit: `scripts/tui-pty-audit.sh`.
- **Web UI** — `switchyard serve`, then open `/`: same operations in a
  browser, zero JavaScript build step.
- **Desktop GUI** — `switchyard desktop` opens a Wails v2 GUI in desktop
  builds (`-tags desktop,production,webkit2_41`, cgo). The default binary
  is cgo-free so it runs everywhere (Docker, scratch, CI); it explains
  how to get the GUI instead of crashing. All three surfaces share one
  logic layer — the binding layer's tests cover the desktop's behavior
  too. Full setup wizard: **[docs/gui-setup.md](docs/gui-setup.md)** —
  every dependency, tag, and verification step, tested on Linux.

**Install** — latest release binary, checksum-verified:

```bash
curl -fsSL https://raw.githubusercontent.com/jonathanngiroux-star/switchyard/main/scripts/install.sh | sh
```

**Or build from source** (Go 1.27+):

```bash
go build -o switchyard ./cmd/switchyard
./switchyard serve --addr :8080
```

**Or Docker** (build locally — the image is not on a public registry yet):

```bash
git clone https://github.com/jonathanngiroux-star/switchyard && cd switchyard
docker build -t switchyard:local .
docker run -d -p 8080:8080 -v switchyard-data:/data switchyard:local \
    serve --addr :8080 --db /data/switchyard.db
```

Flags are evaluated **locally in your services** — no network call per
evaluation, no cloud dependency, forever. Gating evaluation behind a hosted
tier is explicitly forbidden by this project's own governance.

Current HTTP surface (v0.1.0, store-backed — state survives restarts):

```bash
$ curl localhost:8080/healthz
{"status":"ok"}

$ curl -X POST localhost:8080/flags -H 'Content-Type: application/json' \
    -d '{"key":"welcome-banner","kind":"boolean"}'
{"key":"welcome-banner","name":"","kind":"boolean","environments":{}}

$ curl -X POST localhost:8080/flags/welcome-banner/toggle     # production by default
{"enabled":true,"env":"production","key":"welcome-banner"}

$ curl -X POST 'localhost:8080/evaluate/welcome-banner?env=production' \
    -d '{"userKey":"alice"}'
{"enabled":true,"value":true,"variant":"on","reason":"fallthrough"}

$ curl localhost:8080/audit                                   # append-only mutation log
{"entries":[{"id":1,"ts":"2026-09-30T16:24:52Z","actor":"anonymous","action":"create",
  "resource":"flag","key":"welcome-banner","after":"{\"key\":\"welcome-banner\",...}"},
 {"id":2,"ts":"...","actor":"anonymous","action":"toggle","key":"welcome-banner",
  "env":"production","before":"{\"on\":false}","after":"{\"on\":true}"}]}
```

`?format=csv` on `/audit` streams a quoted CSV download. Set
`SWITCHYARD_API_TOKEN` to require `Authorization: Bearer <token>` on the
API; set `SWITCHYARD_SCIM_TOKEN` to mount the SCIM 2.0 endpoint. Both stay
unauthenticated-but-localhost-only when unset — documented, not accidental.

And from the same binary, without the server running:

```bash
$ switchyard eval --env production --user alice welcome-banner --db switchyard.db
{"enabled":true,"value":true,"variant":"on","reason":"fallthrough"}
```

Deploy target: **90 seconds from `git clone` to toggling a flag** (hard cap
15 minutes). Measured (Linux x86_64, Go 1.27, 2026-09-29, `scripts/coldstart.sh`):

| Path | Time |
|---|---|
| Binary start → healthz (fresh dir, fresh SQLite) | **0.02–0.09s** |
| healthz → flag created | ~9ms |
| flag created → toggled | ~9ms |
| `docker run` → healthz (image built) | **0.49s** |

The 90-second budget is not tight — the binary path clears it by ~1000x.
The real budget goes to `go build` (~30s on a laptop) or the first
`docker build` (~1–2 min, cached after). Reproduce: `bash scripts/coldstart.sh`.

### 2. Flag model

- **Variants:** boolean, string, number, JSON
- **Targeting:** user key, context attributes, percentage rollout, off/on
- **Segments:** reusable audiences referenced by rules
- **Prerequisites:** a flag serves only when its parent flag is on
  (enforced, cycle-safe, depth-limited)
- **Environments:** dev / staging / production seeded by default; a flag's
  config (on/off, rollout %, rules) is per-environment
- **Protocol:** OpenFeature-compatible provider — you are not locked
  into a proprietary SDK wire format

### 3. Migration CLI (the product)

Import an incumbent's project, see the diff first, decide with evidence:

```bash
$ switchyard migrate --from=launchdarkly --dry-run \
    --input testdata/fixtures/launchdarkly/sample-export.json
default (Default Project): 2 flags, 1 segments, 3 envs
summary: 2 added, 0 removed, 0 changed, 0 unmapped
```

`--format=json` emits the machine-readable diff — `added` / `removed` /
`changed` / `unmapped` per flag, plus a `fidelity` object with the score
and gap breakdown. Two rules govern this output, and CI enforces them:

1. **Gaps are listed, never dropped.** If a source construct doesn't map
   (weighted multi-variant rollouts, unsupported operators, big segments),
   it appears in `unmapped` with a reason. Silent fidelity loss is the
   fastest way to burn a platform team.
2. **Fidelity is scored and gated.** Fully-mapped flags ÷ total source
   flags. The CI job fails if fidelity on the representative corpus drops
   below **90%**. The human-readable reports are committed and
   CI-checked for staleness: `docs/fidelity/launchdarkly.md`,
   `docs/fidelity/unleash.md`.

`--from=unleash` ships the same contract: representative corpus CI-gated,
edge corpus unmappable-by-design with every gap named. A corpus below the
fidelity gate refuses to import rather than pretending:

```
$ switchyard migrate --from=unleash --dry-run --input edge-corpus.json
migrate: fidelity 0.0% below gate 90.0% — refusing to pretend
```

### 4. SDKs — three, and only three

Go (hand-written, dogfooded), TypeScript and Python (generated). Generated
from a **versioned snapshot protocol**, with a documented breaking-change
policy. No mobile SDKs. No fourth language. A 20-SDK surface is how
solo-maintained flag projects die; the migration CLI outranks new SDKs,
always.

**Go SDK — shipped.** `github.com/switchyard/switchyard/sdk` is a thin,
snapshot-based client. Local evaluation, zero network, no store handle
required. It also exposes the OpenFeature provider, so OpenFeature apps
register Switchyard without touching internal packages:

```go
import (
    "github.com/switchyard/switchyard/sdk"
    of "github.com/open-feature/go-sdk/openfeature"
)

// Plain client:
c := sdk.New(flags, "production")
if c.Boolean("checkouts-v2", false, userID, sdk.Attr("plan", "pro")) { ... }

// Or via OpenFeature:
of.SetProviderAndWait(c.Provider())
client := of.NewClient("my-app")
client.Boolean(ctx, "checkouts-v2", false, of.NewEvaluationContext(userID, nil))
```

The provider satisfies the **real** `openfeature.FeatureProvider` interface —
enforced by a compile-time assertion and an end-to-end test through
`of.Client`, not a lookalike API. Type mismatches and missing flags serve
the caller's default with a proper `FLAG_NOT_FOUND` / `PARSE_ERROR`
resolution error, never a wrong value.

**TypeScript + Python generators — shipped.** One command snapshots the
store and emits a self-contained client — zero dependencies, zero network
calls, snapshot inlined:

```bash
switchyard sdk gen --lang typescript --db switchyard.db --out generated/
switchyard sdk gen --lang python      --db switchyard.db --out generated/
```

```ts
import { newClient } from "./generated/switchyard";
const c = newClient();
if (c.boolean("checkouts-v2", false, { userKey: userId })) { ... }
```

```python
from switchyard import new_client
c = new_client()
if c.boolean("checkouts-v2", False, {"userKey": user_id}): ...
```

**Cross-language conformance is tested, not claimed:** the conformance suite
generates both clients from one store and requires identical decisions to
the Go evaluator across every path — off/on, rules, negation, segments,
percentage rollouts (SHA-256 bucketing included — the pure-JS/Pure-Python
implementations must reproduce Go's `binary.BigEndian.Uint64 % 100`
exactly), typed values, and missing flags. 96 cases, three languages, one
answer. Runs in CI when `node`/`python3` are present on the runner.

---

## Status: what exists

Everything below is shipped in v0.1.0 and covered by tests or CI gates:

| Capability | Status |
|---|---|
| Single binary, SQLite (schema v5, auto-migration from every prior version, FK cascades, WAL) | **Done** |
| `serve` HTTP: CRUD, toggle, evaluate, audit — store-backed, persistent across restarts | **Done** |
| Evaluator: 12 operators, negation, segments, percentage rollouts, variant values, prerequisite enforcement (cycle-safe) | **Done** |
| TUI (bare `switchyard`), embedded web UI, Wails desktop GUI (`-tags desktop`) | **Done** |
| `migrate --from=launchdarkly --dry-run`: real export shapes, fidelity score, CI gate ≥90%, committed report | **Done** |
| `migrate --from=unleash --dry-run`: 7 strategies, 14 constraint operators, 10 named gap types, CI-gated | **Done** |
| SDKs: Go (+ real OpenFeature provider), generated TS + Python, 96-case cross-language conformance | **Done** |
| SCIM 2.0 skeleton (Users/Groups, bearer token, fail-closed unconfigured) | **Done** |
| API auth (`SWITCHYARD_API_TOKEN`), audit log on every mutation, `GET /audit` JSON/CSV | **Done** |
| Stable deploy IDs (`GET /deploy`) for the evidence loop | **Done** |
| Procurement docs: audit schema, DPA stub, replacement-cost sheet | **Done** |

**What is deliberately not built** (scope locks — see `AGENTS.md`):
experimentation/Bayesian stats, a fourth SDK, mobile SDKs, "Flag OS",
Kubernetes requirements, and any gating of evaluation behind a hosted tier.
The locks lift only after 10 unrelated teams are invoiced at list price.

**Known gaps, stated plainly:** weighted multi-variant rollouts map to the
dominant variant only (gap reported); LD big-segment membership is imported
as a gap; SDKs are snapshot-based — regenerate on deploy (no streaming
updates yet). The fidelity reports are the authoritative gap lists.

---

## Funding and roadmap

**There is no hosted tier, no billing, and no company today.** Switchyard
is a self-hosted, MIT-licensed project funded by crypto tips. The hosted
tiers below are the *planned* revenue path for when a hosted offering
exists — nothing here is purchasable yet, and we will not invoice anyone
before the service is real.

| Tier (planned) | What it would sell |
|---|---|
| Hosted Starter | we host it, back it up, upgrade it — you stop running it |
| Hosted Pro | + VPC, SSO/SAML, audit streaming, more seats |
| Enterprise | + air-gap, SLA, SCIM-in-production |

The pitch stays the same as it has always been: a hosted tier would sell
**operational risk transfer** — we page, back up, upgrade, and hold the SLA
— never flags ripped out of the self-hosted core.

### Support the project

If Switchyard saves you a LaunchDarkly invoice or a Kubernetes weekend,
tips are welcome. To be clear: tips are a tip jar, not the business model.
Tips are not counted as traction anywhere in this project's evidence.

| Network | Address |
|---|---|
| Ethereum — ETH and USDC (ERC-20) | `0x85ee7E71f762d772599cbF1EC20E651B30657521` |
| Bitcoin — native SegWit (bech32) | `bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg` |

Both addresses passed checksum validation (EIP-55 / bech32) at commit
time. Send only the listed assets on the listed networks, and treat any
address that reaches you outside this README as phishing — legitimate
addresses are only ever added via a signed commit to this file.

---

## Why the incumbents lose this segment

- **LaunchDarkly** prices mid-market out ($12k entry, SSO gated behind
  enterprise tiers) and leaving it is a consulting project.
- **Unleash** self-hosted requires Kubernetes + Postgres + Redis + an
  operator who reads release notes. That's a platform team's weekend, monthly.
- **OpenFeature** is a schema, not a control plane — it solves the SDK
  protocol, not the "recreate flags across environments" job.

## License & governance

- Core (server, evaluator, migrators, provider, 3 SDKs): **MIT** — `LICENSE`
- Commercial hosted extras: BSL 1.1 stub pending counsel — `LICENSE-CLOUD`
- **DCO-only contributions** (`git commit -s`). No CLA, ever.
- License changes require 4/4 council consensus + 30-day public notice + a
  written migration path — `GOVERNANCE.md`

## Development

```bash
go test ./...     # 168 tests across 10 test packages
go vet ./...
gofmt -l .
```

Desktop GUI (Wails v2; on Linux needs webkit2gtk-4.1 — see docs/gui-setup.md):

```bash
# system deps (Arch): sudo pacman -S --needed gtk3 webkit2gtk-4.1
CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 \
  -o switchyard-desktop ./cmd/switchyard
./switchyard-desktop desktop          # opens the window

# development mode (hot reload, from inside the package):
cd cmd/switchyard && wails dev -tags "desktop,webkit2_41" \
  -appargs "--db /tmp/dev.db" -m -s
```

The GUI covers every wedge action: flag list/toggle/percentage,
environment switch, segment editor, LaunchDarkly + Unleash dry-run
viewer (unmapped gaps and gate refusals rendered), eval console with
deterministic bucketing, audit log, and the donate view. Binding-layer
tests: `CGO_ENABLED=1 go test -tags desktop ./cmd/switchyard/ -count=1`.
GUI audit: `docs/audit/gui.md`.

CI runs all three on every push, plus the migration-fidelity gates
(LD + Unleash, ≥90% with committed reports checked for staleness), the
96-case cross-language SDK conformance suite, and the Wails desktop
build + GUI binding tests.

## Evidence, not vanity

What gets counted and how is documented in `docs/evidence.md` — deploy IDs
(not stars), migration fidelity (CI-gated, not claimed), and opt-out via
`SWITCHYARD_NO_TELEMETRY=1`. The evaluation path has zero network calls,
by design and by test. The 90-day evidence-pack clock and founder
checklist live in `docs/90-day-pack.md`.

---

*Named substitute costs, evidence gates, and the weekly job this kills are
spelled out in `IDEA.md` and `AGENTS.md`. If a PR expands scope past the
wedge, it gets refused with the trap named — that is the deal.*
