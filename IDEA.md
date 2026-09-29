# Switchyard

**Category:** Feature-flag control plane + LaunchDarkly/Unleash migration CLI
**Cohort:** Faster-to-cash
**Incumbent response clock:** ~6–9 months after you spike (Startup Program-style undercut).

## Pitch

Self-hosted feature-flag server (single binary, SQLite, ~90-second deploy) that imports LaunchDarkly/Unleash projects, maps environments, generates client SDKs, and charges $30–$500/mo vs LaunchDarkly’s ~$12k/year entry.

## Weekly job

Platform engineers at Series A–B companies recreate flag configs across environments, audit rollout percentages, and coordinate SDK updates across 5+ services every release.

## Who pays

Platform / infra engineers at 50–500 person companies paying LaunchDarkly $12k–$100k/year or self-hosting Unleash on Kubernetes.

## Why incumbents fail

LaunchDarkly prices out mid-market. Unleash wants Kubernetes + Postgres + Redis. OpenFeature is schema-only — no control plane. Migration is a two-week consulting engagement.

## Why this is still open

Category has had one breakout repo (high stars, ~4% waitlist-to-cloud). Migration tooling maturity is still weak (~2/5). The wedge is the dry-run importer, not another flag UI.

## MVP (6–10 weeks)

- Go binary + embedded UI
- OpenFeature-compatible provider
- `switchyard migrate --from=launchdarkly --dry-run` JSON diff
- Environment mapping
- SDK generator for Go / TypeScript / Python
- Do **not** ship 20 SDKs before migration fidelity ≥95% on rules + segments

## License

MIT core + BSL/ELv2 on hosted control-plane extras (SSO, audit, multi-tenant). Cloud must transfer operational risk (upgrades, backups, pages), not just host the binary.

## Pricing

- Cloud Starter — $29/mo (10 seats)
- Cloud Pro — $500/mo (50 seats, VPC, SSO)
- Enterprise — $1,500/mo (unlimited / air-gap)

## 90-day evidence

- Waitlist-to-paid >3%
- Day-90 contributor retention >4%
- Migration fidelity ≥90% (target 95%) on flag rules/segments
- SCIM by v1.0
- Public pricing + DPA + audit-log schema

## Living-income path

Low-ARPU volume works: ~150 teams at $30 is already ~$4.5k MRR. Path to $50–80k MRR by month 12 if day-90 retention holds. Solo margin on cloud should stay ~90%.

## Investor “no”

Fidelity <90%; no SCIM; cloud = “we run your binary” with no risk transfer.

## Fastest death

Building a “platform” with 20+ SDKs before the migration CLI is trustworthy. LaunchDarkly ships parity and undercuts in 6–9 months.
