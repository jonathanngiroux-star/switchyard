//go:build !fyne

package main

// desktop_default.go: `switchyard desktop` in the default (cgo-free)
// build. The Fyne GUI requires cgo (GLFW/OpenGL), which the default
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
	fmt.Fprintln(stderr, "The Fyne desktop GUI needs cgo (OpenGL); the default switchyard binary is")
	fmt.Fprintln(stderr, "cgo-free so it runs everywhere (Docker, scratch, CI). Two ways to get the GUI:")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "  1. Build it yourself:   go build -tags fyne -o switchyard-desktop ./cmd/switchyard")
	fmt.Fprintln(stderr, "     then run:            switchyard-desktop desktop")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "  2. Download the desktop release for your OS from the releases page.")
	fmt.Fprintln(stderr, "")
	fmt.Fprintln(stderr, "The terminal UI ships in every binary: just run `switchyard` (or `switchyard tui`).")
	return 1
}
