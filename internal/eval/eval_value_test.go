package eval

import (
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func typedFlag(kind model.Kind, value any) model.Flag {
	return model.Flag{
		Key:  "typed",
		Name: "typed",
		Kind: kind,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: value},
		},
	}
}

func TestBooleanOnServesTrueValueAndOnVariant(t *testing.T) {
	f := typedFlag(model.KindBoolean, nil)
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if !d.Enabled || d.Value != true || d.Variant != "on" {
		t.Fatalf("boolean on = %+v, want {Enabled:true Value:true Variant:on}", d)
	}
}

func TestBooleanOffServesFalseValueAndOffVariant(t *testing.T) {
	f := typedFlag(model.KindBoolean, nil)
	f.Environments["production"] = model.FlagEnvironment{On: false}
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if d.Enabled || d.Value != false || d.Variant != "off" {
		t.Fatalf("boolean off = %+v, want {Enabled:false Value:false Variant:off}", d)
	}
}

func TestStringFlagServesConfiguredValue(t *testing.T) {
	f := typedFlag(model.KindString, "checkout-flow-v2")
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if !d.Enabled || d.Value != "checkout-flow-v2" || d.Variant != "on" {
		t.Fatalf("string flag = %+v", d)
	}
}

func TestNumberFlagServesConfiguredValue(t *testing.T) {
	// JSON numbers decode as float64; evaluation normalizes to float64.
	f := typedFlag(model.KindNumber, float64(42))
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if !d.Enabled || d.Value != float64(42) {
		t.Fatalf("number flag = %+v", d)
	}
}

func TestJSONFlagServesStructuredValue(t *testing.T) {
	f := typedFlag(model.KindJSON, map[string]any{"retries": 3})
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	m, ok := d.Value.(map[string]any)
	if !ok || m["retries"] != 3 {
		t.Fatalf("json flag value = %+v (%T)", d.Value, d.Value)
	}
}

func TestNonBooleanOnWithoutValueIsMisconfigured(t *testing.T) {
	// Kind string, no Value set, flag on: must NOT silently serve nil as a
	// legit value — report a parse error so the caller takes its default.
	f := model.Flag{
		Key:  "broken",
		Kind: model.KindString,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true},
		},
	}
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if !d.Enabled || d.Value != nil || d.ErrorCode != "PARSE_ERROR" {
		t.Fatalf("misconfigured string flag = %+v, want ErrorCode PARSE_ERROR and nil Value", d)
	}
}

func TestOffFlagHasEmptyErrorCode(t *testing.T) {
	f := typedFlag(model.KindString, "x")
	f.Environments["production"] = model.FlagEnvironment{On: false}
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if d.ErrorCode != "" || d.Variant != "off" || d.Value != false {
		t.Fatalf("off string flag = %+v, want {false off false} with no error", d)
	}
}

func TestRolloutServesValueWithinPercentage(t *testing.T) {
	f := model.Flag{
		Key:  "gradual",
		Kind: model.KindString,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: "new-copy", Rollout: &model.Rollout{Kind: "percentage", Percentage: 100}},
		},
	}
	d := Evaluate(f, "production", nil, Context{UserKey: "anyone"})
	if !d.Enabled || d.Value != "new-copy" || d.Reason != "rollout" {
		t.Fatalf("100%% rollout = %+v, want value new-copy reason rollout", d)
	}
}

func TestRuleServesValueOnMatch(t *testing.T) {
	f := model.Flag{
		Key:  "tiered",
		Kind: model.KindNumber,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:    true,
				Value: 99,
				Rules: []model.Rule{{
					ID:      "r-pro",
					Clauses: []model.Clause{{Attribute: "plan", Operator: "in", Values: []string{"pro"}}},
				}},
			},
		},
	}
	in := Evaluate(f, "production", nil, Context{UserKey: "a", Attributes: map[string]string{"plan": "pro"}})
	if !in.Enabled || in.Value != 99 || in.Reason != "rule:r-pro" {
		t.Fatalf("rule match = %+v, want value 99 rule:r-pro", in)
	}
}

func TestValueNotServedWhenFlagOff(t *testing.T) {
	f := typedFlag(model.KindJSON, map[string]any{"x": 1})
	f.Environments["production"] = model.FlagEnvironment{On: false, Value: f.Environments["production"].Value}
	d := Evaluate(f, "production", nil, Context{UserKey: "alice"})
	if d.Enabled || d.Value != false || d.Variant != "off" {
		t.Fatalf("off json flag = %+v, want disabled with false value", d)
	}
}
