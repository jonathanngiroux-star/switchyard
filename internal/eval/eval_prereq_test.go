package eval

import (
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

// prerequisiteCtx builds a flag set + segments map for prerequisite tests.
func withPrereqs(parentOn bool, parentVariant string) (model.Flag, map[string]model.Flag) {
	parent := model.Flag{
		Key:  "parent",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: parentOn, Value: parentVariant},
		},
	}
	child := model.Flag{
		Key:  "child",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:            true,
				Prerequisites: []model.Prerequisite{{Flag: "parent", Variation: 0}},
			},
		},
	}
	return child, map[string]model.Flag{"parent": parent}
}

func TestPrerequisiteParentOffDisablesChild(t *testing.T) {
	child, all := withPrereqs(false, "")
	d := EvaluateWithPrereqs(child, "production", nil, Context{UserKey: "alice"}, all, -1)
	if d.Enabled {
		t.Fatalf("parent off must disable child: %+v", d)
	}
	if d.Reason != "prerequisite" {
		t.Fatalf("reason = %q, want prerequisite", d.Reason)
	}
}

func TestPrerequisiteParentOnServesChild(t *testing.T) {
	child, all := withPrereqs(true, "")
	d := EvaluateWithPrereqs(child, "production", nil, Context{UserKey: "alice"}, all, 0)
	if !d.Enabled || d.Reason == "prerequisite" {
		t.Fatalf("parent on must serve child normally: %+v", d)
	}
}

func TestPrerequisiteMissingParentFailsClosed(t *testing.T) {
	child := model.Flag{
		Key:  "child",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:            true,
				Prerequisites: []model.Prerequisite{{Flag: "ghost-parent", Variation: 0}},
			},
		},
	}
	d := EvaluateWithPrereqs(child, "production", nil, Context{UserKey: "a"}, map[string]model.Flag{}, -1)
	if d.Enabled {
		t.Fatal("missing parent must fail closed")
	}
	if d.Reason != "prerequisite" {
		t.Fatalf("reason = %q, want prerequisite", d.Reason)
	}
}

func TestPrerequisiteChainDepthLimit(t *testing.T) {
	// a -> b -> child. Depth limit 4 must let this pass; depth limit 1
	// must cut it at b (child's parent b cannot resolve).
	depth4 := model.Flag{
		Key: "child", Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Prerequisites: []model.Prerequisite{{Flag: "b", Variation: 0}}},
		},
	}
	b := model.Flag{
		Key: "b", Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Prerequisites: []model.Prerequisite{{Flag: "a", Variation: 0}}},
		},
	}
	a := model.Flag{
		Key: "a", Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true},
		},
	}
	all := map[string]model.Flag{"child": depth4, "b": b, "a": a}

	d := EvaluateWithPrereqs(depth4, "production", nil, Context{UserKey: "x"}, all, 4)
	if !d.Enabled {
		t.Fatalf("depth-4 chain must resolve: %+v", d)
	}
	d = EvaluateWithPrereqs(depth4, "production", nil, Context{UserKey: "x"}, all, 1)
	if d.Enabled || d.Reason != "prerequisite" {
		t.Fatalf("depth-1 limit must cut the chain: %+v", d)
	}
}

func TestPrerequisiteCycleFailsClosed(t *testing.T) {
	x := model.Flag{
		Key: "x", Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Prerequisites: []model.Prerequisite{{Flag: "y", Variation: 0}}},
		},
	}
	y := model.Flag{
		Key: "y", Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Prerequisites: []model.Prerequisite{{Flag: "x", Variation: 0}}},
		},
	}
	all := map[string]model.Flag{"x": x, "y": y}
	d := EvaluateWithPrereqs(x, "production", nil, Context{UserKey: "u"}, all, 4)
	if d.Enabled {
		t.Fatal("cyclic prerequisites must fail closed, not loop forever")
	}
}

func TestPrerequisiteVariationMismatchDisables(t *testing.T) {
	// Boolean flags: variation 0 = on (true). Parent ON means variation 0
	// is served, satisfying the prerequisite. Test the negative: parent
	// ON but the prerequisite wants variation 1 (off) — unsatisfied.
	child := model.Flag{
		Key:  "child",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:            true,
				Prerequisites: []model.Prerequisite{{Flag: "parent", Variation: 1}},
			},
		},
	}
	parent := model.Flag{
		Key: "parent", Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true},
		},
	}
	d := EvaluateWithPrereqs(child, "production", nil, Context{UserKey: "a"}, map[string]model.Flag{"parent": parent}, 4)
	if d.Enabled {
		t.Fatal("parent serving variation 0 while prerequisite wants variation 1 must disable the child")
	}
}
