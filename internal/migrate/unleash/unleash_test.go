package unleash

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/model"
)

func corpus(t *testing.T, name string) ([]model.Project, []migrate.Unmapped) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", "unleash", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	projects, unmapped, err := Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return projects, unmapped
}

func flagByKey(t *testing.T, p model.Project, key string) model.Flag {
	t.Helper()
	for _, f := range p.Flags {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("flag %s not found", key)
	return model.Flag{}
}

// --- Representative corpus: mapping semantics ---

func TestRepresentativeProjectShape(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}
	p := projects[0]
	if p.Key != "default" || len(p.Flags) != 8 || len(p.Segments) != 1 {
		t.Fatalf("shape = %d flags, %d segments", len(p.Flags), len(p.Segments))
	}
	if len(p.Environments) != 2 || p.Environments[0].Key != "development" {
		t.Fatalf("environments = %+v", p.Environments)
	}
}

func TestDefaultStrategyEnablesEveryone(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	f := flagByKey(t, projects[0], "checkouts-v2")
	dev := f.Environments["development"]
	if !dev.On || len(dev.Rules) != 0 || dev.Rollout != nil {
		t.Fatalf("default strategy -> on, no rules, no rollout: %+v", dev)
	}
	prod := f.Environments["production"]
	if prod.On {
		t.Fatal("disabled environment must map to off")
	}
}

func TestFlexibleRolloutMapsToPercentage(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	f := flagByKey(t, projects[0], "api-rate-limit")
	prod := f.Environments["production"]
	if !prod.On {
		t.Fatal("enabled env must be on")
	}
	if prod.Rollout == nil || prod.Rollout.Kind != "percentage" || prod.Rollout.Percentage != 50 {
		t.Fatalf("flexibleRollout -> fallthrough percentage 50: %+v", prod.Rollout)
	}
	// Constraint on the rollout strategy becomes a rule? No — Unleash
	// applies constraints to the whole strategy, which IS the fallthrough.
	// v0.1 maps constraints onto the flag env as a pre-rollout rule only
	// when they exist; here the IN[production] constraint is satisfied by
	// the env itself and drops out. See TestConstraintsBecomeRules.
	if len(prod.Rules) != 1 {
		t.Fatalf("userWithId strategy must be its own rule; rules = %d", len(prod.Rules))
	}
	r := prod.Rules[0]
	if len(r.Clauses) != 1 || r.Clauses[0].Attribute != "targetingKey" || r.Clauses[0].Operator != "in" {
		t.Fatalf("userWithId -> targetingKey in rule: %+v", r)
	}
	if len(r.Clauses[0].Values) != 2 || r.Clauses[0].Values[0] != "vip-1" {
		t.Fatalf("userIds comma-split: %+v", r.Clauses[0].Values)
	}
}

func TestSegmentsResolveByID(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	// Segment 1 imported as segmentMatch rule on beta-dashboard.
	f := flagByKey(t, projects[0], "beta-dashboard")
	prod := f.Environments["production"]
	if len(prod.Rules) != 1 {
		t.Fatalf("rules = %d, want 1 (constraint + segment combined)", len(prod.Rules))
	}
	r := prod.Rules[0]
	seen := map[string]bool{}
	for _, c := range r.Clauses {
		if c.Operator == "segmentMatch" {
			seen["seg"] = len(c.Values) == 1 && c.Values[0] == "Beta users"
		}
		if c.Attribute == "country" {
			seen["country"] = c.Operator == "in"
		}
	}
	if !seen["seg"] || !seen["country"] {
		t.Fatalf("segment+constraint rule = %+v", r)
	}
	// And the segment itself must exist in the project.
	found := false
	for _, s := range projects[0].Segments {
		if s.Key == "Beta users" && len(s.Rules) == 1 && s.Rules[0].Clauses[0].Operator == "endsWith" {
			found = true
		}
	}
	if !found {
		t.Fatalf("segment Beta users not imported: %+v", projects[0].Segments)
	}
}

func TestSingleVariantPayloadBecomesValue(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	c := flagByKey(t, projects[0], "checkout-copy")
	if c.Kind != model.KindString {
		t.Fatalf("checkout-copy kind = %s", c.Kind)
	}
	if c.Environments["production"].Value != "legacy-copy" {
		t.Fatalf("string variant value = %v", c.Environments["production"].Value)
	}
	n := flagByKey(t, projects[0], "search-timeout")
	if n.Kind != model.KindNumber {
		t.Fatalf("search-timeout kind = %s", n.Kind)
	}
	if n.Environments["production"].Value != float64(1500) {
		t.Fatalf("number variant value = %v (%T)", n.Environments["production"].Value, n.Environments["production"].Value)
	}
	j := flagByKey(t, projects[0], "recommender-config")
	if j.Kind != model.KindJSON {
		t.Fatalf("recommender-config kind = %s", j.Kind)
	}
	m, ok := j.Environments["production"].Value.(map[string]any)
	if !ok || m["model"] != "v1" {
		t.Fatalf("json variant value = %+v", j.Environments["production"].Value)
	}
}

func TestEnabledNoStrategiesServesNobody(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	f := flagByKey(t, projects[0], "dark-launch")
	prod := f.Environments["production"]
	if !prod.On || prod.Rollout == nil || prod.Rollout.Percentage != 0 {
		t.Fatalf("enabled + zero strategies must map to 0%% rollout: %+v", prod)
	}
}

func TestConstraintOperatorsMap(t *testing.T) {
	projects, _ := corpus(t, "unleash-representative.json")
	f := flagByKey(t, projects[0], "growth-experiments")
	prod := f.Environments["production"]
	ops := map[string]bool{}
	for _, r := range prod.Rules {
		for _, c := range r.Clauses {
			ops[c.Operator] = true
		}
	}
	for _, want := range []string{"startsWith", "greaterThan", "after", "matches"} {
		if !ops[want] {
			t.Fatalf("constraint operator %s not mapped; got %+v", want, ops)
		}
	}
}

func TestRepresentativeCorpusFidelityIsPerfect(t *testing.T) {
	projects, unmapped := corpus(t, "unleash-representative.json")
	r := migrate.FidelityReport("unleash", projects, unmapped)
	if r.Score != 1.0 {
		t.Fatalf("representative corpus must map 100%%, got %.1f%%:\n%s", r.Score*100, r.Markdown())
	}
	if !r.Pass(0.9) {
		t.Fatal("must pass the 90% gate")
	}
}

// --- Edge corpus: every gap type reports ---

func TestEdgeCorpusReportsEveryGapType(t *testing.T) {
	_, unmapped := corpus(t, "unleash-edge.json")
	seen := map[string]bool{}
	for _, u := range unmapped {
		seen[u.Type] = true
	}
	for _, want := range []string{
		"strategy-random-rollout", // gradualRolloutRandom
		"stickiness",              // clientId
		"bucketBy",                // custom groupId
		"strategy-unsupported",    // custom strategy name
		"constraint-operator",     // SEMVER_GT
		"weighted-variants",       // multi-variant weight split
		"constraint-case-insensitive",
		"segment-missing", // unresolved segment id
	} {
		if !seen[want] {
			t.Fatalf("edge corpus missing gap type %q; unmapped = %+v", want, unmapped)
		}
	}
}

func TestEdgeCorpusFidelityIsLowAndHonest(t *testing.T) {
	projects, unmapped := corpus(t, "unleash-edge.json")
	r := migrate.FidelityReport("unleash", projects, unmapped)
	if r.Score >= 0.9 {
		t.Fatalf("edge corpus fidelity = %.1f%%, should be below gate by design", r.Score*100)
	}
}

// --- Parse errors ---

func TestParseRejectsMalformedJSON(t *testing.T) {
	if _, _, err := Parse([]byte("{ not json")); err == nil {
		t.Fatal("must reject malformed JSON")
	}
}

func TestParseEmptyExport(t *testing.T) {
	projects, unmapped, err := Parse([]byte(`{"version": 5, "features": []}`))
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if len(projects) != 0 || len(unmapped) != 0 {
		t.Fatalf("empty export = %d/%d, want 0/0", len(projects), len(unmapped))
	}
}
