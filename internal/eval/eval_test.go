package eval

import (
	"fmt"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

// flag builds a boolean flag configured for the production environment.
func flag(key string, on bool, rolloutPct *int, rules ...model.Rule) model.Flag {
	fe := model.FlagEnvironment{On: on, Rules: rules}
	if rolloutPct != nil {
		fe.Rollout = &model.Rollout{Kind: "percentage", Percentage: *rolloutPct}
	}
	return model.Flag{
		Key:  key,
		Name: key,
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": fe,
		},
	}
}

func pct(n int) *int { return &n }

func clause(attr, op string, values ...string) model.Clause {
	return model.Clause{Attribute: attr, Operator: op, Values: values}
}

func TestOffFlagReturnsFalseWithReasonOff(t *testing.T) {
	d := Evaluate(flag("f", false, nil), "production", nil, Context{UserKey: "alice"})
	if d.Enabled || d.Reason != "off" {
		t.Fatalf("got %+v, want {false off}", d)
	}
}

func TestEnvWithNoConfigIsTreatedAsOff(t *testing.T) {
	// Flag configured only for dev; evaluating production must be off,
	// never a panic or an implicit true.
	devOnly := model.Flag{
		Key:  "f",
		Name: "f",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"dev": {On: true},
		},
	}
	d := Evaluate(devOnly, "production", nil, Context{UserKey: "alice"})
	if d.Enabled || d.Reason != "off" {
		t.Fatalf("production eval on dev-only flag = %+v, want {false off}", d)
	}
	// Sanity: dev itself is on.
	d = Evaluate(devOnly, "dev", nil, Context{UserKey: "alice"})
	if !d.Enabled {
		t.Fatalf("dev eval = %+v, want enabled", d)
	}
}

func TestOnFlagWithoutRolloutServesEveryone(t *testing.T) {
	for _, u := range []string{"alice", "bob", "carol"} {
		d := Evaluate(flag("f", true, nil), "production", nil, Context{UserKey: u})
		if !d.Enabled || d.Reason != "fallthrough" {
			t.Fatalf("user %s: got %+v, want {true fallthrough}", u, d)
		}
	}
}

func TestBucketIsDeterministicAndBounded(t *testing.T) {
	b1 := Bucket("f", "alice")
	b2 := Bucket("f", "alice")
	if b1 != b2 {
		t.Fatalf("bucket not deterministic: %d vs %d", b1, b2)
	}
	if b1 < 0 || b1 > 99 {
		t.Fatalf("bucket %d out of [0,100)", b1)
	}
}

func TestBucketSpreadsAcrossUsers(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 20; i++ {
		seen[Bucket("f", fmt.Sprintf("user-%d", i))] = true
	}
	if len(seen) < 2 {
		t.Fatalf("20 users collapsed into %d buckets — bucketing is broken", len(seen))
	}
}

func TestHundredPercentRolloutEnablesEveryone(t *testing.T) {
	f := flag("f", true, pct(100))
	for i := 0; i < 10; i++ {
		d := Evaluate(f, "production", nil, Context{UserKey: fmt.Sprintf("user-%d", i)})
		if !d.Enabled {
			t.Fatalf("user-%d disabled under 100%% rollout: %+v", i, d)
		}
	}
}

func TestZeroPercentRolloutDisablesEveryone(t *testing.T) {
	f := flag("f", true, pct(0))
	for i := 0; i < 10; i++ {
		d := Evaluate(f, "production", nil, Context{UserKey: fmt.Sprintf("user-%d", i)})
		if d.Enabled {
			t.Fatalf("user-%d enabled under 0%% rollout: %+v", i, d)
		}
	}
}

func TestRuleClauseInMatches(t *testing.T) {
	f := flag("f", true, pct(0), model.Rule{
		ID:      "r-pro",
		Clauses: []model.Clause{clause("plan", "in", "pro")},
	})
	d := Evaluate(f, "production", nil, Context{UserKey: "alice", Attributes: map[string]string{"plan": "pro"}})
	if !d.Enabled || d.Reason != "rule:r-pro" {
		t.Fatalf("got %+v, want {true rule:r-pro}", d)
	}
}

func TestRuleClauseInNoMatchFallsThrough(t *testing.T) {
	f := flag("f", true, nil, model.Rule{
		ID:      "r-pro",
		Clauses: []model.Clause{clause("plan", "in", "pro")},
	})
	d := Evaluate(f, "production", nil, Context{UserKey: "alice", Attributes: map[string]string{"plan": "free"}})
	if !d.Enabled || d.Reason != "fallthrough" {
		t.Fatalf("got %+v, want fallthrough (flag on, no fallthrough rollout)", d)
	}
}

func TestRuleClauseStartsWith(t *testing.T) {
	f := flag("f", true, pct(0), model.Rule{
		ID:      "r-internal",
		Clauses: []model.Clause{clause("email", "startsWith", "admin@")},
	})
	d := Evaluate(f, "production", nil, Context{UserKey: "a", Attributes: map[string]string{"email": "admin@corp.test"}})
	if !d.Enabled {
		t.Fatalf("startsWith match failed: %+v", d)
	}
	d = Evaluate(f, "production", nil, Context{UserKey: "a", Attributes: map[string]string{"email": "user@corp.test"}})
	if d.Enabled {
		t.Fatalf("startsWith should not match: %+v", d)
	}
}

func TestRuleClauseEndsWith(t *testing.T) {
	f := flag("f", true, pct(0), model.Rule{
		ID:      "r-acme",
		Clauses: []model.Clause{clause("email", "endsWith", "@acme.test")},
	})
	d := Evaluate(f, "production", nil, Context{UserKey: "a", Attributes: map[string]string{"email": "joe@acme.test"}})
	if !d.Enabled {
		t.Fatalf("endsWith match failed: %+v", d)
	}
}

func TestRuleRequiresAllClausesToMatch(t *testing.T) {
	f := flag("f", true, pct(0), model.Rule{
		ID: "r-both",
		Clauses: []model.Clause{
			clause("plan", "in", "pro"),
			clause("email", "endsWith", "@acme.test"),
		},
	})
	// Matches only the first clause -> rule must not fire.
	d := Evaluate(f, "production", nil, Context{UserKey: "a", Attributes: map[string]string{"plan": "pro", "email": "x@other.test"}})
	if d.Enabled {
		t.Fatalf("rule fired with only 1/2 clauses matching: %+v", d)
	}
	// Both clauses -> fires.
	d = Evaluate(f, "production", nil, Context{UserKey: "a", Attributes: map[string]string{"plan": "pro", "email": "x@acme.test"}})
	if !d.Enabled || d.Reason != "rule:r-both" {
		t.Fatalf("both clauses matched but rule did not fire: %+v", d)
	}
}

func TestSegmentMatchRule(t *testing.T) {
	segments := map[string]model.Segment{
		"beta-users": {
			Key:  "beta-users",
			Name: "Beta users",
			Rules: []model.Rule{{
				ID:      "seg-1",
				Clauses: []model.Clause{clause("email", "endsWith", "@beta.acme.test")},
			}},
		},
	}
	f := flag("f", true, pct(0), model.Rule{
		ID:      "r-beta",
		Clauses: []model.Clause{clause("segment", "segmentMatch", "beta-users")},
	})
	in := Evaluate(f, "production", segments, Context{UserKey: "a", Attributes: map[string]string{"email": "j@beta.acme.test"}})
	if !in.Enabled || in.Reason != "rule:r-beta" {
		t.Fatalf("segment member not matched: %+v", in)
	}
	out := Evaluate(f, "production", segments, Context{UserKey: "a", Attributes: map[string]string{"email": "j@elsewhere.test"}})
	if out.Enabled {
		t.Fatalf("non-member matched: %+v", out)
	}
}

func TestRuleRolloutBucketsUsersWithinRule(t *testing.T) {
	// Rule rollout 30%: users bucketing below 30 match the rule, the rest
	// fall through. Find one user of each kind via the exported Bucket.
	f := flag("f", true, nil, model.Rule{
		ID:      "r-gradual",
		Clauses: []model.Clause{clause("plan", "in", "pro")},
		Rollout: &model.Rollout{Kind: "percentage", Percentage: 30},
	})
	var below, above string
	for i := 0; i < 100 && (below == "" || above == ""); i++ {
		u := fmt.Sprintf("user-%d", i)
		if Bucket("f", u) < 30 {
			below = u
		} else {
			above = u
		}
	}
	dIn := Evaluate(f, "production", nil, Context{UserKey: below, Attributes: map[string]string{"plan": "pro"}})
	if !dIn.Enabled || dIn.Reason != "rule:r-gradual" {
		t.Fatalf("user below bucket line: %+v", dIn)
	}
	dOut := Evaluate(f, "production", nil, Context{UserKey: above, Attributes: map[string]string{"plan": "pro"}})
	// Bucketed out of the rule -> falls through; flag on with no fallthrough rollout -> enabled.
	if !dOut.Enabled || dOut.Reason != "fallthrough" {
		t.Fatalf("user above bucket line: %+v", dOut)
	}
}

func TestMissingAttributeNeverMatches(t *testing.T) {
	f := flag("f", true, pct(0), model.Rule{
		ID:      "r",
		Clauses: []model.Clause{clause("plan", "in", "pro")},
	})
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if d.Enabled {
		t.Fatalf("missing attribute matched a clause: %+v", d)
	}
}
