//go:build fyne

package main

// desktop_fyne.go: `switchyard desktop` launches the Fyne GUI. This file
// only compiles with `-tags fyne` + CGO_ENABLED=1 (Fyne needs OpenGL).
// The GUI reuses tuiModel for every mutation — one logic layer, two
// renderers, so the TUI's tests cover the desktop's behavior too.

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/switchyard/switchyard/internal/store"
)

func launchDesktop(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("desktop", stderr)
	db := fs.String("db", defaultDBPath, "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "desktop: %v\n", err)
		return 1
	}
	defer st.Close()
	return runDesktop(st)
}

// runDesktop builds and shows the Fyne window. Requires a display.
func runDesktop(st *store.Store) int {
	m := newTUIModel(st)
	a := app.NewWithID("io.switchyard.desktop")
	w := a.NewWindow("switchyard")

	refresh := func(list *widget.List, envLabel *widget.Label) {
		rows, err := m.rows(context.Background())
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		list.Refresh()
		envLabel.SetText("environment: " + m.env)
		_ = rows
	}

	envLabel := widget.NewLabel("environment: production")
	list := widget.NewList(
		func() int {
			rows, _ := m.rows(context.Background())
			return len(rows)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("flag")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			rows, _ := m.rows(context.Background())
			if i < 0 || int(i) >= len(rows) {
				return
			}
			row := rows[i]
			state := "off"
			if row.On {
				state = "on"
			}
			o.(*widget.Label).SetText(fmt.Sprintf("%-28s %-8s %-4s %s", row.Key, row.Kind, state, row.Rollout))
		},
	)

	selectedKey := ""
	list.OnSelected = func(id widget.ListItemID) {
		rows, _ := m.rows(context.Background())
		if int(id) < len(rows) {
			selectedKey = rows[id].Key
		}
	}
	list.OnUnselected = func(_ widget.ListItemID) { selectedKey = "" }

	selected := func() string { return selectedKey }

	toggleBtn := widget.NewButton("Toggle (selected)", func() {
		if key := selected(); key != "" {
			if err := m.toggle(context.Background(), key); err != nil {
				dialog.ShowError(err, w)
			}
			refresh(list, envLabel)
		}
	})

	rolloutEntry := widget.NewEntry()
	rolloutEntry.SetPlaceHolder("rollout % e.g. 25")
	rolloutBtn := widget.NewButton("Set rollout", func() {
		key := selected()
		if key == "" {
			return
		}
		pct, err := strconv.Atoi(rolloutEntry.Text)
		if err != nil {
			dialog.ShowError(fmt.Errorf("rollout must be a number, got %q", rolloutEntry.Text), w)
			return
		}
		if err := m.setRollout(context.Background(), key, pct); err != nil {
			dialog.ShowError(err, w)
		}
		refresh(list, envLabel)
	})

	newEntry := widget.NewEntry()
	newEntry.SetPlaceHolder("new flag key")
	newBtn := widget.NewButton("Create flag", func() {
		if newEntry.Text == "" {
			return
		}
		if err := m.create(context.Background(), newEntry.Text); err != nil {
			dialog.ShowError(err, w)
		}
		newEntry.SetText("")
		refresh(list, envLabel)
	})

	deleteBtn := widget.NewButton("Delete (selected)", func() {
		if key := selected(); key != "" {
			if err := m.remove(context.Background(), key); err != nil {
				dialog.ShowError(err, w)
			}
			refresh(list, envLabel)
		}
	})

	envRadio := widget.NewRadioGroup([]string{"dev", "staging", "production"}, func(val string) {
		if val != "" {
			m.env = val
			refresh(list, envLabel)
		}
	})
	envRadio.SetSelected("production")
	envRadio.Horizontal = true

	controls := container.NewVBox(
		envLabel,
		envRadio,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, toggleBtn, deleteBtn),
		container.NewGridWithColumns(2, rolloutEntry, rolloutBtn),
		container.NewGridWithColumns(2, newEntry, newBtn),
	)

	w.SetContent(container.NewBorder(nil, controls, nil, nil, list))
	w.Resize(fyne.NewSize(720, 480))
	refresh(list, envLabel)
	w.ShowAndRun()
	return 0
}
