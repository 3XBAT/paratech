package sysinfo

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReader(t *testing.T) {
	r := New()
	if info, ok := r.Build(); !ok || info.GoVersion != runtime.Version() {
		t.Fatalf("Build = %+v, %v", info, ok)
	}
	v, dir := r.Windows(context.Background()), r.ProfileDir()
	if runtime.GOOS != "windows" {
		if !v.IsZero() || dir != "" {
			t.Fatalf("Windows %+v, profile %q off Windows", v, dir)
		}
		return
	}
	if v.Major != 10 || v.Build == 0 || !strings.HasPrefix(v.Product, "Windows") || !strings.HasPrefix(v.BuildString(), "10.0.") {
		t.Fatalf("Windows = %+v", v)
	}
	if !filepath.IsAbs(dir) {
		t.Fatalf("profile %q is not absolute", dir)
	}
}
