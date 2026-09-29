package eval

import (
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func ruleFlag(kind model.Kind, value any, rules ...model.Rule) model.Flag {
	// Rollout 0% on the flag: enabled IFF a rule fires. Operator tests
	// assert match/no-match, not fallthrough.
	fe := model.FlagEnvironment{On: true, Rules: rules, Rollout: &model.Rollout{Kind: "percentage", Percentage: 0}}
	if value != nil {
		fe.Value = value
	}
	return model.Flag{
		Key:  "f",
		Kind: kind,
		Environments: map[string]model.FlagEnvironment{
			"production": fe,
		},
	}
}

func nclause(attr, op string, negate bool, values ...string) model.Clause {
	return model.Clause{Attribute: attr, Operator: op, Values: values, Negate: negate}
}

func attrCtx(user string, kv ...string) Context {
	attrs := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		attrs[kv[i]] = kv[i+1]
	}
	return Context{UserKey: user, Attributes: attrs}
}

// --- Negate ---

func TestNegatedClauseFlipsMatch(t *testing.T) {
	f := ruleFlag(model.KindBoolean, nil, model.Rule{
		ID:      "r",
		Clauses: []model.Clause{nclause("plan", "in", true, "free")},
	})
	// plan=pro: clause "plan in [free]" is false, negated -> rule matches.
	if !Evaluate(f, "production", nil, attrCtx("a", "plan", "pro")).Enabled {
		t.Fatal("negated non-match must satisfy the rule")
	}
	// plan=free: clause true, negated -> rule must not fire.
	if Evaluate(f, "production", nil, attrCtx("a", "plan", "free")).Enabled {
		t.Fatal("negated match must fail the rule")
	}
}

// --- New operators ---

func TestContainsOperator(t *testing.T) {
	f := ruleFlag(model.KindBoolean, nil, model.Rule{
		ID:      "r",
		Clauses: []model.Clause{nclause("email", "contains", false, "@corp")},
	})
	if !Evaluate(f, "production", nil, attrCtx("a", "email", "joe@corp.test")).Enabled {
		t.Fatal("contains must match")
	}
	if Evaluate(f, "production", nil, attrCtx("a", "email", "joe@other.test")).Enabled {
		t.Fatal("contains must not match")
	}
}

func TestMatchesOperator(t *testing.T) {
	f := ruleFlag(model.KindBoolean, nil, model.Rule{
		ID:      "r",
		Clauses: []model.Clause{nclause("email", "matches", false, `^admin@.*\.test$`)},
	})
	if !Evaluate(f, "production", nil, attrCtx("a", "email", "admin@corp.test")).Enabled {
		t.Fatal("regex must match")
	}
	if Evaluate(f, "production", nil, attrCtx("a", "email", "user@corp.test")).Enabled {
		t.Fatal("regex must not match")
	}
}

func TestBeforeAfterOperators(t *testing.T) {
	f := ruleFlag(model.KindBoolean, nil, model.Rule{
		ID:      "r-early",
		Clauses: []model.Clause{nclause("memberSince", "before", false, "2024-01-01T00:00:00Z")},
	})
	if !Evaluate(f, "production", nil, attrCtx("a", "memberSince", "2023-06-01T00:00:00Z")).Enabled {
		t.Fatal("before must match an earlier date")
	}
	if Evaluate(f, "production", nil, attrCtx("a", "memberSince", "2024-06-01T00:00:00Z")).Enabled {
		t.Fatal("before must not match a later date")
	}
	f = ruleFlag(model.KindBoolean, nil, model.Rule{
		ID:      "r-late",
		Clauses: []model.Clause{nclause("memberSince", "after", false, "2024-01-01T00:00:00Z")},
	})
	if !Evaluate(f, "production", nil, attrCtx("a", "memberSince", "2024-06-01T00:00:00Z")).Enabled {
		t.Fatal("after must match a later date")
	}
	if Evaluate(f, "production", nil, attrCtx("a", "memberSince", "2023-06-01T00:00:00Z")).Enabled {
		t.Fatal("after must not match an earlier date")
	}
}

func TestNumericComparisonOperators(t *testing.T) {
	cases := []struct {
		op   string
		val  string
		attr string
		want bool
	}{
		{"greaterThan", "100", "150", true},
		{"greaterThan", "100", "50", false},
		{"greaterThanOrEqual", "100", "100", true},
		{"greaterThanOrEqual", "100", "99", false},
		{"lessThan", "100", "50", true},
		{"lessThan", "100", "150", false},
		{"lessThanOrEqual", "100", "100", true},
		{"lessThanOrEqual", "100", "101", false},
	}
	for _, c := range cases {
		f := ruleFlag(model.KindBoolean, nil, model.Rule{
			ID:      "r-" + c.op,
			Clauses: []model.Clause{nclause("age", c.op, false, c.val)},
		})
		got := Evaluate(f, "production", nil, attrCtx("a", "age", c.attr)).Enabled
		if got != c.want {
			t.Fatalf("%s(%s vs %s) = %v, want %v", c.op, c.attr, c.val, got, c.want)
		}
	}
}

func TestNumericOperatorNonNumericAttributeNeverMatches(t *testing.T) {
	f := ruleFlag(model.KindBoolean, nil, model.Rule{
		ID:      "r",
		Clauses: []model.Clause{nclause("age", "greaterThan", false, "10")},
	})
	if Evaluate(f, "production", nil, attrCtx("a", "age", "not-a-number")).Enabled {
		t.Fatal("non-numeric attribute must fail closed on numeric operators")
	}
}

// --- Targeting key resolution (targets-as-rules depends on it) ---

func TestTargetingKeyClauseMatchesUserKey(t *testing.T) {
	for _, attr := range []string{"targetingKey", "userKey", "key"} {
		f := ruleFlag(model.KindBoolean, nil, model.Rule{
			ID:      "r",
			Clauses: []model.Clause{nclause(attr, "in", false, "vip-1", "vip-2")},
		})
		if !Evaluate(f, "production", nil, Context{UserKey: "vip-1"}).Enabled {
			t.Fatalf("attribute %q must resolve to the user key", attr)
		}
		if Evaluate(f, "production", nil, Context{UserKey: "someone-else"}).Enabled {
			t.Fatalf("attribute %q must not match a different user", attr)
		}
	}
}

// --- Prerequisites are carried but not enforced (documented gap) ---

func TestPrerequisitesDoNotBreakEvaluation(t *testing.T) {
	f := model.Flag{
		Key:  "child",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:            true,
				Prerequisites: []model.Prerequisite{{Flag: "parent", Variation: 1}},
			},
		},
	}
	// Evaluation resolves normally; enforcement is a documented W5-7 gap.
	d := Evaluate(f, "production", nil, Context{UserKey: "a"})
	if !d.Enabled {
		t.Fatalf("prerequisites must not disable evaluation yet: %+v", d)
	}
}
