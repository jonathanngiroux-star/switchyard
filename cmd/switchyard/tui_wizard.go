package main

// tui_wizard.go: the first-run TUI wizard (step 5 of the pipeline).
//
// Runs when the store is empty; reopened with '?' from the main screen.
// Same job as the GUI wizard: welcome → pick environment → create the
// first flag → donate. All mutations go through tuiModel — the tested
// logic layer — and the same SQLite the GUI and serve use.
//
// The wizard replaces the old tviewInput/tviewFlash modal helpers,
// which had a fatal flaw: they called app.Run() INSIDE the running app
// (a second event loop) and their handlers called app.Stop(), killing
// the whole TUI. The wizard runs under the ONE app.Run() in runTUI and
// swaps screens with SetRoot — the correct tview pattern. It signals
// completion via a channel; the main loop then re-renders the table.

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	tuiDonateETH = "0x85ee7E71f762d772599cbF1EC20E651B30657521"
	tuiDonateBTC = "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
)

const (
	wizStepWelcome = iota
	wizStepEnv
	wizStepFlag
	wizStepDonate
	wizStepCount
)

// wizard drives the first-run flow. It does NOT call app.Run() itself —
// runTUI owns the single event loop; the wizard only swaps roots and
// reports completion through the done channel.
type wizard struct {
	model     *tuiModel
	app       *tview.Application
	step      int
	flagKey   string
	envChoice string
	status    string
	done      chan bool // true = finished, false = cancelled
}

func newWizard(app *tview.Application, m *tuiModel) *wizard {
	return &wizard{model: m, app: app, done: make(chan bool, 1)}
}

// run shows the wizard under the caller's event loop.
func (w *wizard) run() {
	w.render()
}

func (w *wizard) finish(ok bool) {
	// NEVER call app.Stop() here: Stop kills the app's ONE event loop,
	// which runTUI owns. The waiter goroutine in runTUI reinstalls the
	// table when it receives from done; the loop keeps running.
	select {
	case w.done <- ok:
	default:
	}
}

func (w *wizard) render() {
	var body string
	switch w.step {
	case wizStepWelcome:
		body = "This wizard walks the core workflow once: pick an\n" +
			"environment, create a flag, toggle it.\n\n" +
			"The same SQLite file serves the TUI, the web UI, and the\n" +
			"desktop GUI — everything here is real.\n\n" +
			"Locally evaluated. OpenFeature-compatible. Dry-run\n" +
			"migrations for LaunchDarkly and Unleash."
	case wizStepEnv:
		envs, err := w.model.environments(context.Background())
		if err != nil {
			body = "environment: (error: " + err.Error() + ")"
		} else {
			body = "Flags are configured per environment.\n\n" +
				"Press 1, 2, or 3 to choose, Enter to keep the current one.\n\n"
			for i, e := range envs {
				body += fmt.Sprintf("  [%d] %s\n", i+1, e)
			}
			body += "\ncurrent environment: " + w.model.env
		}
	case wizStepFlag:
		body = "Create your first flag — it appears in the table when\n" +
			"the wizard closes.\n\n" +
			"Type the key and press Enter. Empty + Enter skips creation."
	case wizStepDonate:
		body = "Switchyard is funded by optional tips. No subscriptions,\n" +
			"no feature gates — everything works whether you donate or not.\n\n" +
			"Ethereum — ETH and USDC (ERC-20):\n  " + tuiDonateETH + "\n\n" +
			"Bitcoin — native SegWit (bech32):\n  " + tuiDonateBTC + "\n\n" +
			"These addresses match README byte-for-byte.\n\n" +
			"Press Enter to finish."
	}

	title := fmt.Sprintf(" switchyard setup — step %d/%d (Enter: next · ←: back · Esc: skip) ",
		w.step+1, wizStepCount)
	if w.status != "" {
		title += "— " + w.status + " "
	}

	text := tview.NewTextView().SetText(body)
	text.SetBorder(true).SetTitle(title)

	keyHandler := func(event *tcell.EventKey) *tcell.EventKey {
		// step-specific keys first
		if w.step == wizStepEnv {
			switch event.Rune() {
			case '1', '2', '3':
				envs, err := w.model.environments(context.Background())
				if err == nil {
					idx := int(event.Rune() - '1')
					if idx < len(envs) {
						w.model.env = envs[idx]
						w.status = "environment: " + envs[idx]
						w.render()
					}
				}
				return nil
			}
		}
		switch event.Key() {
		case tcell.KeyEnter:
			w.next()
			return nil
		case tcell.KeyEscape:
			w.finish(false)
			return nil
		case tcell.KeyLeft:
			w.back()
			return nil
		}
		return event
	}

	if w.step == wizStepFlag {
		inputField := tview.NewInputField().
			SetLabel("flag key: ").
			SetText(w.flagKey).
			SetFieldWidth(30).
			SetAcceptanceFunc(func(_ string, r rune) bool {
				return isFlagKeyRune(r)
			})
		// The field's DoneFunc is the ONLY Enter path on this step —
		// no flex-level capture (it would race the field's handler and
		// bypass flag creation). Esc cancels via DoneFunc too.
		inputField.SetDoneFunc(func(key tcell.Key) {
			switch key {
			case tcell.KeyEnter:
				w.flagKey = inputField.GetText()
				if w.flagKey != "" {
					if err := w.model.create(context.Background(), w.flagKey); err != nil {
						w.status = err.Error()
						w.render()
						return
					}
				}
				w.next()
			case tcell.KeyEscape:
				w.finish(false)
			}
		})
		flex := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(text, 0, 1, false).
			AddItem(inputField, 3, 0, true)
		w.app.SetRoot(flex, true).SetFocus(inputField)
		return
	}

	text.SetInputCapture(keyHandler)
	w.app.SetRoot(text, true).SetFocus(text)
}

func (w *wizard) next() {
	if w.step == wizStepDonate {
		w.finish(true)
		return
	}
	w.step++
	w.render()
}

func (w *wizard) back() {
	if w.step > 0 {
		w.step--
		w.render()
	}
}

// isFlagKeyRune: flag keys are letters, digits, '.', '_', '-'.
func isFlagKeyRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-':
		return true
	}
	return false
}
