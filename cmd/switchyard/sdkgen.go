package main

// sdkgen.go implements `switchyard sdk gen`: it snapshots a store's flags
// and segments for one environment and emits self-contained, zero-dependency
// clients (TypeScript and Python) plus the raw snapshot JSON. Generated
// clients evaluate locally — no network, ever — and their behavior is
// pinned by the cross-language conformance test.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// snapshot is the versioned wire format shared by every generated client.
// All three languages parse the same JSON and must produce identical
// decisions (verified by TestCrossLanguageConformance).
type snapshot struct {
	Version  string                   `json:"version"`
	Env      string                   `json:"env"`
	Flags    map[string]model.Flag    `json:"flags"`
	Segments map[string]model.Segment `json:"segments"`
}

const sdkGenVersion = "0.1"

// buildSnapshot loads flags and segments from the store for one environment.
func buildSnapshot(st *store.Store, env string) (*snapshot, error) {
	flags, err := st.ListFlags(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	segs, err := st.ListSegments(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list segments: %w", err)
	}
	snap := &snapshot{
		Version:  sdkGenVersion,
		Env:      env,
		Flags:    map[string]model.Flag{},
		Segments: map[string]model.Segment{},
	}
	for _, f := range flags {
		snap.Flags[f.Key] = f
	}
	for _, s := range segs {
		snap.Segments[s.Key] = s
	}
	return snap, nil
}

// genSDK writes snapshot.json and the requested language client to outDir.
func genSDK(snap *snapshot, lang, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "snapshot.json"), raw, 0o644); err != nil {
		return fmt.Errorf("write snapshot.json: %w", err)
	}
	switch lang {
	case "typescript":
		return os.WriteFile(filepath.Join(outDir, "switchyard.ts"), []byte(tsClient(snap, raw)), 0o644)
	case "python":
		return os.WriteFile(filepath.Join(outDir, "switchyard.py"), []byte(pyClient(snap, raw)), 0o644)
	default:
		return fmt.Errorf("unsupported language %q", lang)
	}
}
