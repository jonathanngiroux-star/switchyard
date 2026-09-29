# Migration dry-run contract

The wedge is a migration CLI platform teams can trust before they touch
anything. Trust = machine-readable diffs + honest gap lists.

## Command

```bash
switchyard migrate --from=launchdarkly --dry-run [--format human|json] [--input PATH]
```

v0.1 is **dry-run only**. The write path lands with the W1–2 store.

## JSON shape

```json
{
  "added":    [ { "key": "...", "name": "...", "kind": "boolean", "environments": { ... } } ],
  "removed":  [],
  "changed":  [],
  "unmapped": [ { "flag": "api-rate-limit", "rule": "rule-before-date",
                  "type": "clause-operator", "detail": "operator \"before\" not supported in v0.1" } ],
  "summary":  { "added": 2, "removed": 0, "changed": 0, "unmapped": 2 }
}
```

Rules: every list serializes as `[]` (never `null`). Source order is
preserved. First import against an empty Switchyard reports every source
flag as `added`.

## Gap policy

- Unsupported constructs produce an `unmapped` entry with `type` + `detail`.
  Known gaps (see `testdata/fixtures/launchdarkly/edge-cases.json` and
  `docs/fidelity/launchdarkly.md`):
  - `weighted-rollout` — multi-variation weighted fallthroughs map to the
    dominant bucket only
  - `clause-operator` — operators outside the supported set (12 operators)
  - `bucketBy` — custom bucketing attributes
  - `segment-unbounded` — LD big-segment membership
  - Prerequisites are imported onto the flag environment but **not enforced**
    during evaluation yet (documented, planned)
- **Never** claim 100% fidelity on a corpus that has unmappable constructs.
  The representative corpus (`full-export.json`) maps 100% and is CI-gated;
  the edge corpus is unmappable by design and its gaps are contract-tested.
- Fidelity score = fully-mapped flags / total source flags. A flag with ANY
  unmapped construct is not fully mapped. CI gate: **fail under 90%**
  (`--fidelity-gate`, default 0.9). Human report:
  `docs/fidelity/launchdarkly.md`, CI verifies it is committed and current.
- Unleash migrator (W8) must list its gaps the same way — strategy types
  that do not map get named, not faked.

## Unleash migration (W8 — shipped)

`switchyard migrate --from=unleash --dry-run` parses Unleash state exports
(schema v5). Strategy mapping:

| Unleash strategy | Switchyard mapping |
|---|---|
| `default` (no constraints) | flag on, serves everyone |
| `default` + constraints | rule with those clauses |
| `userWithId` | rule: `targetingKey in [ids]` |
| `flexibleRollout` / `gradualRollout` (no constraints) | fallthrough percentage rollout |
| `flexibleRollout` + constraints | rule with its own percentage rollout (match AND bucket) |
| `gradualRolloutRandom` | **gap** — non-sticky randomness can't be preserved; flag fails closed (0%) |
| any other/custom strategy | **gap** — `strategy-unsupported` |
| enabled + zero active strategies | 0% rollout (Unleash serves nobody) |

Constraint operators mapped (14): `IN`, `NOT_IN`, `STR_STARTS_WITH`,
`STR_ENDS_WITH`, `STR_CONTAINS`, `NUM_EQ`, `NUM_GT`, `NUM_GTE`, `NUM_LT`,
`NUM_LTE`, `DATE_AFTER`, `DATE_BEFORE`, `REGEX` — with `NOT_IN`/inverted
carrying Negate and `userId` mapping to `targetingKey`.
Environment-scoped constraints are evaluated at import time: tautologies
drop, non-matching envs mark the strategy dead.

Unleash-specific gap types (all reported in `unmapped`, never dropped):
`strategy-random-rollout`, `stickiness`, `bucketBy` (custom `groupId`),
`strategy-unsupported`, `strategy-conflict`, `constraint-operator`,
`constraint-case-insensitive`, `weighted-variants`, `variant-payload`,
`segment-missing`. Corpora: `testdata/fixtures/unleash/` — representative
(CI-gated at 100%) and edge (unmappable by design, all gaps contract-tested).
Report: `docs/fidelity/unleash.md`.

## Environment mapping (W5–7)

LD project/environment → Switchyard environment (`dev`, `staging`,
`production` default keys; explicit mapping table comes with the importer).
