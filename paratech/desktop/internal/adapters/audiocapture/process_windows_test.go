//go:build windows

package audiocapture_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/adapters/audiocapture"
)

// exitedPID starts a process (this test binary running no test) and reaps it, so its PID names no live process.
func exitedPID(t *testing.T) uint32 {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper process: %v", err)
	}
	return uint32(cmd.Process.Pid)
}

func TestPlatformProcessesTellsRunningFromExited(t *testing.T) {
	procs := audiocapture.PlatformProcesses()
	tests := []struct {
		name string
		pid  uint32
		want bool
	}{
		{"this process", uint32(os.Getpid()), true},
		{"the System process", 4, true},
		{"a reaped process", exitedPID(t), false},
		{"PID 0", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := procs.Alive(tt.pid)
			if err != nil || got != tt.want {
				t.Fatalf("Alive(%d) = %v, %v; want %v", tt.pid, got, err, tt.want)
			}
		})
	}
}

// Process loopback activates even for a dead PID (ADR-0002), so the recorder refuses such an INCLUDE target before
// it launches a child.
func TestRecorderRefusesADeadIncludeTarget(t *testing.T) {
	launcher := &helperLauncher{dataDir: t.TempDir(), scenarios: []string{"must-not-run"}}
	rec, err := audiocapture.New(audiocapture.Deps{
		Files: audiocapture.NewTestStore(filepath.Join(t.TempDir(), "artifacts")), Launcher: launcher,
		Processes: audiocapture.PlatformProcesses(),
	}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	err = rec.StartTarget(context.Background(), "rec1", ref(t, "rec1"), audiocapture.Target{Mode: audiocapture.ModeInclude, PID: exitedPID(t)})
	if !errors.Is(err, audiocapture.ErrInvalidTarget) {
		t.Fatalf("StartTarget = %v, want ErrInvalidTarget", err)
	}
	if launcher.count() != 0 {
		t.Fatal("a dead target launched a recorder child")
	}
}
