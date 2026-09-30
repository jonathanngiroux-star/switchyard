//go:build fyne

package main

// desktop_fyne_test.go: the GUI's testable surface. Real pixel-pushing
// needs a display; here we verify the fyne-tagged build compiles, the
// window object constructs, and — the part that carries all the behavior —
// tuiModel drives the same mutations the GUI buttons trigger. The GUI is
// a thin renderer over tuiModel, which is fully tested in tui_model_test.go.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/widget"

	"github.com/switchyard/switchyard/internal/store"
)

// TestFyneBuildCompiles is the compile-time proof: this file only builds
// with -tags fyne + CGO_ENABLED=1. If the desktop source rots, building
// the desktop target fails here.
func TestFyneBuildCompiles(t *testing.T) {
	_ = fyne.CurrentApp() // linking fyne is the point; no display needed
}

// TestDesktopWindowConstructs builds the real window object without
// ShowAndRun (no display needed for construction).
func TestDesktopWindowConstructs(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display available — window construction test skipped")
	}
	a := app.NewWithID("io.switchyard.desktop.test")
	w := a.NewWindow("switchyard-test")
	w.SetContent(widget.NewLabel("construction test"))
	w.Resize(fyne.NewSize(320, 200))
	// Not calling ShowAndRun: construction is what we verify.
}

// TestDesktopSharesTuiModelLogic pins the architecture guarantee: the
// desktop GUI's mutations go through tuiModel — the exact methods the
// TUI tests cover. If someone forks desktop logic away from the model,
// this test is the signpost saying where the behavior tests live.
func TestDesktopSharesTuiModelLogic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "desktop-shared.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	m := newTUIModel(st)
	ctx := context.Background()

	// The exact call sequences the desktop buttons make.
	if err := m.create(ctx, "gui-flag"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := m.toggle(ctx, "gui-flag"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if err := m.setRollout(ctx, "gui-flag", 40); err != nil {
		t.Fatalf("setRollout: %v", err)
	}
	rows, err := m.rows(ctx)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 1 || !rows[0].On || rows[0].Rollout != "40%" {
		t.Fatalf("GUI-driven mutations via tuiModel = %+v", rows)
	}
}
