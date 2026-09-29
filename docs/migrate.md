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
  v0.1 known gaps: flag `prerequisites`, clause operators outside
  `segmentMatch | in | startsWith | endsWith`.
- **Never** claim 100% fidelity. Unleash migrator (W8) must list its gaps
  the same way — strategy types that do not map get named, not faked.
- Fidelity score = mapped flags / total source flags, from
  `migrate.FidelityScore`. CI gate from W5–7: **fail under 90%** on the
  fixture corpus (`testdata/fixtures/`). Human report:
  `docs/fidelity/launchdarkly.md`.

## Environment mapping (W5–7)

LD project/environment → Switchyard environment (`dev`, `staging`,
`production` default keys; explicit mapping table comes with the importer).
