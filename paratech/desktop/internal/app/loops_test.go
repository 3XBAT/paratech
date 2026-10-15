package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"paratech/desktop/testenv"
)

// blockingLoop records its start and end and runs until its context ends, or until release when it ignores the
// context (a window command that hangs after the shell is gone).
func blockingLoop(ev *events, name string, ignoreCtx bool, release <-chan struct{}) loop {
	return loop{name: name, run: func(ctx context.Context) error {
		ev.add(name + " started")
		if ignoreCtx {
			<-release
		} else {
			<-ctx.Done()
		}
		ev.add(name + " stopped")
		return ctx.Err()
	}}
}

// TASK-022: the scheduler and the prompt start after the workers and stop before them, ahead of the recordings.
func TestLoopsStartLastAndStopFirst(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newLifeFakes()
	f.life.loops = []loop{blockingLoop(f.ev, "scheduler", false, nil), blockingLoop(f.ev, "prompt", false, nil)}

	if err := f.life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "both loops run", func() bool {
		return slices.Contains(f.ev.all(), "scheduler started") && slices.Contains(f.ev.all(), "prompt started")
	}, nil)
	for _, name := range []string{"scheduler started", "prompt started"} {
		if f.ev.index(t, name) < f.ev.index(t, "runner.Start") {
			t.Fatalf("%s before the workers: %v", name, f.ev.all())
		}
	}

	if err := f.life.shutdown(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scheduler stopped", "prompt stopped"} {
		if f.ev.index(t, name) > f.ev.index(t, "runner.Stop") {
			t.Fatalf("%s after the workers stopped: %v", name, f.ev.all())
		}
	}
	if f.ev.index(t, "runner.Stop") > f.ev.index(t, "recordings.Close") {
		t.Fatalf("stop order %v", f.ev.all())
	}
}

// A failed start never launches the loops.
func TestLoopsDoNotStartAfterAFailedStart(t *testing.T) {
	testenv.NoGoroutineLeaks(t)
	f := newLifeFakes()
	f.runner.startErr = context.DeadlineExceeded
	f.life.loops = []loop{blockingLoop(f.ev, "scheduler", false, nil)}

	if err := f.life.start(context.Background()); err == nil {
		t.Fatal("start must fail")
	}
	if err := f.life.shutdown(); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(f.ev.all(), "scheduler started") {
		t.Fatalf("a loop started after the failed start: %v", f.ev.all())
	}
}

// A loop that hangs does not block the shutdown: it is reported, and the rest stops in order.
func TestHungLoopDoesNotBlockTheShutdown(t *testing.T) {
	f := newLifeFakes()
	f.life.stopTimeout = 50 * time.Millisecond
	release := make(chan struct{})
	f.life.loops = []loop{blockingLoop(f.ev, "prompt", true, release)}
	t.Cleanup(func() { close(release) })

	if err := f.life.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	testenv.Eventually(t, "the loop runs", func() bool { return slices.Contains(f.ev.all(), "prompt started") }, nil)
	if err := f.life.shutdown(); err == nil {
		t.Fatal("a hung loop must be reported")
	}
	for _, name := range []string{"runner.Stop", "recordings.Close", "close storage", "release lock"} {
		f.ev.index(t, name)
	}
}
