package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"version"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "switchyard") {
		t.Fatalf("output = %q, want it to name the binary and version", out.String())
	}
}

func TestRunMigrateLaunchDarklyDryRunUsesDefaultFixture(t *testing.T) {
	// The v0.1 stub must work with no --input: default fixture path,
	// per the first-response brief.
	restore := chdirRepoRoot(t)
	defer restore()

	var out, errBuf bytes.Buffer
	code := run([]string{"migrate", "--from=launchdarkly", "--dry-run", "--format=json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	var d struct {
		Summary struct {
			Added    int `json:"added"`
			Removed  int `json:"removed"`
			Changed  int `json:"changed"`
			Unmapped int `json:"unmapped"`
		} `json:"summary"`
		Added []struct {
			Key string `json:"key"`
		} `json:"added"`
		Unmapped []struct {
			Flag string `json:"flag"`
			Type string `json:"type"`
		} `json:"unmapped"`
	}
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if d.Summary.Added != 2 || d.Summary.Removed != 0 || d.Summary.Changed != 0 || d.Summary.Unmapped != 2 {
		t.Fatalf("summary = %+v, want added=2 removed=0 changed=0 unmapped=2", d.Summary)
	}
	if len(d.Added) != 2 || d.Added[0].Key != "checkouts-v2" {
		t.Fatalf("added = %+v", d.Added)
	}
}

func TestRunMigrateRefusesUnleashUntilWeek8(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"migrate", "--from=unleash", "--dry-run"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("unleash must exit non-zero until it is implemented (W8)")
	}
	if !strings.Contains(errBuf.String(), "unleash") {
		t.Fatalf("stderr = %q, want it to name unleash", errBuf.String())
	}
}

func TestRunMigrateRequiresDryRun(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()

	var out, errBuf bytes.Buffer
	code := run([]string{"migrate", "--from=launchdarkly", "--format=json"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("v0.1 migrate has no write path; omitting --dry-run must fail")
	}
}

func TestRunUnknownCommandExitsTwo(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errBuf); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func chdirRepoRoot(t *testing.T) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(filepath.Join(old, "..", "..")); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}
