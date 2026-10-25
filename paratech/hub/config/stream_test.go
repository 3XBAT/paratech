package config_test

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"paratech/hub/config"
)

func TestLoadProviderStream(t *testing.T) {
	for name, tt := range map[string]struct {
		args []string
		env  map[string]string
		want bool
	}{
		"off by default":   {},
		"environment":      {env: map[string]string{config.EnvProviderStream: "true"}, want: true},
		"environment off":  {env: map[string]string{config.EnvProviderStream: "0"}},
		"flag wins":        {args: []string{"--provider-stream=false"}, env: map[string]string{config.EnvProviderStream: "true"}},
		"flag":             {args: []string{"--provider-stream"}, want: true},
		"empty env is off": {env: map[string]string{config.EnvProviderStream: ""}},
	} {
		env := withDB(tt.env)
		cfg, err := config.Load(tt.args, io.Discard, func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cfg.ProviderStream != tt.want {
			t.Errorf("%s: ProviderStream = %t, want %t", name, cfg.ProviderStream, tt.want)
		}
	}

	env := withDB(map[string]string{config.EnvProviderStream: "sometimes"})
	if _, err := config.Load(nil, io.Discard, func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), config.EnvProviderStream) {
		t.Fatalf("a non-boolean value: err = %v, want naming %s", err, config.EnvProviderStream)
	}
	if config.Default().ProviderStream {
		t.Fatal("provider streaming is on by default")
	}
}

func TestProviderStreamIsPrinted(t *testing.T) {
	cfg := config.Default()
	cfg.ProviderStream = true
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	if !strings.Contains(cfg.String(), "ProviderStream:true") || !strings.Contains(logged.String(), `"provider_stream":true`) {
		t.Fatalf("output lacks the flag: %s %s", cfg.String(), logged.String())
	}

	var out bytes.Buffer
	_, _ = config.Load([]string{"--help"}, &out, func(k string) string { return withDB(nil)[k] })
	if !strings.Contains(out.String(), "-provider-stream") || !strings.Contains(out.String(), config.EnvProviderStream) {
		t.Fatalf("usage does not describe the flag: %s", out.String())
	}
}
