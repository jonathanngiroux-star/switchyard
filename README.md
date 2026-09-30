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
documented upgrade path, not a requirement to self-host. The binary also
serves an embedded UI at `/` — list flags, toggle, switch environments,
see rollout percentages. No build step, no CDN.

```bash
go build -o switchyard ./cmd/switchyard
./switchyard serve --addr :8080
```

Or Docker:

```bash
docker build -t switchyard .
docker run -p 8080:8080 switchyard
```

Flags are evaluated **locally in your services** — no network call per
evaluation, no cloud dependency, forever. Gating evaluation behind a hosted
tier is explicitly forbidden by this project's own governance.

Current HTTP surface (v0.1, store-backed — state survives restarts):

```bash
$ curl localhost:8080/healthz
{"status":"ok"}

$ curl -X POST localhost:8080/flags -H 'Content-Type: application/json' \
    -d '{"key":"welcome-banner","kind":"boolean"}'
{"key":"welcome-banner","name":"","kind":"boolean","environments":{}}

$ curl localhost:8080/flags
{"flags":[{"key":"welcome-banner","name":"","kind":"boolean","environments":{}}]}

$ curl -X POST localhost:8080/flags/welcome-banner/toggle     # production by default
{"key":"welcome-banner","env":"production","enabled":true}

$ curl -X POST 'localhost:8080/evaluate/welcome-banner?env=production' \
    -d '{"userKey":"alice"}'
{"enabled":true,"reason":"fallthrough"}
```

And from the same binary, without the server running:

```bash
$ switchyard eval --env production --user alice welcome-banner --db switchyard.db
{"enabled":true,"reason":"fallthrough"}
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
- **Environments:** dev / staging / production seeded by default; a flag's
  config (on/off, rollout %, rules) is per-environment
- **Protocol:** OpenFeature-compatible provider (W3–4) — you are not locked
  into a proprietary SDK wire format

### 3. Migration CLI (the product)

Import an incumbent's project, see the diff first, decide with evidence:

```bash
$ ./switchyard migrate --from=launchdarkly --dry-run --format=json \
    --input testdata/fixtures/launchdarkly/sample-export.json
```

Real output against the fixture corpus in `testdata/fixtures/`:

```json
{
  "summary": { "added": 2, "removed": 0, "changed": 0, "unmapped": 2 },
  "added": [
    { "key": "checkouts-v2", "name": "New checkout flow", "kind": "boolean",
      "environments": { "production": { "on": false,
        "rollout": { "kind": "percentage", "percentage": 10 }, "rules": [] } } },
    { "key": "api-rate-limit", "kind": "boolean" }
  ],
  "removed": [],
  "changed": [],
  "unmapped": [
    { "flag": "api-rate-limit", "type": "prerequisites",
      "detail": "1 prerequisites not mapped in v0.1" },
    { "flag": "api-rate-limit", "rule": "rule-before-date", "type": "clause-operator",
      "detail": "operator \"before\" not supported in v0.1" }
  ]
}
```

Two rules govern this output, and CI enforces them:

1. **Gaps are listed, never dropped.** If a source construct doesn't map
   (prerequisites, exotic clause operators, Unleash strategy types), it
   appears in `unmapped` with a reason. Silent fidelity loss is the fastest
   way to burn a platform team.
2. **Fidelity is scored and gated.** Mapped flags ÷ total source flags.
   The CI job fails if fidelity on the fixture corpus drops below **90%**
   (target 95%) on rules and segments. The human-readable report lives in
   `docs/fidelity/launchdarkly.md`.

`--from=unleash` is shipped with the same contract: representative corpus
CI-gated, edge corpus unmappable-by-design with every gap named. A corpus
below the fidelity gate refuses to import rather than pretending:

```
$ ./switchyard migrate --from=unleash --dry-run --input edge-corpus.json
migrate: fidelity 0.0% below gate 90.0% — refusing to pretend
```

### 4. SDKs — three, and only three

Go (hand-written, dogfooded), TypeScript (W9, generated), Python (W9, generated).
Generated, not hand-maintained, from a **versioned snapshot protocol**, with
a documented breaking-change policy. No mobile SDKs. No fourth language. A
20-SDK surface is how solo-maintained flag projects die; the migration CLI
outranks new SDKs, always.

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

## Status: what exists vs. what's scheduled

Honest inventory — this is a young repo. Do not deploy it past a dev box yet.

| Capability | Status |
|---|---|
| Single binary, SQLite store (schema v1, FK cascades, WAL) | **Done** |
| `migrate --from=launchdarkly --dry-run` JSON diff + gap list | **Done** (fixture corpus in-repo) |
| `serve` HTTP: healthz / list / create / update / delete / toggle, store-backed, persistent across restarts | **Done** |
| Evaluator: attributes, percentage rollout, rules, segments — **with variant values (string/number/JSON)** | **Done** (W1–2 + W3–4 value layer) |
| `switchyard eval` CLI: local evaluation with `--user`/`--attr` context | **Done** |
| Cold-start measurement published here | **Done** — see table above |
| OpenFeature provider (real go-sdk `FeatureProvider`, e2e-tested through `of.Client`) | **Done** (W3–4 shipped) |
| Go SDK (`sdk/` package: snapshot client + provider) | **Done** (W3–4 shipped) |
| Schema v2: per-environment `value` column, auto-migration from v1, data preserved | **Done** |
| TS + Python SDK generators (`sdk gen --lang typescript\|python`): zero-dep clients, versioned snapshot protocol, **96-case cross-language conformance with Go** | **Done** (W9 shipped) |
| LD migrator: real export shapes (variations, targets, weighted rollouts, 12 operators, negation, prerequisites), fidelity scoring, CI gate ≥90%, committed report | **Done** (W5–7 shipped — `docs/fidelity/launchdarkly.md`) |
| Unleash migrator (`--from=unleash --dry-run`): 7 strategies, 14 constraint operators, variants→values, segments by ID, 10 named gap types, CI-gated | **Done** (W8 shipped — `docs/fidelity/unleash.md`) |
| SCIM 2.0 provider skeleton (Users/Groups, bearer-token auth, fail-closed when unconfigured; schema v4 in every binary) | **Done** (W10 shipped) |
| Embedded UI (list, toggle, env switch, rollout %, create) — one static page, no build step | **Done** (W10 shipped) |
| Audit-log schema (`docs/audit-log.md`) + DPA stub (`docs/dpa.md`) + replacement-cost sheet (`docs/replacement-cost.md`) | **Done** (W10 shipped) |

---

## What it will not build

These are scope locks, not a TODO list. They lift only after **10 unrelated
teams are invoiced at list price** — see `AGENTS.md`:

- No experimentation engine, no Bayesian percentage stats, no warehouse sync
- No fourth official SDK, no React Native, no mobile
- No "Flag OS" / change-data-capture-everywhere platform
- No LaunchDarkly integration-catalog parity
- No Kubernetes requirement for the self-hosted path, ever
- No feature-gating the evaluator behind the hosted tier

---

## Pricing (public, day one)

No "contact sales" below Enterprise. Self-serve with a card.

| Tier | Price | What you get |
|---|---|---|
| Self-host core | **Free** (MIT) | everything above, forever — evaluation included |
| Cloud Starter | **$29/mo** | hosted, backups, upgrades, 10 seats |
| Cloud Pro | **$500/mo** | + VPC, SSO/SAML, audit logs, 50 seats |
| Enterprise | **$1,500/mo** | + air-gap, SLA, SCIM, unlimited seats |

The cloud tier sells **operational risk transfer** — we page, back up,
upgrade, and hold the SLA — not flags ripped out of core. Full breakdown and
the LaunchDarkly replacement-cost sheet: `docs/pricing.md`.

## Why the incumbents lose this segment

- **LaunchDarkly** prices mid-market out ($12k entry, SSO gated behind
  enterprise tiers) and leaving it is a consulting project.
- **Unleash** self-hosted requires Kubernetes + Postgres + Redis + an
  operator who reads release notes. That's a platform team's weekend, monthly.
- **OpenFeature** is a schema, not a control plane — it solves the SDK
  protocol, not the "recreate flags across environments" job.

## License & governance

- Core (server, evaluator, migrators, provider, 3 SDKs): **MIT** — `LICENSE`
- Commercial hosted extras (SSO, multi-tenant, audit pipeline): BSL 1.1
  stub pending counsel — `LICENSE-CLOUD`
- **DCO-only contributions** (`git commit -s`). No CLA, ever.
- License changes require 4/4 council consensus + 30-day public notice + a
  written migration path — `GOVERNANCE.md`

## Development

```bash
go test ./...     # 22 tests across 5 packages
go vet ./...
gofmt -l .
```

CI runs all three on every push and will grow the migration-fidelity gate in
W5–7 (fails under 90%).

## Support the project

If Switchyard saves you a LaunchDarkly invoice or a Kubernetes weekend, tips
are welcome. To be clear: tips are a tip jar, not the business model —
Cloud Pro is (`docs/pricing.md`). Donations are not counted as traction
anywhere in this project's evidence.

| Network | Address |
|---|---|
| Ethereum — ETH and USDC (ERC-20) | `0x85ee7E71f762d772599cbF1EC20E651B30657521` |
| Bitcoin — native SegWit (bech32) | `bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg` |

Both addresses passed checksum validation (EIP-55 / bech32) at commit time.
Send only the listed assets on the listed networks, and treat any address
that reaches you outside this README as phishing — legitimate addresses are
only ever added via a signed commit to this file.

---

*Named substitute costs, evidence gates, and the weekly job this kills are
spelled out in `IDEA.md` and `AGENTS.md`. If a PR expands scope past the
wedge, it gets refused with the trap named — that is the deal.*
