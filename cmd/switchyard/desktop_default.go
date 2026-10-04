//go:build !desktop

package main

// desktop_default.go: `switchyard desktop` in the default (cgo-free)
// build. The Wails GUI requires cgo (webkit2gtk), which the default
// binary deliberately omits — Docker images, scratch containers, and CI
// all stay pure-Go. This stub explains the situation and how to get the
// GUI build instead of crashing or silently doing nothing.

import (
	"fmt"
	"io"
)

func launchDesktop(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("desktop", stderr)
	db := fs.String("db", defaultDBPath, "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_ = db
	fmt.Fprintln(stderr, "switchyard: this binary was built without the desktop GUI.")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "The Wails desktop GUI needs cgo (webkit2gtk); the default switchyard binary is")
	fmt.Fprintln(stderr, "cgo-free so it runs everywhere (Docker, scratch, CI). Two ways to get the GUI:")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "  1. Build it yourself:   CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -o switchyard-desktop ./cmd/switchyard")
	fmt.Fprintln(stderr, "     then run:            switchyard-desktop desktop")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "  2. Download the desktop release for your OS from the releases page.")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "The terminal UI ships in every binary: just run `switchyard` (or `switchyard tui`).")
	return 1
}

// launchDefault in the default build is the TUI (the operator's front
// door); the desktop build overrides this to open the GUI.
func launchDefault(stdout, stderr io.Writer) int {
	return launchTUI(nil, stdout, stderr)
}
