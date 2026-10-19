package audiocapture_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/audiocapture"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/testenv"
)

var comet = audiocapture.ProcessIdentity{App: "comet.exe", PID: 500, Created: 5}

func cometSource(t *testing.T) values.Source {
	t.Helper()
	s, err := values.SourceOf(comet.App)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func switchTo(t *testing.T, e *recorderEnv, source values.Source) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return e.rec.Switch(ctx, "rec1", source)
}

// checkChunksNumberedOn checks closed chunks numbered 1, 2, ... without a gap or an overwrite.
func checkChunksNumberedOn(t *testing.T, e *recorderEnv) {
	t.Helper()
	names := e.names(t)
	if len(names) == 0 {
		t.Fatal("no chunks")
	}
	for i, n := range names {
		if want := fmt.Sprintf("chunk-%06d.wav", i+1); n != want {
			t.Fatalf("chunks = %v, want closed chunks numbered on", names)
		}
	}
}

// TASK-082: the user switches the same recording to the app that plays: one child after the other, the chunks go on.
func TestRecorderSwitchToAnotherApp(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	finder := newFakeFinder(zoom1, comet)
	cfg := rebindConfig()
	cfg.MaxRestarts = 0 // a switch is no restart
	e, logs := newAppEnv(t, cfg, finder, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	e.waitDiag(t, "the first chunk closes", func(d audiocapture.Diagnostics) bool { return d.Chunks >= 1 })

	if err := switchTo(t, e, cometSource(t)); err != nil {
		t.Fatalf("Switch = %v", err)
	}
	d := e.diag(t)
	if d.App != "comet.exe" || d.TargetMode != audiocapture.ModeInclude || d.TargetPID != comet.PID || !d.Running ||
		d.SourceSwitches != 1 || d.Restarts != 0 || e.launcher.count() != 2 {
		t.Fatalf("diagnostics %+v, %d children; want comet captured by the second child", d, e.launcher.count())
	}
	if _, ok := d.Fallback(); ok {
		t.Fatal("a switch of the user is no fallback")
	}
	chunks := d.Chunks
	e.waitDiag(t, "the new child closes a chunk", func(d audiocapture.Diagnostics) bool { return d.Chunks > chunks })

	// Comet exits: the capture follows comet now, not zoom.
	finder.set(zoom1)
	e.waitDiag(t, "the capture waits for comet", func(d audiocapture.Diagnostics) bool { return d.WaitingForApp })
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	checkChunksNumberedOn(t, e)
	if tr, err := e.rec.Finalize(context.Background(), e.chunks, e.track); err != nil || tr.Silent() {
		t.Fatalf("Finalize = %+v, %v", tr, err)
	}
	if logs.count("capture switched by the user") != 1 {
		t.Fatalf("log:\n%s", logs)
	}
}

func TestRecorderSwitchToTheWholeComputer(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	e, _ := newAppEnv(t, rebindConfig(), newFakeFinder(zoom1), testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	if err := switchTo(t, e, values.WholeComputer()); err != nil {
		t.Fatalf("Switch = %v", err)
	}
	if d := e.diag(t); d.App != "" || d.TargetMode != audiocapture.ModeExclude || d.TargetPID != uint32(os.Getpid()) ||
		!d.Running || d.SourceSwitches != 1 {
		t.Fatalf("diagnostics %+v; want EXCLUDE of this process", d)
	}

	// The same source again changes nothing.
	if err := switchTo(t, e, values.WholeComputer()); err != nil || e.launcher.count() != 2 {
		t.Fatalf("Switch to the same source = %v, %d children", err, e.launcher.count())
	}
	e.waitDiag(t, "a chunk closes", func(d audiocapture.Diagnostics) bool { return d.Chunks >= 1 })
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	checkChunksNumberedOn(t, e)
}

// A switch that cannot be done leaves the capture as it was: the app does not run, the capture stops or ended.
func TestRecorderSwitchRefused(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	t.Run("the app does not run", func(t *testing.T) {
		e, _ := newAppEnv(t, rebindConfig(), newFakeFinder(zoom1), testenv.RecorderTone)
		if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
			t.Fatal(err)
		}
		before := e.diag(t)
		err := switchTo(t, e, cometSource(t))
		if !errors.Is(err, recording.ErrSourceNotRunning) {
			t.Fatalf("Switch = %v, want ErrSourceNotRunning", err)
		}
		time.Sleep(100 * time.Millisecond)
		if d := e.diag(t); d.App != "zoom.exe" || d.TargetPID != before.TargetPID || !d.Running || d.SourceSwitches != 0 ||
			e.launcher.count() != 1 {
			t.Fatalf("diagnostics %+v, %d children; want the zoom capture untouched", d, e.launcher.count())
		}
		if err := stop(t, e); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("during the stop", func(t *testing.T) {
		// The deaf child ignores its closed stdin, so the stop lasts the grace period.
		e, _ := newAppEnv(t, rebindConfig(), newFakeFinder(zoom1, comet), testenv.RecorderDeaf)
		if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
			t.Fatal(err)
		}
		stopped := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			stopped <- e.rec.Stop(ctx, "rec1")
		}()
		time.Sleep(200 * time.Millisecond)
		err := switchTo(t, e, cometSource(t))
		if !errors.Is(err, audiocapture.ErrSwitchRefused) && !errors.Is(err, audiocapture.ErrNotCapturing) {
			t.Fatalf("Switch during the stop = %v, want refused", err)
		}
		<-stopped
		if e.launcher.count() != 1 {
			t.Fatalf("%d children, want no child for the refused switch", e.launcher.count())
		}
	})
	t.Run("after the end", func(t *testing.T) {
		e, _ := newAppEnv(t, rebindConfig(), newFakeFinder(zoom1, comet), testenv.RecorderTone)
		if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
			t.Fatal(err)
		}
		if err := stop(t, e); err != nil {
			t.Fatal(err)
		}
		if err := switchTo(t, e, cometSource(t)); !errors.Is(err, audiocapture.ErrNotCapturing) {
			t.Fatalf("Switch after the end = %v, want ErrNotCapturing", err)
		}
		if err := e.rec.Switch(context.Background(), "unknown", values.WholeComputer()); !errors.Is(err, audiocapture.ErrNotCapturing) {
			t.Fatalf("Switch of an unknown recording = %v", err)
		}
	})
}

// The child of the new source does not start: the capture goes back to the old source, without a restart.
func TestRecorderSwitchGoesBackWhenTheNewSourceFails(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	cfg := rebindConfig()
	cfg.MaxRestarts = 0
	e, _ := newAppEnv(t, cfg, newFakeFinder(zoom1, comet), testenv.RecorderTone, testenv.RecorderUnavailable, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	err := switchTo(t, e, cometSource(t))
	if !errors.Is(err, audiocapture.ErrSwitchFailed) {
		t.Fatalf("Switch = %v, want ErrSwitchFailed", err)
	}
	e.waitDiag(t, "zoom is captured again", func(d audiocapture.Diagnostics) bool {
		return d.Running && d.App == "zoom.exe" && d.TargetPID == zoom1.PID
	})
	if d := e.diag(t); d.SourceSwitches != 0 || d.Restarts != 0 || e.launcher.count() != 3 {
		t.Fatalf("diagnostics %+v, %d children", d, e.launcher.count())
	}
	e.waitDiag(t, "a chunk closes", func(d audiocapture.Diagnostics) bool { return d.Chunks >= 1 })
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
	if err := e.waitEnded(t); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	checkChunksNumberedOn(t, e)
}

// The chosen app was closed and the capture waits for it: a switch captures the app that plays.
func TestRecorderSwitchWhileWaitingForTheApp(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	finder := newFakeFinder(zoom1, comet)
	e, _ := newAppEnv(t, rebindConfig(), finder, testenv.RecorderTone)
	if err := e.rec.Start(context.Background(), "rec1", e.chunks, zoomSource(t)); err != nil {
		t.Fatal(err)
	}
	finder.set(comet)
	e.waitDiag(t, "the capture waits for zoom", func(d audiocapture.Diagnostics) bool { return d.WaitingForApp })
	if err := switchTo(t, e, cometSource(t)); err != nil {
		t.Fatalf("Switch = %v", err)
	}
	if d := e.diag(t); d.App != "comet.exe" || d.WaitingForApp || !d.Running {
		t.Fatalf("diagnostics %+v", d)
	}
	if err := stop(t, e); err != nil {
		t.Fatal(err)
	}
}
