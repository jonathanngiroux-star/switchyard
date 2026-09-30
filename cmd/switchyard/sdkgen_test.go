package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// snapDB builds a store with a representative flag set spanning all kinds
// and evaluation paths.
func snapDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "snap.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	flags := []model.Flag{
		{Key: "plain-on", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{
			"production": {On: true},
		}},
		{Key: "plain-off", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{
			"production": {On: false},
		}},
		{Key: "fifty-fifty", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Rollout: &model.Rollout{Kind: "percentage", Percentage: 50}},
		}},
		{Key: "rule-plan", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Rollout: &model.Rollout{Kind: "percentage", Percentage: 0}, Rules: []model.Rule{{
				ID:      "r-pro",
				Clauses: []model.Clause{{Attribute: "plan", Operator: "in", Values: []string{"pro"}}},
			}}},
		}},
		{Key: "string-copy", Kind: model.KindString, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: "v2-short"},
		}},
		{Key: "number-timeout", Kind: model.KindNumber, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: float64(1500)},
		}},
		{Key: "json-config", Kind: model.KindJSON, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: map[string]any{"model": "v1", "topK": float64(10)}},
		}},
		{Key: "negated", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Rollout: &model.Rollout{Kind: "percentage", Percentage: 0}, Rules: []model.Rule{{
				ID:      "r-not-free",
				Clauses: []model.Clause{{Attribute: "plan", Operator: "in", Values: []string{"free"}, Negate: true}},
			}}},
		}},
		{Key: "segment-beta", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Rollout: &model.Rollout{Kind: "percentage", Percentage: 0}, Rules: []model.Rule{{
				ID:      "r-seg",
				Clauses: []model.Clause{{Attribute: "segment", Operator: "segmentMatch", Values: []string{"beta-users"}}},
			}}},
		}},
	}
	for _, f := range flags {
		if err := st.PutFlag(ctx, f); err != nil {
			t.Fatalf("put %s: %v", f.Key, err)
		}
	}
	if err := st.PutSegment(ctx, model.Segment{
		Key: "beta-users", Name: "Beta users",
		Rules: []model.Rule{{ID: "seg-1", Clauses: []model.Clause{
			{Attribute: "email", Operator: "endsWith", Values: []string{"@beta.test"}},
		}}},
	}); err != nil {
		t.Fatalf("put segment: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return dbPath
}

func TestSdkGenRequiresLanguage(t *testing.T) {
	var out, errBuf strings.Builder
	code := run([]string{"sdk", "gen"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("sdk gen without --lang must exit 2")
	}
}

func TestSdkGenRejectsUnknownLanguage(t *testing.T) {
	var out, errBuf strings.Builder
	code := run([]string{"sdk", "gen", "--lang", "rust"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("rust must be rejected — three languages, no more")
	}
	if !strings.Contains(errBuf.String(), "rust") {
		t.Fatalf("stderr = %q", errBuf.String())
	}
}

func TestSdkGenTypeScriptWritesClient(t *testing.T) {
	dbPath := snapDB(t)
	dir := t.TempDir()
	var out, errBuf strings.Builder
	code := run([]string{"sdk", "gen", "--lang", "typescript", "--db", dbPath, "--out", dir}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "switchyard.ts"))
	if err != nil {
		t.Fatalf("switchyard.ts not written: %v", err)
	}
	src := string(data)
	for _, want := range []string{
		"export interface", // typed surface
		`"plain-on"`,       // the snapshot data
		`"json-config"`,
		"beta-users", // segments present
		"evaluate",   // evaluation function
		"bucket",     // deterministic bucketing
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("generated TS missing %q", want)
		}
	}
	// Zero dependencies: no imports of anything.
	if imp := strings.Contains(src, "import "); imp {
		t.Fatal("generated TS must have zero imports")
	}
	// It must be self-verifying: a snapshot digest embedded.
	if !strings.Contains(src, "SWITCHYARD_SNAPSHOT") {
		t.Fatal("generated client must embed the snapshot constant")
	}
}

func TestSdkGenPythonWritesClient(t *testing.T) {
	dbPath := snapDB(t)
	dir := t.TempDir()
	var out, errBuf strings.Builder
	code := run([]string{"sdk", "gen", "--lang", "python", "--db", dbPath, "--out", dir}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "switchyard.py"))
	if err != nil {
		t.Fatalf("switchyard.py not written: %v", err)
	}
	src := string(data)
	for _, want := range []string{
		"class SwitchyardClient",
		"def evaluate",
		"def _bucket",
		`"plain-on"`,
		"beta-users",
		"SWITCHYARD_SNAPSHOT",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("generated Python missing %q", want)
		}
	}
	if strings.Contains(src, "import requests") || strings.Contains(src, "import aiohttp") {
		t.Fatal("generated Python must be stdlib-only")
	}
}

func TestSdkGenGoSnapshotIsJSON(t *testing.T) {
	// --lang go is not a generator (the Go SDK is hand-written); but the
	// snapshot export must be available as JSON for all languages.
	dbPath := snapDB(t)
	dir := t.TempDir()
	var out, errBuf strings.Builder
	code := run([]string{"sdk", "gen", "--lang", "typescript", "--db", dbPath, "--out", dir}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	// The snapshot JSON must also be written for cross-language conformance.
	snapPath := filepath.Join(dir, "snapshot.json")
	data, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("snapshot.json not written: %v", err)
	}
	var snap struct {
		Flags    map[string]json.RawMessage `json:"flags"`
		Env      string                     `json:"env"`
		Segments map[string]json.RawMessage `json:"segments"`
	}
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("snapshot.json not valid JSON: %v", err)
	}
	if len(snap.Flags) != 9 || snap.Env != "production" || len(snap.Segments) != 1 {
		t.Fatalf("snapshot shape wrong: %d flags, env %q, %d segments",
			len(snap.Flags), snap.Env, len(snap.Segments))
	}
}
