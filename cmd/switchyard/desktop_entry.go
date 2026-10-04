//go:build desktop

package main

import "io"

// desktop_entry.go: `switchyard desktop` in the Wails build
// (-tags desktop, CGO_ENABLED=1). launchDesktop is the entry; the real
// implementation is desktop_wails.go. This indirection keeps the tag
// wiring in one place per build flavor.
//
// Wails' `wails dev` compiles the app with its own `dev` tag (user
// -tags are not merged into that build) and it expects the main
// package at the project root — this repo keeps it at cmd/switchyard/.
// So in the desktop build, BARE invocation also routes to the GUI:
// that is what `wails dev` actually launches, and a desktop binary
// whose default surface is the GUI is the least surprising behavior.
// `switchyard tui` still opens the TUI explicitly, in any build.
func launchDesktop(args []string, stdout, stderr io.Writer) int {
	_ = stdout
	return launchDesktopWails(args, nil, stderr)
}

// launchDefault routes bare `switchyard` in the desktop build to the GUI.
func launchDefault(stdout, stderr io.Writer) int {
	return launchDesktopWails(nil, stdout, stderr)
}
