package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

// v1Schema is the frozen W1–2 schema, before the value column existed.
// Migration tests need the old world to prove the upgrade path.
const v1Schema = `
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
    PRIMARY KEY (flag_key, env_key)
);
CREATE TABLE IF NOT EXISTS segments (
    key         TEXT PRIMARY KEY,
    project_key TEXT NOT NULL REFERENCES projects(key),
    name        TEXT NOT NULL DEFAULT '',
    rules       TEXT
);
`

// openV1 creates a database in the old v1 state: schema applied,
// user_version=1, one flag with a production environment.
func openV1(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open v1: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(v1Schema); err != nil {
		t.Fatalf("apply v1 schema: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatalf("set v1: %v", err)
	}
	seed := []string{
		"INSERT INTO projects(key, name) VALUES('default', 'Default')",
		"INSERT INTO environments(key, name, sort_order) VALUES('production', 'production', 2)",
		"INSERT INTO flags(key, project_key, name, kind) VALUES('legacy', 'default', 'Legacy', 'boolean')",
		"INSERT INTO flag_environments(flag_key, env_key, enabled, rollout, rules) VALUES('legacy', 'production', 1, NULL, NULL)",
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	return dbPath
}

func TestOpenUpgradesV1ThroughV3AndPreservesData(t *testing.T) {
	dbPath := openV1(t)
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open (migrate) v1 db: %v", err)
	}
	defer st.Close()

	if v, _ := st.SchemaVersion(context.Background()); v != 5 {
		t.Fatalf("schema version = %d, want 5 after migration (full chain v1->v5)", v)
	}
	// Legacy row must survive the migration intact.
	f, err := st.GetFlag(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("get legacy flag: %v", err)
	}
	prod := f.Environments["production"]
	if !prod.On || prod.Value != nil {
		t.Fatalf("legacy prod env after migration = %+v, want on with nil value", prod)
	}
	// And the new value column must be writable.
	if err := st.SetFlagEnvironment(context.Background(), "legacy", "production",
		model.FlagEnvironment{On: true, Value: "upgraded-value"}); err != nil {
		t.Fatalf("set value on migrated table: %v", err)
	}
	f, _ = st.GetFlag(context.Background(), "legacy")
	if f.Environments["production"].Value != "upgraded-value" {
		t.Fatalf("value round trip on migrated table = %+v", f.Environments["production"])
	}
}

func TestFreshOpenIsCurrent(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open fresh: %v", err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(context.Background()); v != 5 {
		t.Fatalf("fresh schema version = %d, want 5", v)
	}
}

func TestFlagValueRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "v2.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	in := model.Flag{
		Key:  "cfg",
		Kind: model.KindJSON,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: map[string]any{"retries": 3, "backoff": "exp"}},
		},
	}
	if err := st.PutFlag(ctx, in); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := st.GetFlag(ctx, "cfg")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	v, ok := got.Environments["production"].Value.(map[string]any)
	if !ok || v["retries"] != 3.0 || v["backoff"] != "exp" {
		t.Fatalf("json value round trip = %+v (%T)", got.Environments["production"].Value, got.Environments["production"].Value)
	}
}
