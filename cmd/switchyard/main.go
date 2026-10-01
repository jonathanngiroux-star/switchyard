// Command switchyard is the single binary: flag server, evaluator, and
// migration CLI. One process, one SQLite file, no external dependencies.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/switchyard/switchyard/internal/eval"
	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/migrate/launchdarkly"
	"github.com/switchyard/switchyard/internal/migrate/unleash"
	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/serve"
	"github.com/switchyard/switchyard/internal/store"
)

// version is the single source of truth for the binary version. Overridden
// at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

const (
	defaultDBPath = "switchyard.db"
	// Defaults live in defaultInputFor so --input default always matches
	// --from. A single LD fixture as the universal default made
	// `migrate --from=unleash` silently parse the LD export and report
	// an empty diff at "100%" fidelity.
)

// defaultInputFor returns the fixture path used when --input is omitted.
func defaultInputFor(from string) string {
	switch from {
	case "unleash":
		return "testdata/fixtures/unleash/unleash-representative.json"
	default:
		return "testdata/fixtures/launchdarkly/sample-export.json"
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// Bare `switchyard` in a terminal opens the TUI — the operator's
		// front door. In non-TTY contexts launchTUI prints a hint instead.
		return launchTUI(args, stdout, stderr)
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "switchyard version %s\n", version)
		return 0
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "eval":
		return runEval(args[1:], stdout, stderr)
	case "tui":
		return launchTUI(args[1:], stdout, stderr)
	case "desktop":
		return launchDesktop(args[1:], stdout, stderr)
	case "sdk":
		return runSDK(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `switchyard — self-hosted feature-flag control plane

Usage:
  switchyard                      # TUI (interactive terminal)
  switchyard tui [--db PATH]
  switchyard desktop [--db PATH]  # Fyne GUI (desktop build: -tags fyne)
  switchyard version
  switchyard serve [--addr :8080] [--db PATH]
  switchyard eval [--env ENV] --user KEY [--db PATH] [--attr k=v ...] FLAG
  switchyard migrate --from=launchdarkly --dry-run [--format human|json] [--input PATH]
  switchyard sdk ...                                 # W3–4
`)
}

// --- serve ---

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", ":8080", "listen address")
	db := fs.String("db", defaultDBPath, "path to the SQLite database file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	defer st.Close()
	fmt.Fprintf(stdout, "switchyard serving on %s (db: %s)\n", *addr, *db)
	srv := serve.New(st)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	return 0
}

// --- eval ---

func runEval(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	db := fs.String("db", defaultDBPath, "path to the SQLite database file")
	env := fs.String("env", "production", "environment to evaluate against")
	user := fs.String("user", "", "user key for bucketing and targeting")
	attrs := attrFlag{}
	fs.Var(&attrs, "attr", "context attribute k=v (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "eval: missing flag key argument")
		return 2
	}
	flagKey := fs.Arg(0)
	if *user == "" {
		fmt.Fprintln(stderr, "eval: --user is required")
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "eval: %v\n", err)
		return 1
	}
	defer st.Close()
	f, err := st.GetFlag(context.Background(), flagKey)
	if err == store.ErrNotFound {
		fmt.Fprintf(stderr, "eval: flag %q not found\n", flagKey)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "eval: %v\n", err)
		return 1
	}
	segs, err := st.ListSegments(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "eval: %v\n", err)
		return 1
	}
	segMap := make(map[string]model.Segment, len(segs))
	for _, sg := range segs {
		segMap[sg.Key] = sg
	}
	// Prerequisites: load the sibling flag set and enforce (default depth).
	allFlags, err := st.ListFlags(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "eval: %v\n", err)
		return 1
	}
	flagMap := make(map[string]model.Flag, len(allFlags))
	for _, af := range allFlags {
		flagMap[af.Key] = af
	}
	d := eval.EvaluateWithPrereqs(f, *env, segMap, eval.Context{UserKey: *user, Attributes: attrs}, flagMap, -1)
	if err := json.NewEncoder(stdout).Encode(d); err != nil {
		fmt.Fprintf(stderr, "eval: encode: %v\n", err)
		return 1
	}
	return 0
}

// attrFlag collects repeatable --attr k=v flags.
type attrFlag map[string]string

func (a *attrFlag) String() string { return "" }

func (a *attrFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("attr must be k=v, got %q", v)
	}
	if *a == nil {
		*a = attrFlag{}
	}
	(*a)[k] = val
	return nil
}

// --- migrate ---

func runMigrate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "source system: launchdarkly|unleash")
	dryRun := fs.Bool("dry-run", false, "print what an import would change (v0.1 is dry-run only)")
	format := fs.String("format", "human", "output format: human|json")
	input := fs.String("input", "", "path to the source export file (default: source-specific fixture)")
	fidelityPath := fs.String("fidelity", "", "write the human-readable fidelity report to this path")
	fidelityGate := fs.Float64("fidelity-gate", 0.9, "minimum fidelity score; exit 1 below (CI gate)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *from == "" {
		fmt.Fprintln(stderr, "migrate: missing --from (launchdarkly|unleash)")
		return 1
	}
	if !*dryRun {
		fmt.Fprintln(stderr, "migrate: v0.1 is dry-run only; the write path lands with the store")
		return 1
	}

	var parse func(data []byte) ([]model.Project, []migrate.Unmapped, error)
	var describe func(p model.Project) string
	switch *from {
	case "launchdarkly":
		parse = launchdarkly.Parse
		describe = launchdarkly.Describe
	case "unleash":
		parse = unleash.Parse
		describe = unleash.Describe
	default:
		fmt.Fprintf(stderr, "migrate: unknown --from %q (launchdarkly|unleash)\n", *from)
		return 1
	}
	if *input == "" {
		*input = defaultInputFor(*from)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(stderr, "migrate: read %s: %v\n", *input, err)
		return 1
	}
	projects, unmapped, err := parse(data)
	if err != nil {
		fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	diff := migrate.DiffProjects(projects, nil, unmapped)
	report := migrate.FidelityReport(*from, projects, unmapped)
	if *fidelityPath != "" {
		if err := os.WriteFile(*fidelityPath, []byte(report.Markdown()), 0o644); err != nil {
			fmt.Fprintf(stderr, "migrate: write fidelity report: %v\n", err)
			return 1
		}
	}
	if !report.Pass(*fidelityGate) {
		fmt.Fprintf(stderr, "migrate: fidelity %.1f%% below gate %.1f%% — refusing to pretend\n",
			report.Score*100, *fidelityGate*100)
		return 1
	}
	if *format == "json" {
		out := struct {
			Diff     migrate.Diff    `json:"diff"`
			Fidelity *migrate.Report `json:"fidelity"`
		}{Diff: diff, Fidelity: report}
		if err := json.NewEncoder(stdout).Encode(out); err != nil {
			fmt.Fprintf(stderr, "migrate: encode: %v\n", err)
			return 1
		}
		return 0
	}
	for _, p := range projects {
		fmt.Fprintln(stdout, describe(p))
	}
	fmt.Fprintf(stdout, "summary: %d added, %d removed, %d changed, %d unmapped\n",
		diff.Summary.Added, diff.Summary.Removed, diff.Summary.Changed, diff.Summary.Unmapped)
	for _, u := range diff.Unmapped {
		fmt.Fprintf(stdout, "unmapped: [%s] flag=%s rule=%s detail=%s\n", u.Type, u.Flag, u.Rule, u.Detail)
	}
	return 0
}
