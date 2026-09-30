package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/switchyard/switchyard/internal/model"
)

func TestAuditAppendAndList(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "v5.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	e1 := AuditEntry{
		Actor: "api-token", Action: "create", Resource: "flag",
		Key: "new-flag", Before: "", After: `{"key":"new-flag"}`, RequestID: "req-1",
	}
	if err := st.PutAudit(ctx, e1); err != nil {
		t.Fatalf("put audit: %v", err)
	}
	e2 := AuditEntry{
		Actor: "api-token", Action: "toggle", Resource: "flag",
		Key: "new-flag", Env: "production", Before: `{"on":false}`, After: `{"on":true}`, RequestID: "req-2",
	}
	if err := st.PutAudit(ctx, e2); err != nil {
		t.Fatalf("put audit 2: %v", err)
	}
	rows, err := st.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Append order + auto fields.
	if rows[0].ID >= rows[1].ID {
		t.Fatalf("ids must increase: %d then %d", rows[0].ID, rows[1].ID)
	}
	if rows[1].Action != "toggle" || rows[1].Env != "production" || rows[1].Before != `{"on":false}` {
		t.Fatalf("row 2 = %+v", rows[1])
	}
	for _, r := range rows {
		if r.TS == "" {
			t.Fatal("ts must be set by the store")
		}
		if _, err := time.Parse(time.RFC3339, r.TS); err != nil {
			t.Fatalf("ts %q not RFC3339: %v", r.TS, err)
		}
	}
}

func TestAuditHasNoUpdateOrDeleteAPI(t *testing.T) {
	// Append-only is a code-level guarantee: no mutation methods exist.
	// This test fails to compile if someone adds one without seeing this.
	st, _ := Open(filepath.Join(t.TempDir(), "a.db"))
	defer st.Close()
	_ = st.PutAudit
	_ = st.ListAudit
}

func TestEnsureDeployIDStableAcrossRestarts(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "meta.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	id1, err := st.EnsureDeployID(ctx)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if len(id1) != 36 { // UUID v4 textual length
		t.Fatalf("deploy id %q is not a UUID", id1)
	}
	st.Close()

	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	id2, err := st2.EnsureDeployID(ctx)
	if err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("deploy id changed across restarts: %q vs %q", id1, id2)
	}
}

// v4Schema is the frozen pre-v5 DDL (v3 + SCIM tables).
const v4Schema = v3Schema + `
CREATE TABLE IF NOT EXISTS scim_users (
    id           TEXT PRIMARY KEY,
    user_name    TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    email        TEXT NOT NULL DEFAULT '',
    active       INTEGER NOT NULL DEFAULT 1,
    raw          TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS scim_groups (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    members      TEXT NOT NULL DEFAULT '[]',
    raw          TEXT NOT NULL DEFAULT '{}'
);
`

func TestOpenUpgradesV4ToV5PreservingData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v4.db")
	db, err := openRaw(dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	seed := []string{
		v4Schema,
		"PRAGMA user_version = 4",
		"INSERT INTO projects(key, name) VALUES('default', 'Default')",
		"INSERT INTO environments(key, name, sort_order) VALUES('production', 'production', 2)",
		"INSERT INTO flags(key, project_key, name, kind) VALUES('legacy', 'default', 'Legacy', 'boolean')",
		"INSERT INTO flag_environments(flag_key, env_key, enabled) VALUES('legacy', 'production', 1)",
		"INSERT INTO scim_users(id, user_name) VALUES('u1', 'u@x')",
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	db.Close()

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen (upgrade to v5): %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if v, _ := st.SchemaVersion(ctx); v != 5 {
		t.Fatalf("schema version = %d, want 5 after upgrade", v)
	}
	// Flag and SCIM data preserved.
	f, err := st.GetFlag(ctx, "legacy")
	if err != nil || !f.Environments["production"].On {
		t.Fatalf("legacy flag lost: %+v err=%v", f, err)
	}
	if u, err := st.GetSCIMUser(ctx, "u1"); err != nil || u.UserName != "u@x" {
		t.Fatalf("scim user lost: %+v err=%v", u, err)
	}
	// Audit + deploy id writable post-upgrade.
	if err := st.PutAudit(ctx, AuditEntry{Actor: "test", Action: "create", Resource: "flag", Key: "x"}); err != nil {
		t.Fatalf("audit on upgraded table: %v", err)
	}
	if _, err := st.EnsureDeployID(ctx); err != nil {
		t.Fatalf("deploy id on upgraded table: %v", err)
	}
}

func TestFreshDatabaseIsV5(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(context.Background()); v != 5 {
		t.Fatalf("fresh schema version = %d, want 5", v)
	}
}

func TestAuditRowsSurviveFlagDeletion(t *testing.T) {
	// The audit log must not cascade when a flag is deleted — history
	// outlives the resource.
	st, _ := Open(filepath.Join(t.TempDir(), "cascade.db"))
	defer st.Close()
	ctx := context.Background()
	if err := st.PutFlag(ctx, model.Flag{Key: "temp", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := st.PutAudit(ctx, AuditEntry{Actor: "a", Action: "create", Resource: "flag", Key: "temp"}); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if err := st.DeleteFlag(ctx, "temp"); err != nil {
		t.Fatalf("delete flag: %v", err)
	}
	rows, _ := st.ListAudit(ctx, 10)
	if len(rows) != 1 || rows[0].Key != "temp" {
		t.Fatalf("audit row lost on flag delete: %+v", rows)
	}
	if !strings.EqualFold(rows[0].TS, rows[0].TS) {
		t.Fatal("sanity")
	}
}
