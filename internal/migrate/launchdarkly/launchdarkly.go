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

// supportedClauseOps is the W5–7 operator set, matching eval.clauseMatches.
// Anything else becomes an Unmapped clause-operator entry so the fidelity
// report stays honest.
var supportedClauseOps = map[string]bool{
	"segmentMatch":       true,
	"in":                 true,
	"startsWith":         true,
	"endsWith":           true,
	"contains":           true,
	"matches":            true,
	"before":             true,
	"after":              true,
	"greaterThan":        true,
	"greaterThanOrEqual": true,
	"lessThan":           true,
	"lessThanOrEqual":    true,
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
	Variations    []ldVariation        `json:"variations"`
	Prerequisites []ldPrerequisite     `json:"prerequisites"`
	Environments  map[string]ldFlagEnv `json:"environments"`
}

type ldVariation struct {
	Value any    `json:"value"`
	Name  string `json:"name"`
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
	Kind               string              `json:"kind"`
	Percentage         int                 `json:"percentage"`
	BucketBy           string              `json:"bucketBy"`
	WeightedVariations []ldWeightedVariant `json:"weightedVariations"`
}

type ldWeightedVariant struct {
	Variation int `json:"variation"`
	Weight    int `json:"weight"`
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
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Unbounded bool     `json:"unbounded"`
	Rules     []ldRule `json:"rules"`
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
			seg, segUnmapped := parseSegment(p.Key, s)
			proj.Segments = append(proj.Segments, seg)
			unmapped = append(unmapped, segUnmapped...)
		}

		projects = append(projects, proj)
	}
	return projects, unmapped, nil
}

// variationValue resolves a variation index to its configured value.
func variationValue(f ldFlag, idx int) any {
	if idx < 0 || idx >= len(f.Variations) {
		return nil
	}
	return f.Variations[idx].Value
}

func parseFlag(projectKey string, f ldFlag) (model.Flag, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	flag := model.Flag{
		Key:          f.Key,
		Name:         f.Name,
		Kind:         model.Kind(f.Kind),
		Environments: map[string]model.FlagEnvironment{},
	}
	for envKey, fe := range f.Environments {
		fenv := model.FlagEnvironment{On: fe.On}

		// Prerequisites: preserved on the environment (per-env in LD),
		// evaluated-but-not-enforced — the report documents it.
		for _, pr := range f.Prerequisites {
			fenv.Prerequisites = append(fenv.Prerequisites, model.Prerequisite{
				Flag: pr.Flag, Variation: pr.Variation,
			})
		}

		// Targets become explicit targetingKey rules, first in order —
		// LD evaluates targets before rules.
		for _, tg := range fe.Targets {
			if len(tg.Values) == 0 {
				continue
			}
			fenv.Rules = append(fenv.Rules, model.Rule{
				ID: fmt.Sprintf("target-%d-%d", tg.Variation, len(fenv.Rules)),
				Clauses: []model.Clause{{
					Attribute: "targetingKey",
					Operator:  "in",
					Values:    tg.Values,
				}},
				Value: variationValue(f, tg.Variation),
			})
		}

		// Rules: resolve the variation index to its value.
		for _, r := range fe.Rules {
			rule, ruleUnmapped := parseRule(projectKey, f.Key, r, f)
			fenv.Rules = append(fenv.Rules, rule)
			unmapped = append(unmapped, ruleUnmapped...)
		}

		// Fallthrough: simple percentage rollout, or weighted variations.
		if ft := fe.Fallthrough; ft.Rollout != nil {
			if len(ft.Rollout.WeightedVariations) > 0 {
				// Weighted rollout over multiple variations: map the
				// dominant bucket, report the loss honestly.
				dominant, _ := dominantWeighted(ft.Rollout.WeightedVariations)
				fenv.Value = variationValue(f, dominant)
				if len(ft.Rollout.WeightedVariations) > 1 {
					unmapped = append(unmapped, migrate.Unmapped{
						Project: projectKey,
						Flag:    f.Key,
						Type:    "weighted-rollout",
						Detail: fmt.Sprintf("weighted fallthrough over %d variations mapped to dominant variation %d only",
							len(ft.Rollout.WeightedVariations), dominant),
					})
				}
			} else {
				fenv.Rollout = &model.Rollout{
					Kind:       ft.Rollout.Kind,
					Percentage: ft.Rollout.Percentage,
				}
			}
			if ft.Rollout.BucketBy != "" {
				unmapped = append(unmapped, migrate.Unmapped{
					Project: projectKey,
					Flag:    f.Key,
					Type:    "bucketBy",
					Detail:  fmt.Sprintf("bucketBy %q not mapped in v0.1", ft.Rollout.BucketBy),
				})
			}
		}
		// Boolean flags carry no value; other kinds resolve the fallthrough
		// variation's value whenever one is set (weighted rollouts set it
		// above). The rollout gates enabled; the value rides the match.
		if flag.Kind != model.KindBoolean && fenv.Value == nil && fe.Fallthrough.Variation >= 0 {
			fenv.Value = variationValue(f, fe.Fallthrough.Variation)
		}

		flag.Environments[envKey] = fenv
	}
	return flag, unmapped
}

func dominantWeighted(wv []ldWeightedVariant) (int, int) {
	best, weight := 0, -1
	for _, w := range wv {
		if w.Weight > weight {
			best, weight = w.Variation, w.Weight
		}
	}
	return best, weight
}

func parseRule(projectKey, flagKey string, r ldRule, f ldFlag) (model.Rule, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	rule := model.Rule{
		ID:    r.ID,
		Value: variationValue(f, r.Variation),
	}
	if r.Rollout != nil {
		rule.Rollout = &model.Rollout{Kind: r.Rollout.Kind, Percentage: r.Rollout.Percentage}
		if r.Rollout.BucketBy != "" {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey,
				Flag:    flagKey,
				Rule:    r.ID,
				Type:    "bucketBy",
				Detail:  fmt.Sprintf("bucketBy %q not mapped in v0.1", r.Rollout.BucketBy),
			})
		}
	}
	for _, c := range r.Clauses {
		if !supportedClauseOps[c.Operator] {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey,
				Flag:    flagKey,
				Rule:    r.ID,
				Type:    "clause-operator",
				Detail:  fmt.Sprintf("operator %q not supported in v0.1", c.Operator),
			})
			continue
		}
		rule.Clauses = append(rule.Clauses, model.Clause{
			Attribute: c.Attribute,
			Operator:  c.Operator,
			Values:    c.Values,
			Negate:    c.Negate,
		})
	}
	return rule, unmapped
}

func parseSegment(projectKey string, s ldSegment) (model.Segment, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	seg := model.Segment{Key: s.Key, Name: s.Name}
	for _, r := range s.Rules {
		// Segment rules carry no flag variations; pass a zero-value flag.
		rule, _ := parseRule(projectKey, s.Key, r, ldFlag{})
		seg.Rules = append(seg.Rules, rule)
	}
	if s.Unbounded {
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey,
			Flag:    s.Key,
			Type:    "segment-unbounded",
			Detail:  "unbounded (big-segment) membership not supported in v0.1",
		})
	}
	return seg, unmapped
}

// Describe renders a one-line human summary of a project — used by the
// CLI's non-JSON mode.
func Describe(p model.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): %d flags, %d segments, %d envs",
		p.Key, p.Name, len(p.Flags), len(p.Segments), len(p.Environments))
	return b.String()
}
