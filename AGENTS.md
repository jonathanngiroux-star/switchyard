# AGENTS.md — Switchyard working rules

Binding brief: `IDEA.md`. This file is the enforcement summary. If a request fights the wedge, refuse it and name the trap.

## The wedge (only this)

- Self-hosted flag server: single Go binary + SQLite + embedded UI. ~90-second deploy (hard cap 15 minutes).
- OpenFeature-compatible provider.
- Migration CLI: `switchyard migrate --from=launchdarkly|unleash --dry-run` with machine-readable JSON diffs.
- Environment mapping (dev/stage/prod; LD project/environment → Switchyard env).
- SDKs: Go, TypeScript, Python only.
- Cloud sells operational risk transfer (VPC, SAML/SCIM, audit logs, SLA, backups, upgrades) — never flags ripped from core.

## Hard scope locks (until 10 invoiced Cloud Pro / mid-tier teams)

- No 4th official SDK. No mobile SDKs.
- No experimentation / Bayesian stats / warehouse sync.
- No "Flag OS" / change-data-capture everywhere.
- No LaunchDarkly integration-catalog parity.
- No gating the evaluation engine behind cloud.
- No Kubernetes requirement, ever, for the self-hosted path.

## Stack rules

- Go. SQLite default (Postgres = documented upgrade only).
- One binary + `docker run` + embedded UI.
- MIT core; BSL/ELv2 commercial layer; see `GOVERNANCE.md`.
- CI: GitHub Actions; contract tests on migrate diffs; fidelity gate ≥90%.
- Solo-maintainable surface area is a hard constraint. Every added file is a liability.

## Working rules

1. Migration fidelity outranks new SDKs. Always.
2. Every feature must answer yes to one of: does this help migrate, evaluate, deploy in ≤90s, or pass 50-seat procurement? If no → do not build it.
3. Traps to refuse out loud when requested: React Native SDK, Rust SDK, percentage-experiment stats, "just host the binary" as the entire cloud tier, waitlist theater, 20-SDK sprawl.
4. Output patches, commands, and file paths. Not essays.
5. Evidence is instrumented from commit 1: migration fidelity %, weekly active self-host deploys, waitlist→paid, SCIM by v1.0. Stars/Docker pulls/Discord are not evidence.

## MVP order (do not reorder without saying why)

W1–2 binary + evaluator + SQLite → W3–4 OpenFeature provider + Go SDK → W5–7 LaunchDarkly migrate (fixture corpus + CI fidelity gate ≥90%) → W8 Unleash migrate → W9 TS + Python generators → W10 procurement stubs + docs.

## Definition of done for v0.1

One binary, SQLite, embedded UI path documented, cold start measured and published, OpenFeature provider + Go SDK, LD dry-run migrate with fidelity markdown from fixtures (CI fails <90%), Unleash dry-run with gaps listed, TS + Python generators scheduled or shipped, GOVERNANCE.md + pricing markdown. No experimentation engine, no 4th SDK, no K8s requirement.
