package app_test

import (
	"strings"
	"testing"

	"paratech/hub/config"
	"paratech/hub/internal/app"
)

// fakeSealHex is an obviously fake 32-byte seal secret.
var fakeSealHex = strings.Repeat("5e", 32)

// TestServeWithAgent: with a seal key and the planner alias in the allowlist the embedded prompts load and New
// succeeds; without a seal key AgentStep is simply off.
func TestServeWithAgent(t *testing.T) {
	on := validConfig(":8080")
	on.AgentSealKeys = "1:" + fakeSealHex
	if _, err := app.New(on, discard()); err != nil {
		t.Fatalf("agent on: %v", err)
	}
	if _, err := app.New(validConfig(":8080"), discard()); err != nil {
		t.Fatalf("agent off: %v", err)
	}
}

func TestServeNeedsTheAgentParts(t *testing.T) {
	noPlanner := validConfig(":8080")
	noPlanner.AgentSealKeys = "1:" + fakeSealHex
	noPlanner.LLMModels = "default=claude-sonnet-5-5:8192"
	otherPlanner := validConfig(":8080")
	otherPlanner.AgentSealKeys = "1:" + fakeSealHex
	otherPlanner.AgentPlannerAlias = "cheap"
	badKey := validConfig(":8080")
	badKey.AgentSealKeys = "1:" + fakeSealHex[:10]
	for name, c := range map[string]struct {
		cfg  config.Config
		want string
	}{
		"planner not allowed":   {cfg: noPlanner, want: config.EnvAgentPlannerAlias},
		"unknown planner alias": {cfg: otherPlanner, want: config.EnvAgentPlannerAlias},
		"short seal secret":     {cfg: badKey, want: config.EnvAgentSealKeys},
	} {
		_, err := app.New(c.cfg, discard())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want containing %q", name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), "5e5e") {
			t.Errorf("%s: error leaks the seal secret: %v", name, err)
		}
	}
	// Without a seal key the planner alias is not needed.
	off := validConfig(":8080")
	off.LLMModels = "default=claude-sonnet-5-5:8192"
	if _, err := app.New(off, discard()); err != nil {
		t.Fatalf("agent off without a planner alias: %v", err)
	}
}
