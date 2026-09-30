package eval

// prereq.go: prerequisite enforcement. A flag with prerequisites only
// serves when every parent flag resolves to the required variation in the
// same environment. Missing parents, depth overruns, and cycles all fail
// closed (child off, reason "prerequisite") — never a loop, never an
// implicit serve.

import (
	"github.com/switchyard/switchyard/internal/model"
)

// defaultPrereqDepth bounds the prerequisite chain. LD documents nesting
// limits; 4 matches what real exports contain without punishing depth.
const defaultPrereqDepth = 4

// EvaluateWithPrereqs resolves a flag whose prerequisites must be checked
// against the sibling flag set (all flags, by key). maxDepth < 0 means
// the default bound (4); 0 disables prerequisite checking entirely
// (legacy callers); > 0 bounds the chain depth.
func EvaluateWithPrereqs(f model.Flag, env string, segments map[string]model.Segment, ctx Context, all map[string]model.Flag, maxDepth int) Decision {
	if maxDepth == 0 {
		// Checking disabled: fall back to plain evaluation (the
		// pre-enforcement behavior; also the documented v0.1 default for
		// callers that haven't opted in).
		return Evaluate(f, env, segments, ctx)
	}
	if maxDepth < 0 {
		maxDepth = defaultPrereqDepth
	}
	if !prereqsSatisfied(f, env, segments, ctx, all, maxDepth, nil) {
		return Decision{Enabled: false, Reason: "prerequisite", Value: false, Variant: "off"}
	}
	return Evaluate(f, env, segments, ctx)
}

// prereqsSatisfied walks the parents of f (bounded, cycle-safe).
// visited is the chain of flag keys from the original child; a repeat
// means a cycle -> unsatisfied.
func prereqsSatisfied(f model.Flag, env string, segments map[string]model.Segment, ctx Context, all map[string]model.Flag, depth int, visited []string) bool {
	if depth <= 0 {
		return false // chain too deep: fail closed
	}
	fe, ok := f.Environments[env]
	if !ok {
		return true // no env config: Evaluate reports off; nothing to gate
	}
	if len(fe.Prerequisites) == 0 {
		return true // vacuously satisfied
	}
	for _, pr := range fe.Prerequisites {
		key := pr.Flag
		// Cycle check: if this parent is already in the chain, fail.
		for _, v := range visited {
			if v == key {
				return false
			}
		}
		parent, ok := all[key]
		if !ok {
			return false // missing parent: fail closed
		}
		// The parent itself must satisfy ITS prerequisites first.
		if !prereqsSatisfied(parent, env, segments, ctx, all, depth-1, append(visited, key)) {
			return false
		}
		// Parent resolves; check which variation it serves.
		pd := Evaluate(parent, env, segments, ctx)
		if pd.ErrorCode != "" {
			return false // misconfigured parent: fail closed
		}
		if !servesVariation(pd, parent, pr.Variation) {
			return false
		}
	}
	return true
}

// servesVariation maps a parent's decision to the LD variation index the
// prerequisite demands. Booleans: variation 0 = true/on, 1 = false/off.
// Non-boolean parents compare the served value to the variation at that
// index — but non-boolean parents carry no variations[] in the Switchyard
// model, so only the boolean mapping is defined; anything else fails.
func servesVariation(pd Decision, parent model.Flag, variation int) bool {
	if parent.Kind != model.KindBoolean {
		return false
	}
	switch variation {
	case 0:
		return pd.Enabled
	case 1:
		return !pd.Enabled
	default:
		return false
	}
}
