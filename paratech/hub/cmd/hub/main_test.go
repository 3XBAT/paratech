package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func TestRunExitsWithoutServing(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		env        map[string]string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "help", args: []string{"--help"}, wantCode: 0, wantStderr: "-log-level"},
		{name: "version", args: []string{"--version"}, wantCode: 0, wantStdout: "dev"},
		{name: "check ok", args: []string{"--check"}, wantCode: 0},
		{name: "unknown flag", args: []string{"--nope"}, wantCode: 2, wantStderr: "flag provided but not defined"},
		{name: "bad flag log level", args: []string{"--check", "--log-level", "loud"}, wantCode: 2, wantStderr: "unknown log level"},
		{name: "bad env log level", args: []string{"--check"}, env: map[string]string{"HUB_LOG_LEVEL": "loud"}, wantCode: 2, wantStderr: "unknown log level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			getenv := func(k string) string { return tt.env[k] }
			done := make(chan int, 1)
			go func() { done <- run(context.Background(), tt.args, &stdout, &stderr, getenv) }()
			select {
			case code := <-done:
				if code != tt.wantCode {
					t.Fatalf("exit code = %d, want %d (stderr: %s)", code, tt.wantCode, stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("run did not return: it must not block on this path")
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout %q does not contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run(ctx, nil, &bytes.Buffer{}, &stderr, noEnv) }()
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop after the context was cancelled")
	}
}
