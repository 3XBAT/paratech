package main

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"paratech/desktop/spikes/audio-capture/wasapi"
	"paratech/desktop/spikes/audio-capture/wavchunk"
)

func TestParseFlags(t *testing.T) {
	out := t.TempDir()
	tests := []struct {
		name string
		args []string
		ok   bool
	}{
		{name: "system", args: []string{"-mode", "system", "-out", out}, ok: true},
		{name: "include by exe", args: []string{"-mode", "include", "-exe", "chrome.exe", "-out", out}, ok: true},
		{name: "include by pid", args: []string{"-mode", "include", "-pid", "1234", "-out", out}, ok: true},
		{name: "exclude own pid", args: []string{"-mode", "exclude", "-out", out}, ok: true},
		{name: "auto with exe", args: []string{"-mode", "auto", "-exe", "Zoom.EXE", "-out", out, "-chunk", "60s", "-duration", "3h"}, ok: true},
		{name: "child", args: []string{"-mode", "system", "-out", out, "-child"}, ok: true},
		{name: "recorder", args: []string{"--recorder", "-mode", "system", "-out", out}, ok: true},
		{name: "short chunk with test-short", args: []string{"-mode", "system", "-out", out, "-chunk", "2s", "-test-short"}, ok: true},

		{name: "missing out", args: []string{"-mode", "system"}},
		{name: "unknown mode", args: []string{"-mode", "mic", "-out", out}},
		{name: "include without target", args: []string{"-mode", "include", "-out", out}},
		{name: "include with both targets", args: []string{"-mode", "include", "-exe", "a.exe", "-pid", "1", "-out", out}},
		{name: "auto without target", args: []string{"-mode", "auto", "-out", out}},
		{name: "exclude with exe", args: []string{"-mode", "exclude", "-exe", "a.exe", "-out", out}},
		{name: "system with pid", args: []string{"-mode", "system", "-pid", "5", "-out", out}},
		{name: "exe with path", args: []string{"-mode", "include", "-exe", `C:\x\chrome.exe`, "-out", out}},
		{name: "exe with traversal", args: []string{"-mode", "include", "-exe", "../chrome.exe", "-out", out}},
		{name: "exe without extension", args: []string{"-mode", "include", "-exe", "chrome", "-out", out}},
		{name: "chunk too short", args: []string{"-mode", "system", "-out", out, "-chunk", "29s"}},
		{name: "chunk too long", args: []string{"-mode", "system", "-out", out, "-chunk", "61s"}},
		{name: "test-short chunk under 1s", args: []string{"-mode", "system", "-out", out, "-chunk", "500ms", "-test-short"}},
		{name: "duration too long", args: []string{"-mode", "system", "-out", out, "-duration", "3h1s"}},
		{name: "zero duration", args: []string{"-mode", "system", "-out", out, "-duration", "0s"}},
		{name: "child and recorder", args: []string{"-mode", "system", "-out", out, "-child", "--recorder"}},
		{name: "pid out of range", args: []string{"-mode", "include", "-pid", "4294967296", "-out", out}},
		{name: "extra argument", args: []string{"-mode", "system", "-out", out, "extra"}},
		{name: "unknown flag", args: []string{"-mode", "system", "-out", out, "-x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parseFlags(tt.args, io.Discard)
			if !tt.ok {
				if !errors.Is(err, errUsage) {
					t.Fatalf("parseFlags = %+v, %v; want errUsage", c, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !filepath.IsAbs(c.out) {
				t.Fatalf("out %q is not absolute", c.out)
			}
		})
	}
}

func TestChildArgsRoundTrip(t *testing.T) {
	out := t.TempDir()
	tests := [][]string{
		{"-mode", "auto", "-exe", "chrome.exe", "-out", out, "-chunk", "45s", "-duration", "2m", "-child"},
		{"-mode", "exclude", "-pid", "77", "-out", out, "-chunk", "3s", "-duration", "8s", "-test-short", "-child"},
		{"-mode", "system", "-out", out, "-child"},
	}
	for _, args := range tests {
		parent, err := parseFlags(args, io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		child, err := parseFlags(parent.childArgs(), io.Discard)
		if err != nil {
			t.Fatalf("child args %q: %v", parent.childArgs(), err)
		}
		want := parent
		want.child, want.recorder = false, true
		if child != want {
			t.Fatalf("child config %+v, want %+v", child, want)
		}
	}
}

func TestSpecs(t *testing.T) {
	resolve := func(exe string) (uint32, error) {
		if exe == "chrome.exe" {
			return 200, nil
		}
		return 0, wasapi.ErrNoProcess
	}
	sys := wasapi.Spec{Mode: wasapi.ModeSystem}
	tests := []struct {
		name    string
		c       config
		want    []wasapi.Spec
		wantErr bool
	}{
		{name: "system", c: config{mode: modeSystem}, want: []wasapi.Spec{sys}},
		{name: "exclude defaults to own pid", c: config{mode: modeExclude}, want: []wasapi.Spec{{Mode: wasapi.ModeExclude, PID: 9}}},
		{name: "exclude other pid", c: config{mode: modeExclude, pid: 5}, want: []wasapi.Spec{{Mode: wasapi.ModeExclude, PID: 5}}},
		{name: "include exe", c: config{mode: modeInclude, exe: "chrome.exe"}, want: []wasapi.Spec{{Mode: wasapi.ModeInclude, PID: 200}}},
		{name: "include pid", c: config{mode: modeInclude, pid: 33}, want: []wasapi.Spec{{Mode: wasapi.ModeInclude, PID: 33}}},
		{name: "include missing exe", c: config{mode: modeInclude, exe: "zoom.exe"}, wantErr: true},
		{name: "auto", c: config{mode: modeAuto, exe: "chrome.exe"}, want: []wasapi.Spec{{Mode: wasapi.ModeInclude, PID: 200}, sys}},
		{name: "auto missing exe falls back to system", c: config{mode: modeAuto, exe: "zoom.exe"}, want: []wasapi.Spec{sys}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.specs(resolve, 9)
			if (err != nil) != tt.wantErr || !slices.Equal(got, tt.want) {
				t.Fatalf("specs = %v, %v; want %v (error %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestChunkSinkAndReport(t *testing.T) {
	dir := t.TempDir()
	s := &chunkSink{cfg: wavchunk.Config{Dir: dir, ChunkDuration: 10 * time.Millisecond}}
	f := wasapi.Format{SampleRate: 1000, Channels: 1, BitsPerSample: 16}
	if err := s.SetFormat(f); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(make([]byte, 25*2)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFormat(wasapi.Format{SampleRate: 1000, Channels: 2, BitsPerSample: 32, Float: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Write(make([]byte, 3*8)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	valid, invalid := report(dir, slog.New(slog.DiscardHandler))
	if valid != 4 || invalid != 0 {
		t.Fatalf("report = %d valid, %d invalid; want 4 and 0", valid, invalid)
	}
	if err := (&chunkSink{}).Close(); err != nil {
		t.Fatalf("closing an unused sink: %v", err)
	}
}
