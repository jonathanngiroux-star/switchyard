package main

// tui.go: the terminal UI. tview/tcell is pure Go (no cgo), so the TUI
// ships in the default cgo-free binary. All state mutation lives on
// tuiModel — fully testable without a TTY; rendering is a thin layer.

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

// tviewInput shows a modal prompt and resolves with the entered text.
func tviewInput(app *tview.Application, label string) string {
	// Simplified synchronous prompt via a form in a modal overlay.
	var answer string
	done := make(chan struct{})
	form := tview.NewForm()
	form.AddInputField(label, "", 30, nil, func(text string) { answer = text })
	form.AddButton("OK", func() { close(done); app.Stop() })
	app.SetRoot(form, true).SetFocus(form)
	go func() { <-done }()
	if err := app.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(answer)
}

// tviewFlash shows a transient error message.
func tviewFlash(app *tview.Application, msg string) {
	modal := tview.NewModal().SetText(msg).AddButtons([]string{"OK"})
	modal.SetDoneFunc(func(_ int, _ string) { app.Stop() })
	app.SetRoot(modal, true)
	_ = app.Run()
}

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
func runTUI(st *store.Store) int {
	m := newTUIModel(st)
	app := tview.NewApplication()

	table := tview.NewTable().SetBorders(true).SetSelectable(true, false)
	table.SetBorder(true).SetTitle(" switchyard — flags (Enter: toggle, r: rollout, n: new, d: delete, 1/2/3: env, q: quit) ")

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

	app.SetInputCapture(func(event *tcellEvent) *tcellEvent {
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
			refresh()
			return nil
		case 'n':
			key := tviewInput(app, "new flag key")
			if key != "" {
				if err := m.create(context.Background(), key); err != nil {
					tviewFlash(app, err.Error())
				}
			}
			refresh()
			return nil
		case 'd':
			if key := selectedKey(); key != "" {
				if err := m.remove(context.Background(), key); err != nil {
					tviewFlash(app, err.Error())
				}
			}
			refresh()
			return nil
		case 'r':
			if key := selectedKey(); key != "" {
				pctStr := tviewInput(app, "rollout % (0-100)")
				if pct, err := strconv.Atoi(strings.TrimSpace(pctStr)); err == nil {
					if err := m.setRollout(context.Background(), key, pct); err != nil {
						tviewFlash(app, err.Error())
					}
				} else if pctStr != "" {
					tviewFlash(app, "rollout must be a number")
				}
			}
			refresh()
			return nil
		}
		return event
	})

	table.SetSelectedFunc(func(row, col int) {
		if key := selectedKey(); key != "" {
			if err := m.toggle(context.Background(), key); err != nil {
				tviewFlash(app, err.Error())
			}
		}
		refresh()
	})

	refresh()
	if err := app.SetRoot(table, true).SetFocus(table).Run(); err != nil {
		return 1
	}
	return 0
}
