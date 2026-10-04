//go:build !desktop

package main

import (
	"strings"
	"testing"
)

// TestBareSwitchyardOpensTUI pins the product decision: bare `switchyard`
// in a terminal launches the TUI (interactive mode) in the DEFAULT build,
// not the usage dump. In the desktop build bare invocation opens the
// Wails GUI instead — pinned by TestBareInvocationRoutesToGUI in
// desktop_wails_test.go — so this test is default-build-only.
func TestBareSwitchyardOpensTUI(t *testing.T) {
	var out, errBuf strings.Builder
	// In tests stdin is not a TTY; the launcher must detect that and fall
	// back to printing usage with a hint — NOT crash, NOT hang.
	code := run([]string{}, &out, &errBuf)
	if code != 2 {
		t.Fatalf("bare run in non-TTY = exit %d, want 2 (usage hint)", code)
	}
	combined := out.String() + errBuf.String()
	if !strings.Contains(combined, "switchyard") || !strings.Contains(combined, "serve") {
		t.Fatalf("usage hint must name switchyard and serve:\n%s", combined)
	}
}
