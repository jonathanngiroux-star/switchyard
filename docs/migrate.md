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

## Environment mapping (W5–7)

LD project/environment → Switchyard environment (`dev`, `staging`,
`production` default keys; explicit mapping table comes with the importer).
