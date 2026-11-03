//go:build e2e

package config_test

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/config"
)

func TestE2EFlags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	data := filepath.Join(t.TempDir(), "data")
	none := func(string) string { return "" }
	tests := []struct {
		name    string
		args    []string
		want    config.E2E
		wantErr []string
	}{
		{name: "none", args: nil},
		{name: "dir", args: []string{"--e2e-dir", dir}, want: config.E2E{Dir: dir}},
		{
			name: "all",
			args: []string{"--data-dir", data, "--e2e-dir", dir, "--e2e-cdp-port", "9333", "--e2e-window", "1280x720"},
			want: config.E2E{Dir: dir, CDPPort: 9333, Window: "1280x720"},
		},
		{name: "relative dir", args: []string{"--e2e-dir", "run"}, wantErr: []string{"must be an absolute path"}},
		{name: "port without dir", args: []string{"--e2e-cdp-port", "9333"}, wantErr: []string{"need --e2e-dir"}},
		{name: "port not a number", args: []string{"--e2e-dir", dir, "--e2e-cdp-port", "x"}, wantErr: []string{"invalid value"}},
		{
			name:    "joined with the other problems",
			args:    []string{"--log-level", "loud", "--e2e-dir", dir, "--e2e-cdp-port", "80", "--e2e-window", "10x10"},
			wantErr: []string{"unknown log level", "outside 1024..65535", "must be WxH"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(tt.args, io.Discard, none)
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatal("Load = nil error")
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				return
			}
			if err != nil || got.E2E != tt.want {
				t.Fatalf("Load = %+v, %v; want %+v", got.E2E, err, tt.want)
			}
		})
	}

	var out bytes.Buffer
	if _, err := config.Load([]string{"--help"}, &out, none); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	for _, want := range []string{"-e2e-dir", "-e2e-cdp-port", "-e2e-window"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage has no %q", want)
		}
	}
}

// A paratech: link launch still parses no flag, the E2E ones included.
func TestE2EFlagsIgnoredInASchemeLaunch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	got, err := config.Load([]string{"paratech://open", "--e2e-dir", dir}, io.Discard, func(string) string { return "" })
	if err != nil || got.E2E.On() {
		t.Fatalf("Load = %+v, %v", got.E2E, err)
	}
}
