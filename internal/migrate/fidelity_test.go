package migrate

import (
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func proj(key string, flags ...model.Flag) []model.Project {
	return []model.Project{{Key: key, Flags: flags}}
}

func bflag(key string) model.Flag {
	return model.Flag{Key: key, Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}
}

func TestFidelityPerfectCorpus(t *testing.T) {
	projects := proj("p", bflag("a"), bflag("b"), bflag("c"))
	r := FidelityReport(projects, nil)
	if r.TotalFlags != 3 || r.FullyMappedFlags != 3 || len(r.Unmapped) != 0 {
		t.Fatalf("perfect corpus = %+v", r)
	}
	if r.Score != 1.0 {
		t.Fatalf("score = %v, want 1.0", r.Score)
	}
	if !r.Pass(0.9) {
		t.Fatal("1.0 fidelity must pass the 90% gate")
	}
}

func TestFidelityPartialUnmappedFlags(t *testing.T) {
	projects := proj("p", bflag("a"), bflag("b"), bflag("c"), bflag("d"))
	unmapped := []Unmapped{
		{Project: "p", Flag: "b", Type: "clause-operator", Detail: "op X unsupported"},
		{Project: "p", Flag: "d", Type: "weighted-rollout", Detail: "3 variations collapsed"},
	}
	r := FidelityReport(projects, unmapped)
	// 2 of 4 flags fully mapped = 0.5
	if r.TotalFlags != 4 || r.FullyMappedFlags != 2 {
		t.Fatalf("counts = %d/%d, want 2/4", r.FullyMappedFlags, r.TotalFlags)
	}
	if r.Score != 0.5 {
		t.Fatalf("score = %v, want 0.5", r.Score)
	}
	if r.Pass(0.9) {
		t.Fatal("0.5 must fail the 90% gate")
	}
}

func TestFidelityMultipleIssuesSameFlagCountsOnce(t *testing.T) {
	projects := proj("p", bflag("a"), bflag("b"))
	unmapped := []Unmapped{
		{Project: "p", Flag: "b", Type: "clause-operator"},
		{Project: "p", Flag: "b", Type: "bucketBy"},
		{Project: "p", Flag: "b", Type: "weighted-rollout"},
	}
	r := FidelityReport(projects, unmapped)
	if r.FullyMappedFlags != 1 || r.TotalFlags != 2 {
		t.Fatalf("counts = %d/%d, want 1/2 — one flag with 3 issues counts once", r.FullyMappedFlags, r.TotalFlags)
	}
	if r.Score != 0.5 {
		t.Fatalf("score = %v, want 0.5", r.Score)
	}
}

func TestFidelityByTypeBreakdown(t *testing.T) {
	projects := proj("p", bflag("a"), bflag("b"), bflag("c"))
	unmapped := []Unmapped{
		{Project: "p", Flag: "a", Type: "clause-operator"},
		{Project: "p", Flag: "a", Type: "bucketBy"},
		{Project: "p", Flag: "b", Type: "clause-operator"},
	}
	r := FidelityReport(projects, unmapped)
	if r.ByType["clause-operator"] != 2 || r.ByType["bucketBy"] != 1 {
		t.Fatalf("by-type = %v", r.ByType)
	}
}

func TestFidelityReportRendersMarkdown(t *testing.T) {
	projects := proj("p", bflag("a"), bflag("b"))
	unmapped := []Unmapped{
		{Project: "p", Flag: "b", Type: "clause-operator", Detail: "operator \"semverEqual\" not supported"},
	}
	r := FidelityReport(projects, unmapped)
	md := r.Markdown()
	for _, want := range []string{
		"# LaunchDarkly migration fidelity",
		"- **Total flags:** 2",
		"- **Fully mapped:** 1",
		"- **Fidelity:** 50.0%",
		"clause-operator",
		"semverEqual",
	} {
		if !contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
