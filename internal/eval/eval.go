// Package eval implements local flag evaluation. This is the hot path and
// the product: evaluation must work offline, in-process, with zero network
// calls, forever. Cloud never gates it.
package eval

import (
	"crypto/sha256"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"time"

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
// so SDK consumers can log why a flag resolved the way it did. Value carries
// the configured payload for non-boolean kinds; Variant names what was
// served ("on"/"off" in v0.1). ErrorCode is set (OpenFeature-style) when a
// flag is misconfigured — the caller should fall back to its default.
type Decision struct {
	Enabled   bool   `json:"enabled"`
	Value     any    `json:"value"`
	Variant   string `json:"variant"`
	Reason    string `json:"reason"`
	ErrorCode string `json:"errorCode,omitempty"`
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
// percentage; else fallthrough enabled. The decision carries the configured
// value: booleans serve true (variant "on") when enabled, false (variant
// "off") when disabled; non-boolean kinds serve FlagEnvironment.Value, and
// an enabled non-boolean flag with no configured value reports
// ErrorCode PARSE_ERROR — the caller must take its own default.
func Evaluate(f model.Flag, env string, segments map[string]model.Segment, ctx Context) Decision {
	fe, ok := f.Environments[env]
	if !ok || !fe.On {
		return Decision{Enabled: false, Reason: "off", Value: false, Variant: "off"}
	}
	var d Decision
	for _, r := range fe.Rules {
		if ruleMatches(r, f.Key, ctx, segments) {
			d = Decision{Enabled: true, Reason: "rule:" + r.ID}
			return withValue(d, f, fe)
		}
	}
	if fe.Rollout != nil && fe.Rollout.Kind == "percentage" {
		if Bucket(f.Key, ctx.UserKey) < fe.Rollout.Percentage {
			return withValue(Decision{Enabled: true, Reason: "rollout"}, f, fe)
		}
		return Decision{Enabled: false, Reason: "rollout", Value: false, Variant: "off"}
	}
	return withValue(Decision{Enabled: true, Reason: "fallthrough"}, f, fe)
}

// withValue attaches the configured payload per flag kind. A non-boolean
// flag with no value is misconfigured: PARSE_ERROR, nil value, caller
// takes its default.
func withValue(d Decision, f model.Flag, fe model.FlagEnvironment) Decision {
	switch f.Kind {
	case model.KindBoolean:
		d.Value = true
		d.Variant = "on"
	case model.KindString, model.KindNumber, model.KindJSON:
		if fe.Value == nil {
			d.ErrorCode = "PARSE_ERROR"
			d.Value = nil
			return d
		}
		d.Value = fe.Value
		d.Variant = "on"
	default:
		d.ErrorCode = "PARSE_ERROR"
		d.Value = nil
	}
	return d
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
	matched := clauseMatchesPositive(c, ctx, segments)
	if c.Negate {
		return !matched
	}
	return matched
}

// attrValue resolves a clause attribute, treating the targeting-key aliases
// as the evaluation user key so imported user-targeting rules work.
func attrValue(c model.Clause, ctx Context) (string, bool) {
	switch c.Attribute {
	case "targetingKey", "userKey", "key":
		return ctx.UserKey, ctx.UserKey != ""
	default:
		v, ok := ctx.Attributes[c.Attribute]
		return v, ok
	}
}

func clauseMatchesPositive(c model.Clause, ctx Context, segments map[string]model.Segment) bool {
	switch c.Operator {
	case "segmentMatch":
		for _, segKey := range c.Values {
			if segmentMatches(segments[segKey], ctx) {
				return true
			}
		}
		return false
	case "in":
		v, ok := attrValue(c, ctx)
		if !ok {
			return false
		}
		return contains(c.Values, v)
	case "startsWith", "endsWith", "contains":
		v, ok := attrValue(c, ctx)
		if !ok {
			return false
		}
		for _, w := range c.Values {
			switch c.Operator {
			case "startsWith":
				if strings.HasPrefix(v, w) {
					return true
				}
			case "endsWith":
				if strings.HasSuffix(v, w) {
					return true
				}
			case "contains":
				if strings.Contains(v, w) {
					return true
				}
			}
		}
		return false
	case "matches":
		v, ok := attrValue(c, ctx)
		if !ok {
			return false
		}
		for _, pattern := range c.Values {
			re, err := regexp.Compile(pattern)
			if err != nil {
				continue // invalid pattern never matches; migrator reports it
			}
			if re.MatchString(v) {
				return true
			}
		}
		return false
	case "before", "after":
		v, ok := attrValue(c, ctx)
		if !ok {
			return false
		}
		attrT, err1 := parseTime(v)
		valT, err2 := parseTime(c.Values[0])
		if err1 != nil || err2 != nil {
			return false // fail closed on unparseable dates
		}
		if c.Operator == "before" {
			return attrT.Before(valT)
		}
		return attrT.After(valT)
	case "greaterThan", "greaterThanOrEqual", "lessThan", "lessThanOrEqual":
		v, ok := attrValue(c, ctx)
		if !ok {
			return false
		}
		attrN, err1 := strconv.ParseFloat(v, 64)
		valN, err2 := strconv.ParseFloat(c.Values[0], 64)
		if err1 != nil || err2 != nil {
			return false // fail closed on non-numbers
		}
		switch c.Operator {
		case "greaterThan":
			return attrN > valN
		case "greaterThanOrEqual":
			return attrN >= valN
		case "lessThan":
			return attrN < valN
		case "lessThanOrEqual":
			return attrN <= valN
		}
		return false
	default:
		// Unknown operator: never match. The migrator already reported it
		// as unmapped; failing closed is the safe behavior.
		return false
	}
}

// parseTime accepts RFC 3339 dates (LD's semantic for before/after).
func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
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
