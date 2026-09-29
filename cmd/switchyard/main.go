// Command switchyard is the single binary: flag server, migration CLI,
// and (W3–4) OpenFeature provider scaffolding and SDK generation.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/migrate/launchdarkly"
	"github.com/switchyard/switchyard/internal/serve"
)

// version is the single source of truth for the binary version. Overridden
// at build time via -ldflags "-X main.version=...".
var version = "0.1.0-dev"

const defaultLDFixture = "testdata/fixtures/launchdarkly/sample-export.json"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "switchyard version %s\n", version)
		return 0
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "eval", "sdk":
		fmt.Fprintf(stderr, "%s: not implemented in the v0.1 stub (scheduled W3–4)\n", args[0])
		return 1
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", ":8080", "listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	fmt.Fprintf(stdout, "switchyard serving on %s (in-memory flags, W1–2 store pending)\n", *addr)
	if err := http.ListenAndServe(*addr, serve.New().Handler()); err != nil {
		fmt.Fprintf(stderr, "serve: %v\n", err)
		return 1
	}
	return 0
}

func runMigrate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "source system: launchdarkly|unleash")
	dryRun := fs.Bool("dry-run", false, "print what an import would change (v0.1 is dry-run only)")
	format := fs.String("format", "human", "output format: human|json")
	input := fs.String("input", defaultLDFixture, "path to the source export file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *from == "" {
		fmt.Fprintln(stderr, "migrate: missing --from (launchdarkly|unleash)")
		return 1
	}
	if !*dryRun {
		fmt.Fprintln(stderr, "migrate: v0.1 is dry-run only; the write path lands with the W1–2 store")
		return 1
	}

	switch *from {
	case "launchdarkly":
		data, err := os.ReadFile(*input)
		if err != nil {
			fmt.Fprintf(stderr, "migrate: read %s: %v\n", *input, err)
			return 1
		}
		projects, unmapped, err := launchdarkly.Parse(data)
		if err != nil {
			fmt.Fprintf(stderr, "migrate: %v\n", err)
			return 1
		}
		diff := migrate.DiffProjects(projects, nil, unmapped)
		if *format == "json" {
			if err := json.NewEncoder(stdout).Encode(diff); err != nil {
				fmt.Fprintf(stderr, "migrate: encode: %v\n", err)
				return 1
			}
			return 0
		}
		for _, p := range projects {
			fmt.Fprintln(stdout, launchdarkly.Describe(p))
		}
		fmt.Fprintf(stdout, "summary: %d added, %d removed, %d changed, %d unmapped\n",
			diff.Summary.Added, diff.Summary.Removed, diff.Summary.Changed, diff.Summary.Unmapped)
		for _, u := range diff.Unmapped {
			fmt.Fprintf(stdout, "unmapped: [%s] flag=%s rule=%s detail=%s\n", u.Type, u.Flag, u.Rule, u.Detail)
		}
		return 0
	case "unleash":
		fmt.Fprintln(stderr, "migrate: --from=unleash is not implemented until Week 8; refusing to pretend fidelity")
		return 1
	default:
		fmt.Fprintf(stderr, "migrate: unknown --from %q (launchdarkly|unleash)\n", *from)
		return 1
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `switchyard — self-hosted feature-flag control plane (v0.1 stub)

Usage:
  switchyard version
  switchyard serve [--addr :8080]
  switchyard migrate --from=launchdarkly|unleash --dry-run [--format human|json] [--input PATH]
  switchyard eval ...   (W3–4)
  switchyard sdk ...    (W3–4)
`)
}
