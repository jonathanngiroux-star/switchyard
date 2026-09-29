package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// setupEvalDB creates a store with one flag enabled in production.
func setupEvalDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	f := model.Flag{
		Key:  "checkouts-v2",
		Name: "checkouts-v2",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true},
		},
	}
	if err := st.PutFlag(context.Background(), f); err != nil {
		t.Fatalf("put flag: %v", err)
	}
	return dbPath
}

func TestRunEvalAgainstStore(t *testing.T) {
	dbPath := setupEvalDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"eval", "--db", dbPath, "--env", "production", "--user", "alice", "checkouts-v2"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("eval exit = %d, stderr = %q", code, errBuf.String())
	}
	var d struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if !d.Enabled || d.Reason != "fallthrough" {
		t.Fatalf("eval = %+v, want {true fallthrough}", d)
	}
}

func TestRunEvalUnknownFlagExitsOne(t *testing.T) {
	dbPath := setupEvalDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"eval", "--db", dbPath, "--env", "production", "--user", "alice", "ghost"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("eval of unknown flag must exit 1")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("stderr = %q, want 'not found'", errBuf.String())
	}
}
