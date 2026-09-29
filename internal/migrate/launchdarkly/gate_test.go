package launchdarkly

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/model"
)

// corpus loads a named fixture and parses it.
func corpus(t *testing.T, name string) ([]model.Project, []migrate.Unmapped) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "launchdarkly", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	projects, unmapped, err := Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return projects, unmapped
}

func TestFullCorpusFidelityMeetsGate(t *testing.T) {
	// CI gate: the representative corpus must map 100%. If a parser change
	// drops fidelity on mappable constructs, this test fails CI.
	projects, unmapped := corpus(t, "full-export.json")
	r := migrate.FidelityReport("launchdarkly", projects, unmapped)
	if r.Score != 1.0 {
		t.Fatalf("representative corpus should map 100%%, got %.1f%%:\n%s", r.Score*100, r.Markdown())
	}
	if !r.Pass(0.9) {
		t.Fatalf("full corpus fidelity = %.1f%%, must stay >= 90%% (CI gate):\n%s",
			r.Score*100, r.Markdown())
	}
}

func TestEdgeCorpusReportsEveryGapType(t *testing.T) {
	// Edge corpus: unmappable BY DESIGN. The contract is that every gap
	// type appears in the unmapped list — nothing silently dropped.
	_, unmapped := corpus(t, "edge-cases.json")
	seen := map[string]bool{}
	for _, u := range unmapped {
		seen[u.Type] = true
	}
	for _, want := range []string{"weighted-rollout", "clause-operator", "bucketBy", "segment-unbounded"} {
		if !seen[want] {
			t.Fatalf("edge corpus missing gap type %q; unmapped = %+v", want, unmapped)
		}
	}
}

func TestEdgeCorpusFidelityIsLowAndHonest(t *testing.T) {
	projects, unmapped := corpus(t, "edge-cases.json")
	r := migrate.FidelityReport("launchdarkly", projects, unmapped)
	if r.Score >= 0.9 {
		t.Fatalf("edge corpus fidelity = %.1f%%, should be well below the gate (it is unmappable by design)", r.Score*100)
	}
	// All 3 flags carry a gap (weighted, semver, bucketBy) — 0 fully mapped.
	// The segment-unbounded gap must not be miscounted as an affected flag.
	if r.TotalFlags != 3 || r.FullyMappedFlags != 0 {
		t.Fatalf("edge corpus counts = %d/%d, want 0/3 fully mapped", r.FullyMappedFlags, r.TotalFlags)
	}
}
