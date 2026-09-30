package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func TestPrerequisitesRoundTrip(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "v3.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	in := model.Flag{
		Key:  "child",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:            true,
				Prerequisites: []model.Prerequisite{{Flag: "parent", Variation: 1}},
			},
		},
	}
	if err := st.PutFlag(ctx, in); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := st.GetFlag(ctx, "child")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	pr := got.Environments["production"].Prerequisites
	if len(pr) != 1 || pr[0].Flag != "parent" || pr[0].Variation != 1 {
		t.Fatalf("prerequisites round trip = %+v, want parent/1", pr)
	}
}

func TestOpenUpgradesV2ToV3PreservingData(t *testing.T) {
	// Build a real v2 database from frozen DDL (v1 + value column).
	dbPath := filepath.Join(t.TempDir(), "v2.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	seed := []string{
		v1Schema,
		"ALTER TABLE flag_environments ADD COLUMN value TEXT",
		"PRAGMA user_version = 2",
		"INSERT INTO projects(key, name) VALUES('default', 'Default')",
		"INSERT INTO environments(key, name, sort_order) VALUES('production', 'production', 2)",
		"INSERT INTO flags(key, project_key, name, kind) VALUES('legacy', 'default', 'Legacy', 'string')",
		"INSERT INTO flag_environments(flag_key, env_key, enabled, rollout, rules, value) VALUES('legacy', 'production', 1, NULL, NULL, '\"kept\"')",
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	db.Close()

	// Reopen with current code: must migrate to v3 and preserve data.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen (upgrade to v3): %v", err)
	}
	defer st2.Close()
	ctx := context.Background()
	if v, _ := st2.SchemaVersion(ctx); v != 5 {
		t.Fatalf("schema version = %d, want 5 after upgrade", v)
	}
	f, err := st2.GetFlag(ctx, "legacy")
	if err != nil {
		t.Fatalf("get legacy: %v", err)
	}
	if f.Environments["production"].Value != "kept" {
		t.Fatalf("value lost in upgrade: %+v", f.Environments["production"])
	}
	if err := st2.SetFlagEnvironment(ctx, "legacy", "production",
		model.FlagEnvironment{On: true, Value: "kept", Prerequisites: []model.Prerequisite{{Flag: "p", Variation: 0}}}); err != nil {
		t.Fatalf("set prerequisites on upgraded table: %v", err)
	}
	f, _ = st2.GetFlag(ctx, "legacy")
	if len(f.Environments["production"].Prerequisites) != 1 {
		t.Fatalf("prerequisites after upgrade = %+v", f.Environments["production"])
	}
}

func TestFreshDatabaseIsV3(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	if v, _ := st.SchemaVersion(context.Background()); v != 5 {
		t.Fatalf("fresh schema version = %d, want 5", v)
	}
}
