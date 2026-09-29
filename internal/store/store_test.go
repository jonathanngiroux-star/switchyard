package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "switchyard.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSchemaVersionIsCurrent(t *testing.T) {
	s := newTestStore(t)
	v, err := s.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if v != 3 {
		t.Fatalf("schema version = %d, want 3", v)
	}
}

func TestFlagRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	in := model.Flag{
		Key:  "checkouts-v2",
		Name: "New checkout flow",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:      false,
				Rollout: &model.Rollout{Kind: "percentage", Percentage: 10},
				Rules: []model.Rule{{
					ID: "r1",
					Clauses: []model.Clause{{
						Attribute: "segment", Operator: "segmentMatch", Values: []string{"beta-users"},
					}},
				}},
			},
		},
	}
	if err := s.PutFlag(ctx, in); err != nil {
		t.Fatalf("put flag: %v", err)
	}
	got, err := s.GetFlag(ctx, in.Key)
	if err != nil {
		t.Fatalf("get flag: %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip mismatch:\ngot  %+v\nwant %+v", got, in)
	}
}

func TestGetFlagMissingReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetFlag(context.Background(), "does-not-exist")
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPutFlagUpserts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	v1 := model.Flag{Key: "a", Name: "First name", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}
	v2 := model.Flag{Key: "a", Name: "Second name", Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}
	if err := s.PutFlag(ctx, v1); err != nil {
		t.Fatalf("put v1: %v", err)
	}
	if err := s.PutFlag(ctx, v2); err != nil {
		t.Fatalf("put v2: %v", err)
	}
	got, err := s.GetFlag(ctx, "a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Second name" {
		t.Fatalf("name = %q, want upserted %q", got.Name, "Second name")
	}
}

func TestListAndDeleteFlag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, k := range []string{"a", "b"} {
		if err := s.PutFlag(ctx, model.Flag{Key: k, Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}); err != nil {
			t.Fatalf("put %s: %v", k, err)
		}
	}
	flags, err := s.ListFlags(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(flags) != 2 {
		t.Fatalf("len(flags) = %d, want 2", len(flags))
	}
	if err := s.DeleteFlag(ctx, "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	flags, err = s.ListFlags(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(flags) != 1 || flags[0].Key != "b" {
		t.Fatalf("flags after delete = %+v, want [b]", flags)
	}
	if _, err := s.GetFlag(ctx, "a"); err != ErrNotFound {
		t.Fatalf("get deleted = %v, want ErrNotFound", err)
	}
	if err := s.DeleteFlag(ctx, "a"); err != ErrNotFound {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}
}
