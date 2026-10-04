//go:build desktop

package main

// desktop_wails.go: the Wails v2 desktop GUI. Only compiles with
// `-tags desk` + CGO_ENABLED=1 (webkit2gtk). One binding file over the
// existing store/eval/migrate functions — no second data model, no
// browser-only admin site. The frontend (cmd/switchyard/frontend/) calls
// window.go.main.App.* bindings injected by the Wails runtime.
//
// Step 3 of the 7-step pipeline (03-gui.md): Wails is the binding GUI
// toolkit. Every mutation goes through the same store methods the HTTP
// API and TUI use, and every mutation appends to the audit log — the
// GUI must never be a side door around it.

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/switchyard/switchyard/internal/eval"
	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/migrate/launchdarkly"
	"github.com/switchyard/switchyard/internal/migrate/unleash"
	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// Donations listed in every UI footer per the funding rule. Byte-for-byte
// the README addresses; do not gate anything on them.
const (
	guiDonateETH  = "0x85ee7E71f762d772599cbF1EC20E651B30657521"
	guiDonateBTC  = "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
	guiDonateNote = "tips only — never a paywall, never counted as traction"
)

// frontend is embedded into the desktop binary: one vanilla HTML file,
// no npm, no bundler. Solo-maintainable by decree.
//
//go:embed all:frontend
var frontendEmbed embed.FS

// frontendAssets serves the embedded frontend at /.
func frontendAssets() fs.FS {
	sub, err := fs.Sub(frontendEmbed, "frontend")
	if err != nil {
		panic("desktop: embedded frontend missing: " + err.Error())
	}
	return sub
}

// App is the binding struct for the Wails frontend. All methods are
// bound via Bind: []any{app}; the frontend calls window.go.main.App.X.
type App struct {
	st  *store.Store
	db  string
	env string
	ctx context.Context // wails context; used by the test bridge only
}

// FlagRow is one row in the GUI flag table: the flag plus its resolved
// state in the selected environment.
type FlagRow struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	On      bool   `json:"on"`
	Rollout string `json:"rollout"`
	Rules   int    `json:"rules"`
}

// EvalResult is what the eval console shows for one evaluation.
type EvalResult struct {
	Enabled   bool   `json:"enabled"`
	Value     any    `json:"value"`
	Variant   string `json:"variant"`
	Reason    string `json:"reason"`
	ErrorCode string `json:"errorCode,omitempty"`
	Bucket    int    `json:"bucket"`
}

// MigrateDryRun is the dry-run viewer payload: summary + unmapped gaps
// for one source system.
type MigrateDryRun struct {
	Source   string             `json:"source"`
	Projects []string           `json:"projects"`
	Summary  migrate.Summary    `json:"summary"`
	Unmapped []migrate.Unmapped `json:"unmapped"`
	Fidelity float64            `json:"fidelity"`
	Gate     float64            `json:"gate"`
	Refused  bool               `json:"refused"` // true = below gate, import would be refused
}

func newApp(dbPath string) (*App, error) {
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	return &App{st: st, db: dbPath, env: "production"}, nil
}

func (a *App) close() { _ = a.st.Close() }

// startup receives the wails context and arms the test bridge (a no-op
// unless SWITCHYARD_GUI_TESTBRIDGE=1). Store calls use
// context.Background() so a hung window event loop can never starve
// mutations.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startTestBridge()
}

// --- flags -------------------------------------------------------------

// ListFlags returns all flags with their resolved state in the selected
// environment.
func (a *App) ListFlags() ([]FlagRow, error) {
	flags, err := a.st.ListFlags(context.Background())
	if err != nil {
		return nil, err
	}
	rows := make([]FlagRow, 0, len(flags))
	for _, f := range flags {
		fe := f.Environments[a.env]
		rollout := "—"
		if fe.Rollout != nil && fe.Rollout.Kind == "percentage" {
			rollout = fmt.Sprintf("%d%%", fe.Rollout.Percentage)
		}
		rows = append(rows, FlagRow{
			Key: f.Key, Name: f.Name, Kind: string(f.Kind),
			On: fe.On, Rollout: rollout, Rules: len(fe.Rules),
		})
	}
	return rows, nil
}

// GetFlag returns one flag's full definition.
func (a *App) GetFlag(key string) (model.Flag, error) {
	return a.st.GetFlag(context.Background(), key)
}

// CreateFlag validates and creates a boolean flag.
func (a *App) CreateFlag(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("flag key is required")
	}
	if strings.ContainsAny(key, " \t") {
		return fmt.Errorf("flag key cannot contain whitespace")
	}
	f := model.Flag{Key: key, Kind: model.KindBoolean, Environments: map[string]model.FlagEnvironment{}}
	if err := a.st.PutFlag(context.Background(), f); err != nil {
		return err
	}
	a.audit("create", key, "", "", jsonString(f))
	return nil
}

// ToggleFlag flips a flag in the selected environment.
func (a *App) ToggleFlag(key string) error {
	f, err := a.st.GetFlag(context.Background(), key)
	if err != nil {
		return err
	}
	fe := f.Environments[a.env]
	before := jsonString(fe)
	fe.On = !fe.On
	if err := a.st.SetFlagEnvironment(context.Background(), key, a.env, fe); err != nil {
		return err
	}
	a.audit("toggle", key, a.env, before, jsonString(fe))
	return nil
}

// SetRollout sets the percentage rollout in the selected environment.
func (a *App) SetRollout(key string, pct int) error {
	if pct < 0 || pct > 100 {
		return fmt.Errorf("rollout must be 0..100, got %d", pct)
	}
	f, err := a.st.GetFlag(context.Background(), key)
	if err != nil {
		return err
	}
	fe := f.Environments[a.env]
	before := jsonString(fe)
	fe.Rollout = &model.Rollout{Kind: "percentage", Percentage: pct}
	if err := a.st.SetFlagEnvironment(context.Background(), key, a.env, fe); err != nil {
		return err
	}
	a.audit("rollout", key, a.env, before, jsonString(fe))
	return nil
}

// DeleteFlag removes a flag.
func (a *App) DeleteFlag(key string) error {
	before, _ := a.st.GetFlag(context.Background(), key)
	if err := a.st.DeleteFlag(context.Background(), key); err != nil {
		return err
	}
	a.audit("delete", key, "", jsonString(before), "")
	return nil
}

// --- environments & segments ------------------------------------------

// ListEnvironments returns the seeded environments.
func (a *App) ListEnvironments() ([]model.Environment, error) {
	return a.st.ListEnvironments(context.Background())
}

// SetEnvironment switches the GUI's selected environment. Validated
// against the store so a typo can't silently show empty state.
func (a *App) SetEnvironment(env string) error {
	envs, err := a.st.ListEnvironments(context.Background())
	if err != nil {
		return err
	}
	for _, e := range envs {
		if e.Key == env {
			a.env = env
			return nil
		}
	}
	return fmt.Errorf("unknown environment %q", env)
}

// Environment returns the currently selected environment key.
func (a *App) Environment() string { return a.env }

// ListSegments returns all segments.
func (a *App) ListSegments() ([]model.Segment, error) {
	return a.st.ListSegments(context.Background())
}

// PutSegment upserts a segment from its JSON (edited in a textarea).
func (a *App) PutSegment(segJSON string) error {
	var seg model.Segment
	if err := json.Unmarshal([]byte(segJSON), &seg); err != nil {
		return fmt.Errorf("segment JSON invalid: %v", err)
	}
	if seg.Key == "" {
		return fmt.Errorf("segment key is required")
	}
	if err := a.st.PutSegment(context.Background(), seg); err != nil {
		return err
	}
	a.audit("segment", seg.Key, "", "", segJSON)
	return nil
}

// --- eval console ------------------------------------------------------

// Evaluate runs the local evaluator for the eval console. Uses the same
// eval.Evaluate the SDKs and /evaluate use — no forked semantics.
func (a *App) Evaluate(flagKey, userKey, attributesJSON string) (EvalResult, error) {
	f, err := a.st.GetFlag(context.Background(), flagKey)
	if err != nil {
		return EvalResult{}, err
	}
	segments, err := a.segmentMap()
	if err != nil {
		return EvalResult{}, err
	}
	ctx := eval.Context{UserKey: userKey}
	if attributesJSON = strings.TrimSpace(attributesJSON); attributesJSON != "" {
		attrs := map[string]string{}
		if err := json.Unmarshal([]byte(attributesJSON), &attrs); err != nil {
			return EvalResult{}, fmt.Errorf("attributes must be a JSON object of strings: %v", err)
		}
		ctx.Attributes = attrs
	}
	d := eval.Evaluate(f, a.env, segments, ctx)
	return EvalResult{
		Enabled: d.Enabled, Value: d.Value, Variant: d.Variant,
		Reason: d.Reason, ErrorCode: d.ErrorCode,
		Bucket: eval.Bucket(flagKey, userKey),
	}, nil
}

func (a *App) segmentMap() (map[string]model.Segment, error) {
	segs, err := a.st.ListSegments(context.Background())
	if err != nil {
		return nil, err
	}
	m := make(map[string]model.Segment, len(segs))
	for _, s := range segs {
		m[s.Key] = s
	}
	return m, nil
}

// --- migrate dry-run viewer --------------------------------------------

// DryRunMigrate runs the LaunchDarkly/Unleash dry-run on a file and
// returns the summary + unmapped list for the viewer. A corpus below
// the 90% gate is reported as refused — a result, not an error; the
// honesty contract says the viewer must show the refusal, not hide it.
func (a *App) DryRunMigrate(source, path string) (MigrateDryRun, error) {
	var parse func(data []byte) ([]model.Project, []migrate.Unmapped, error)
	switch source {
	case "launchdarkly":
		parse = launchdarkly.Parse
	case "unleash":
		parse = unleash.Parse
	default:
		return MigrateDryRun{}, fmt.Errorf("unknown source %q (launchdarkly|unleash)", source)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return MigrateDryRun{}, fmt.Errorf("read %s: %w", path, err)
	}
	projects, unmapped, err := parse(data)
	if err != nil {
		return MigrateDryRun{}, err
	}
	diff := migrate.DiffProjects(projects, nil, unmapped)
	report := migrate.FidelityReport(source, projects, unmapped)
	out := MigrateDryRun{
		Source:   source,
		Summary:  diff.Summary,
		Unmapped: unmapped,
		Fidelity: report.Score,
		Gate:     0.9,
		Refused:  !report.Pass(0.9),
	}
	out.Projects = make([]string, 0, len(projects))
	for _, p := range projects {
		out.Projects = append(out.Projects, p.Key)
	}
	return out, nil
}

// --- audit & diagnostics ------------------------------------------------

// RecentAudit returns the last audit entries, newest last (append order).
func (a *App) RecentAudit(limit int) ([]store.AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	return a.st.ListAudit(context.Background(), limit)
}

// DBInfo returns diagnostics for the status view.
func (a *App) DBInfo() map[string]string {
	abs, _ := filepath.Abs(a.db)
	return map[string]string{"db": abs, "env": a.env}
}

// --- donate ------------------------------------------------------------

// DonateInfo returns the donation addresses shown in the footer view.
func (a *App) DonateInfo() map[string]string {
	return map[string]string{
		"ethereum": guiDonateETH,
		"bitcoin":  guiDonateBTC,
		"note":     guiDonateNote,
	}
}

// audit appends one audit row. GUI mutations must not bypass the audit
// log — that was an explicit audit item. Failures are swallowed here
// rather than failing the mutation after it already committed; the
// audit log is best-effort on the GUI path, same as the HTTP path.
func (a *App) audit(action, key, env, before, after string) {
	_ = a.st.PutAudit(context.Background(), store.AuditEntry{
		TS: time.Now().UTC().Format(time.RFC3339), Actor: "gui", Action: action,
		Resource: "flag", Key: key, Env: env, Before: before, After: after,
	})
}

// --- wails app wiring ---------------------------------------------------

// launchDesktopWails is the real GUI entry, called from main_desk.go.
func launchDesktopWails(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("desktop", stderr)
	db := fs.String("db", defaultDBPath, "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	app, err := newApp(*db)
	if err != nil {
		fmt.Fprintf(stderr, "desktop: %v\n", err)
		return 1
	}
	defer app.close()

	err = wails.Run(&options.App{
		Title:            "switchyard",
		Width:            1024,
		Height:           700,
		MinWidth:         720,
		MinHeight:        480,
		AssetServer:      &assetserver.Options{Assets: frontendAssets()},
		BackgroundColour: &options.RGBA{R: 18, G: 18, B: 24, A: 255},
		OnStartup:        app.startup,
		Bind:             []any{app},
	})
	if err != nil {
		fmt.Fprintf(stderr, "desktop: %v\n", err)
		return 1
	}
	return 0
}

// jsonString is the shared serialization helper (mirrors serve.go).
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
