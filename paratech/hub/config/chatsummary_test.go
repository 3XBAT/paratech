package config_test

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"paratech/hub/config"
)

// TestLoadChatSummary: the chat summary runs on the cheap planner alias with a cap of 4096 by default; env and flags
// override both, the flag winning.
func TestLoadChatSummary(t *testing.T) {
	cfg, err := config.Load(nil, io.Discard, getenv(withDB(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatSummaryAlias != "planner" || cfg.ChatSummaryMaxOutput != 4096 {
		t.Fatalf("defaults = %s %d", cfg.ChatSummaryAlias, cfg.ChatSummaryMaxOutput)
	}

	env := withDB(map[string]string{config.EnvChatSummaryAlias: "cheap", config.EnvChatSummaryMaxOutput: "2048"})
	cfg, err = config.Load([]string{"-chat-summary-max-output", "1"}, io.Discard, getenv(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatSummaryAlias != "cheap" || cfg.ChatSummaryMaxOutput != 1 {
		t.Fatalf("overrides = %s %d", cfg.ChatSummaryAlias, cfg.ChatSummaryMaxOutput)
	}
	cfg, err = config.Load([]string{"-chat-summary-alias", "flagged", "-chat-summary-max-output", "1048576"}, io.Discard, getenv(env))
	if err != nil || cfg.ChatSummaryAlias != "flagged" || cfg.ChatSummaryMaxOutput != 1<<20 {
		t.Fatalf("flags = %+v, %v", cfg, err)
	}

	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("config", "cfg", cfg)
	if !strings.Contains(cfg.String(), "ChatSummaryAlias:flagged ChatSummaryMaxOutput:1048576") ||
		!strings.Contains(logged.String(), `"chat_summary_alias":"flagged","chat_summary_max_output":1048576`) {
		t.Fatalf("output lacks the settings: %s %s", cfg.String(), logged.String())
	}
}

func TestChatSummaryRejected(t *testing.T) {
	for name, c := range map[string]struct {
		env  map[string]string
		args []string
		want string
	}{
		"alias with space": {env: map[string]string{config.EnvChatSummaryAlias: "a b"}, want: config.EnvChatSummaryAlias},
		"cap not a number": {env: map[string]string{config.EnvChatSummaryMaxOutput: "many"}, want: config.EnvChatSummaryMaxOutput},
		"cap zero":         {env: map[string]string{config.EnvChatSummaryMaxOutput: "0"}, want: config.EnvChatSummaryMaxOutput},
		"cap negative":     {args: []string{"-chat-summary-max-output", "-1"}, want: config.EnvChatSummaryMaxOutput},
		"cap over bound":   {env: map[string]string{config.EnvChatSummaryMaxOutput: "1048577"}, want: config.EnvChatSummaryMaxOutput},
	} {
		_, err := config.Load(c.args, io.Discard, getenv(withDB(c.env)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want a %s error", name, err, c.want)
		}
	}
	// The key commands neither need nor check them.
	env := withDB(map[string]string{config.EnvChatSummaryAlias: "a b", config.EnvChatSummaryMaxOutput: "0"})
	if _, err := config.Load([]string{"keys", "list"}, io.Discard, getenv(env)); err != nil {
		t.Fatalf("keys list: %v", err)
	}
}

func TestChatSummaryUsage(t *testing.T) {
	var out bytes.Buffer
	if _, err := config.Load([]string{"-h"}, &out, getenv(withDB(nil))); err == nil {
		t.Fatal("no help error")
	}
	for _, want := range []string{"-chat-summary-alias", config.EnvChatSummaryAlias, "-chat-summary-max-output", config.EnvChatSummaryMaxOutput} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage does not mention %q", want)
		}
	}
}
