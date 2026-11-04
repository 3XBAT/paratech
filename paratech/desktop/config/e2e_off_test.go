//go:build !e2e

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

// Without tag e2e no E2E flag exists, so the shipped exe stops at once on any of them (MustLoad: exit 2).
func TestE2EFlagsAbsentWithoutTag(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	for _, args := range [][]string{
		{"--e2e-dir", dir},
		{"-e2e-dir=" + dir},
		{"--e2e-cdp-port", "9333"},
		{"--e2e-window", "1280x720"},
	} {
		_, err := config.Load(args, io.Discard, func(string) string { return "" })
		if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
			t.Errorf("Load(%q) = %v, want an undefined flag", args, err)
		}
	}

	var out bytes.Buffer
	if _, err := config.Load([]string{"--help"}, &out, func(string) string { return "" }); !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "e2e") {
		t.Errorf("the usage lists an e2e flag:\n%s", out.String())
	}

	cfg, err := config.Load(nil, io.Discard, func(string) string { return "" })
	if err != nil || cfg.E2E != (config.E2E{}) {
		t.Fatalf("Load = %+v, %v: the E2E mode must stay zero", cfg.E2E, err)
	}
}
