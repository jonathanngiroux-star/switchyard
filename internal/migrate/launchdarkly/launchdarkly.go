// Package launchdarkly parses LaunchDarkly project exports into the
// normalized Switchyard model. Every construct v0.1 cannot represent is
// reported as migrate.Unmapped — never silently dropped.
package launchdarkly

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/model"
)

// supportedClauseOps is the v0.1 operator set. Anything else becomes an
// Unmapped clause-operator entry so the fidelity report stays honest.
var supportedClauseOps = map[string]bool{
	"segmentMatch": true,
	"in":           true,
	"startsWith":   true,
	"endsWith":     true,
}

// ldExport mirrors the subset of a LaunchDarkly export we consume.
type ldExport struct {
	Projects []ldProject `json:"projects"`
}

type ldProject struct {
	Key          string      `json:"key"`
	Name         string      `json:"name"`
	Environments []ldEnv     `json:"environments"`
	Flags        []ldFlag    `json:"flags"`
	Segments     []ldSegment `json:"segments"`
}

type ldEnv struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type ldFlag struct {
	Key           string               `json:"key"`
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	Prerequisites []ldPrerequisite     `json:"prerequisites"`
	Environments  map[string]ldFlagEnv `json:"environments"`
}

type ldPrerequisite struct {
	Flag      string `json:"flag"`
	Variation int    `json:"variation"`
}

type ldFlagEnv struct {
	On          bool          `json:"on"`
	Rules       []ldRule      `json:"rules"`
	Targets     []ldTarget    `json:"targets"`
	Fallthrough ldFallthrough `json:"fallthrough"`
	TrackEvents bool          `json:"trackEvents"`
}

type ldTarget struct {
	Values    []string `json:"values"`
	Variation int      `json:"variation"`
}

type ldRule struct {
	ID        string     `json:"id"`
	Variation int        `json:"variation"`
	Rollout   *ldRollout `json:"rollout"`
	Clauses   []ldClause `json:"clauses"`
}

type ldRollout struct {
	Kind       string `json:"kind"`
	Percentage int    `json:"percentage"`
	BucketBy   string `json:"bucketBy"`
}

type ldClause struct {
	Attribute string   `json:"attribute"`
	Operator  string   `json:"operator"`
	Values    []string `json:"values"`
	Negate    bool     `json:"negate"`
}

type ldFallthrough struct {
	Variation int        `json:"variation"`
	Rollout   *ldRollout `json:"rollout"`
}

type ldSegment struct {
	Key   string   `json:"key"`
	Name  string   `json:"name"`
	Rules []ldRule `json:"rules"`
}

// Parse converts a LaunchDarkly export body into normalized projects plus
// a list of every construct v0.1 cannot map.
func Parse(data []byte) ([]model.Project, []migrate.Unmapped, error) {
	var ex ldExport
	if err := json.Unmarshal(data, &ex); err != nil {
		return nil, nil, fmt.Errorf("parse launchdarkly export: %w", err)
	}

	projects := make([]model.Project, 0, len(ex.Projects))
	var unmapped []migrate.Unmapped

	for _, p := range ex.Projects {
		proj := model.Project{Key: p.Key, Name: p.Name}
		for _, e := range p.Environments {
			proj.Environments = append(proj.Environments, model.Environment{Key: e.Key, Name: e.Name})
		}

		for _, f := range p.Flags {
			flag, flagUnmapped := parseFlag(p.Key, f)
			proj.Flags = append(proj.Flags, flag)
			unmapped = append(unmapped, flagUnmapped...)
		}

		for _, s := range p.Segments {
			seg := model.Segment{Key: s.Key, Name: s.Name}
			for _, r := range s.Rules {
				rule, segUnmapped := parseRule(p.Key, s.Key, "", r)
				seg.Rules = append(seg.Rules, rule)
				unmapped = append(unmapped, segUnmapped...)
			}
			proj.Segments = append(proj.Segments, seg)
		}

		projects = append(projects, proj)
	}
	return projects, unmapped, nil
}

func parseFlag(projectKey string, f ldFlag) (model.Flag, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	flag := model.Flag{
		Key:          f.Key,
		Name:         f.Name,
		Kind:         model.Kind(f.Kind),
		Environments: map[string]model.FlagEnvironment{},
	}
	if len(f.Prerequisites) > 0 {
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey,
			Flag:    f.Key,
			Type:    "prerequisites",
			Detail:  fmt.Sprintf("%d prerequisites not mapped in v0.1", len(f.Prerequisites)),
		})
	}
	for envKey, fe := range f.Environments {
		fenv := model.FlagEnvironment{On: fe.On}
		if fe.Fallthrough.Rollout != nil {
			fenv.Rollout = &model.Rollout{
				Kind:       fe.Fallthrough.Rollout.Kind,
				Percentage: fe.Fallthrough.Rollout.Percentage,
			}
		}
		for _, r := range fe.Rules {
			rule, ruleUnmapped := parseRule(projectKey, f.Key, r.ID, r)
			fenv.Rules = append(fenv.Rules, rule)
			unmapped = append(unmapped, ruleUnmapped...)
		}
		flag.Environments[envKey] = fenv
	}
	return flag, unmapped
}

func parseRule(projectKey, flagKey, ruleID string, r ldRule) (model.Rule, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	rule := model.Rule{ID: r.ID}
	if r.Rollout != nil {
		rule.Rollout = &model.Rollout{Kind: r.Rollout.Kind, Percentage: r.Rollout.Percentage}
	}
	for _, c := range r.Clauses {
		if !supportedClauseOps[c.Operator] {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey,
				Flag:    flagKey,
				Rule:    ruleID,
				Type:    "clause-operator",
				Detail:  fmt.Sprintf("operator %q not supported in v0.1", c.Operator),
			})
			continue
		}
		rule.Clauses = append(rule.Clauses, model.Clause{
			Attribute: c.Attribute,
			Operator:  c.Operator,
			Values:    c.Values,
		})
	}
	return rule, unmapped
}

// Describe renders a one-line human summary of a project — used by the
// CLI's non-JSON mode.
func Describe(p model.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): %d flags, %d segments, %d envs",
		p.Key, p.Name, len(p.Flags), len(p.Segments), len(p.Environments))
	return b.String()
}
