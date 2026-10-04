package main

// tui.go: the terminal UI. tview/tcell is pure Go (no cgo), so the TUI
// ships in the default cgo-free binary. All state mutation lives on
// tuiModel — fully testable without a TTY; rendering is a thin layer.
//
// ONE event loop per process: runTUI owns the single app.Run(). The
// wizard and prompts swap roots with SetRoot and never call Run() or
// Stop() themselves — the old tviewInput/tviewFlash helpers did both
// and killed the app on every n/r/error path (see docs/audit/tui.md).

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// Aliases keep the render layer terse.
type tcellEvent = tcell.EventKey

const tcellColorGreen = tcell.ColorGreen

// tuiModel is the TUI's testable core: it owns the store handle, the
// selected environment, and every mutation the UI can perform.
type tuiModel struct {
	st  *store.Store
	env string
}

func newTUIModel(st *store.Store) *tuiModel {
	return &tuiModel{st: st, env: "production"}
}

// rows returns one display row per flag: key, kind, on/off, rollout %.
func (m *tuiModel) rows(ctx context.Context) ([]tuiRow, error) {
	flags, err := m.st.ListFlags(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Key < flags[j].Key })
	rows := make([]tuiRow, 0, len(flags))
	for _, f := range flags {
		fe := f.Environments[m.env]
		pct := "—"
		if fe.Rollout != nil && fe.Rollout.Kind == "percentage" {
			pct = strconv.Itoa(fe.Rollout.Percentage) + "%"
		}
		rows = append(rows, tuiRow{Key: f.Key, Kind: string(f.Kind), On: fe.On, Rollout: pct})
	}
	return rows, nil
}

type tuiRow struct {
	Key     string
	Kind    string
	On      bool
	Rollout string
}

// toggle flips a flag in the selected environment.
func (m *tuiModel) toggle(ctx context.Context, key string) error {
	f, err := m.st.GetFlag(ctx, key)
	if err != nil {
		return err
	}
	fe := f.Environments[m.env]
	fe.On = !fe.On
	return m.st.SetFlagEnvironment(ctx, key, m.env, fe)
}

// setRollout sets the percentage rollout in the selected environment.
func (m *tuiModel) setRollout(ctx context.Context, key string, pct int) error {
	if pct < 0 || pct > 100 {
		return fmt.Errorf("rollout must be 0..100, got %d", pct)
	}
	f, err := m.st.GetFlag(ctx, key)
	if err != nil {
		return err
	}
	fe := f.Environments[m.env]
	fe.Rollout = &model.Rollout{Kind: "percentage", Percentage: pct}
	return m.st.SetFlagEnvironment(ctx, key, m.env, fe)
}

// create adds a new boolean flag.
func (m *tuiModel) create(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("flag key is required")
	}
	if strings.ContainsAny(key, " \t") {
		return fmt.Errorf("flag key cannot contain whitespace")
	}
	return m.st.PutFlag(ctx, model.Flag{
		Key: key, Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{},
	})
}

// remove deletes a flag.
func (m *tuiModel) remove(ctx context.Context, key string) error {
	return m.st.DeleteFlag(ctx, key)
}

// environments lists the seeded environments in order.
func (m *tuiModel) environments(ctx context.Context) ([]string, error) {
	envs, err := m.st.ListEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(envs))
	for _, e := range envs {
		out = append(out, e.Key)
	}
	return out, nil
}

// runTUI renders the model with tview. Interactive only — the launcher
// guarantees a TTY before calling this.
//
// Structure: ONE app.Run() at the bottom — the only place app.Run or
// app.Stop is ever called. The first-run wizard (empty store) swaps the
// root under that loop; when it finishes, its done channel tells the
// waiter goroutine to reinstall the table. Prompts ('n', 'r') are
// SetRoot swaps too; answers come back on channels.
func runTUI(st *store.Store) int {
	m := newTUIModel(st)
	app := tview.NewApplication()

	const keybar = " Enter: toggle · r: rollout · n: new · d: delete · ?: setup wizard · 1/2/3: env · q: quit "
	table := tview.NewTable().SetBorders(true).SetSelectable(true, false)
	table.SetBorder(true).SetTitle(" switchyard — flags " + keybar)

	// uiModal is true while the wizard or a prompt owns the screen. The
	// global input capture must NOT react to single letters/digits then
	// (it stole '2' from the wizard's env picker and killed the flow).
	uiModal := false

	refresh := func() {
		rows, err := m.rows(context.Background())
		if err != nil {
			table.SetCell(0, 0, tview.NewTableCell(err.Error()))
			return
		}
		table.Clear()
		if len(rows) == 0 {
			table.SetCell(0, 0, tview.NewTableCell("no flags — press n to create"))
			return
		}
		header := []string{"flag", "kind", "state", "rollout"}
		for c, h := range header {
			table.SetCell(0, c, tview.NewTableCell(h).SetSelectable(false).SetTextColor(tview.Styles.SecondaryTextColor))
		}
		for r, row := range rows {
			state := "off"
			color := tview.Styles.PrimaryTextColor
			if row.On {
				state = "on"
				color = tcellColorGreen
			}
			table.SetCell(r+1, 0, tview.NewTableCell(row.Key).SetTextColor(color))
			table.SetCell(r+1, 1, tview.NewTableCell(row.Kind).SetTextColor(tview.Styles.SecondaryTextColor))
			table.SetCell(r+1, 2, tview.NewTableCell(state).SetTextColor(color))
			table.SetCell(r+1, 3, tview.NewTableCell(row.Rollout).SetTextColor(tview.Styles.SecondaryTextColor))
		}
	}

	var selectedKey func() string
	selectedKey = func() string {
		row, _ := table.GetSelection()
		if row < 1 {
			return ""
		}
		cell := table.GetCell(row, 0)
		if cell == nil {
			return ""
		}
		return cell.Text
	}

	installTable := func() {
		uiModal = false
		refresh()
		app.SetRoot(table, true).SetFocus(table)
	}

	showStatus := func(msg string) {
		table.SetTitle(" " + msg + " " + keybar)
	}

	// promptFor swaps in a one-field form. The answer is handled by the
	// onDone callback — the input capture returns immediately and NEVER
	// blocks the event loop (a <-channel read inside the capture would
	// deadlock: the sender runs on this same loop).
	promptFor := func(label, initial string, onDone func(string)) {
		uiModal = true
		input := tview.NewInputField().
			SetLabel(label).
			SetText(initial).
			SetFieldWidth(30)
		input.SetDoneFunc(func(key tcell.Key) {
			answer := ""
			if key == tcell.KeyEnter {
				answer = strings.TrimSpace(input.GetText())
			}
			installTable()
			onDone(answer)
		})
		app.SetRoot(input, true).SetFocus(input)
	}

	// startWizard runs the wizard under the single loop and reinstalls
	// the table when it completes (finish or cancel).
	startWizard := func() {
		uiModal = true
		w := newWizard(app, m)
		go func() {
			<-w.done
			app.QueueUpdateDraw(func() { installTable() })
		}()
		w.run()
	}

	app.SetInputCapture(func(event *tcellEvent) *tcellEvent {
		// While the wizard or a prompt owns the screen, ONLY q is
		// honored globally (kill switch); everything else goes to the
		// modal's own handler.
		if uiModal && event.Rune() != 'q' {
			return event
		}
		switch event.Rune() {
		case 'q':
			app.Stop()
			return nil
		case '1', '2', '3':
			idx := int(event.Rune() - '1')
			envs, err := m.environments(context.Background())
			if err == nil && idx < len(envs) {
				m.env = envs[idx]
			}
			installTable()
			return nil
		case 'n':
			promptFor("new flag key: ", "", func(key string) {
				if key == "" {
					return
				}
				if err := m.create(context.Background(), key); err != nil {
					showStatus(err.Error())
				} else {
					showStatus("created " + key)
				}
				installTable()
			})
			return nil
		case 'd':
			if key := selectedKey(); key != "" {
				if err := m.remove(context.Background(), key); err != nil {
					showStatus(err.Error())
				} else {
					showStatus("deleted " + key)
				}
			}
			installTable()
			return nil
		case 'r':
			if key := selectedKey(); key != "" {
				promptFor("rollout % (0-100): ", "", func(answer string) {
					if answer == "" {
						return
					}
					pct, err := strconv.Atoi(strings.TrimSpace(answer))
					if err != nil {
						showStatus("rollout must be a number")
					} else if err := m.setRollout(context.Background(), key, pct); err != nil {
						showStatus(err.Error())
					} else {
						showStatus(fmt.Sprintf("rollout %s = %d%%", key, pct))
					}
					installTable()
				})
			}
			return nil
		case '?':
			startWizard()
			return nil
		}
		return event
	})

	table.SetSelectedFunc(func(row, col int) {
		if key := selectedKey(); key != "" {
			if err := m.toggle(context.Background(), key); err != nil {
				showStatus(err.Error())
			} else {
				showStatus("toggled " + key)
			}
		}
		installTable()
	})

	// First run on an empty store: the wizard replaces the table until
	// it completes. Non-empty stores go straight to the table.
	if rows, err := m.rows(context.Background()); err == nil && len(rows) == 0 {
		startWizard()
	} else {
		installTable()
	}
	if err := app.Run(); err != nil {
		return 1
	}
	return 0
}
