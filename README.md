# Switchyard

Self-hosted feature-flag control plane. One Go binary. SQLite. Replaces the
LaunchDarkly / Unleash-on-Kubernetes line item for platform teams.

**This is a v0.1 scaffold.** Evaluator, OpenFeature provider, SDKs, and the
migration write-path land in W1–10 per `IDEA.md` / `AGENTS.md`.

## Quickstart (measured cold start lands W1–2; target 90s, cap 15min)

```bash
go build -o switchyard ./cmd/switchyard
./switchyard serve --addr :8080
```

```bash
curl -s localhost:8080/healthz
# {"status":"ok"}

curl -s localhost:8080/flags
# {"flags":[{"key":"welcome-banner","enabled":false}]}

curl -s -X POST localhost:8080/flags/welcome-banner/toggle
# {"key":"welcome-banner","enabled":true}
```

Docker:

```bash
docker build -t switchyard .
docker run -p 8080:8080 switchyard
```

## Migration dry-run (the wedge)

```bash
./switchyard migrate --from=launchdarkly --dry-run --format=json \
  --input testdata/fixtures/launchdarkly/sample-export.json
```

Output is a machine-readable diff — `added` / `removed` / `changed` /
`unmapped` — plus a summary. Constructs v0.1 cannot map are **listed, never
silently dropped** (`prerequisites`, unsupported clause operators). Fidelity
target ≥90% on rules + segments by W7; CI gates it. See `docs/migrate.md`.

`--from=unleash` refuses to run until Week 8 — it will not pretend.

## Pricing

Public, from day one: `docs/pricing.md`. Self-host core is free forever;
cloud sells operational risk transfer (backups, upgrades, SSO/SAML, SCIM,
audit, SLA), never flags ripped from core.

## License

MIT core (`LICENSE`). Commercial hosted extras: BSL 1.1 stub
(`LICENSE-CLOUD`, pending counsel). DCO-only contributions; no CLA, ever.
License changes need 4/4 council consensus + 30-day notice + migration
path — see `GOVERNANCE.md`.

## Scope locks

No 4th official SDK, no experimentation engine, no Kubernetes requirement,
no platform. Ten invoiced Cloud Pro teams come before any of that.
