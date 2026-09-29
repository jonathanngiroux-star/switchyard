package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "switchyard.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSetFlagEnvironmentUpsertsOneEnv(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.PutFlag(ctx, model.Flag{Key: "f", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}); err != nil {
		t.Fatalf("put flag: %v", err)
	}
	fe := model.FlagEnvironment{On: true, Rollout: &model.Rollout{Kind: "percentage", Percentage: 25}}
	if err := s.SetFlagEnvironment(ctx, "f", "staging", fe); err != nil {
		t.Fatalf("set env: %v", err)
	}
	got, err := s.GetFlag(ctx, "f")
	if err != nil {
		t.Fatalf("get flag: %v", err)
	}
	if !got.Environments["staging"].On || got.Environments["staging"].Rollout.Percentage != 25 {
		t.Fatalf("staging env = %+v, want on @25%%", got.Environments["staging"])
	}
	// Upsert: same call with different values overwrites, doesn't duplicate.
	fe2 := model.FlagEnvironment{On: false}
	if err := s.SetFlagEnvironment(ctx, "f", "staging", fe2); err != nil {
		t.Fatalf("set env again: %v", err)
	}
	got, _ = s.GetFlag(ctx, "f")
	if got.Environments["staging"].On || got.Environments["staging"].Rollout != nil {
		t.Fatalf("staging after upsert = %+v, want off, no rollout", got.Environments["staging"])
	}
	if len(got.Environments) != 1 {
		t.Fatalf("env count = %d, want 1 (upsert must not duplicate rows)", len(got.Environments))
	}
}

func TestSetFlagEnvironmentMissingFlagReturnsNotFound(t *testing.T) {
	s := newStore(t)
	err := s.SetFlagEnvironment(context.Background(), "ghost", "staging", model.FlagEnvironment{On: true})
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetFlagEnvironmentUnknownEnvViolatesForeignKey(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if err := s.PutFlag(ctx, model.Flag{Key: "f", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}); err != nil {
		t.Fatalf("put flag: %v", err)
	}
	// "bogus" is not a seeded environment; the FK must reject it.
	if err := s.SetFlagEnvironment(ctx, "f", "bogus", model.FlagEnvironment{On: true}); err == nil {
		t.Fatal("expected FK error for unknown environment, got nil")
	}
}

func TestSegmentRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	in := model.Segment{
		Key:  "beta-users",
		Name: "Beta users",
		Rules: []model.Rule{{
			ID:      "seg-1",
			Clauses: []model.Clause{{Attribute: "email", Operator: "endsWith", Values: []string{"@beta.test"}}},
		}},
	}
	if err := s.PutSegment(ctx, in); err != nil {
		t.Fatalf("put segment: %v", err)
	}
	got, err := s.GetSegment(ctx, "beta-users")
	if err != nil {
		t.Fatalf("get segment: %v", err)
	}
	if got.Name != in.Name || len(got.Rules) != 1 || got.Rules[0].Clauses[0].Operator != "endsWith" {
		t.Fatalf("segment round trip = %+v", got)
	}
	segs, err := s.ListSegments(ctx)
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	if len(segs) != 1 || segs[0].Key != "beta-users" {
		t.Fatalf("list segments = %+v", segs)
	}
}

func TestGetSegmentMissingReturnsNotFound(t *testing.T) {
	s := newStore(t)
	if _, err := s.GetSegment(context.Background(), "ghost"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListEnvironmentsSeededInOrder(t *testing.T) {
	s := newStore(t)
	envs, err := s.ListEnvironments(context.Background())
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}
	if len(envs) != 3 || envs[0].Key != "dev" || envs[1].Key != "staging" || envs[2].Key != "production" {
		t.Fatalf("environments = %+v, want dev/staging/production in order", envs)
	}
}
