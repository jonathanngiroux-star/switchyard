package main

// runSDK implements `switchyard sdk gen`.

import (
	"flag"
	"fmt"
	"io"

	"github.com/switchyard/switchyard/internal/store"
)

func runSDK(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "sdk: expected subcommand: gen")
		return 2
	}
	if args[0] != "gen" {
		fmt.Fprintf(stderr, "sdk: unknown subcommand %q (gen)\n", args[0])
		return 2
	}
	return runSDKGen(args[1:], stdout, stderr)
}

func runSDKGen(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sdk gen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lang := fs.String("lang", "", "target language: typescript|python (go SDK is the hand-written sdk/ package)")
	db := fs.String("db", defaultDBPath, "path to the SQLite database file")
	env := fs.String("env", "production", "environment to snapshot")
	out := fs.String("out", ".", "output directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *lang == "" {
		fmt.Fprintln(stderr, "sdk gen: --lang is required (typescript|python)")
		return 2
	}
	if *lang != "typescript" && *lang != "python" {
		fmt.Fprintf(stderr, "sdk gen: %q is not a supported language (typescript|python) — three languages, no more\n", *lang)
		return 2
	}
	st, err := store.Open(*db)
	if err != nil {
		fmt.Fprintf(stderr, "sdk gen: %v\n", err)
		return 1
	}
	defer st.Close()
	snap, err := buildSnapshot(st, *env)
	if err != nil {
		fmt.Fprintf(stderr, "sdk gen: %v\n", err)
		return 1
	}
	if err := genSDK(snap, *lang, *out); err != nil {
		fmt.Fprintf(stderr, "sdk gen: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "sdk gen: wrote snapshot.json + switchyard client (%s) for env %q to %s\n",
		*lang, *env, *out)
	return 0
}
