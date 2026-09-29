// Package migrate defines the migration contract shared by all importers:
// normalized model.Project trees plus machine-readable diffs.
package migrate

import (
	"github.com/switchyard/switchyard/internal/model"
)

// Unmapped documents a source construct Switchyard v0.1 cannot represent.
// Every gap must be listed explicitly — pretending 100% fidelity is the
// fastest way to lose a platform team's trust.
type Unmapped struct {
	Project string `json:"project,omitempty"`
	Flag    string `json:"flag,omitempty"`
	Rule    string `json:"rule,omitempty"`
	Type    string `json:"type"`
	Detail  string `json:"detail,omitempty"`
}

// Summary counts the diff sections. Contract tests pin these numbers.
type Summary struct {
	Added    int `json:"added"`
	Removed  int `json:"removed"`
	Changed  int `json:"changed"`
	Unmapped int `json:"unmapped"`
}

// Diff is the machine-readable dry-run output: what an import would add,
// remove, and change, plus every construct that does not map.
type Diff struct {
	Added    []model.Flag `json:"added"`
	Removed  []model.Flag `json:"removed"`
	Changed  []model.Flag `json:"changed"`
	Unmapped []Unmapped   `json:"unmapped"`
	Summary  Summary      `json:"summary"`
}

// Fidelity is the share of source flags fully represented in the diff
// output (added or changed), after subtracting unmapped constructs.
type Fidelity struct {
	Total    int     `json:"total_flags"`
	Mapped   int     `json:"mapped_flags"`
	Unmapped int     `json:"unmapped_flags"`
	Fidelity float64 `json:"fidelity"`
}

// DiffProjects compares a source (importer output) against a destination
// (current Switchyard state) across all projects. Empty dest means every
// source flag is an add — the first-import case.
//
// Slices are always non-nil so JSON emits [] rather than null; CI contract
// tests parse this shape.
func DiffProjects(source, dest []model.Project, unmapped []Unmapped) Diff {
	d := Diff{
		Added:    []model.Flag{},
		Removed:  []model.Flag{},
		Changed:  []model.Flag{},
		Unmapped: []Unmapped{},
	}

	srcFlags := flagsByProject(source)
	dstFlags := flagsByProject(dest)

	for projKey, src := range srcFlags {
		dst, exists := dstFlags[projKey]
		if !exists {
			d.Added = append(d.Added, src...)
			continue
		}
		dstByFlag := map[string]model.Flag{}
		for _, f := range dst {
			dstByFlag[f.Key] = f
		}
		srcByFlag := map[string]model.Flag{}
		for _, f := range src {
			srcByFlag[f.Key] = f
		}
		for _, f := range src {
			if _, ok := dstByFlag[f.Key]; !ok {
				d.Added = append(d.Added, f)
			} else if !flagsEqual(f, dstByFlag[f.Key]) {
				d.Changed = append(d.Changed, f)
			}
		}
		for _, f := range dst {
			if _, ok := srcByFlag[f.Key]; !ok {
				d.Removed = append(d.Removed, f)
			}
		}
	}

	for _, u := range unmapped {
		d.Unmapped = append(d.Unmapped, u)
	}

	d.Summary = Summary{
		Added:    len(d.Added),
		Removed:  len(d.Removed),
		Changed:  len(d.Changed),
		Unmapped: len(d.Unmapped),
	}
	return d
}

// FidelityScore computes mapped vs unmapped flag counts across source
// projects. A flag counts as unmapped if it appears in any Unmapped entry.
func FidelityScore(source []model.Project, unmapped []Unmapped) Fidelity {
	unmappedFlags := map[string]bool{}
	for _, u := range unmapped {
		if u.Flag != "" {
			unmappedFlags[u.Flag] = true
		}
	}
	f := Fidelity{}
	for _, p := range source {
		f.Total += len(p.Flags)
		for _, flag := range p.Flags {
			if unmappedFlags[flag.Key] {
				f.Unmapped++
			} else {
				f.Mapped++
			}
		}
	}
	if f.Total > 0 {
		f.Fidelity = float64(f.Mapped) / float64(f.Total)
	}
	return f
}

func flagsByProject(projects []model.Project) map[string][]model.Flag {
	m := map[string][]model.Flag{}
	for _, p := range projects {
		m[p.Key] = append(m[p.Key], p.Flags...)
	}
	return m
}

// flagsEqual compares the parts of a flag the v0.1 diff surface exposes.
// Name drives "changed" detection; per-env config equality is structural.
func flagsEqual(a, b model.Flag) bool {
	if a.Key != b.Key || a.Name != b.Name || a.Kind != b.Kind {
		return false
	}
	if len(a.Environments) != len(b.Environments) {
		return false
	}
	for envKey, ae := range a.Environments {
		be, ok := b.Environments[envKey]
		if !ok || !flagEnvsEqual(ae, be) {
			return false
		}
	}
	return true
}

func flagEnvsEqual(a, b model.FlagEnvironment) bool {
	if a.On != b.On {
		return false
	}
	if (a.Rollout == nil) != (b.Rollout == nil) {
		return false
	}
	if a.Rollout != nil && *a.Rollout != *b.Rollout {
		return false
	}
	if len(a.Rules) != len(b.Rules) {
		return false
	}
	for i := range a.Rules {
		if !rulesEqual(a.Rules[i], b.Rules[i]) {
			return false
		}
	}
	return true
}

func rulesEqual(a, b model.Rule) bool {
	if a.ID != b.ID || (a.Rollout == nil) != (b.Rollout == nil) {
		return false
	}
	if a.Rollout != nil && *a.Rollout != *b.Rollout {
		return false
	}
	if len(a.Clauses) != len(b.Clauses) {
		return false
	}
	for i := range a.Clauses {
		if !clausesEqual(a.Clauses[i], b.Clauses[i]) {
			return false
		}
	}
	return true
}

func clausesEqual(a, b model.Clause) bool {
	if a.Attribute != b.Attribute || a.Operator != b.Operator || len(a.Values) != len(b.Values) {
		return false
	}
	for i := range a.Values {
		if a.Values[i] != b.Values[i] {
			return false
		}
	}
	return true
}
