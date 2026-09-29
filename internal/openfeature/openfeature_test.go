package openfeature

import (
	"context"
	"strings"
	"testing"

	of "github.com/open-feature/go-sdk/openfeature"
	"github.com/switchyard/switchyard/internal/model"
)

// Compile-time proof that Provider implements the real OpenFeature
// FeatureProvider interface — not a lookalike.
var _ of.FeatureProvider = (*Provider)(nil)

func booleanFlag(key string, on bool, rules ...model.Rule) model.Flag {
	fe := model.FlagEnvironment{On: on, Rules: rules}
	return model.Flag{
		Key:  key,
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": fe,
		},
	}
}

func valueFlag(kind model.Kind, value any) model.Flag {
	return model.Flag{
		Key:  "typed",
		Kind: kind,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: value},
		},
	}
}

func TestProviderMetadataNamesSwitchyard(t *testing.T) {
	p := New(map[string]model.Flag{}, "production")
	if p.Metadata().Name != "Switchyard" {
		t.Fatalf("metadata name = %q, want Switchyard", p.Metadata().Name)
	}
}

func TestBooleanEvaluationOn(t *testing.T) {
	p := New(map[string]model.Flag{"f": booleanFlag("f", true)}, "production")
	d := p.BooleanEvaluation(context.Background(), "f", false, of.FlattenedContext{"targetingKey": "alice"})
	if d.Value != true || d.Variant != "on" || d.Reason != of.DefaultReason {
		t.Fatalf("bool on = %+v", d)
	}
}

func TestBooleanEvaluationOffServesConfiguredFalse(t *testing.T) {
	p := New(map[string]model.Flag{"f": booleanFlag("f", false)}, "production")
	d := p.BooleanEvaluation(context.Background(), "f", true, of.FlattenedContext{"targetingKey": "alice"})
	// Configured off is a valid resolution: value false, variant off, no error.
	// The caller's default (true) must NOT be served.
	if d.Value != false || d.Variant != "off" {
		t.Fatalf("configured-off flag = %+v, want value false variant off", d)
	}
	if d.Error() != nil {
		t.Fatalf("configured-off flag must carry no error, got %v", d.Error())
	}
}

func TestBooleanEvaluationUnknownFlagReturnsDefaultAndError(t *testing.T) {
	p := New(map[string]model.Flag{}, "production")
	d := p.BooleanEvaluation(context.Background(), "ghost", true, of.FlattenedContext{"targetingKey": "alice"})
	if d.Value != true || !hasFlagNotFound(d.ResolutionError) {
		t.Fatalf("unknown flag = %+v, want default true + FLAG_NOT_FOUND", d)
	}
}

func TestStringEvaluationServesValue(t *testing.T) {
	p := New(map[string]model.Flag{"typed": valueFlag(model.KindString, "checkout-v2")}, "production")
	d := p.StringEvaluation(context.Background(), "typed", "old", of.FlattenedContext{"targetingKey": "a"})
	if d.Value != "checkout-v2" || d.Variant != "on" {
		t.Fatalf("string eval = %+v", d)
	}
}

func TestFloatEvaluationServesValue(t *testing.T) {
	p := New(map[string]model.Flag{"typed": valueFlag(model.KindNumber, float64(42))}, "production")
	d := p.FloatEvaluation(context.Background(), "typed", 1.0, of.FlattenedContext{"targetingKey": "a"})
	if d.Value != 42 {
		t.Fatalf("float eval = %+v", d)
	}
}

func TestObjectEvaluationServesJSON(t *testing.T) {
	p := New(map[string]model.Flag{"typed": valueFlag(model.KindJSON, map[string]any{"retries": 3})}, "production")
	d := p.ObjectEvaluation(context.Background(), "typed", nil, of.FlattenedContext{"targetingKey": "a"})
	m, ok := d.Value.(map[string]any)
	if !ok || m["retries"] != 3 {
		t.Fatalf("object eval = %+v", d)
	}
}

func TestTypeMismatchReturnsParseErrorAndDefault(t *testing.T) {
	// Flag is a string, caller asked for a boolean — must not serve the
	// string as truthy; must return the default with a type-mismatch error.
	p := New(map[string]model.Flag{"typed": valueFlag(model.KindString, "checkout-v2")}, "production")
	d := p.BooleanEvaluation(context.Background(), "typed", true, of.FlattenedContext{"targetingKey": "a"})
	if d.Value != true || !hasParseError(d.ResolutionError) {
		t.Fatalf("type mismatch = %+v, want default + PARSE_ERROR", d)
	}
}

// End-to-end through the real OpenFeature client: provider registered
// globally, evaluated through the of.Client — proving the wiring, not
// just the provider methods.
func TestEndToEndThroughOpenFeatureClient(t *testing.T) {
	flags := map[string]model.Flag{"f": booleanFlag("f", true)}
	p := New(flags, "production")
	if err := of.SetProviderAndWait(p); err != nil {
		t.Fatalf("SetProviderAndWait: %v", err)
	}
	client := of.NewClient("e2e")
	got := client.Boolean(context.Background(), "f", false, of.NewEvaluationContext("alice", nil))
	if got != true {
		t.Fatalf("client.Boolean = %v, want true", got)
	}
}

// hasFlagNotFound asserts the resolution error carries FLAG_NOT_FOUND.
func hasFlagNotFound(r of.ResolutionError) bool {
	return strings.HasPrefix(r.Error(), string(of.FlagNotFoundCode))
}

// hasParseError asserts the resolution error carries PARSE_ERROR.
func hasParseError(r of.ResolutionError) bool {
	return strings.HasPrefix(r.Error(), string(of.ParseErrorCode))
}

// hasCodePrefix kept for the first replacement variant if used.
func hasCodePrefix(msg, code string) bool {
	return strings.HasPrefix(msg, code)
}
