//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"golang.org/x/sys/windows"

	"paratech/desktop/spikes/audio-capture/wasapi"
	"paratech/desktop/spikes/audio-capture/wavchunk"
)

const (
	dirPerm = 0o700

	openTimeout = 15 * time.Second // whole mode chain, including process loopback activation waits
	stopTimeout = 5 * time.Second  // capture goroutine exit after Stop

	childSlack = 5 * time.Second  // the child stops itself at -duration; the parent asks after this extra time
	childGrace = 10 * time.Second // after stdin is closed, then the child is killed
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	cfg, err := parseFlags(args, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	role := "recorder"
	switch {
	case cfg.child:
		role = "parent"
	case cfg.recorder:
		role = "child"
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil)).With("role", role, "pid", os.Getpid())

	v := windows.RtlGetVersion()
	log.Info("windows", "version", fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := os.MkdirAll(cfg.out, dirPerm); err != nil {
		log.Error("create output directory", "error", err.Error())
		return 1
	}

	if cfg.child {
		return supervise(ctx, cfg, log)
	}
	return record(ctx, cfg, log)
}

// record captures until the duration ends, Ctrl+C, stdin closes (child mode) or the stream fails.
func record(ctx context.Context, cfg config, log *slog.Logger) int {
	specs, err := cfg.specs(wasapi.FindTreeRoot, windows.GetCurrentProcessId())
	if err != nil {
		if len(specs) == 0 {
			log.Error("resolve target", "error", err.Error())
			return 1
		}
		log.Warn("include target not found, falling back", "error", err.Error())
	}

	sink := &chunkSink{cfg: wavchunk.Config{
		Dir:           cfg.out,
		ChunkDuration: cfg.chunk,
		OnChunk: func(c wavchunk.Chunk) {
			log.Info("chunk closed", "name", c.Name, "frames", c.Frames, "duration", c.Duration().String(), "format", c.Format.String())
		},
	}}

	openCtx, cancelOpen := context.WithTimeout(ctx, openTimeout)
	stream, attempts, err := wasapi.OpenChain(openCtx, wasapi.NewOpener(wasapi.Options{Log: log}), sink, specs...)
	cancelOpen()
	for _, a := range attempts {
		log.Info("attempt", "spec", a.Spec.String(), "ok", a.Err == nil, "hresult", hrString(a.Err), "detail", a.String())
	}
	if err != nil {
		log.Error("no capture mode started", "error", err.Error())
		return 1
	}
	log.Info("capturing", "spec", stream.Spec().String(), "format", stream.Format().String(), "duration", cfg.duration.String())

	var stdinClosed chan struct{}
	if cfg.recorder {
		stdinClosed = make(chan struct{})
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			close(stdinClosed)
		}()
	}

	timer := time.NewTimer(cfg.duration)
	defer timer.Stop()
	reason := ""
	select {
	case <-ctx.Done():
		reason = "interrupt"
	case <-timer.C:
		reason = "duration reached"
	case <-stdinClosed:
		reason = "stdin closed"
	case <-stream.Done():
		reason = "stream ended"
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), stopTimeout)
	defer cancelStop()
	if err := stream.Stop(stopCtx); err != nil {
		// The capture goroutine may still write: do not touch the sink, exit and let the OS clean up.
		log.Error("stop", "error", err.Error())
		return 1
	}

	code := 0
	streamErr := stream.Err()
	if streamErr != nil {
		code = 1
	}
	log.Info("stopped", "reason", reason, "stats", stream.Stats().String(), "stream_error", errText(streamErr), "hresult", hrString(streamErr),
		"cpu", cpuTime().String())

	if err := sink.Close(); err != nil {
		log.Error("close chunk writer", "error", err.Error())
		code = 1
	}
	report(cfg.out, log)
	return code
}

// supervise runs this binary with --recorder as a child, stops it by closing its stdin and kills it if it does not
// exit within childGrace. A crash or kill of the child loses at most its current chunk.
func supervise(ctx context.Context, cfg config, log *slog.Logger) int {
	exe, err := os.Executable()
	if err != nil {
		log.Error("find executable", "error", err.Error())
		return 1
	}

	cmd := exec.Command(exe, cfg.childArgs()...) //nolint:gosec // G204: this binary, with flags rebuilt from the validated config
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Error("stdin pipe", "error", err.Error())
		return 1
	}
	if err := cmd.Start(); err != nil {
		log.Error("start child", "error", err.Error())
		return 1
	}
	log.Info("child started", "child_pid", cmd.Process.Pid)

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	timer := time.NewTimer(cfg.duration + childSlack)
	defer timer.Stop()

	var waitErr error
	select {
	case waitErr = <-exited:
		log.Info("child exited by itself")
	case <-ctx.Done():
		waitErr = stopChild(cmd, stdin, exited, "interrupt", log)
	case <-timer.C:
		waitErr = stopChild(cmd, stdin, exited, "child overran its duration", log)
	}

	code := cmd.ProcessState.ExitCode()
	log.Info("child finished", "exit_code", code, "error", errText(waitErr))
	report(cfg.out, log)
	if code != 0 {
		return 1
	}
	return 0
}

func stopChild(cmd *exec.Cmd, stdin io.Closer, exited <-chan error, reason string, log *slog.Logger) error {
	log.Info("stopping child", "reason", reason)
	_ = stdin.Close()

	grace := time.NewTimer(childGrace)
	defer grace.Stop()
	select {
	case err := <-exited:
		return err
	case <-grace.C:
		log.Warn("child did not stop, killing it")
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Error("kill child", "error", err.Error())
		}
		return <-exited
	}
}

// cpuTime is the user plus kernel time of this process, for the CPU comparison in the ADR.
func cpuTime() time.Duration {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return 0
	}
	ticks := func(f windows.Filetime) int64 { return int64(f.HighDateTime)<<32 | int64(f.LowDateTime) }
	return time.Duration(ticks(kernel)+ticks(user)) * 100 // FILETIME counts 100 ns
}

func hrString(err error) string {
	if hr, ok := wasapi.HRESULTOf(err); ok {
		return hr.String()
	}
	return ""
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
