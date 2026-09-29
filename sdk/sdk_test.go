package sdk_test

import (
	"context"
	"testing"

	of "github.com/open-feature/go-sdk/openfeature"
	"github.com/switchyard/switchyard/internal/model"
	syof "github.com/switchyard/switchyard/internal/openfeature"
	"github.com/switchyard/switchyard/sdk"
)

func booleanFlag(key string, on bool) model.Flag {
	return model.Flag{
		Key:  key,
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: on},
		},
	}
}

func stringFlag(key, value string) model.Flag {
	return model.Flag{
		Key:  key,
		Kind: model.KindString,
		Environments: map[string]model.FlagEnvironment{
			"production": {On: true, Value: value},
		},
	}
}

// Compile-time: the SDK client must be constructible from a snapshot +
// environment with no store handle required — pure local evaluation.
var _ = sdk.New

func TestClientBooleanEvaluation(t *testing.T) {
	c := sdk.New(map[string]model.Flag{"f": booleanFlag("f", true)}, "production")
	if !c.Boolean("f", false, "alice") {
		t.Fatal("enabled flag must evaluate true")
	}
}

func TestClientBooleanDefaultOnUnknown(t *testing.T) {
	c := sdk.New(map[string]model.Flag{}, "production")
	if c.Boolean("ghost", true, "alice") != true {
		t.Fatal("unknown flag must serve the caller default")
	}
	if c.Boolean("ghost", false, "alice") != false {
		t.Fatal("unknown flag must serve the caller default (false variant)")
	}
}

func TestClientStringValue(t *testing.T) {
	c := sdk.New(map[string]model.Flag{"copy": stringFlag("copy", "v2")}, "production")
	if c.String("copy", "v1", "alice") != "v2" {
		t.Fatal("string flag must serve configured value")
	}
}

func TestClientAttachesAttributes(t *testing.T) {
	// Rule-gated flag with NO fallthrough rollout: rule match enables;
	// non-match stays disabled (rollout 0 on the rule is implicit — a rule
	// without its own rollout fires only on clause match, and the flag's
	// fallthrough is off because the flag env carries rollout 0).
	pro := model.Flag{
		Key:  "tiered",
		Kind: model.KindBoolean,
		Environments: map[string]model.FlagEnvironment{
			"production": {
				On:      true,
				Rollout: &model.Rollout{Kind: "percentage", Percentage: 0},
				Rules: []model.Rule{{
					ID:      "r-pro",
					Clauses: []model.Clause{{Attribute: "plan", Operator: "in", Values: []string{"pro"}}},
				}},
			},
		},
	}
	c := sdk.New(map[string]model.Flag{"tiered": pro}, "production")
	if !c.Boolean("tiered", false, "alice", sdk.Attr("plan", "pro")) {
		t.Fatal("attribute match must enable")
	}
	if c.Boolean("tiered", false, "alice", sdk.Attr("plan", "free")) {
		t.Fatal("attribute mismatch must not enable")
	}
}

func TestClientIsOpenFeatureCompatible(t *testing.T) {
	// The SDK client must expose the provider so any OpenFeature app can
	// register Switchyard without importing internal packages.
	c := sdk.New(map[string]model.Flag{"f": booleanFlag("f", true)}, "production")
	if err := of.SetProviderAndWait(c.Provider()); err != nil {
		t.Fatalf("SetProviderAndWait: %v", err)
	}
	client := of.NewClient("switchyard-sdk")
	if !client.Boolean(context.Background(), "f", false, of.NewEvaluationContext("alice", nil)) {
		t.Fatal("OpenFeature client through SDK provider must evaluate true")
	}
}

// The provider returned by the SDK must be the internal one — one
// implementation, no drift between the two surfaces.
var _ of.FeatureProvider = (*syof.Provider)(nil)
