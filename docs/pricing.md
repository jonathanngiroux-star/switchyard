# Pricing

**Status: there is no hosted tier and no billing today.** Switchyard is a
self-hosted, MIT-licensed project funded by crypto tips (see the README).
Nothing on this page is purchasable. The tiers below are the *planned*
revenue path for when a hosted offering exists — documented now so the
direction is public, priced later when the service is real.

## The self-hosted core (today, and free forever)

One binary + SQLite. You run it, you own it. Every capability in the
README — server, evaluator, migrators, SDK generation, SCIM skeleton,
audit log, TUI/web/desktop UIs — is in the free core.

## Planned hosted tiers (not yet available)

| Tier (planned) | What it would sell |
|---|---|
| Hosted Starter | we host it, back it up, upgrade it — you stop running it |
| Hosted Pro | + VPC peering, SSO/SAML, audit streaming |
| Enterprise | + air-gap install, SLA, DPA, SCIM-in-production |

The hosted tier will sell **operational risk transfer** — we page, back
up, upgrade, and hold the SLA — not flags ripped from core. The self-hosted
evaluator stays complete regardless.

## Replacement cost (named substitute, self-hosted vs. incumbent)

| Incumbent line item | Annual |
|---|---|
| LaunchDarkly entry contract | ~$12,000 |
| LaunchDarkly mid-market (with SSO) | $25,000–$100,000 |
| Unleash on Kubernetes (cluster, Postgres, Redis, upgrade labor) | ~2 engineer-months/yr |
| **Switchyard self-hosted** | **$0 + one binary** |

## What will never be paywalled

Evaluation. Flags must evaluate locally in your services, self-hosted,
forever. Gating evaluation behind a hosted tier is an investor "no" and a
product sin.
