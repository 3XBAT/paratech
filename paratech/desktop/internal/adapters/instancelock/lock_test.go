package instancelock

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

// holderEnv makes the test binary a helper process that holds the lock of the named directory until its stdin closes.
const holderEnv = "INSTANCELOCK_TEST_HOLD"

func TestMain(m *testing.M) {
	if dir := os.Getenv(holderEnv); dir != "" {
		os.Exit(hold(dir))
	}
	os.Exit(m.Run())
}

func hold(dir string) int {
	l, err := Acquire(dir)
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = l.Release()
	return 0
}

func TestAcquireIsExclusive(t *testing.T) {
	dir := t.TempDir()

	a, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrHeld) {
		t.Fatalf("second Acquire = %v, want ErrHeld", err)
	}
	other, err := Acquire(t.TempDir())
	if err != nil {
		t.Fatalf("Acquire of another directory: %v", err)
	}
	if err := other.Release(); err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		if err := a.Release(); err != nil {
			t.Fatalf("Release #%d: %v", i+1, err)
		}
	}
	b, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	if err := b.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("lock file: %v; it must stay", err)
	}
}

func TestAcquireRejects(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want error // nil: any error
	}{
		{name: "empty", dir: ""},
		{name: "relative", dir: "data"},
		{name: "missing directory", dir: filepath.Join(t.TempDir(), "missing"), want: shared.ErrStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := Acquire(tt.dir)
			if err == nil {
				_ = l.Release()
				t.Fatal("Acquire succeeded")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("Acquire = %v, want %v", err, tt.want)
			}
		})
	}
}

// A lock failure other than "held" keeps the OS error code for diagnosis, still as a storage error.
func TestLockFailureKeepsTheErrno(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	err = lockFile(f) // a closed file has an invalid handle
	var errno syscall.Errno
	if !errors.Is(err, shared.ErrStorage) || !errors.As(err, &errno) {
		t.Fatalf("lockFile on a closed file = %v, want shared.ErrStorage wrapping the errno", err)
	}
}

// Another process holds the lock until it ends; then the lock is free again without any cleanup.
func TestAcquireAgainstAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-test.run=^$")
	cmd.Env = append(os.Environ(), holderEnv+"="+dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		_ = stdin.Close()
		_ = cmd.Wait()
		t.Fatalf("helper: %q, %v", line, err)
	}

	if _, err := Acquire(dir); !errors.Is(err, ErrHeld) {
		t.Errorf("Acquire while another process holds the lock = %v, want ErrHeld", err)
	}

	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		l, err := Acquire(dir)
		if err == nil {
			_ = l.Release()
			return
		}
		if !errors.Is(err, ErrHeld) || time.Now().After(deadline) {
			t.Fatalf("Acquire after the other process ended: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
