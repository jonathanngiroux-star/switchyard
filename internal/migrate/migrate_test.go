package migrate

import (
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func boolFlag(key, name string) model.Flag {
	return model.Flag{Key: key, Name: name, Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}
}

func TestDiffProjectsWithEmptyDestReportsAllAdded(t *testing.T) {
	source := []model.Project{{
		Key:   "default",
		Flags: []model.Flag{boolFlag("checkouts-v2", "New checkout flow"), boolFlag("api-rate-limit", "API rate limiting")},
	}}
	unmapped := []Unmapped{{Flag: "api-rate-limit", Type: "prerequisites", Detail: "1 prerequisites not mapped in v0.1"}}
	d := DiffProjects(source, nil, unmapped)
	if d.Summary.Added != 2 || d.Summary.Removed != 0 || d.Summary.Changed != 0 || d.Summary.Unmapped != 1 {
		t.Fatalf("summary = %+v, want {Added:2 Removed:0 Changed:0 Unmapped:1}", d.Summary)
	}
	if len(d.Added) != 2 || d.Added[0].Key != "checkouts-v2" || d.Added[1].Key != "api-rate-limit" {
		t.Fatalf("added = %+v (must preserve source order)", d.Added)
	}
	// Empty slices must serialize as [] not null — contract tests depend on it.
	if d.Removed == nil || d.Changed == nil || d.Unmapped == nil {
		t.Fatal("removed/changed/unmapped must be non-nil so JSON emits [] not null")
	}
}

func TestDiffProjectsDetectsRemovedAndChanged(t *testing.T) {
	src := []model.Project{{Key: "p", Flags: []model.Flag{boolFlag("a", "A v2")}}}
	dst := []model.Project{{Key: "p", Flags: []model.Flag{boolFlag("a", "A v1"), boolFlag("b", "B")}}}
	d := DiffProjects(src, dst, nil)
	if d.Summary.Changed != 1 {
		t.Fatalf("changed = %d, want 1", d.Summary.Changed)
	}
	if d.Summary.Removed != 1 {
		t.Fatalf("removed = %d, want 1", d.Summary.Removed)
	}
	if d.Summary.Added != 0 {
		t.Fatalf("added = %d, want 0", d.Summary.Added)
	}
	if d.Changed[0].Key != "a" || d.Changed[0].Name != "A v2" {
		t.Fatalf("changed[0] = %+v", d.Changed[0])
	}
	if d.Removed[0].Key != "b" {
		t.Fatalf("removed[0] = %+v, want b", d.Removed[0])
	}
}

func TestDiffProjectsIdenticalFlagsAreNotChanged(t *testing.T) {
	p := []model.Project{{Key: "p", Flags: []model.Flag{boolFlag("a", "A")}}}
	d := DiffProjects(p, p, nil)
	if d.Summary.Changed != 0 {
		t.Fatalf("changed = %d, want 0", d.Summary.Changed)
	}
}
