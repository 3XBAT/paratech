package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/cases/jobrunner"
	"paratech/desktop/internal/cases/recordingsession"
)

// The parts of the core the lifecycle starts and stops; narrow so that the order is tested on fakes.
type (
	runnerLife interface {
		Recover(ctx context.Context) (jobrunner.RecoveryReport, error)
		Start(ctx context.Context) error
		Stop(ctx context.Context) error
	}
	recordingLife interface {
		Recover(ctx context.Context) ([]recordingsession.Info, error)
		Close(ctx context.Context) error
	}
	deliveryLife interface {
		Recover(ctx context.Context) (int, error)
		Resume(ctx context.Context) error
	}
	transcriptionLife interface {
		EnqueuePending(ctx context.Context) (int, error)
	}
)

// loop is a background loop that runs until its context ends.
type loop struct {
	name string
	run  func(ctx context.Context) error
}

// closer releases one resource at the end of the shutdown.
type closer struct {
	name  string
	close func() error
}

// lifecycle starts the background work of an opened core and stops it again. It runs only after the single-instance
// guard is held (openCore), so a RUNNING job or an unfinished recording it finds belongs to no live process.
type lifecycle struct {
	log        *slog.Logger
	runner     runnerLife
	recordings recordingLife
	deliveries deliveryLife
	// transcriptions enqueues what a crash between a COMPLETED save and its listener call lost.
	transcriptions transcriptionLife
	// calendar asks for a calendar sync when signed in (nil: none); it only logs.
	calendar    func(ctx context.Context)
	cleanups    []cleanup
	loops       []loop   // started last, stopped first
	closers     []closer // released last, in this order
	stopTimeout time.Duration

	bg sync.WaitGroup // the background recording recovery

	stopLoops context.CancelFunc // set when the loops started
	looping   sync.WaitGroup

	stopOnce sync.Once
	stopErr  error
}

// newLifecycle is the lifecycle of an opened core; the closers are released last, in order, after the work of the
// model manager stopped (it writes through the storage) and a waiting Google sign-in was cancelled.
func (c *core) newLifecycle(log *slog.Logger, stopTimeout time.Duration, closers ...closer) *lifecycle {
	if c.googleSignIn != nil {
		// A sign-in waiting for the browser would keep its loopback listener until the sign-in timeout.
		closers = append([]closer{{name: "google sign-in", close: func() error {
			c.googleSignIn.CancelSignIn()
			return nil
		}}}, closers...)
	}
	if c.manager != nil {
		closers = append([]closer{{name: "model manager", close: c.manager.close}}, closers...)
	}
	if c.speech != nil {
		// An install writes through the component folder and the storage.
		closers = append([]closer{{name: "speech component install", close: func() error { return c.closeSpeech(stopTimeout) }}},
			closers...)
	}
	return &lifecycle{
		log: log, runner: c.runner, recordings: c.recordings, deliveries: c.deliveries, transcriptions: c.transcriptions,
		calendar: func(ctx context.Context) { c.requestCalendarSync(ctx, log, "start") },
		cleanups: c.cleanups, loops: c.loops(), closers: closers, stopTimeout: stopTimeout,
	}
}

// start runs the startup in this order: the cleanups of a crashed run, the job recovery, the recording recovery in the
// background (assembling long recordings must not hold up the jobs), the delivery recovery and resumption, the sweep
// of completed recordings without a transcription job, the calendar sync request, the workers, then the loops
// (scheduler and recording prompt;
// the prompt starts no recording before the recovery marked the interrupted ones). A failed job recovery or worker
// start stops what started and returns the error; the other steps only log. Cancelling ctx stops the startup, never
// the started work: shutdown stops that.
func (l *lifecycle) start(ctx context.Context) error {
	for _, c := range l.cleanups {
		n, err := c.run(ctx)
		switch {
		case err != nil:
			l.log.Error("startup cleanup failed", "step", c.name, "err", err)
		case n > 0:
			l.log.Info("startup cleanup", "step", c.name, "removed", n)
		}
	}

	rep, err := l.runner.Recover(ctx)
	if err != nil {
		l.unwind()
		return fmt.Errorf("app: job recovery: %w", err)
	}
	for _, e := range rep.Errors {
		l.log.Warn("stale job left for the next start", "job", e.Job.String(), "type", string(e.Type), "err", e.Err)
	}

	// Its own context: Close bounds and ends it through recordings.Close.
	life := context.WithoutCancel(ctx)
	l.bg.Add(1)
	go func() {
		defer l.bg.Done()
		infos, err := l.recordings.Recover(life)
		switch {
		case errors.Is(err, recordingsession.ErrClosed):
			l.log.Info("recording recovery skipped: shutting down")
		case err != nil:
			l.log.Error("recording recovery incomplete; the next start tries again", "err", err)
		default:
			l.log.Info("recording recovery finished", "recordings", len(infos))
		}
	}()

	if n, err := l.deliveries.Recover(ctx); err != nil {
		l.log.Error("delivery recovery failed", "err", err)
	} else if n > 0 {
		l.log.Warn("interrupted email deliveries have an unknown outcome", "deliveries", n)
	}
	if err := l.deliveries.Resume(ctx); err != nil {
		l.log.Error("resuming email deliveries failed", "err", err)
	}
	// Idempotent by the job key, so racing the recording recovery's own completions creates nothing twice.
	if n, err := l.transcriptions.EnqueuePending(ctx); err != nil {
		l.log.Error("enqueueing the transcription of completed recordings failed", "enqueued", n, "err", err)
	} else if n > 0 {
		l.log.Info("transcription jobs enqueued for completed recordings", "jobs", n)
	}
	// After the job recovery: a requeued sync job covers the request, so no second one is made.
	if l.calendar != nil {
		l.calendar(ctx)
	}

	if err := ctx.Err(); err != nil {
		l.unwind()
		return err
	}
	// Not ctx: cancelling the Start context is a forced stop of the workers, and shutdown stops them gracefully.
	if err := l.runner.Start(life); err != nil {
		l.unwind()
		return fmt.Errorf("app: start the job runner: %w", err)
	}
	l.startLoops()
	l.log.Info("background work started")
	return nil
}

// startLoops runs each loop on its own goroutine until stopWork.
func (l *lifecycle) startLoops() {
	if len(l.loops) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.stopLoops = cancel
	for _, lp := range l.loops {
		l.looping.Go(func() {
			if err := lp.run(ctx); err != nil && ctx.Err() == nil {
				l.log.Error("background loop ended", "loop", lp.name, "err", err)
			}
		})
	}
}

// endLoops cancels the loops and waits for them within stopTimeout: a loop may hang in a window command when the UI
// shell is already gone, and that must not block the shutdown.
func (l *lifecycle) endLoops() error {
	if l.stopLoops == nil {
		return nil
	}
	l.stopLoops()
	done := make(chan struct{})
	go func() {
		l.looping.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(l.stopTimeout):
		return errors.New("app: background loops did not stop in time")
	}
}

// unwind stops what start launched after a failed step; the storage stays open for the UI until shutdown.
func (l *lifecycle) unwind() {
	if err := l.stopWork(); err != nil {
		l.log.Error("stopping after a failed start", "err", err)
	}
}

// stopWork stops the loops, the job runner, then the recording session, each within stopTimeout, and waits for the
// background recovery. The stops are safe to repeat and before a start.
func (l *lifecycle) stopWork() error {
	loopErr := l.endLoops()

	ctx, cancel := context.WithTimeout(context.Background(), l.stopTimeout)
	runErr := l.runner.Stop(ctx)
	cancel()

	ctx, cancel = context.WithTimeout(context.Background(), l.stopTimeout)
	recErr := l.recordings.Close(ctx)
	cancel()

	l.bg.Wait()
	var errs []error
	if loopErr != nil {
		errs = append(errs, loopErr)
	}
	if runErr != nil {
		errs = append(errs, fmt.Errorf("app: stop the job runner: %w", runErr))
	}
	if recErr != nil {
		errs = append(errs, fmt.Errorf("app: close the recordings: %w", recErr))
	}
	return errors.Join(errs...)
}

// shutdown stops the work in reverse order of the start and then releases the closers. It is safe without a start and
// returns the same result when repeated.
func (l *lifecycle) shutdown() error {
	l.stopOnce.Do(func() {
		errs := []error{l.stopWork()}
		for _, c := range l.closers {
			if err := c.close(); err != nil {
				errs = append(errs, fmt.Errorf("app: release the %s: %w", c.name, err))
			}
		}
		l.stopErr = errors.Join(errs...)
	})
	return l.stopErr
}
