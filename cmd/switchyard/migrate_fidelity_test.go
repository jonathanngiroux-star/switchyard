package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateFidelityFlagWritesReport(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()
	outDir := t.TempDir()
	reportPath := filepath.Join(outDir, "fidelity.md")

	var out, errBuf strings.Builder
	code := run([]string{"migrate", "--from=launchdarkly", "--dry-run", "--format=json",
		"--input", "testdata/fixtures/launchdarkly/full-export.json",
		"--fidelity-gate", "0",
		"--fidelity", reportPath}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	md := string(data)
	for _, want := range []string{"# LaunchDarkly migration fidelity", "**Total flags:** 8", "**Fidelity:**"} {
		if !strings.Contains(md, want) {
			t.Fatalf("report missing %q", want)
		}
	}
}

func TestMigrateJSONIncludesFidelityScore(t *testing.T) {
	restore := chdirRepoRoot(t)
	defer restore()
	var out, errBuf strings.Builder
	code := run([]string{"migrate", "--from=launchdarkly", "--dry-run", "--format=json",
		"--input", "testdata/fixtures/launchdarkly/full-export.json",
		"--fidelity-gate", "0"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), `"fidelity"`) {
		t.Fatalf("JSON output must carry a fidelity object: %s", out.String())
	}
	if !strings.Contains(out.String(), `"score"`) {
		t.Fatalf("JSON output must carry a fidelity score: %s", out.String())
	}
}
