//go:build desktop

package main

// desktop_wails_test.go: the GUI's testable surface. Real pixel-pushing
// needs a display (wails dev); here we verify the binding layer — the
// exact methods the frontend calls — against a real SQLite store. The
// frontend is a thin renderer over these methods, same as the TUI over
// tuiModel.
//
// Step 4 (04-gui-audit.md) checks, as tests:
//   - toggle → eval shows the new variant
//   - percentage rollout stable for a fixed key
//   - migrate fixture renders unmapped (edge corpora list their gaps)
//   - donate addresses byte-for-byte
//   - bound methods return errors the frontend shows (error strings
//     carry actionable messages; no swallowed errors)

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/eval"
	"github.com/switchyard/switchyard/internal/migrate"
	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "gui-test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &App{st: st, db: "test.db", env: "production"}
}

// TestToggleChangesEvalResult: toggle a flag and eval shows the new variant.
func TestToggleChangesEvalResult(t *testing.T) {
	a := newTestApp(t)
	if err := a.CreateFlag("checkout-v2"); err != nil {
		t.Fatalf("create: %v", err)
	}
	r1, err := a.Evaluate("checkout-v2", "alice", "")
	if err != nil {
		t.Fatalf("eval off: %v", err)
	}
	if r1.Enabled || r1.Variant != "off" {
		t.Fatalf("fresh flag should eval off, got %+v", r1)
	}
	if err := a.ToggleFlag("checkout-v2"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	r2, err := a.Evaluate("checkout-v2", "alice", "")
	if err != nil {
		t.Fatalf("eval on: %v", err)
	}
	if !r2.Enabled || r2.Variant != "on" {
		t.Fatalf("toggled flag should eval on, got %+v", r2)
	}
	// And the audit log recorded the GUI toggle.
	entries, err := a.RecentAudit(10)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Actor == "gui" && e.Action == "toggle" && e.Key == "checkout-v2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gui toggle not audited: %+v", entries)
	}
}

// TestRolloutStableForFixedKey: percentage rollout must be deterministic
// for a fixed (flag, user) pair — same bucket every evaluation.
func TestRolloutStableForFixedKey(t *testing.T) {
	a := newTestApp(t)
	if err := a.CreateFlag("rollme"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.SetRollout("rollme", 50); err != nil {
		t.Fatalf("setRollout: %v", err)
	}
	b1 := eval.Bucket("rollme", "alice")
	for i := 0; i < 25; i++ {
		r, err := a.Evaluate("rollme", "alice", "")
		if err != nil {
			t.Fatalf("eval %d: %v", i, err)
		}
		if r.Bucket != b1 {
			t.Fatalf("bucket drifted: %d then %d", b1, r.Bucket)
		}
		wantEnabled := b1 < 50
		if r.Enabled != wantEnabled {
			t.Fatalf("rollout decision unstable for bucket %d: enabled=%v want=%v", r.Bucket, r.Enabled, wantEnabled)
		}
	}
	// Rollout bounds enforced.
	if err := a.SetRollout("rollme", 101); err == nil {
		t.Fatal("rollout 101 should error")
	}
	if err := a.SetRollout("rollme", -1); err == nil {
		t.Fatal("rollout -1 should error")
	}
}

// TestMigrateDryRunShowsUnmapped: the edge corpora must list every gap
// type in the viewer payload — the honesty contract, rendered.
func TestMigrateDryRunShowsUnmapped(t *testing.T) {
	a := newTestApp(t)
	for _, tc := range []struct {
		source, fixture string
		wantTypes       []string
	}{
		{"launchdarkly", "../../testdata/fixtures/launchdarkly/edge-cases.json",
			[]string{"weighted-rollout", "clause-operator", "bucketBy", "segment-unbounded"}},
		{"unleash", "../../testdata/fixtures/unleash/unleash-edge.json",
			[]string{"strategy-random-rollout", "strategy-unsupported", "constraint-operator", "stickiness", "bucketBy", "weighted-variants", "segment-missing"}},
	} {
		r, err := a.DryRunMigrate(tc.source, tc.fixture)
		if err != nil {
			t.Fatalf("%s dry-run: %v", tc.source, err)
		}
		got := map[string]bool{}
		for _, u := range r.Unmapped {
			got[u.Type] = true
		}
		for _, want := range tc.wantTypes {
			if !got[want] {
				t.Fatalf("%s edge corpus missing gap type %q in %+v", tc.source, want, got)
			}
		}
		if r.Refused {
			t.Logf("%s edge corpus refused at gate (expected: edge is unmappable by design)", tc.source)
		}
	}
	// Representative corpora pass the gate and show no refusal.
	for _, tc := range []struct{ source, fixture string }{
		{"launchdarkly", "../../testdata/fixtures/launchdarkly/full-export.json"},
		{"unleash", "../../testdata/fixtures/unleash/unleash-representative.json"},
	} {
		r, err := a.DryRunMigrate(tc.source, tc.fixture)
		if err != nil {
			t.Fatalf("%s rep dry-run: %v", tc.source, err)
		}
		if r.Refused {
			t.Fatalf("%s representative corpus should pass the gate: fidelity %.1f", tc.source, r.Fidelity*100)
		}
	}
}

// TestDonateAddressesByteForByte: the GUI must show the README addresses
// exactly. A wrong address burns donor funds permanently.
func TestDonateAddressesByteForByte(t *testing.T) {
	a := newTestApp(t)
	info := a.DonateInfo()
	const wantETH = "0x85ee7E71f762d772599cbF1EC20E651B30657521"
	const wantBTC = "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"
	if info["ethereum"] != wantETH {
		t.Fatalf("ETH address drifted: %q", info["ethereum"])
	}
	if info["bitcoin"] != wantBTC {
		t.Fatalf("BTC address drifted: %q", info["bitcoin"])
	}
}

// TestBindingErrorsSurface: every failure path returns an error the
// frontend can render — no silent empty results.
func TestBindingErrorsSurface(t *testing.T) {
	a := newTestApp(t)
	if err := a.CreateFlag("  "); err == nil {
		t.Fatal("whitespace key should error")
	}
	if err := a.CreateFlag(""); err == nil {
		t.Fatal("empty key should error")
	}
	if err := a.ToggleFlag("missing-flag"); err == nil {
		t.Fatal("toggling a missing flag should error")
	}
	if _, err := a.Evaluate("missing-flag", "alice", ""); err == nil {
		t.Fatal("evaluating a missing flag should error")
	}
	if _, err := a.Evaluate("x", "u", "not-json"); err == nil {
		t.Fatal("bad attributes JSON should error")
	}
	if err := a.SetEnvironment("staging"); err != nil {
		t.Fatalf("staging is seeded: %v", err)
	}
	if err := a.SetEnvironment("typo-env"); err == nil {
		t.Fatal("unknown environment should error")
	}
	if _, err := a.DryRunMigrate("statsig", "x.json"); err == nil {
		t.Fatal("unknown source should error")
	}
	if _, err := a.DryRunMigrate("launchdarkly", "/nonexistent.json"); err == nil {
		t.Fatal("missing file should error")
	}
	if err := a.PutSegment("{not json"); err == nil {
		t.Fatal("invalid segment JSON should error")
	}
}

// TestSegmentRoundTrip: the segment editor saves and reloads segments
// through the same store methods the API uses.
func TestSegmentRoundTrip(t *testing.T) {
	a := newTestApp(t)
	seg := `{"key":"pro-users","name":"Pro plan","rules":[{"id":"r1","clauses":[{"attribute":"plan","operator":"in","values":["pro"]}]}]}`
	if err := a.PutSegment(seg); err != nil {
		t.Fatalf("putSegment: %v", err)
	}
	segs, err := a.ListSegments()
	if err != nil {
		t.Fatalf("listSegments: %v", err)
	}
	if len(segs) != 1 || segs[0].Key != "pro-users" || len(segs[0].Rules) != 1 {
		t.Fatalf("segment round trip: %+v", segs)
	}
	// Segment-driven eval: create a flag whose rule targets the segment.
	if err := a.CreateFlag("seg-flag"); err != nil {
		t.Fatalf("create: %v", err)
	}
	f, err := a.st.GetFlag(context.Background(), "seg-flag")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	fe := f.Environments["production"]
	fe.On = true
	fe.Rules = []model.Rule{{
		ID: "seg", Clauses: []model.Clause{{Attribute: "targetingKey", Operator: "segmentMatch", Values: []string{"pro-users"}}},
	}}
	if err := a.st.SetFlagEnvironment(context.Background(), "seg-flag", "production", fe); err != nil {
		t.Fatalf("set env: %v", err)
	}
	attrs := `{"plan":"pro"}`
	// segmentMatch resolves via user membership implied by rule clauses;
	// the eval console must show the rule reason when it matches.
	rIn, err := a.Evaluate("seg-flag", "alice", attrs)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !strings.HasPrefix(rIn.Reason, "rule:") {
		t.Fatalf("expected rule match reason, got %q", rIn.Reason)
	}
}

// TestListFlagsShowsSelectedEnv: switching environments must change the
// rendered state — the GUI reads per-env config, not a frozen snapshot.
func TestListFlagsShowsSelectedEnv(t *testing.T) {
	a := newTestApp(t)
	if err := a.CreateFlag("envflag"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.SetEnvironment("dev"); err != nil {
		t.Fatalf("setEnv dev: %v", err)
	}
	if err := a.ToggleFlag("envflag"); err != nil {
		t.Fatalf("toggle in dev: %v", err)
	}
	rows, err := a.ListFlags()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || !rows[0].On {
		t.Fatalf("dev toggle not reflected: %+v", rows)
	}
	if err := a.SetEnvironment("production"); err != nil {
		t.Fatalf("setEnv prod: %v", err)
	}
	rows, err = a.ListFlags()
	if err != nil {
		t.Fatalf("list prod: %v", err)
	}
	if len(rows) != 1 || rows[0].On {
		t.Fatalf("production should still be off (per-env state): %+v", rows)
	}
}

// TestRecentAuditAppendOrder: entries come back newest-last (append
// order), the same contract the /audit API documents.
func TestRecentAuditAppendOrder(t *testing.T) {
	a := newTestApp(t)
	for _, k := range []string{"a", "b", "c"} {
		if err := a.CreateFlag(k); err != nil {
			t.Fatalf("create %s: %v", k, err)
		}
	}
	entries, err := a.RecentAudit(10)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("expected >=3 audit entries, got %d", len(entries))
	}
	last := entries[len(entries)-1]
	if last.Key != "c" {
		t.Fatalf("append order broken: newest-last expected c, got %q", last.Key)
	}
	var _ migrate.Summary
}

// TestBareInvocationRoutesToGUI pins the desktop-build product decision:
// bare `switchyard` (what `wails dev` launches) must route to the GUI
// launcher, not the TUI. In a non-TTY test context the GUI launcher
// reports a non-zero exit — the assertion is that it does NOT print the
// TUI's headless hint.
func TestBareInvocationRoutesToGUI(t *testing.T) {
	var out, errBuf strings.Builder
	code := launchDefault(&out, &errBuf)
	combined := out.String() + errBuf.String()
	if strings.Contains(combined, "the TUI needs an interactive terminal") {
		t.Fatalf("bare invocation hit the TUI path in the desktop build:\n%s", combined)
	}
	if code == 0 && !strings.Contains(combined, "desktop") {
		t.Fatalf("bare invocation exited 0 without opening/mentioning the GUI: code=%d\n%s", code, combined)
	}
}
