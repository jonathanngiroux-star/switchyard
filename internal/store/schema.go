// Package store will hold the SQLite-backed store (W1–2). The driver
// dependency (modernc.org/sqlite — pure Go, no cgo, keeps the single-binary
// promise) is pending approval; the DDL contract is pinned here first so
// the store implementation, the docs, and the migrate write-path all share
// one schema definition.
package store

// Schema is the v0.1 SQLite DDL.
//
// Design decisions (deliberate, solo-maintainable):
//   - rules and rollout live as JSON columns. Lossless round-trip of the
//     model types, no clause-by-clause normalization until fidelity work
//     demands it.
//   - flag_environments is keyed on (flag_key, env_key) and cascades from
//     both sides, so deleting a flag or environment cannot leave orphans.
const Schema = `
CREATE TABLE IF NOT EXISTS projects (
    key  TEXT PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS environments (
    key        TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS flags (
    key         TEXT PRIMARY KEY,
    project_key TEXT NOT NULL REFERENCES projects(key),
    name        TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL DEFAULT 'boolean'
);
CREATE INDEX IF NOT EXISTS idx_flags_project ON flags(project_key);

CREATE TABLE IF NOT EXISTS flag_environments (
    flag_key TEXT NOT NULL REFERENCES flags(key) ON DELETE CASCADE,
    env_key  TEXT NOT NULL REFERENCES environments(key) ON DELETE CASCADE,
    enabled  INTEGER NOT NULL DEFAULT 0,
    rollout  TEXT,
    rules    TEXT,
    value    TEXT,
    prerequisites TEXT,
    PRIMARY KEY (flag_key, env_key)
);

CREATE TABLE IF NOT EXISTS segments (
    key         TEXT PRIMARY KEY,
    project_key TEXT NOT NULL REFERENCES projects(key),
    name        TEXT NOT NULL DEFAULT '',
    rules       TEXT
);
`
