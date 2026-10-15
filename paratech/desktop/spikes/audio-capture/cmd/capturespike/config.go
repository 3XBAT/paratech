// Command capturespike records system audio into closed WAV chunks with the pure-Go WASAPI prototype
// (spike of TASK-015, Windows only). See the package audiocapture for how to run it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"paratech/desktop/spikes/audio-capture/wasapi"
)

const (
	minChunk      = 30 * time.Second
	maxChunk      = 60 * time.Second
	minShortChunk = time.Second // only with -test-short, for smoke tests of rotation and kills
	maxDuration   = 3 * time.Hour
	maxExeName    = 255

	modeInclude = "include"
	modeExclude = "exclude"
	modeSystem  = "system"
	modeAuto    = "auto"
)

type config struct {
	mode      string
	exe       string
	pid       uint32
	out       string
	chunk     time.Duration
	duration  time.Duration
	child     bool
	recorder  bool
	testShort bool
}

var errUsage = errors.New("capturespike: invalid flags")

// parseFlags parses and validates the command line; usage and parse errors go to stderr.
func parseFlags(args []string, stderr io.Writer) (config, error) {
	var (
		c   config
		pid uint64
	)
	fs := flag.NewFlagSet("capturespike", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.mode, "mode", modeAuto, "include | exclude | system | auto (include, then system)")
	fs.StringVar(&c.exe, "exe", "", "executable name of the include target, for example chrome.exe")
	fs.Uint64Var(&pid, "pid", 0, "PID of the include target, or of the exclude target (default: this process)")
	fs.StringVar(&c.out, "out", "", "output directory for chunk files (required; inherits the parent folder's ACL, use one under your profile such as %TEMP%)")
	fs.DurationVar(&c.chunk, "chunk", minChunk, "chunk length, 30s..60s")
	fs.DurationVar(&c.duration, "duration", 10*time.Second, "recording length, at most 3h")
	fs.BoolVar(&c.child, "child", false, "record in a supervised child process (this binary with --recorder)")
	fs.BoolVar(&c.recorder, "recorder", false, "internal: child mode, stops when stdin closes")
	fs.BoolVar(&c.testShort, "test-short", false, "allow chunks from 1s for smoke tests")
	if err := fs.Parse(args); err != nil {
		return config{}, fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	}
	if pid > math.MaxUint32 {
		return config{}, fmt.Errorf("%w: -pid %d out of range", errUsage, pid)
	}
	c.pid = uint32(pid)

	if err := c.validate(); err != nil {
		return config{}, err
	}
	abs, err := filepath.Abs(c.out)
	if err != nil {
		return config{}, fmt.Errorf("%w: -out: %v", errUsage, err)
	}
	c.out = abs
	return c, nil
}

func (c config) validate() error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
	}

	switch c.mode {
	case modeInclude, modeAuto:
		if (c.exe == "") == (c.pid == 0) {
			return bad("-mode %s needs exactly one of -exe and -pid", c.mode)
		}
	case modeExclude:
		if c.exe != "" {
			return bad("-mode exclude takes -pid (default: this process), not -exe")
		}
	case modeSystem:
		if c.exe != "" || c.pid != 0 {
			return bad("-mode system takes neither -exe nor -pid")
		}
	default:
		return bad("unknown -mode %q", c.mode)
	}

	if c.exe != "" {
		// A bare executable name: it is only compared with snapshot entries, never opened.
		if len(c.exe) > maxExeName || strings.ContainsAny(c.exe, "/\\:\x00") || !strings.EqualFold(filepath.Ext(c.exe), ".exe") {
			return bad("-exe must be a file name like chrome.exe")
		}
	}

	if strings.TrimSpace(c.out) == "" {
		return bad("-out is required")
	}

	low := minChunk
	if c.testShort {
		low = minShortChunk
	}
	if c.chunk < low || c.chunk > maxChunk {
		return bad("-chunk %v outside %v..%v", c.chunk, low, maxChunk)
	}
	if c.duration <= 0 || c.duration > maxDuration {
		return bad("-duration %v outside (0, %v]", c.duration, maxDuration)
	}
	if c.child && c.recorder {
		return bad("-child and --recorder exclude each other")
	}
	return nil
}

// childArgs rebuilds the command line for the recorder child from the validated config.
func (c config) childArgs() []string {
	args := []string{"--recorder", "-mode", c.mode, "-out", c.out, "-chunk", c.chunk.String(), "-duration", c.duration.String()}
	if c.exe != "" {
		args = append(args, "-exe", c.exe)
	}
	if c.pid != 0 {
		args = append(args, "-pid", strconv.FormatUint(uint64(c.pid), 10))
	}
	if c.testShort {
		args = append(args, "-test-short")
	}
	return args
}

// specs turns the mode into the chain to open. resolve finds the tree root of an executable name.
func (c config) specs(resolve func(exe string) (uint32, error), ownPID uint32) ([]wasapi.Spec, error) {
	target := func() (uint32, error) {
		if c.pid != 0 {
			return c.pid, nil
		}
		return resolve(c.exe)
	}

	switch c.mode {
	case modeSystem:
		return []wasapi.Spec{{Mode: wasapi.ModeSystem}}, nil
	case modeExclude:
		pid := c.pid
		if pid == 0 {
			pid = ownPID
		}
		return []wasapi.Spec{{Mode: wasapi.ModeExclude, PID: pid}}, nil
	case modeInclude:
		pid, err := target()
		if err != nil {
			return nil, err
		}
		return []wasapi.Spec{{Mode: wasapi.ModeInclude, PID: pid}}, nil
	case modeAuto:
		// Owner decision: the picked app first, the whole system only as a fallback.
		pid, err := target()
		if err != nil {
			return []wasapi.Spec{{Mode: wasapi.ModeSystem}}, fmt.Errorf("include target: %w", err)
		}
		return []wasapi.Spec{{Mode: wasapi.ModeInclude, PID: pid}, {Mode: wasapi.ModeSystem}}, nil
	}
	return nil, fmt.Errorf("%w: unknown -mode %q", errUsage, c.mode)
}
