package config_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/hub/config"
)

// fakeKey is an obviously fake provider key; it must never show up in output.
const fakeKey = "test-key-not-real"

func TestLoadLLM(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		want    func(config.Config) bool
		wantErr string
	}{
		{
			name: "defaults",
			want: func(c config.Config) bool {
				return c.AnthropicAPIKey == "" && c.AnthropicBaseURL == "https://api.anthropic.com" && c.AnthropicAuth == config.AnthropicAuthXAPIKey &&
					c.LLMTimeout == 5*time.Minute &&
					c.LLMModels == "default=claude-sonnet-5-5:8192,planner=claude-haiku-5-5:4096"
			},
		},
		{
			name: "environment",
			env: map[string]string{config.EnvAnthropicAPIKey: fakeKey, config.EnvAnthropicBaseURL: "http://127.0.0.1:8089",
				config.EnvLLMTimeout: "2m", config.EnvLLMModels: "default=claude-sonnet-5-5:4096,fast=claude-haiku-5-5:1024"},
			want: func(c config.Config) bool {
				return c.AnthropicAPIKey == fakeKey && c.AnthropicBaseURL == "http://127.0.0.1:8089" && c.LLMTimeout == 2*time.Minute &&
					c.LLMModels == "default=claude-sonnet-5-5:4096,fast=claude-haiku-5-5:1024"
			},
		},
		{
			name: "flags win",
			args: []string{"--anthropic-base-url", "https://proxy.example", "--llm-timeout", "30s", "--llm-models", "a=m:1"},
			env:  map[string]string{config.EnvAnthropicBaseURL: "https://other.example", config.EnvLLMTimeout: "1m", config.EnvLLMModels: "b=m:2"},
			want: func(c config.Config) bool {
				return c.AnthropicBaseURL == "https://proxy.example" && c.LLMTimeout == 30*time.Second && c.LLMModels == "a=m:1"
			},
		},
		{
			name: "bounds",
			env:  map[string]string{config.EnvLLMTimeout: "10m", config.EnvQuotaReservationTTL: "20m"},
			want: func(c config.Config) bool { return c.LLMTimeout == 10*time.Minute },
		},
		{name: "localhost over http", env: map[string]string{config.EnvAnthropicBaseURL: "http://localhost:9"}, want: func(config.Config) bool { return true }},
		{name: "ipv6 loopback over http", env: map[string]string{config.EnvAnthropicBaseURL: "http://[::1]:9/"}, want: func(config.Config) bool { return true }},
		{
			name: "bearer gateway with a path",
			env:  map[string]string{config.EnvAnthropicAuth: "bearer", config.EnvAnthropicBaseURL: "https://gw.example.ru/anthropic/"},
			want: func(c config.Config) bool {
				return c.AnthropicAuth == config.AnthropicAuthBearer && c.AnthropicBaseURL == "https://gw.example.ru/anthropic/"
			},
		},
		{
			name: "explicit x-api-key",
			env:  map[string]string{config.EnvAnthropicAuth: "x-api-key"},
			want: func(c config.Config) bool { return c.AnthropicAuth == config.AnthropicAuthXAPIKey },
		},
		{
			name: "auth flag wins",
			args: []string{"--anthropic-auth", "bearer"},
			env:  map[string]string{config.EnvAnthropicAuth: "x-api-key"},
			want: func(c config.Config) bool { return c.AnthropicAuth == config.AnthropicAuthBearer },
		},
		{
			name: "path over http to loopback",
			env:  map[string]string{config.EnvAnthropicBaseURL: "http://127.0.0.1:9/a-b/c_d.e~f"},
			want: func(config.Config) bool { return true },
		},
		{name: "unknown auth", env: map[string]string{config.EnvAnthropicAuth: "Bearer"}, wantErr: config.EnvAnthropicAuth + ": unknown value, want x-api-key or bearer"},
		{name: "key pasted as auth", env: map[string]string{config.EnvAnthropicAuth: fakeKey}, wantErr: config.EnvAnthropicAuth},
		{name: "unknown auth flag", args: []string{"-anthropic-auth", "basic"}, wantErr: config.EnvAnthropicAuth},
		{name: "path with query", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/anthropic?a=1"}, wantErr: "query"},
		{name: "path with fragment", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/anthropic#f"}, wantErr: "fragment"},
		{name: "path with user info", env: map[string]string{config.EnvAnthropicBaseURL: "https://u:pw-marker@gw.example.ru/anthropic"}, wantErr: "user info"},
		{name: "http to a remote gateway", env: map[string]string{config.EnvAnthropicBaseURL: "http://gw.example.ru/anthropic"}, wantErr: "scheme must be https"},
		{name: "dot-dot path", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/a/../b"}, wantErr: "path"},
		{name: "dot path", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/./b"}, wantErr: "path"},
		{name: "double slash", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru//a"}, wantErr: "path"},
		{name: "encoded slash", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/a%2Fb"}, wantErr: "path"},
		{name: "encoded dots", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/%2e%2e"}, wantErr: "path"},
		{name: "backslash", env: map[string]string{config.EnvAnthropicBaseURL: `https://gw.example.ru/a\b`}, wantErr: config.EnvAnthropicBaseURL},
		{name: "semicolon", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/a;b"}, wantErr: "path"},
		{name: "long path", env: map[string]string{config.EnvAnthropicBaseURL: "https://gw.example.ru/" + strings.Repeat("a", 300)}, wantErr: "path"},

		{name: "timeout not a duration", env: map[string]string{config.EnvLLMTimeout: "long"}, wantErr: config.EnvLLMTimeout},
		{name: "timeout too short", env: map[string]string{config.EnvLLMTimeout: "9s"}, wantErr: "LLM timeout"},
		{name: "timeout too long", env: map[string]string{config.EnvLLMTimeout: "11m", config.EnvQuotaReservationTTL: "1h"}, wantErr: "LLM timeout"},
		{name: "timeout above half the TTL", env: map[string]string{config.EnvLLMTimeout: "6m"}, wantErr: "half the quota reservation TTL"},
		{name: "TTL below twice the timeout", env: map[string]string{config.EnvQuotaReservationTTL: "9m"}, wantErr: "half the quota reservation TTL"},
		{name: "http to a remote host", env: map[string]string{config.EnvAnthropicBaseURL: "http://api.anthropic.com"}, wantErr: "scheme must be https"},
		{name: "http to a loopback-looking name", env: map[string]string{config.EnvAnthropicBaseURL: "http://127.0.0.1.evil.example"}, wantErr: "scheme must be https"},
		{name: "ftp", env: map[string]string{config.EnvAnthropicBaseURL: "ftp://api.anthropic.com"}, wantErr: "scheme must be https"},
		{name: "relative base URL", env: map[string]string{config.EnvAnthropicBaseURL: "api.anthropic.com"}, wantErr: config.EnvAnthropicBaseURL},
		{name: "base URL with user info", env: map[string]string{config.EnvAnthropicBaseURL: "https://u:pw-marker@api.anthropic.com"}, wantErr: "user info"},
		{name: "base URL with query", env: map[string]string{config.EnvAnthropicBaseURL: "https://api.anthropic.com?a=1"}, wantErr: "query"},
		{name: "base URL unparsable", env: map[string]string{config.EnvAnthropicBaseURL: "https://h:port"}, wantErr: config.EnvAnthropicBaseURL},
		{name: "key with a space", env: map[string]string{config.EnvAnthropicAPIKey: "test key-not-real"}, wantErr: config.EnvAnthropicAPIKey},
		{name: "key with a newline", env: map[string]string{config.EnvAnthropicAPIKey: fakeKey + "\n"}, wantErr: config.EnvAnthropicAPIKey},
		{name: "models syntax", env: map[string]string{config.EnvLLMModels: "default:claude"}, wantErr: "want alias=model-id:max_output_tokens"},
		{name: "models flag syntax", args: []string{"--llm-models", "a=m"}, wantErr: "want alias=model-id:max_output_tokens"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := withDB(tt.env)
			got, err := config.Load(tt.args, io.Discard, func(k string) string { return env[k] })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "key-not-real") || strings.Contains(err.Error(), "pw-marker") {
					t.Fatalf("error leaks a secret: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.want(got) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestParseModels(t *testing.T) {
	got, err := config.ParseModels(" default=claude-sonnet-5-5:8192 , fast_1=claude-haiku-4-5-20251001:1024")
	if err != nil {
		t.Fatal(err)
	}
	want := []config.Model{
		{Alias: "default", ID: "claude-sonnet-5-5", MaxOutput: 8192},
		{Alias: "fast_1", ID: "claude-haiku-4-5-20251001", MaxOutput: 1024},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	for _, spec := range []string{
		"", " ", "default", "default=", "=m:1", "a=:1", "a=m:", "a=m:x", "a=m:0", "a=m:-5", "a=m:1.5",
		"a=m:99999999999999999999", "a=m:1,", ",a=m:1", "a=m:1,,b=m:2", "a=m:1,a=n:2", "a b=m:1", "a=m n:1", "a=m:1 0",
	} {
		if models, err := config.ParseModels(spec); err == nil {
			t.Errorf("ParseModels(%q) = %+v, want an error", spec, models)
		}
	}
}

func TestConfigModels(t *testing.T) {
	cfg := config.Default()
	if got := cfg.Models(); len(got) != 2 || got[0] != (config.Model{Alias: "default", ID: "claude-sonnet-5-5", MaxOutput: 8192}) ||
		got[1] != (config.Model{Alias: "planner", ID: "claude-haiku-5-5", MaxOutput: 4096}) {
		t.Fatalf("default models = %+v", got)
	}
	cfg.LLMModels = "broken"
	if got := cfg.Models(); got != nil {
		t.Fatalf("models of an invalid allowlist = %+v, want nil", got)
	}
}

func TestLLMSettingsAreCheckedOnlyForServe(t *testing.T) {
	env := withDB(map[string]string{config.EnvLLMTimeout: "1h", config.EnvAnthropicBaseURL: "http://remote.example", config.EnvLLMModels: "x",
		config.EnvAnthropicAuth: "x"})
	if _, err := config.Load([]string{"keys", "list"}, io.Discard, func(k string) string { return env[k] }); err != nil {
		t.Fatalf("keys list with bad LLM settings: %v", err)
	}
	_, err := config.Load(nil, io.Discard, func(k string) string { return env[k] })
	for _, want := range []string{"LLM timeout", config.EnvAnthropicBaseURL, config.EnvLLMModels, config.EnvAnthropicAuth} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("serve: error = %v, want containing %q", err, want)
		}
	}
}

func TestAnthropicKeyIsNotAFlag(t *testing.T) {
	getenv := func(k string) string { return withDB(nil)[k] }
	for _, name := range []string{"--anthropic-api-key", "--api-key", "--anthropic-key"} {
		_, err := config.Load([]string{name, fakeKey}, io.Discard, getenv)
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
			t.Fatalf("%s: error = %v", name, err)
		}
	}

	var out bytes.Buffer
	_, _ = config.Load([]string{"--help"}, &out, getenv)
	for _, want := range []string{"-anthropic-base-url", "-anthropic-auth", config.EnvAnthropicAuth, "-llm-timeout", "-llm-models", config.EnvAnthropicAPIKey} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage does not mention %q: %s", want, out.String())
		}
	}
}

func TestConfigDoesNotPrintTheAnthropicKey(t *testing.T) {
	env := withDB(map[string]string{config.EnvAnthropicAPIKey: fakeKey})
	cfg, err := config.Load(nil, io.Discard, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}

	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	slog.New(slog.NewTextHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	for _, out := range []string{
		fmt.Sprint(cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%v", &cfg), fmt.Sprintf("%#v", cfg), fmt.Sprintf("%#v", &cfg), logged.String(),
	} {
		if strings.Contains(out, fakeKey) {
			t.Errorf("output leaks the provider key: %s", out)
		}
		if strings.Contains(out, "s3cret-marker") {
			t.Errorf("output leaks the database password: %s", out)
		}
		if !strings.Contains(out, "AnthropicAPIKey:(redacted)") && !strings.Contains(out, `"anthropic_api_key":"(redacted)"`) {
			t.Errorf("output %s does not mark the provider key as redacted", out)
		}
		for _, want := range []string{"api.anthropic.com", "5m0s", "default=claude-sonnet-5-5:8192"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q: %s", want, out)
			}
		}
	}

	cfg.AnthropicBaseURL = "https://u:pw-marker@api.anthropic.com"
	if out := cfg.String(); strings.Contains(out, "pw-marker") {
		t.Errorf("output leaks base URL credentials: %s", out)
	}
}

func TestConfigPrintsTheAuthModeNotTheKey(t *testing.T) {
	for _, mode := range []string{config.AnthropicAuthXAPIKey, config.AnthropicAuthBearer} {
		env := withDB(map[string]string{config.EnvAnthropicAPIKey: fakeKey, config.EnvAnthropicAuth: mode,
			config.EnvAnthropicBaseURL: "https://gw.example.ru/anthropic"})
		cfg, err := config.Load(nil, io.Discard, func(k string) string { return env[k] })
		if err != nil {
			t.Fatal(err)
		}
		var logged bytes.Buffer
		slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
		for _, out := range []string{fmt.Sprint(cfg), fmt.Sprintf("%#v", cfg), logged.String()} {
			if strings.Contains(out, fakeKey) {
				t.Errorf("%s: output leaks the provider key: %s", mode, out)
			}
			if !strings.Contains(out, "AnthropicAuth:"+mode) && !strings.Contains(out, `"anthropic_auth":"`+mode+`"`) {
				t.Errorf("%s: output does not show the auth mode: %s", mode, out)
			}
			if !strings.Contains(out, "gw.example.ru/anthropic") {
				t.Errorf("%s: output does not show the base URL path: %s", mode, out)
			}
		}
	}

	// A key pasted into the auth variable by mistake is printed as invalid, not as is.
	cfg := config.Default()
	cfg.AnthropicAuth = fakeKey
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("cfg", "cfg", cfg)
	for _, out := range []string{cfg.String(), logged.String()} {
		if strings.Contains(out, fakeKey) {
			t.Errorf("output leaks a misplaced key: %s", out)
		}
	}
}
