// Package unleash parses Unleash state exports (schema v5) into the
// normalized Switchyard model. Unleash is strategy-centric; every strategy
// maps to a Switchyard rule or the fallthrough rollout. Constructs that
// cannot preserve semantics are reported as migrate.Unmapped — never
// silently dropped.
package unleash

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/model"
)

// supportedConstraintOps maps Unleash constraint operators to Switchyard
// clause operators. Anything absent is reported unmapped.
var supportedConstraintOps = map[string]string{
	"IN":              "in",
	"NOT_IN":          "in", // + Negate
	"STR_STARTS_WITH": "startsWith",
	"STR_ENDS_WITH":   "endsWith",
	"STR_CONTAINS":    "contains",
	"NUM_EQ":          "in", // single-value equality
	"NUM_GT":          "greaterThan",
	"NUM_GTE":         "greaterThanOrEqual",
	"NUM_LT":          "lessThan",
	"NUM_LTE":         "lessThanOrEqual",
	"DATE_AFTER":      "after",
	"DATE_BEFORE":     "before",
	"REGEX":           "matches",
}

// ulExport mirrors the subset of an Unleash state export we consume.
type ulExport struct {
	Version  int         `json:"version"`
	Projects []ulProject `json:"projects"`
	Features []ulFeature `json:"features"`
	Segments []ulSegment `json:"segments"`
}

type ulProject struct {
	ID   string       `json:"id"`
	Name string       `json:"name"`
	Envs []ulEnvState `json:"environments"`
}

type ulEnvState struct {
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Type    string    `json:"type"`
	Strats  []ulStrat `json:"strategies"`
}

type ulFeature struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Project     string      `json:"project"`
	Archived    bool        `json:"archived"`
	Variants    []ulVariant `json:"variants"`
	Envs        []ulEnv     `json:"environments"`
}

type ulEnv struct {
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Strats  []ulStrat `json:"strategies"`
}

type ulStrat struct {
	Name        string         `json:"name"`
	Params      map[string]any `json:"parameters"`
	Constraints []ulConstraint `json:"constraints"`
	Segments    []int          `json:"segments"`
	Disabled    bool           `json:"disabled"`
}

type ulConstraint struct {
	ContextName     string   `json:"contextName"`
	Operator        string   `json:"operator"`
	Values          []string `json:"values"`
	CaseInsensitive bool     `json:"caseInsensitive"`
	Inverted        bool     `json:"inverted"`
}

type ulVariant struct {
	Name    string     `json:"name"`
	Weight  int        `json:"weight"`
	Payload *ulPayload `json:"payload"`
}

type ulPayload struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type ulSegment struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Constraints []ulConstraint `json:"constraints"`
}

// Parse converts an Unleash state export into normalized projects plus
// every construct that cannot be mapped.
func Parse(data []byte) ([]model.Project, []migrate.Unmapped, error) {
	var ex ulExport
	if err := json.Unmarshal(data, &ex); err != nil {
		return nil, nil, fmt.Errorf("parse unleash export: %w", err)
	}

	segNames := map[int]string{}
	for _, s := range ex.Segments {
		segNames[s.ID] = s.Name
	}

	// Group features by project (Unleash default project is "default").
	featByProject := map[string][]ulFeature{}
	for _, f := range ex.Features {
		key := f.Project
		if key == "" {
			key = "default"
		}
		featByProject[key] = append(featByProject[key], f)
	}

	var projects []model.Project
	var unmapped []migrate.Unmapped

	projectsWithFeatures := map[string]bool{}
	for key := range featByProject {
		projectsWithFeatures[key] = true
	}
	declared := map[string]ulProject{}
	for _, p := range ex.Projects {
		declared[p.ID] = p
	}

	// Emit projects: declared ones plus any feature-referenced ones.
	keys := make([]string, 0)
	for k := range declared {
		keys = append(keys, k)
	}
	for k := range featByProject {
		if _, ok := declared[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		proj := model.Project{Key: key, Name: key}
		if d, ok := declared[key]; ok {
			proj.Name = d.Name
			for _, e := range d.Envs {
				proj.Environments = append(proj.Environments, model.Environment{Key: e.Name, Name: e.Name})
			}
		}
		for _, f := range featByProject[key] {
			flag, fUnmapped := parseFeature(key, f, segNames)
			proj.Flags = append(proj.Flags, flag)
			unmapped = append(unmapped, fUnmapped...)
		}
		// Segments referenced by this project's features live globally in
		// the export; attach all segments to the default project only.
		if key == "default" {
			for _, s := range ex.Segments {
				seg, sUnmapped := parseSegment(key, s)
				proj.Segments = append(proj.Segments, seg)
				unmapped = append(unmapped, sUnmapped...)
			}
		}
		projects = append(projects, proj)
	}
	return projects, unmapped, nil
}

func parseFeature(projectKey string, f ulFeature, segNames map[int]string) (model.Flag, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	flag := model.Flag{
		Key:          f.Name,
		Name:         f.Name,
		Environments: map[string]model.FlagEnvironment{},
	}

	// Variants -> kind + value. Single variant maps exactly; multiple map
	// to the dominant weight with an honest gap.
	kind, value, vUnmapped := resolveVariants(projectKey, f)
	flag.Kind = kind
	unmapped = append(unmapped, vUnmapped...)

	for _, e := range f.Envs {
		fenv, eUnmapped := parseEnvConfig(projectKey, f, e, segNames)
		fenv.Value = value
		flag.Environments[e.Name] = fenv
		unmapped = append(unmapped, eUnmapped...)
	}
	return flag, unmapped
}

// resolveVariants maps Unleash variants to a Switchyard kind + value.
func resolveVariants(projectKey string, f ulFeature) (model.Kind, any, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	if len(f.Variants) == 0 {
		return model.KindBoolean, nil, nil
	}
	if len(f.Variants) > 1 {
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey,
			Flag:    f.Name,
			Type:    "weighted-variants",
			Detail:  fmt.Sprintf("%d weighted variants mapped to dominant variant only", len(f.Variants)),
		})
	}
	// Dominant variant (first wins ties).
	best := f.Variants[0]
	for _, v := range f.Variants[1:] {
		if v.Weight > best.Weight {
			best = v
		}
	}
	if best.Payload == nil {
		return model.KindBoolean, nil, unmapped
	}
	switch best.Payload.Type {
	case "string":
		var s string
		if err := json.Unmarshal(best.Payload.Value, &s); err == nil {
			return model.KindString, s, unmapped
		}
		return model.KindString, string(best.Payload.Value), unmapped
	case "number":
		var n float64
		if err := json.Unmarshal(best.Payload.Value, &n); err == nil {
			return model.KindNumber, n, unmapped
		}
		// Payload values may arrive as JSON strings containing numbers.
		var s string
		if err := json.Unmarshal(best.Payload.Value, &s); err == nil {
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return model.KindNumber, n, unmapped
			}
		}
		return model.KindString, string(best.Payload.Value), unmapped
	case "json":
		var v any
		if err := json.Unmarshal(best.Payload.Value, &v); err == nil {
			// Payload may be a JSON string containing JSON (Unleash stores
			// payloads as strings); unwrap one level.
			if s, isStr := v.(string); isStr {
				var v2 any
				if err := json.Unmarshal([]byte(s), &v2); err == nil {
					return model.KindJSON, v2, unmapped
				}
				return model.KindString, s, unmapped
			}
			return model.KindJSON, v, unmapped
		}
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey, Flag: f.Name, Type: "variant-payload",
			Detail: fmt.Sprintf("unparseable json payload: %s", string(best.Payload.Value)),
		})
		return model.KindJSON, nil, unmapped
	default:
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey, Flag: f.Name, Type: "variant-payload",
			Detail: fmt.Sprintf("payload type %q not supported", best.Payload.Type),
		})
		return model.KindBoolean, nil, unmapped
	}
}

// parseEnvConfig converts one Unleash environment state.
func parseEnvConfig(projectKey string, f ulFeature, e ulEnv, segNames map[int]string) (model.FlagEnvironment, []migrate.Unmapped) {
	var unmapped []migrate.Unmapped
	fenv := model.FlagEnvironment{On: e.Enabled}

	usedFallthrough := false
	for _, s := range e.Strats {
		if s.Disabled {
			continue
		}
		stratUnmapped, dead := parseStrategy(projectKey, f.Name, e.Name, s, segNames, &fenv, &usedFallthrough)
		if dead {
			continue
		}
		unmapped = append(unmapped, stratUnmapped...)
	}

	// Enabled with no active strategies: serves nobody in Unleash —
	// Switchyard equivalent is a 0% fallthrough rollout.
	if e.Enabled && !usedFallthrough && len(fenv.Rules) == 0 {
		fenv.Rollout = &model.Rollout{Kind: "percentage", Percentage: 0}
	}
	return fenv, unmapped
}

// parseStrategy maps one strategy into the flag environment. dead=true
// means the strategy never activates in this env (safe to drop).
func parseStrategy(projectKey, flagKey, envKey string, s ulStrat, segNames map[int]string, fenv *model.FlagEnvironment, usedFallthrough *bool) ([]migrate.Unmapped, bool) {
	var unmapped []migrate.Unmapped

	// Classify constraints: environment constraints, and the rest.
	var clauses []model.Clause
	constraintOnly := true
	for _, c := range s.Constraints {
		if c.ContextName == "environment" {
			// Tautology or dead config for this env.
			matches := false
			for _, v := range c.Values {
				if v == envKey {
					matches = true
					break
				}
			}
			if c.Inverted {
				matches = !matches
			}
			if !matches {
				return nil, true // strategy never fires here
			}
			continue // tautology: drop
		}
		op, ok := supportedConstraintOps[c.Operator]
		if !ok {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: flagKey, Type: "constraint-operator",
				Detail: fmt.Sprintf("constraint operator %q not supported", c.Operator),
			})
			continue
		}
		if c.CaseInsensitive {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: flagKey, Type: "constraint-case-insensitive",
				Detail: fmt.Sprintf("case-insensitive constraint on %q not supported", c.ContextName),
			})
			continue
		}
		attr := c.ContextName
		if attr == "userId" {
			attr = "targetingKey"
		}
		clauses = append(clauses, model.Clause{
			Attribute: attr,
			Operator:  op,
			Values:    c.Values,
			Negate:    c.Inverted,
		})
	}

	// Segment references resolve to names; unknown IDs are honest gaps.
	for _, id := range s.Segments {
		name, ok := segNames[id]
		if !ok {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: flagKey, Type: "segment-missing",
				Detail: fmt.Sprintf("segment id %d not found in export", id),
			})
			continue
		}
		clauses = append(clauses, model.Clause{
			Attribute: "segment", Operator: "segmentMatch", Values: []string{name},
		})
	}

	if len(clauses) > 0 {
		constraintOnly = false
	}

	switch s.Name {
	case "default":
		if constraintOnly {
			// Plain default: everyone gets the flag. If another strategy
			// already claimed the fallthrough, this adds nothing new —
			// but multiple defaults with no clauses is unusual; the first
			// wins.
			if !*usedFallthrough {
				*usedFallthrough = true
				fenv.Rollout = nil // on, no rollout: serves everyone
			}
			return unmapped, false
		}
		// Default + constraints = a rule.
		fenv.Rules = append(fenv.Rules, model.Rule{
			ID:      fmt.Sprintf("strategy-default-%d", len(fenv.Rules)),
			Clauses: clauses,
		})
		return unmapped, false

	case "userWithId":
		ids, _ := s.Params["userIds"].(string)
		values := []string{}
		for _, id := range strings.Split(ids, ",") {
			if id != "" {
				values = append(values, strings.TrimSpace(id))
			}
		}
		fenv.Rules = append(fenv.Rules, model.Rule{
			ID: fmt.Sprintf("strategy-userWithId-%d", len(fenv.Rules)),
			Clauses: []model.Clause{{
				Attribute: "targetingKey", Operator: "in", Values: values,
			}},
		})
		// userWithId + extra constraints would tighten the rule; v0.1 keeps
		// the ID list only when constraints also present.
		if len(clauses) > 0 {
			fenv.Rules[len(fenv.Rules)-1].Clauses = append(fenv.Rules[len(fenv.Rules)-1].Clauses, clauses...)
		}
		return unmapped, false

	case "flexibleRollout", "gradualRollout":
		rolloutPct := 100
		if v, ok := s.Params["rollout"].(string); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				rolloutPct = n
			}
		} else if v, ok := s.Params["rollout"].(float64); ok {
			rolloutPct = int(v)
		}
		if v, ok := s.Params["percentage"].(string); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				rolloutPct = n
			}
		}
		// Stickiness: userId (or absent) is Switchyard's bucketing; other
		// values change the bucketing input and are reported.
		stick, hasStick := s.Params["stickiness"].(string)
		if hasStick && stick != "" && stick != "userId" && stick != "default" {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: flagKey, Type: "stickiness",
				Detail: fmt.Sprintf("stickiness %q not supported (Switchyard buckets on user key)", stick),
			})
		}
		// groupId: Switchyard buckets on flag key; a custom groupId is a
		// different bucket input — reported like LD's bucketBy.
		if g, ok := s.Params["groupId"].(string); ok && g != "" && g != flagKey {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: flagKey, Type: "bucketBy",
				Detail: fmt.Sprintf("groupId %q differs from flag key; bucketing differs", g),
			})
		}
		if constraintOnly {
			// Pure rollout: the fallthrough.
			if *usedFallthrough {
				// Two pure rollouts: keep the first, report the conflict.
				unmapped = append(unmapped, migrate.Unmapped{
					Project: projectKey, Flag: flagKey, Type: "strategy-conflict",
					Detail: "multiple rollout strategies map to one fallthrough; first kept",
				})
				return unmapped, false
			}
			*usedFallthrough = true
			fenv.Rollout = &model.Rollout{Kind: "percentage", Percentage: rolloutPct}
			return unmapped, false
		}
		// Rollout + constraints: a rule with its own rollout preserves the
		// exact semantics (clause match AND bucket).
		fenv.Rules = append(fenv.Rules, model.Rule{
			ID:      fmt.Sprintf("strategy-%s-%d", s.Name, len(fenv.Rules)),
			Clauses: clauses,
			Rollout: &model.Rollout{Kind: "percentage", Percentage: rolloutPct},
		})
		return unmapped, false

	case "gradualRolloutRandom":
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey, Flag: flagKey, Type: "strategy-random-rollout",
			Detail: "random (non-sticky) rollout cannot preserve semantics",
		})
		// The flag still gets *something*: treat as a 0% fallthrough so it
		// serves nobody rather than everyone — fail closed.
		if !*usedFallthrough && len(fenv.Rules) == 0 {
			*usedFallthrough = true
			fenv.Rollout = &model.Rollout{Kind: "percentage", Percentage: 0}
		}
		return unmapped, false

	default:
		unmapped = append(unmapped, migrate.Unmapped{
			Project: projectKey, Flag: flagKey, Type: "strategy-unsupported",
			Detail: fmt.Sprintf("strategy %q not supported", s.Name),
		})
		return unmapped, false
	}
}

func parseSegment(projectKey string, s ulSegment) (model.Segment, []migrate.Unmapped) {
	seg := model.Segment{Key: s.Name, Name: s.Name}
	var unmapped []migrate.Unmapped
	var clauses []model.Clause
	for _, c := range s.Constraints {
		op, ok := supportedConstraintOps[c.Operator]
		if !ok {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: s.Name, Type: "constraint-operator",
				Detail: fmt.Sprintf("segment constraint operator %q not supported", c.Operator),
			})
			continue
		}
		if c.CaseInsensitive {
			unmapped = append(unmapped, migrate.Unmapped{
				Project: projectKey, Flag: s.Name, Type: "constraint-case-insensitive",
				Detail: fmt.Sprintf("case-insensitive segment constraint on %q", c.ContextName),
			})
			continue
		}
		attr := c.ContextName
		if attr == "userId" {
			attr = "targetingKey"
		}
		clauses = append(clauses, model.Clause{
			Attribute: attr, Operator: op, Values: c.Values, Negate: c.Inverted,
		})
	}
	if len(clauses) > 0 {
		seg.Rules = append(seg.Rules, model.Rule{ID: "segment-rule", Clauses: clauses})
	}
	return seg, unmapped
}

// Describe renders a one-line human summary of a project.
func Describe(p model.Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): %d flags, %d segments, %d envs",
		p.Key, p.Name, len(p.Flags), len(p.Segments), len(p.Environments))
	return b.String()
}
