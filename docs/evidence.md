# Evidence instrumentation

What Switchyard counts, how, and how to opt out. Stars, Docker pulls,
Discord members, and HN upvotes are **not** evidence — this file defines
the numbers that are.

## What we count: weekly active self-hosted deployments

The one metric that matters for a self-hosted-wedge product is *deployed
and running* instances, week over week — not downloads (a download is a
curiosity; a deploy is a decision).

### Mechanism

1. Every Switchyard data directory contains a stable **deploy ID**
   (UUID v4, generated on first run, stored in SQLite `instance_meta`,
   surviving restarts and upgrades). Exposed at `GET /deploy`.
2. The install script (and the README quickstart) registers the deploy:
   one HTTPS POST of `{deploy_id, version, source}` to the registry.
   This happens **once per deploy ID** — not per request, not per day.
3. The registry counts **distinct deploy IDs active in the trailing 7
   days** (a deploy "checks in" only when the binary restarts with a new
   version or the operator re-registers).

### What is sent — and what never is

| Field | Value | Why |
|---|---|---|
| `deploy_id` | random UUID, generated locally | distinguishes instances; not a user identity |
| `version` | binary version string | upgrade tracking |
| `source` | `install.sh` / `docker` / `manual` | which path converts |

**Never sent:** flag keys, flag values, rule contents, user keys, emails,
company identifiers, IP addresses (not logged at the registry beyond
standard transient access logs), or anything from the data directory.

### Opt-out

- Environment: `SWITCHYARD_NO_TELEMETRY=1` — the install script skips
  registration entirely.
- Flag: `install.sh --no-telemetry`
- Verify: nothing in the binary dials out except `serve` on the port you
  chose and, when registration runs, the single registry POST above.
  The evaluation path has **zero** network calls, by design and by test.

## The other evidence numbers (per the brief)

| Metric | Definition | Target |
|---|---|---|
| Migration fidelity | fully-mapped flags / total, per corpus | ≥90%, CI-gated at 100% on representative corpora |
| Waitlist→paid | invoiced teams / waitlist signups | >3% |
| Day-90 contributor retention | contributors active at day 90 / week-1 contributors | >4% |
| SCIM by v1.0 | shipped (skeleton) | done |
| Cloud pages, backs up, upgrades | operational risk transfer demonstrated | post-launch evidence pack |
| Independent paying teams | logos with no design-partner overlap | 10 before scope locks lift |

## Status (v0.1)

- Deploy ID: **shipped** (`GET /deploy`, stable across restarts).
- Registration endpoint: **spec'd here**, ships with the first hosted
  release channel (the `TODO` in `scripts/install.sh` marks the exact
  point where the URL lands).
- Everything else: **counted when real**, not before.
