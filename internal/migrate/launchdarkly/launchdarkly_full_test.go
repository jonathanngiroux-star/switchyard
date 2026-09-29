package launchdarkly

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/model"
)

func fullFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "launchdarkly", "full-export.json"))
	if err != nil {
		t.Fatalf("read full fixture: %v", err)
	}
	return data
}

func mustParseFull(t *testing.T) ([]model.Project, []unmappedView) {
	t.Helper()
	projects, unmapped, err := Parse(fullFixture(t))
	if err != nil {
		t.Fatalf("parse full fixture: %v", err)
	}
	return projects, toView(unmapped)
}

type unmappedView struct {
	Project, Flag, Rule, Type string
}

func toView(u []migrate.Unmapped) []unmappedView {
	out := make([]unmappedView, 0, len(u))
	for _, e := range u {
		out = append(out, unmappedView{e.Project, e.Flag, e.Rule, e.Type})
	}
	return out
}

func TestFullCorpusProjectShape(t *testing.T) {
	projects, _ := mustParseFull(t)
	if len(projects) != 3 {
		t.Fatalf("projects = %d, want 3", len(projects))
	}
	if projects[0].Key != "acme-prod" || len(projects[0].Flags) != 6 || len(projects[0].Segments) != 2 {
		t.Fatalf("acme-prod shape = %d flags, %d segments", len(projects[0].Flags), len(projects[0].Segments))
	}
}

func TestFullCorpusBooleanVariations(t *testing.T) {
	projects, _ := mustParseFull(t)
	c := flagByKey(t, projects[0], "checkouts-v2")
	prod := c.Environments["production"]
	if prod.On {
		t.Fatal("production must be off")
	}
	if prod.Rollout == nil || prod.Rollout.Percentage != 10 {
		t.Fatalf("production rollout = %+v, want 10%%", prod.Rollout)
	}
	if prod.Value != nil {
		t.Fatalf("boolean flags must not carry a value: %+v", prod.Value)
	}
	staging := c.Environments["staging"]
	if staging.Rollout == nil || staging.Rollout.Percentage != 25 {
		t.Fatalf("staging rollout = %+v", staging.Rollout)
	}
}

func TestFullCorpusRuleOperatorsAndTargetingKey(t *testing.T) {
	projects, _ := mustParseFull(t)
	a := flagByKey(t, projects[0], "api-rate-limit")
	prod := a.Environments["production"]
	if len(prod.Rules) != 8 {
		t.Fatalf("rules = %d, want 8", len(prod.Rules))
	}
	byID := map[string]model.Rule{}
	for _, r := range prod.Rules {
		byID[r.ID] = r
	}
	checks := []struct {
		id string
		op string
	}{
		{"rule-vips", "in"},
		{"rule-email-contains", "contains"},
		{"rule-regex", "matches"},
		{"rule-numeric", "greaterThanOrEqual"},
		{"rule-negated", "in"},
		{"rule-date", "before"},
	}
	for _, c := range checks {
		r, ok := byID[c.id]
		if !ok || len(r.Clauses) != 1 || r.Clauses[0].Operator != c.op {
			t.Fatalf("rule %s = %+v, want operator %s", c.id, r, c.op)
		}
	}
	// Negation preserved
	if !byID["rule-negated"].Clauses[0].Negate {
		t.Fatal("rule-negated must preserve Negate=true")
	}
	// Rule-level rollout preserved
	if byID["rule-regex"].Rollout == nil || byID["rule-regex"].Rollout.Percentage != 50 {
		t.Fatalf("rule-regex rollout = %+v, want 50%%", byID["rule-regex"].Rollout)
	}
	// Multi-clause rule preserved
	if len(byID["rule-multi-clause"].Clauses) != 2 {
		t.Fatalf("rule-multi-clause clauses = %d, want 2", len(byID["rule-multi-clause"].Clauses))
	}
	// Prerequisites mapped onto the environment
	if len(prod.Prerequisites) != 1 || prod.Prerequisites[0].Flag != "checkouts-v2" {
		t.Fatalf("prerequisites = %+v, want checkouts-v2", prod.Prerequisites)
	}
}

func TestFullCorpusStringVariationsMapToValue(t *testing.T) {
	projects, _ := mustParseFull(t)
	c := flagByKey(t, projects[0], "checkout-copy")
	if c.Kind != model.KindString {
		t.Fatalf("kind = %s", c.Kind)
	}
	prod := c.Environments["production"]
	// Rule picks variation 1 = "v2-short"; the parsed rule must carry that value.
	if len(prod.Rules) != 1 || prod.Rules[0].Value == nil {
		t.Fatalf("rule must carry its variation value: %+v", prod.Rules)
	}
	if prod.Rules[0].Value != "v2-short" {
		t.Fatalf("rule value = %v, want v2-short (variation 1)", prod.Rules[0].Value)
	}
	// Fallthrough variation 0 = "legacy"
	if prod.Value != "legacy" {
		t.Fatalf("fallthrough value = %v, want legacy (variation 0)", prod.Value)
	}
}

func TestFullCorpusNumberAndJSONValues(t *testing.T) {
	projects, _ := mustParseFull(t)
	n := flagByKey(t, projects[0], "search-timeout-ms")
	prod := n.Environments["production"]
	// W5-7 corpus: simple percentage rollout carrying variation 1 (1500).
	if prod.Value == nil || prod.Value != float64(1500) {
		t.Fatalf("fallthrough value = %v (%T), want float64 1500", prod.Value, prod.Value)
	}
	j := flagByKey(t, projects[0], "recommender-config")
	jprod := j.Environments["production"]
	m, ok := jprod.Value.(map[string]any)
	if !ok || m["model"] != "v1" || m["topK"] != 10.0 {
		t.Fatalf("json fallthrough value = %+v, want {model v1 topK 10}", jprod.Value)
	}
}

func TestFullCorpusTargetsBecomeRules(t *testing.T) {
	projects, _ := mustParseFull(t)
	s := flagByKey(t, projects[0], "superadmin-mode")
	prod := s.Environments["production"]
	if len(prod.Rules) == 0 {
		t.Fatal("targets must be converted to rules")
	}
	r := prod.Rules[0]
	if len(r.Clauses) != 1 || r.Clauses[0].Attribute != "targetingKey" || r.Clauses[0].Operator != "in" {
		t.Fatalf("target rule = %+v", r)
	}
	if len(r.Clauses[0].Values) != 2 || r.Clauses[0].Values[0] != "admin-1@acme.test" {
		t.Fatalf("target values = %+v", r.Clauses[0].Values)
	}
	if r.Value != true {
		t.Fatalf("target rule must carry the true variation: %v", r.Value)
	}
}

func TestSegmentsAndUnknownOperatorsReportedUnmapped(t *testing.T) {
	// These constructs live in the edge corpus (unmappable by design).
	// The contract: they appear in unmapped, never silently dropped.
	_, unmapped := corpus(t, "edge-cases.json")
	seen := map[string]bool{}
	for _, u := range unmapped {
		seen[u.Type+"/"+u.Flag] = true
	}
	if !seen["segment-unbounded/big-segment"] {
		t.Fatalf("unbounded segment must be reported unmapped: %+v", unmapped)
	}
	if !seen["clause-operator/semver-flag"] {
		t.Fatalf("unknown operator must be reported unmapped: %+v", unmapped)
	}
	if !seen["bucketBy/bucketby-flag"] {
		t.Fatalf("bucketBy must be reported unmapped: %+v", unmapped)
	}
}

func TestFullCorpusFlagEnvOff(t *testing.T) {
	projects, _ := mustParseFull(t)
	// acme-legacy flag: off states preserved.
	c := flagByKey(t, projects[2], "legacy-experiment")
	prod := c.Environments["production"]
	if !prod.On || len(prod.Rules) != 1 {
		t.Fatalf("legacy-experiment prod = %+v", prod)
	}
}

// --- helpers ---

func flagByKey(t *testing.T, p model.Project, key string) model.Flag {
	t.Helper()
	for _, f := range p.Flags {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("flag %s not found in project %s", key, p.Key)
	return model.Flag{}
}

func mustParseUnmapped(t *testing.T) []unmappedView {
	_, u := mustParseFull(t)
	return u
}

func hasUnmapped(list []unmappedView, project, flag, rule, typ string) bool {
	for _, u := range list {
		if u.Project == project && u.Flag == flag && u.Rule == rule && u.Type == typ {
			return true
		}
	}
	return false
}
