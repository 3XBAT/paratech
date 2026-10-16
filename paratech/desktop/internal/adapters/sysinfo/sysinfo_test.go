package sysinfo

import (
	"context"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/recording/values"
)

func TestReader(t *testing.T) {
	r := New()
	if b := r.Build(); b.GoVersion != runtime.Version() || b.OS != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("Build = %+v", b)
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

func TestBuildOf(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want values.AppBuild
	}{
		{name: "none", info: nil, want: values.AppBuild{}},
		{
			name: "a VCS build",
			info: &debug.BuildInfo{GoVersion: "go1.26.0", Main: debug.Module{Path: "paratech/desktop", Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "-ldflags", Value: `-X main.dir=C:\Users\student`}, {Key: "vcs.revision", Value: "abc123"},
					{Key: "vcs.time", Value: "2026-10-15T21:00:00Z"}, {Key: "vcs.modified", Value: "true"},
					{Key: "GOOS", Value: "windows"}, {Key: "GOARCH", Value: "amd64"}, {Key: "CGO_ENABLED", Value: "0"},
				}},
			want: values.AppBuild{Module: "paratech/desktop", Version: "(devel)", Revision: "abc123",
				RevisionTime: "2026-10-15T21:00:00Z", Modified: true, GoVersion: "go1.26.0", OS: "windows/amd64"},
		},
		{
			name: "no VCS",
			info: &debug.BuildInfo{GoVersion: "go1.26.0", Main: debug.Module{Path: "paratech/desktop", Version: "v0.1.0"},
				Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "false"}}},
			want: values.AppBuild{Module: "paratech/desktop", Version: "v0.1.0", GoVersion: "go1.26.0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildOf(tt.info); got != tt.want {
				t.Fatalf("buildOf = %+v, want %+v", got, tt.want)
			}
		})
	}
}
