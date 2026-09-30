package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSCIMUserRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "v4.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	in := SCIMUser{
		ID: "usr-1", UserName: "alice@corp.test", DisplayName: "Alice",
		Email: "alice@corp.test", Active: true, Raw: `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"]}`,
	}
	if err := st.PutSCIMUser(ctx, in); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := st.GetSCIMUser(ctx, "usr-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserName != in.UserName || !got.Active || got.DisplayName != "Alice" {
		t.Fatalf("round trip = %+v", got)
	}
	users, err := st.ListSCIMUsers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(users) != 1 || users[0].ID != "usr-1" {
		t.Fatalf("list = %+v", users)
	}
	if err := st.DeleteSCIMUser(ctx, "usr-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetSCIMUser(ctx, "usr-1"); err != ErrNotFound {
		t.Fatalf("get deleted = %v, want ErrNotFound", err)
	}
	if err := st.DeleteSCIMUser(ctx, "usr-1"); err != ErrNotFound {
		t.Fatalf("double delete = %v, want ErrNotFound", err)
	}
}

func TestSCIMGroupRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "v4.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	in := SCIMGroup{
		ID: "grp-1", DisplayName: "Platform team", Members: []string{"usr-1", "usr-2"},
		Raw: `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"]}`,
	}
	if err := st.PutSCIMGroup(ctx, in); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := st.GetSCIMGroup(ctx, "grp-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "Platform team" || len(got.Members) != 2 || got.Members[1] != "usr-2" {
		t.Fatalf("round trip = %+v", got)
	}
	if err := st.DeleteSCIMGroup(ctx, "grp-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetSCIMGroup(ctx, "grp-1"); err != ErrNotFound {
		t.Fatalf("get deleted = %v, want ErrNotFound", err)
	}
}

// v3Schema is the frozen pre-SCIM DDL. Migration tests prove the upgrade.
const v3Schema = `
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

func TestOpenUpgradesV3ToV4PreservingData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v3.db")
	db, err := openRaw(dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	seed := []string{
		v3Schema,
		"PRAGMA user_version = 3",
		"INSERT INTO projects(key, name) VALUES('default', 'Default')",
		"INSERT INTO environments(key, name, sort_order) VALUES('production', 'production', 2)",
		"INSERT INTO flags(key, project_key, name, kind) VALUES('legacy', 'default', 'Legacy', 'boolean')",
		"INSERT INTO flag_environments(flag_key, env_key, enabled) VALUES('legacy', 'production', 1)",
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	db.Close()

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen (upgrade to v4): %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if v, _ := st.SchemaVersion(ctx); v != 4 {
		t.Fatalf("schema version = %d, want 4 after upgrade", v)
	}
	f, err := st.GetFlag(ctx, "legacy")
	if err != nil || !f.Environments["production"].On {
		t.Fatalf("legacy flag lost in upgrade: %+v err=%v", f, err)
	}
	// SCIM tables must be writable post-upgrade.
	if err := st.PutSCIMUser(ctx, SCIMUser{ID: "u", UserName: "u@x", Active: true, Raw: "{}"}); err != nil {
		t.Fatalf("put scim user on upgraded table: %v", err)
	}
}

func TestFreshDatabaseIsV4(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(context.Background()); v != 4 {
		t.Fatalf("fresh schema version = %d, want 4", v)
	}
}
