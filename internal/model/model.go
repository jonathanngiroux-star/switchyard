// Package model defines the core Switchyard data model:
// projects, environments, flags, segments, rules, and rollouts.
//
// This is the model every importer (LaunchDarkly, Unleash) must map into
// and every SDK generator must map out of. Fidelity is measured against it.
package model

// Kind is the value type a flag holds.
type Kind string

const (
	KindBoolean Kind = "boolean"
	KindString  Kind = "string"
	KindNumber  Kind = "number"
	KindJSON    Kind = "json"
)

// Environment is a named deployment environment (dev, staging, production).
type Environment struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Clause is a single matching predicate over a context attribute.
// The attribute "targetingKey" (aliases: "userKey", "key") resolves to the
// evaluation user key, so imported user-targeting rules keep working.
type Clause struct {
	Attribute string   `json:"attribute"`
	Operator  string   `json:"operator"` // see eval.clauseMatches for the supported set
	Values    []string `json:"values,omitempty"`
	Negate    bool     `json:"negate,omitempty"`
}

// Rollout is a percentage rollout configuration.
type Rollout struct {
	Kind       string `json:"kind"` // "percentage" for now
	Percentage int    `json:"percentage"`
}

// Rule is an ordered targeting rule made of clauses, an optional rollout,
// and the value served when the rule matches (resolved from the source
// system's variation index at import time).
type Rule struct {
	ID      string   `json:"id"`
	Clauses []Clause `json:"clauses,omitempty"`
	Rollout *Rollout `json:"rollout,omitempty"`
	Value   any      `json:"value,omitempty"`
}

// Segment is a reusable audience definition.
type Segment struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Rules []Rule `json:"rules,omitempty"`
}

// Prerequisite is a flag dependency: this flag should only serve when the
// parent flag is serving the given variation. Imported from LD flag-level
// prerequisites; the evaluator does not enforce them yet (documented gap in
// docs/fidelity/launchdarkly.md).
type Prerequisite struct {
	Flag      string `json:"flag"`
	Variation int    `json:"variation"`
}

// FlagEnvironment is a flag's per-environment configuration. Value holds
// the configured variant payload for non-boolean kinds (or nil for
// booleans, where On carries the meaning).
type FlagEnvironment struct {
	On            bool           `json:"on"`
	Value         any            `json:"value,omitempty"`
	Rollout       *Rollout       `json:"rollout,omitempty"`
	Rules         []Rule         `json:"rules,omitempty"`
	Prerequisites []Prerequisite `json:"prerequisites,omitempty"`
}

// Flag is a feature flag and its per-environment configuration.
type Flag struct {
	Key          string                     `json:"key"`
	Name         string                     `json:"name"`
	Kind         Kind                       `json:"kind"`
	Environments map[string]FlagEnvironment `json:"environments"`
}

// Project is the top-level container: environments, flags, segments.
type Project struct {
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	Environments []Environment `json:"environments,omitempty"`
	Flags        []Flag        `json:"flags,omitempty"`
	Segments     []Segment     `json:"segments,omitempty"`
}
