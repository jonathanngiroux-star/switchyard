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
type Clause struct {
	Attribute string   `json:"attribute"`
	Operator  string   `json:"operator"` // segmentMatch | in | startsWith | endsWith (v0.1 set)
	Values    []string `json:"values,omitempty"`
}

// Rollout is a percentage rollout configuration.
type Rollout struct {
	Kind       string `json:"kind"` // "percentage" for now
	Percentage int    `json:"percentage"`
}

// Rule is an ordered targeting rule made of clauses and an optional rollout.
type Rule struct {
	ID      string   `json:"id"`
	Clauses []Clause `json:"clauses,omitempty"`
	Rollout *Rollout `json:"rollout,omitempty"`
}

// Segment is a reusable audience definition.
type Segment struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Rules []Rule `json:"rules,omitempty"`
}

// FlagEnvironment is a flag's per-environment configuration.
type FlagEnvironment struct {
	On      bool     `json:"on"`
	Rollout *Rollout `json:"rollout,omitempty"`
	Rules   []Rule   `json:"rules,omitempty"`
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
