// Package eval implements local flag evaluation. This is the hot path and
// the product: evaluation must work offline, in-process, with zero network
// calls, forever. Cloud never gates it.
package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"

	"github.com/switchyard/switchyard/internal/model"
)

// Context is the evaluation context: a stable user key plus attributes
// used by targeting rules. Attributes are strings in v0.1 — sufficient for
// the migrate-supported operators.
type Context struct {
	UserKey    string            `json:"userKey"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Decision is the outcome of one evaluation, with a machine-readable reason
// so SDK consumers can log why a flag resolved the way it did.
type Decision struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

// Bucket maps (flagKey, userKey) to a stable bucket in [0, 100).
// Deterministic across processes and languages: SHA-256 of
// "flagKey:userKey", first 8 bytes, mod 100. Same inputs — same bucket,
// always, everywhere.
func Bucket(flagKey, userKey string) int {
	h := sha256.Sum256([]byte(flagKey + ":" + userKey))
	return int(binary.BigEndian.Uint64(h[:8]) % 100)
}

// Evaluate resolves a flag for one environment and context. segments maps
// segment keys to definitions for segmentMatch clauses; nil/missing
// segments simply never match.
//
// Order: off wins; rules in order (first match wins); fallthrough rollout
// percentage; else fallthrough enabled.
func Evaluate(f model.Flag, env string, segments map[string]model.Segment, ctx Context) Decision {
	fe, ok := f.Environments[env]
	if !ok || !fe.On {
		return Decision{Enabled: false, Reason: "off"}
	}
	for _, r := range fe.Rules {
		if ruleMatches(r, f.Key, ctx, segments) {
			return Decision{Enabled: true, Reason: "rule:" + r.ID}
		}
	}
	if fe.Rollout != nil && fe.Rollout.Kind == "percentage" {
		if Bucket(f.Key, ctx.UserKey) < fe.Rollout.Percentage {
			return Decision{Enabled: true, Reason: "rollout"}
		}
		return Decision{Enabled: false, Reason: "rollout"}
	}
	return Decision{Enabled: true, Reason: "fallthrough"}
}

// ruleMatches: every clause must match, then the rule's own rollout (if
// any) buckets the user within the rule.
func ruleMatches(r model.Rule, flagKey string, ctx Context, segments map[string]model.Segment) bool {
	for _, c := range r.Clauses {
		if !clauseMatches(c, ctx, segments) {
			return false
		}
	}
	if r.Rollout != nil && r.Rollout.Kind == "percentage" {
		return Bucket(flagKey, ctx.UserKey) < r.Rollout.Percentage
	}
	return true
}

func clauseMatches(c model.Clause, ctx Context, segments map[string]model.Segment) bool {
	switch c.Operator {
	case "segmentMatch":
		for _, segKey := range c.Values {
			if segmentMatches(segments[segKey], ctx) {
				return true
			}
		}
		return false
	case "in":
		v, ok := ctx.Attributes[c.Attribute]
		if !ok {
			return false
		}
		return contains(c.Values, v)
	case "startsWith":
		v, ok := ctx.Attributes[c.Attribute]
		if !ok {
			return false
		}
		for _, w := range c.Values {
			if strings.HasPrefix(v, w) {
				return true
			}
		}
		return false
	case "endsWith":
		v, ok := ctx.Attributes[c.Attribute]
		if !ok {
			return false
		}
		for _, w := range c.Values {
			if strings.HasSuffix(v, w) {
				return true
			}
		}
		return false
	default:
		// Unknown operator: never match. The migrator already reported it
		// as unmapped; failing closed is the safe behavior.
		return false
	}
}

// segmentMatches: a context is in a segment if any of the segment's rules
// matches. Segment rules do not nest segments in v0.1.
func segmentMatches(seg model.Segment, ctx Context) bool {
	for _, r := range seg.Rules {
		if ruleMatches(r, "", ctx, nil) {
			return true
		}
	}
	return false
}

func contains(values []string, v string) bool {
	for _, w := range values {
		if w == v {
			return true
		}
	}
	return false
}
