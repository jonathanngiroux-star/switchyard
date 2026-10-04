//go:build !desktop

package main

import (
	"strings"
	"testing"
)

// TestDesktopCommandExists pins that `switchyard desktop` is a recognized
// command in every build: in default builds it explains the build-tag
// situation; in desktop builds it launches the GUI.
func TestDesktopCommandExists(t *testing.T) {
	var out, errBuf strings.Builder
	// Non-TTY stdin in tests: the desktop launcher must not crash/hang;
	// it reports and exits non-zero.
	code := run([]string{"desktop"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("desktop without a display must exit non-zero, not pretend success")
	}
	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "desktop") {
		t.Fatalf("desktop output must mention desktop:\n%s", combined)
	}
}

// TestTUICommandFlagContracts pins the flag surface of the TUI launcher.
func TestTUICommandFlagContracts(t *testing.T) {
	// tui is the explicit form of the bare launch.
	var out, errBuf strings.Builder
	code := run([]string{"tui"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("tui without a TTY must not report success")
	}
	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "terminal") && !strings.Contains(combined, "TTY") && !strings.Contains(combined, "terminal") {
		t.Fatalf("tui non-TTY message should explain the terminal requirement:\n%s", combined)
	}
}
