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
		Diff struct {
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
		} `json:"diff"`
	}
	if err := json.Unmarshal([]byte(out.String()), &d); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if d.Diff.Summary.Added != 2 || d.Diff.Summary.Unmapped != 0 {
		t.Fatalf("summary = %+v, want added=2 unmapped=0 (sample export fully maps)", d.Diff.Summary)
	}
	if len(d.Diff.Added) != 2 || d.Diff.Added[0].Key != "checkouts-v2" {
		t.Fatalf("added = %+v", d.Diff.Added)
	}
}

func TestRunMigrateUnleashDryRun(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()
	var out, errBuf strings.Builder
	code := run([]string{"migrate", "--from=unleash", "--dry-run", "--format=json",
		"--input", "testdata/fixtures/unleash/unleash-representative.json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	var d struct {
		Diff struct {
			Summary struct {
				Added int `json:"added"`
			} `json:"summary"`
		} `json:"diff"`
		Fidelity struct {
			Score float64 `json:"score"`
		} `json:"fidelity"`
	}
	if err := json.Unmarshal([]byte(out.String()), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Diff.Summary.Added != 8 {
		t.Fatalf("added = %d, want 8", d.Diff.Summary.Added)
	}
	if d.Fidelity.Score != 1.0 {
		t.Fatalf("representative corpus fidelity = %v, want 1.0", d.Fidelity.Score)
	}
}

// The default --input must match --from: an Unleash run without --input
// used to silently parse the LaunchDarkly fixture with the Unleash
// parser, report "0 flags", score "100%" fidelity, and exit 0.
func TestRunMigrateUnleashDefaultInputIsUnleashFixture(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()
	var out, errBuf strings.Builder
	code := run([]string{"migrate", "--from=unleash", "--dry-run", "--format=json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	var d struct {
		Diff struct {
			Summary struct {
				Added int `json:"added"`
			} `json:"summary"`
		} `json:"diff"`
	}
	if err := json.Unmarshal([]byte(out.String()), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Diff.Summary.Added != 8 {
		t.Fatalf("default unleash input added = %d, want 8 (unleash-representative corpus)", d.Diff.Summary.Added)
	}
}

// An empty source corpus must not sail through the fidelity gate as
// "100%". Either a wrong-format export (0 flags parse out) or a
// genuinely empty project is a failed migration, not a perfect one —
// the honesty contract refuses to pretend.
func TestRunMigrateEmptyCorpusFailsGate(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty-export.json")
	if err := os.WriteFile(empty, []byte(`{"version": 7, "projects": [], "flags": []}`), 0o644); err != nil {
		t.Fatalf("write empty fixture: %v", err)
	}
	var out, errBuf strings.Builder
	code := run([]string{"migrate", "--from=launchdarkly", "--dry-run", "--format=json",
		"--input", empty, "--fidelity-gate", "0.9"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("empty corpus must exit 1 below the gate — refusing to pretend 100% fidelity on nothing")
	}
	if !strings.Contains(errBuf.String(), "below gate") {
		t.Fatalf("stderr = %q", errBuf.String())
	}
}

func TestRunMigrateUnleashEdgeReportsGaps(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()
	var out, errBuf strings.Builder
	// Edge corpus is below the gate BY DESIGN; --fidelity-gate 0 lets us
	// inspect the JSON, and the honesty contract is in the unmapped list.
	code := run([]string{"migrate", "--from=unleash", "--dry-run", "--format=json",
		"--input", "testdata/fixtures/unleash/unleash-edge.json", "--fidelity-gate", "0"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "strategy-random-rollout") ||
		!strings.Contains(out.String(), "strategy-unsupported") ||
		!strings.Contains(out.String(), "constraint-operator") {
		t.Fatalf("edge corpus gaps missing from JSON: %s", out.String())
	}
}

func TestRunMigrateUnleashRefusesBelowGate(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()
	var out, errBuf strings.Builder
	code := run([]string{"migrate", "--from=unleash", "--dry-run",
		"--input", "testdata/fixtures/unleash/unleash-edge.json"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("edge corpus must exit 1 at the default 90% gate — refusing to pretend")
	}
	if !strings.Contains(errBuf.String(), "below gate") {
		t.Fatalf("stderr = %q", errBuf.String())
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
