package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/switchyard/switchyard/internal/store"
)

func tuiTestStore(t *testing.T) (*tuiModel, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tui.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return newTUIModel(st), st
}

func TestTUIModelRowsEmptyThenPopulated(t *testing.T) {
	m, _ := tuiTestStore(t)
	ctx := context.Background()

	rows, err := m.rows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("fresh store rows = %d, want 0", len(rows))
	}
	if err := m.create(ctx, "b-flag"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.create(ctx, "a-flag"); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	rows, err = m.rows(ctx)
	if err != nil {
		t.Fatalf("rows 2: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Sorted by key.
	if rows[0].Key != "a-flag" || rows[1].Key != "b-flag" {
		t.Fatalf("rows not sorted: %+v", rows)
	}
	// Fresh flags are off with no rollout.
	if rows[0].On || rows[0].Rollout != "—" || rows[0].Kind != "boolean" {
		t.Fatalf("row shape = %+v", rows[0])
	}
}

func TestTUIModelToggleFlipsState(t *testing.T) {
	m, _ := tuiTestStore(t)
	ctx := context.Background()
	if err := m.create(ctx, "tg"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.toggle(ctx, "tg"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	rows, _ := m.rows(ctx)
	if !rows[0].On {
		t.Fatalf("after toggle = %+v, want on", rows[0])
	}
	if err := m.toggle(ctx, "tg"); err != nil {
		t.Fatalf("toggle 2: %v", err)
	}
	rows, _ = m.rows(ctx)
	if rows[0].On {
		t.Fatalf("after second toggle = %+v, want off", rows[0])
	}
}

func TestTUIModelSetRolloutBoundsChecked(t *testing.T) {
	m, _ := tuiTestStore(t)
	ctx := context.Background()
	m.create(ctx, "ro")
	if err := m.setRollout(ctx, "ro", 25); err != nil {
		t.Fatalf("setRollout 25: %v", err)
	}
	rows, _ := m.rows(ctx)
	if rows[0].Rollout != "25%" {
		t.Fatalf("rollout = %q, want 25%%", rows[0].Rollout)
	}
	for _, bad := range []int{-1, 101} {
		if err := m.setRollout(ctx, "ro", bad); err == nil {
			t.Fatalf("setRollout %d must error", bad)
		}
	}
	// 0 and 100 are valid edges.
	for _, ok := range []int{0, 100} {
		if err := m.setRollout(ctx, "ro", ok); err != nil {
			t.Fatalf("setRollout %d: %v", ok, err)
		}
	}
}

func TestTUIModelCreateValidatesKey(t *testing.T) {
	m, _ := tuiTestStore(t)
	ctx := context.Background()
	if err := m.create(ctx, ""); err == nil {
		t.Fatal("empty key must error")
	}
	if err := m.create(ctx, "has space"); err == nil {
		t.Fatal("whitespace key must error")
	}
	if err := m.create(ctx, "valid_key.1-x"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
}

func TestTUIModelRemoveAndEnvironments(t *testing.T) {
	m, _ := tuiTestStore(t)
	ctx := context.Background()
	m.create(ctx, "gone")
	if err := m.remove(ctx, "gone"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	rows, _ := m.rows(ctx)
	if len(rows) != 0 {
		t.Fatalf("rows after remove = %d, want 0", len(rows))
	}
	envs, err := m.environments(ctx)
	if err != nil {
		t.Fatalf("environments: %v", err)
	}
	if len(envs) != 3 || envs[0] != "dev" || envs[2] != "production" {
		t.Fatalf("environments = %v", envs)
	}
	// env switching changes which state rows show.
	m.create(ctx, "envtest")
	m.env = "dev"
	if err := m.toggle(ctx, "envtest"); err != nil {
		t.Fatalf("toggle in dev: %v", err)
	}
	m.env = "staging"
	rows, _ = m.rows(ctx)
	if rows[0].On {
		t.Fatal("dev-only toggle must not show in staging")
	}
}

func TestTUIModelPersistsToDisk(t *testing.T) {
	// The model's mutations must hit the real store — the TUI is a view,
	// not a separate state copy.
	dbPath := filepath.Join(t.TempDir(), "persist.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	m := newTUIModel(st)
	ctx := context.Background()
	if err := m.create(ctx, "persist-flag"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.toggle(ctx, "persist-flag"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	st.Close()

	st2, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	f, err := st2.GetFlag(ctx, "persist-flag")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !f.Environments["production"].On {
		t.Fatalf("TUI mutation did not persist: %+v", f.Environments)
	}
}
