# The 90-day pack — clock started 2026-09-30

The evidence pack is the only document that moves an angel. This file is
its skeleton and its clock. The countdown starts at the v0.1.0 release:
**day 90 is 2026-12-29.**

## The gates (from the brief — not negotiable)

| Day | Gate | Evidence |
|---|---|---|
| 30 | First external deploys live | 3–5 distinct deploy IDs answering on `/deploy` |
| 60 | Migration fidelity proven on real corpora | dry-run reports from at least one real LD or Unleash export (not fixtures) |
| 90 | Pack assembled | everything below |

## What goes in the pack

1. **Deploy evidence** — distinct deploy IDs per week (from the registry
   once shipped; manual `GET /deploy` screenshots before that), not stars.
2. **Migration-fidelity reports on real exports** — `switchyard migrate
   --dry-run --format=json` output from a named design partner's actual
   flag corpus, with the gap list and their sign-off that the gaps are
   acceptable.
3. **LOIs → paid pilots** — letters of intent with named substitute costs
   (their LD invoice), then invoiced pilots at list price. Design partners
   who never get invoiced are theater; the pack distinguishes them by
   invoice number.
4. **Procurement surface** — pricing page, DPA (counsel-reviewed by then),
   audit-log schema + `GET /audit` demo, SCIM skeleton. All shipped in
   v0.1.0; the pack shows them being used.
5. **Conversion instrumentation** — UTM on the install path, waitlist→paid
   >3%, star-to-paid tracked from deploy IDs, not follower counts.

## Founder's checklist (the part only you can do)

- [ ] List Switchyard where platform engineers actually look (HN Show HN,
      r/selfhosted, r/devops, Lobsters) with the 90-second demo GIF
- [ ] DM 20 platform engineers at Series A–B companies paying LD — the
      pitch is the fidelity report, not the UI
- [ ] Offer 3 design partners the migration dry-run on their real export
      (free, one command, no commitment) — the export *is* the sales call
- [ ] Convert the best dry-run into a paid pilot at list price ($500/mo
      Cloud Pro) by day 60
- [ ] Get the first `docker run` from someone you cannot name a prior
      relationship with — that is deploy #1 that counts
- [ ] Weekly: record distinct deploy IDs in this file's table below

## Deploy log (update weekly)

| Week | Date | Distinct deploy IDs | Notes |
|---|---|---|---|
| 1 | 2026-10-07 | — | release shipped 2026-09-30 |

## Investor "no" the pack must survive

- Fidelity <90% → the CI gate makes this impossible to hide; real-export
  reports show it
- No SCIM → shipped (skeleton) in v0.1.0
- Cloud = managed binary → `docs/pricing.md` sells risk transfer; the pack
  shows backups/upgrades/SSO being operated, not just promised
- Concentration → 10+ logos before scope locks lift; the pack counts
  unrelated invoiced teams

The clock is the moat-reminder too: LaunchDarkly's retaliation window is
6–9 months from visibility. Day 90 is 2026-12-29; move like the window is
real.
