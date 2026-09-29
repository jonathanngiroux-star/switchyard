package launchdarkly

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "launchdarkly", "sample-export.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestParseFixtureProjectShape(t *testing.T) {
	projects, _, err := Parse(fixture(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	p := projects[0]
	if p.Key != "default" || p.Name != "Default Project" {
		t.Fatalf("project = %s / %s", p.Key, p.Name)
	}
	if len(p.Environments) != 3 || p.Environments[0].Key != "dev" {
		t.Fatalf("environments = %+v", p.Environments)
	}
	if len(p.Flags) != 2 {
		t.Fatalf("flags = %d, want 2", len(p.Flags))
	}
	if len(p.Segments) != 1 || p.Segments[0].Key != "beta-users" {
		t.Fatalf("segments = %+v", p.Segments)
	}
}

func TestParseFixtureMapsRolloutAndRules(t *testing.T) {
	projects, _, err := Parse(fixture(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	byKey := map[string]int{}
	for i, f := range projects[0].Flags {
		byKey[f.Key] = i
	}
	c := projects[0].Flags[byKey["checkouts-v2"]]
	prod, ok := c.Environments["production"]
	if !ok {
		t.Fatal("checkouts-v2 missing production env")
	}
	if prod.On {
		t.Fatal("checkouts-v2 production should be off")
	}
	if prod.Rollout == nil || prod.Rollout.Kind != "percentage" || prod.Rollout.Percentage != 10 {
		t.Fatalf("checkouts-v2 production rollout = %+v, want percentage 10", prod.Rollout)
	}
	a := projects[0].Flags[byKey["api-rate-limit"]]
	aprod := a.Environments["production"]
	if len(aprod.Rules) != 3 {
		t.Fatalf("api-rate-limit production rules = %d, want 3", len(aprod.Rules))
	}
	var acme = aprod.Rules[2]
	if acme.ID != "rule-acme-domain" || acme.Rollout == nil || acme.Rollout.Percentage != 50 {
		t.Fatalf("rule-acme-domain = %+v", acme)
	}
}

func TestParseFixtureReportsUnmappedGaps(t *testing.T) {
	projects, unmapped, err := Parse(fixture(t))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// W5–7 semantics: prerequisites are mapped onto the environment, and
	// 'before' is a supported operator. The sample export has no genuinely
	// unmapped constructs left.
	if len(unmapped) != 0 {
		t.Fatalf("unmapped = %+v, want 0 entries (prereqs now mapped, before now supported)", unmapped)
	}
	a := projects[0].Flags[1]
	prod := a.Environments["production"]
	if len(prod.Prerequisites) != 1 || prod.Prerequisites[0].Flag != "checkouts-v2" {
		t.Fatalf("prerequisites must map onto the environment: %+v", prod.Prerequisites)
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	_, _, err := Parse([]byte("{ not json"))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseEmptyExport(t *testing.T) {
	projects, unmapped, err := Parse([]byte(`{"projects": []}`))
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if len(projects) != 0 || len(unmapped) != 0 {
		t.Fatalf("empty export should yield 0/0, got %d/%d", len(projects), len(unmapped))
	}
}
