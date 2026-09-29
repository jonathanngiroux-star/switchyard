// Package openfeature implements a native OpenFeature provider for
// Switchyard. It satisfies the real go-sdk FeatureProvider interface —
// verified by compile-time assertion in the tests — so any OpenFeature
// application can evaluate Switchyard flags with zero Switchyard code.
//
// The provider holds a read-only snapshot of flags: local evaluation, no
// network, ever. Callers build a snapshot from the store (SnapshotFromStore)
// and swap providers when flags change. This is the W3–4 static shape;
// polling/daemon refresh is a later concern and deliberately not built yet.
package openfeature

import (
	"context"

	of "github.com/open-feature/go-sdk/openfeature"
	"github.com/switchyard/switchyard/internal/eval"
	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// Compile-time interface proof: the provider IS an OpenFeature FeatureProvider.
var _ of.FeatureProvider = (*Provider)(nil)

// Provider evaluates a Switchyard flag snapshot through the OpenFeature API.
type Provider struct {
	flags map[string]model.Flag
	env   string
	segs  map[string]model.Segment
}

// New builds a provider over an in-memory flag set for one environment.
func New(flags map[string]model.Flag, env string) *Provider {
	return &Provider{flags: flags, env: env, segs: map[string]model.Segment{}}
}

// NewFromStore builds a provider snapshot from the SQLite store. Read once
// at startup; swap providers on change.
func NewFromStore(st *store.Store, env string) (*Provider, error) {
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
	return &Provider{flags: m, env: env, segs: sm}, nil
}

// Metadata identifies the provider.
func (p *Provider) Metadata() of.Metadata {
	return of.Metadata{Name: "Switchyard"}
}

// Hooks: none in v0.1.
func (p *Provider) Hooks() []of.Hook { return nil }

// resolve runs the evaluation and maps Switchyard reasons to OpenFeature
// reasons and error codes. Missing flags and type mismatches never serve a
// configured value — the caller's default wins, with the error surfaced.
type resError = of.ResolutionError

func (p *Provider) resolve(key string, flatCtx of.FlattenedContext) (eval.Decision, bool) {
	f, ok := p.flags[key]
	if !ok {
		return eval.Decision{}, false
	}
	ctx := contextFromFlattened(flatCtx)
	return eval.Evaluate(f, p.env, p.segs, ctx), true
}

func contextFromFlattened(flat of.FlattenedContext) eval.Context {
	ctx := eval.Context{Attributes: map[string]string{}}
	if tk, ok := flat["targetingKey"].(string); ok {
		ctx.UserKey = tk
	}
	for k, v := range flat {
		if k == "targetingKey" {
			continue
		}
		if s, ok := v.(string); ok {
			ctx.Attributes[k] = s
		}
	}
	return ctx
}

func reasonFor(d eval.Decision, found bool) (of.Reason, resError, bool) {
	if !found {
		return of.DefaultReason, of.NewFlagNotFoundResolutionError("flag not in snapshot"), false
	}
	if d.ErrorCode != "" {
		return of.DefaultReason, of.NewParseErrorResolutionError("flag misconfigured: no value set"), false
	}
	switch d.Reason {
	case "off":
		return of.DefaultReason, resError{}, true
	case "fallthrough", "rollout":
		return of.DefaultReason, resError{}, true
	default:
		return of.TargetingMatchReason, resError{}, true
	}
}

// BooleanEvaluation resolves a boolean flag.
func (p *Provider) BooleanEvaluation(ctx context.Context, flag string, defaultValue bool, flatCtx of.FlattenedContext) of.BoolResolutionDetail {
	d, found := p.resolve(flag, flatCtx)
	reason, resErr, ok := reasonFor(d, found)
	if !ok {
		return of.BoolResolutionDetail{
			Value:                    defaultValue,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: resErr},
		}
	}
	if v, vok := d.Value.(bool); vok && d.Variant != "" {
		return of.BoolResolutionDetail{
			Value:                    v,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Variant: d.Variant, Reason: reason},
		}
	}
	return of.BoolResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: of.NewParseErrorResolutionError("type mismatch")},
	}
}

// StringEvaluation resolves a string flag.
func (p *Provider) StringEvaluation(ctx context.Context, flag string, defaultValue string, flatCtx of.FlattenedContext) of.StringResolutionDetail {
	d, found := p.resolve(flag, flatCtx)
	reason, resErr, ok := reasonFor(d, found)
	if !ok {
		return of.StringResolutionDetail{
			Value:                    defaultValue,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: resErr},
		}
	}
	if s, ok := d.Value.(string); ok {
		return of.StringResolutionDetail{
			Value:                    s,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Variant: d.Variant, Reason: reason},
		}
	}
	return of.StringResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: of.NewParseErrorResolutionError("type mismatch")},
	}
}

// FloatEvaluation resolves a number flag as float64.
func (p *Provider) FloatEvaluation(ctx context.Context, flag string, defaultValue float64, flatCtx of.FlattenedContext) of.FloatResolutionDetail {
	d, found := p.resolve(flag, flatCtx)
	reason, resErr, ok := reasonFor(d, found)
	if !ok {
		return of.FloatResolutionDetail{
			Value:                    defaultValue,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: resErr},
		}
	}
	if n, ok := d.Value.(float64); ok {
		return of.FloatResolutionDetail{
			Value:                    n,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Variant: d.Variant, Reason: reason},
		}
	}
	return of.FloatResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: of.NewParseErrorResolutionError("type mismatch")},
	}
}

// IntEvaluation resolves a number flag as int64.
func (p *Provider) IntEvaluation(ctx context.Context, flag string, defaultValue int64, flatCtx of.FlattenedContext) of.IntResolutionDetail {
	d, found := p.resolve(flag, flatCtx)
	reason, resErr, ok := reasonFor(d, found)
	if !ok {
		return of.IntResolutionDetail{
			Value:                    defaultValue,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: resErr},
		}
	}
	if n, ok := d.Value.(float64); ok {
		return of.IntResolutionDetail{
			Value:                    int64(n),
			ProviderResolutionDetail: of.ProviderResolutionDetail{Variant: d.Variant, Reason: reason},
		}
	}
	return of.IntResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: of.NewParseErrorResolutionError("type mismatch")},
	}
}

// ObjectEvaluation resolves a JSON flag.
func (p *Provider) ObjectEvaluation(ctx context.Context, flag string, defaultValue any, flatCtx of.FlattenedContext) of.InterfaceResolutionDetail {
	d, found := p.resolve(flag, flatCtx)
	reason, resErr, ok := reasonFor(d, found)
	if !ok {
		return of.InterfaceResolutionDetail{
			Value:                    defaultValue,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: resErr},
		}
	}
	if d.Value != nil {
		return of.InterfaceResolutionDetail{
			Value:                    d.Value,
			ProviderResolutionDetail: of.ProviderResolutionDetail{Variant: d.Variant, Reason: reason},
		}
	}
	return of.InterfaceResolutionDetail{
		Value:                    defaultValue,
		ProviderResolutionDetail: of.ProviderResolutionDetail{Reason: reason, ResolutionError: of.NewParseErrorResolutionError("type mismatch")},
	}
}
