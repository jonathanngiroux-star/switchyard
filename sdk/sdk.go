// Package sdk is the public Go SDK for Switchyard. It is deliberately thin:
// a snapshot-based client for pure local evaluation, and the OpenFeature
// provider for apps already in the OpenFeature ecosystem.
//
// This is hand-written for W3–4 dogfooding; W9's generator (Go/TS/Python)
// generalizes the shape. The SDK never dials the network — evaluation is
// local, always.
package sdk

import (
	"context"
	"encoding/json"

	"github.com/switchyard/switchyard/internal/eval"
	"github.com/switchyard/switchyard/internal/model"
	syof "github.com/switchyard/switchyard/internal/openfeature"
	"github.com/switchyard/switchyard/internal/store"
)

// Client evaluates a flag snapshot locally for one environment.
type Client struct {
	flags map[string]model.Flag
	env   string
	segs  map[string]model.Segment
}

// New builds a client over an in-memory flag snapshot.
func New(flags map[string]model.Flag, env string) *Client {
	return &Client{flags: flags, env: env, segs: map[string]model.Segment{}}
}

// NewFromStore snapshots flags and segments from the SQLite store.
func NewFromStore(st *store.Store, env string) (*Client, error) {
	flags, err := st.ListFlags(context.Background())
	if err != nil {
		return nil, err
	}
	segs, err := st.ListSegments(context.Background())
	if err != nil {
		return nil, err
	}
	m := make(map[string]model.Flag, len(flags))
	for _, f := range flags {
		m[f.Key] = f
	}
	sm := make(map[string]model.Segment, len(segs))
	for _, s := range segs {
		sm[s.Key] = s
	}
	return &Client{flags: m, env: env, segs: sm}, nil
}

// Provider returns the OpenFeature provider for this snapshot — the same
// implementation the internal package ships, no drift.
func (c *Client) Provider() *syof.Provider {
	return syof.New(c.flags, c.env)
}

// Attr builds one evaluation-context attribute.
func Attr(key, value string) [2]string {
	return [2]string{key, value}
}

func ctxFrom(userKey string, attrs [][2]string) eval.Context {
	ctx := eval.Context{UserKey: userKey, Attributes: map[string]string{}}
	for _, a := range attrs {
		ctx.Attributes[a[0]] = a[1]
	}
	return ctx
}

func (c *Client) resolve(key, userKey string, attrs [][2]string) (eval.Decision, bool) {
	f, ok := c.flags[key]
	if !ok {
		return eval.Decision{}, false
	}
	return eval.Evaluate(f, c.env, c.segs, ctxFrom(userKey, attrs)), true
}

// Boolean evaluates a boolean flag; unknown or misconfigured flags serve
// the caller's default.
func (c *Client) Boolean(key string, defaultValue bool, userKey string, attrs ...[2]string) bool {
	d, ok := c.resolve(key, userKey, attrs)
	if !ok || d.ErrorCode != "" {
		return defaultValue
	}
	if v, isBool := d.Value.(bool); isBool {
		return v
	}
	return defaultValue
}

// String evaluates a string flag; unknown or misconfigured flags serve
// the caller's default.
func (c *Client) String(key, defaultValue, userKey string, attrs ...[2]string) string {
	d, ok := c.resolve(key, userKey, attrs)
	if !ok || d.ErrorCode != "" {
		return defaultValue
	}
	if s, isStr := d.Value.(string); isStr {
		return s
	}
	return defaultValue
}

// JSON evaluates a JSON flag into out via unmarshal; unknown or
// misconfigured flags leave out untouched and return false.
func (c *Client) JSON(key string, out any, userKey string, attrs ...[2]string) bool {
	d, ok := c.resolve(key, userKey, attrs)
	if !ok || d.ErrorCode != "" || d.Value == nil {
		return false
	}
	// Value came from JSON decode; re-marshal is the simple correct path.
	b, err := json.Marshal(d.Value)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, out) == nil
}
