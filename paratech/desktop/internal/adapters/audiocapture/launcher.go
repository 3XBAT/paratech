package audiocapture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Launcher starts a recorder child process.
type Launcher interface {
	// Launch starts the child with pipes to its stdin, stdout and stderr.
	Launch() (Process, error)
}

// Process is a started recorder child.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader
	PID() int

	// Kill ends the process at once; killing an exited process is not an error.
	Kill() error

	// Wait reaps the process and returns its exit code. Call it once, after Stdout and Stderr reached EOF.
	Wait() (int, error)
}

// CommandLauncher runs a program as the recorder child: no shell, the given arguments, the given environment only
// (on Windows Go adds SYSTEMROOT), no console window.
type CommandLauncher struct {
	Path string
	Args []string
	Env  []string
}

// ChildLauncher runs this executable in the --recorder run mode on the data directory: the only arguments, and no
// environment, so neither carries anything secret.
func ChildLauncher(dataDir string) (*CommandLauncher, error) {
	if dataDir == "" || !filepath.IsAbs(dataDir) {
		return nil, errors.New("audiocapture: the data directory must be an absolute path")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("audiocapture: find the executable: %w", err)
	}
	return &CommandLauncher{Path: exe, Args: []string{"--recorder", "--data-dir", dataDir}}, nil
}

// Launch starts the program.
func (l *CommandLauncher) Launch() (Process, error) {
	cmd := exec.Command(l.Path, l.Args...) //nolint:gosec // G204: our own executable with fixed arguments
	cmd.Env = append([]string{}, l.Env...)
	hideWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("audiocapture: start the recorder child: %w", err)
	}
	return &command{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

type command struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

func (c *command) Stdin() io.WriteCloser { return c.stdin }
func (c *command) Stdout() io.Reader     { return c.stdout }
func (c *command) Stderr() io.Reader     { return c.stderr }
func (c *command) PID() int              { return c.cmd.Process.Pid }

func (c *command) Kill() error {
	if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func (c *command) Wait() (int, error) {
	err := c.cmd.Wait()
	var exit *exec.ExitError
	if err == nil || errors.As(err, &exit) {
		return c.cmd.ProcessState.ExitCode(), nil
	}
	return -1, err
}
