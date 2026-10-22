package config_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"paratech/hub/config"
)

// Obviously fake seal secrets (32 and 40 bytes of hex).
var (
	sealHexA = strings.Repeat("a1", 32)
	sealHexB = strings.Repeat("b2", 40)
)

func getenv(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestLoadAgent(t *testing.T) {
	cfg, err := config.Load(nil, io.Discard, getenv(withDB(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentPlannerAlias != "planner" || cfg.AgentStrongAlias != "default" || cfg.AgentSealKeys != "" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if keys, err := cfg.SealKeys(); err != nil || keys != nil {
		t.Fatalf("no seal keys = %v, %v", keys, err)
	}

	env := withDB(map[string]string{
		config.EnvAgentPlannerAlias: "cheap", config.EnvAgentStrongAlias: "strong",
		config.EnvAgentSealKeys: "2:" + sealHexB + ", 1:" + sealHexA,
		config.EnvLLMModels:     "default=m:1,cheap=m:1,strong=m:1",
	})
	cfg, err = config.Load([]string{"-agent-planner-alias", "flagged"}, io.Discard, getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentPlannerAlias != "flagged" || cfg.AgentStrongAlias != "strong" {
		t.Fatalf("overrides = %+v", cfg)
	}
	keys, err := cfg.SealKeys()
	if err != nil || len(keys) != 2 || keys[0].Version != 2 || len(keys[0].Secret) != 40 || keys[1].Version != 1 ||
		!bytes.Equal(keys[1].Secret, bytes.Repeat([]byte{0xa1}, 32)) {
		t.Fatalf("seal keys = %v, %v", keys, err)
	}
}

func TestSealKeysRejected(t *testing.T) {
	for name, value := range map[string]string{
		"no version":     sealHexA,
		"version 0":      "0:" + sealHexA,
		"version 256":    "256:" + sealHexA,
		"version text":   "v1:" + sealHexA,
		"not hex":        "1:" + strings.Repeat("zz", 32),
		"short":          "1:" + strings.Repeat("a1", 31),
		"repeated":       "1:" + sealHexA + ",1:" + sealHexB,
		"empty entry":    "1:" + sealHexA + ",",
		"odd hex length": "1:" + sealHexA + "a",
	} {
		_, err := config.Load(nil, io.Discard, getenv(withDB(map[string]string{config.EnvAgentSealKeys: value})))
		if err == nil || !strings.Contains(err.Error(), config.EnvAgentSealKeys) {
			t.Errorf("%s: err = %v, want a %s error", name, err, config.EnvAgentSealKeys)
			continue
		}
		if strings.Contains(err.Error(), "a1a1") || strings.Contains(err.Error(), "b2b2") || strings.Contains(err.Error(), "zzzz") {
			t.Errorf("%s: the error quotes the secret: %v", name, err)
		}
	}
	for name, env := range map[string]map[string]string{
		"planner alias with space": {config.EnvAgentPlannerAlias: "a b"},
		"strong alias with space":  {config.EnvAgentStrongAlias: "a b"},
	} {
		if _, err := config.Load(nil, io.Discard, getenv(withDB(env))); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestAgentSettingsAreCheckedOnlyForServe: the key commands neither need nor check the agent settings.
func TestAgentSettingsAreCheckedOnlyForServe(t *testing.T) {
	env := withDB(map[string]string{config.EnvAgentSealKeys: "broken", config.EnvAgentPlannerAlias: "a b"})
	if _, err := config.Load([]string{"keys", "list"}, io.Discard, getenv(env)); err != nil {
		t.Fatalf("keys list: %v", err)
	}
}

func TestSealKeysAreNeverPrinted(t *testing.T) {
	cfg, err := config.Load(nil, io.Discard, getenv(withDB(map[string]string{config.EnvAgentSealKeys: "1:" + sealHexA})))
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("config", "cfg", cfg)
	for _, out := range []string{fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), fmt.Sprintf("%v", &cfg), logged.String()} {
		if strings.Contains(out, "a1a1") {
			t.Errorf("output leaks the seal secret: %s", out)
		}
		if !strings.Contains(out, "AgentSealKeys:(redacted)") && !strings.Contains(out, `"agent_seal_keys":"(redacted)"`) {
			t.Errorf("output %s does not mark the seal keys as redacted", out)
		}
	}
}
