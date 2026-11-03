package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"paratech/desktop/config"
)

// The rules of the E2E fields hold in every build; only the flags that set them need tag e2e.
func TestValidateE2E(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	tests := []struct {
		name    string
		e2e     config.E2E
		wantErr []string
		wantW   int
		wantH   int
	}{
		{name: "off"},
		{name: "dir only", e2e: config.E2E{Dir: dir}},
		{name: "all set", e2e: config.E2E{Dir: dir, CDPPort: 9333, Window: "1280x720"}, wantW: 1280, wantH: 720},
		{name: "upper case X", e2e: config.E2E{Dir: dir, Window: "800X640"}, wantW: 800, wantH: 640},
		{name: "port bounds", e2e: config.E2E{Dir: dir, CDPPort: 1024}},
		{name: "port top", e2e: config.E2E{Dir: dir, CDPPort: 65535}},
		{name: "window bounds", e2e: config.E2E{Dir: dir, Window: "640x3840"}, wantW: 640, wantH: 3840},
		{name: "relative dir", e2e: config.E2E{Dir: "run"}, wantErr: []string{"must be an absolute path"}},
		{name: "port without dir", e2e: config.E2E{CDPPort: 9333}, wantErr: []string{"need --e2e-dir"}},
		{name: "window without dir", e2e: config.E2E{Window: "800x600"}, wantErr: []string{"need --e2e-dir"}},
		{name: "low port", e2e: config.E2E{Dir: dir, CDPPort: 1023}, wantErr: []string{"outside 1024..65535"}},
		{name: "high port", e2e: config.E2E{Dir: dir, CDPPort: 65536}, wantErr: []string{"outside 1024..65535"}},
		{name: "negative port", e2e: config.E2E{Dir: dir, CDPPort: -1}, wantErr: []string{"outside 1024..65535"}},
		{name: "small window", e2e: config.E2E{Dir: dir, Window: "639x720"}, wantErr: []string{"must be WxH"}},
		{name: "large window", e2e: config.E2E{Dir: dir, Window: "1280x3841"}, wantErr: []string{"must be WxH"}},
		{name: "no x", e2e: config.E2E{Dir: dir, Window: "1280"}, wantErr: []string{"must be WxH"}},
		{name: "garbage", e2e: config.E2E{Dir: dir, Window: "axb"}, wantErr: []string{"must be WxH"}},
		{name: "three parts", e2e: config.E2E{Dir: dir, Window: "800x600x2"}, wantErr: []string{"must be WxH"}},
		{
			name:    "all problems at once",
			e2e:     config.E2E{Dir: "run", CDPPort: 80, Window: "1x1"},
			wantErr: []string{"absolute path", "outside 1024..65535", "must be WxH"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.E2E = tt.e2e
			err := cfg.Validate()
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Validate = %v", err)
				}
				if w, h := tt.e2e.WindowSize(); w != tt.wantW || h != tt.wantH {
					t.Fatalf("WindowSize = %dx%d, want %dx%d", w, h, tt.wantW, tt.wantH)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate = nil, want an error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestE2EOn(t *testing.T) {
	if (config.E2E{}).On() || (config.E2E{CDPPort: 9333}).On() {
		t.Fatal("the mode is on without a dir")
	}
	if !(config.E2E{Dir: filepath.Join(t.TempDir(), "run")}).On() {
		t.Fatal("the mode is off with a dir")
	}
}
