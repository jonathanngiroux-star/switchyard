//go:build desktop

package main

// testbridge.go: in-app GUI test bridge (the FogOS/Proofspan pattern,
// per the go-gui-audit-testing skill). A Wayland/X11 WebKitGTK webview
// cannot be driven from outside — xdotool keystrokes never reach it —
// so the app tests itself: when launched with
// SWITCHYARD_GUI_TESTBRIDGE=1, a goroutine polls a command file and
// forwards each command as a "test:cmd" event into the webview. The
// frontend interpreter (frontend/index.html) executes it against the
// REAL DOM — real handlers, real bindings, real store writes — and
// returns results via the TestResult binding, which appends
// `TEST-RESULT {json}` lines to a log the external driver reads.
//
// Security: the interpreter is a fixed switch — no eval — so the page
// CSP holds. The bridge arms ONLY on the env var; production launches
// never poll and never arm.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	bridgeDefaultCmdFile    = "/tmp/switchyard-gui-cmd.jsonl"
	bridgeDefaultResultFile = "/tmp/switchyard-gui-result.log"
)

type bridgeConfig struct {
	enabled    bool
	cmdFile    string
	resultFile string
}

var bridge struct {
	mu     sync.Mutex
	config bridgeConfig
}

func loadBridgeConfig() bridgeConfig {
	c := bridgeConfig{
		enabled:    os.Getenv("SWITCHYARD_GUI_TESTBRIDGE") == "1",
		cmdFile:    os.Getenv("SWITCHYARD_GUI_CMD_FILE"),
		resultFile: os.Getenv("SWITCHYARD_GUI_RESULT_FILE"),
	}
	if c.cmdFile == "" {
		c.cmdFile = bridgeDefaultCmdFile
	}
	if c.resultFile == "" {
		c.resultFile = bridgeDefaultResultFile
	}
	return c
}

// startTestBridge arms the bridge (called from startup) and begins
// polling. No-op in a normal launch.
func (a *App) startTestBridge() {
	bridge.mu.Lock()
	bridge.config = loadBridgeConfig()
	c := bridge.config
	bridge.mu.Unlock()
	if !c.enabled {
		return
	}
	// stale commands from a previous run must never fire
	_ = os.Remove(c.cmdFile)
	if f, err := os.OpenFile(c.resultFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	} else {
		return // result channel broken → bridge stays off
	}
	go a.pollBridgeCommands(c)
}

// pollBridgeCommands forwards command-file lines into the webview. The
// driver keeps ONE command in flight (it waits for each result), so
// read-all-then-truncate cannot drop a command.
func (a *App) pollBridgeCommands(c bridgeConfig) {
	for {
		time.Sleep(100 * time.Millisecond)
		if a.ctx == nil {
			continue
		}
		data, err := os.ReadFile(c.cmdFile)
		if err != nil || len(data) == 0 {
			continue
		}
		_ = os.Truncate(c.cmdFile, 0)
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var cmd map[string]any
			if err := json.Unmarshal([]byte(line), &cmd); err != nil {
				a.appendBridgeResult(`{"id":"","ok":false,"error":"bad command json: ` + err.Error() + `"}`)
				continue
			}
			runtime.EventsEmit(a.ctx, "test:cmd", cmd)
		}
	}
}

// TestResult receives one interpreter result from the frontend and
// appends it to the result log. Bridge-only; ignored in normal launches.
func (a *App) TestResult(resultJSON string) {
	bridge.mu.Lock()
	c := bridge.config
	bridge.mu.Unlock()
	if !c.enabled {
		return
	}
	f, err := os.OpenFile(c.resultFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "TEST-RESULT %s\n", resultJSON)
}

// appendBridgeResult is the Go-side error path (malformed command JSON).
func (a *App) appendBridgeResult(json string) {
	bridge.mu.Lock()
	c := bridge.config
	bridge.mu.Unlock()
	if !c.enabled {
		return
	}
	if f, err := os.OpenFile(c.resultFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintf(f, "TEST-RESULT %s\n", json)
		f.Close()
	}
}
