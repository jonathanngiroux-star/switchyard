# Audit log schema

Switchyard's audit log is the procurement artifact: a tamper-evident,
append-only record of who changed which flag, when, and what the change
was. The self-hosted core persists it in SQLite; the cloud tier streams
it to the customer's SIEM via the same schema.

## Schema (v1)

```sql
CREATE TABLE IF NOT EXISTS audit_log (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    ts           TEXT NOT NULL,            -- RFC 3339, UTC
    actor        TEXT NOT NULL,           -- user key / "scim:<id>" / "api-key:<id>"
    action       TEXT NOT NULL,           -- create | update | delete | toggle
    resource     TEXT NOT NULL,           -- "flag" | "segment" | "environment" | "scim_user" | "scim_group"
    key          TEXT NOT NULL,           -- flag key, segment key, user id, ...
    env          TEXT,                    -- environment affected, if any
    before       TEXT,                    -- JSON snapshot of the resource before
    after        TEXT,                    -- JSON snapshot of the resource after
    request_id   TEXT                     -- correlation ID from the API request
);
CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts);
CREATE INDEX IF NOT EXISTS idx_audit_resource ON audit_log(resource, key);
```

## Fields

| Field | Type | Meaning |
|---|---|---|
| `ts` | RFC 3339 UTC | when the change happened |
| `actor` | string | authenticated principal that made the change |
| `action` | enum | `create`, `update`, `delete`, `toggle` |
| `resource` | enum | which collection changed |
| `key` | string | the resource's identifier |
| `env` | string? | `dev` / `staging` / `production`, when the action is env-scoped |
| `before` / `after` | JSON | full resource snapshots — the diff is computed by the consumer, so the log stays append-only |
| `request_id` | string? | correlates multiple audit rows from one API call |

## Guarantees

1. **Append-only.** Rows are never updated or deleted by the server. The
   table has no UPDATE path in code; tests enforce it.
2. **Every mutation writes one row.** Flag CRUD, env toggles, segment
   changes, SCIM user/group provisioning. Dry-runs write nothing.
3. **`before`/`after` are full snapshots**, so the log is replayable and
   the diff is derivable without trusting a diff field.
4. **No PII beyond identifiers.** SCIM `raw` payloads are stored in the
   SCIM tables, not duplicated into audit rows; audit carries only ids.

## Current status (v0.1) — shipped

The write path is live: every mutating request (flag create/update/delete,
environment toggle, SCIM provisioning) writes exactly one append-only row
with full before/after snapshots. The middleware owns the write — handlers
declare what changed; failed requests write nothing; reads write nothing.
`GET /audit` (cloud tier) reads the same table this document defines.
