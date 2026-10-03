//go:build desktop

package main

import "io"

// desktop_desk.go: `switchyard desktop` in the Wails build
// (-tags desk, CGO_ENABLED=1). launchDesktop is the entry; the real
// implementation is desktop_wails.go. This indirection keeps the tag
// wiring in one place per build flavor.
func launchDesktop(args []string, stdout, stderr io.Writer) int {
	_ = stdout
	return launchDesktopWails(args, nil, stderr)
}
