package recordingsession

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names used in errors.
const (
	opStart   = "start recording"
	opStop    = "stop recording"
	opGet     = "get recording"
	opDelete  = "delete recording"
	opRecover = "recover recordings"
	opSwitch  = "switch the recording source"
	opDismiss = "dismiss the source advice"
)

// Problem texts stored on a recording; an error text may follow after a colon.
const (
	problemStartAborted   = "the start was cancelled before the capture ran"
	problemStartFailed    = "the capture could not start"
	problemCaptureEnded   = "the capture ended unexpectedly"
	problemFinalizeFailed = "the recording could not be assembled"
)

// Retries of a save that starts or ends a recording: a transient storage error is retried with a doubling delay on
// the Sleeper, so the stored state does not wait for the recovery after a restart.
const (
	saveAttempts  = 4
	saveRetryBase = 200 * time.Millisecond
)

// Retries of the assembly of the final track: a transient error (a locked file, a busy disk) is retried with a
// doubling delay before the recording is FAILED.
const (
	finalizeAttempts  = 3
	finalizeRetryBase = time.Second
)

// adviceListTimeout bounds one listing of the apps for the silent-source advice.
const adviceListTimeout = 3 * time.Second

// detachedTimeout bounds, on the Sleeper's time, a call that outlives its caller or a shutdown (a save of the state,
// a stop of the capture), so that a hung port cannot block Close.
const detachedTimeout = 10 * time.Second

// Service records classes (PRD section 6): Start and Stop from the popup or the UI (AC-03), chunked capture through
// the Recorder, finalization at the hard cap (AC-04) and deletion with the transcripts (ADR-0004(a)).
//
// One recording is in progress at a time. Its session owns the aggregate from Start until a final status: it is the
// only writer, so a version conflict means someone else changed the recording, and the session then stops capturing
// and writing. An intermediate save that fails otherwise is logged and the session goes on: the next save carries
// the full state, and a recording whose last save never succeeded is found by Recover after a restart. The heartbeat
// of a running capture is written by the same session goroutine (a case of the follow loop), never by a second
// writer, which would make the saves fail with version conflicts. A recording saved COMPLETED with a final track, by a
// session or by Recover, is handed to the CompletionListener.
type Service struct {
	recordings  RecordingRepository
	transcripts RecordingTranscripts
	recorder    Recorder
	artifacts   ArtifactStore
	tx          Tx
	clock       Clock
	sleeper     Sleeper
	ids         IDGenerator
	completions CompletionListener
	apps        AudioApps      // nil: no silent-source advice
	advice      AdviceListener // nil: nobody is told
	log         *slog.Logger

	policy      limits.Policy
	trackExt    string
	heartbeat   time.Duration
	adviceEvery time.Duration

	life     context.Context // parent of the session work; ends on a forced Close
	shutdown context.CancelFunc

	mu       sync.Mutex
	active   *session
	recovery *recoveryRun // the running Recover; nil if none
	closed   bool
	wg       sync.WaitGroup // one per session, from the reservation in Start until it ends, and one per Recover

	marked     chan struct{} // closed when the first Recover has passed its marking pass (RecoveryMarked)
	markedOnce sync.Once
}

// New validates the configuration and builds the Service.
func New(deps Deps, cfg Config) (*Service, error) {
	if deps.Recordings == nil || deps.Transcripts == nil || deps.Recorder == nil || deps.Artifacts == nil ||
		deps.Tx == nil || deps.Clock == nil || deps.Sleeper == nil || deps.IDs == nil || deps.Completions == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}

	policy, err := limits.New(cfg.MaxDuration)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	if err := recording.ValidateTrackExtension(cfg.TrackExtension); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if cfg.HeartbeatInterval <= 0 {
		return nil, fmt.Errorf("%w: heartbeat interval %v", ErrInvalidConfig, cfg.HeartbeatInterval)
	}
	switch {
	case cfg.AdviceInterval < 0:
		return nil, fmt.Errorf("%w: advice interval %v", ErrInvalidConfig, cfg.AdviceInterval)
	case cfg.AdviceInterval == 0:
		cfg.AdviceInterval = DefaultAdviceInterval
	}

	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	s := &Service{
		recordings: deps.Recordings, transcripts: deps.Transcripts, recorder: deps.Recorder, artifacts: deps.Artifacts,
		tx: deps.Tx, clock: deps.Clock, sleeper: deps.Sleeper, ids: deps.IDs, completions: deps.Completions,
		apps: deps.Apps, advice: deps.Advice, log: log,
		policy: policy, trackExt: cfg.TrackExtension, heartbeat: cfg.HeartbeatInterval, adviceEvery: cfg.AdviceInterval,
		marked: make(chan struct{}),
	}
	s.life, s.shutdown = context.WithCancel(context.Background())
	return s, nil
}

// session is the recording in progress.
type session struct {
	id shared.RecordingID

	stopOnce sync.Once
	stopCh   chan struct{} // closed when a stop is requested
	done     chan struct{} // closed when the session ended; info and err are final then

	switching sync.Mutex // one switch of the source at a time

	mu          sync.Mutex
	info        Info
	err         error                   // why the session ended without a final status
	cancelStart context.CancelCauseFunc // set while the capture starts
	watch       values.SourceWatch      // the silent-source advice of the captured source (TASK-082)
	watchGen    uint64                  // bumped whenever watch is replaced for another source
	advice      values.SourceAdvice     // the advice last published; zero when none
}

func (ss *session) requestStop() {
	ss.stopOnce.Do(func() { close(ss.stopCh) })

	ss.mu.Lock()
	cancel := ss.cancelStart
	ss.mu.Unlock()
	if cancel != nil {
		cancel(errStopRequested)
	}
}

func (ss *session) stopRequested() bool {
	select {
	case <-ss.stopCh:
		return true
	default:
		return false
	}
}

func (ss *session) publish(r *recording.Recording) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.info = infoOf(r)
}

func (ss *session) snapshot() (Info, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	info := ss.info
	info.Source, info.Advice = ss.watch.Source(), ss.advice
	return info, ss.err
}

// updateWatch applies f, under the lock of the session, to the watch of the source and returns the advice to show, and
// whether it changed.
func (ss *session) updateWatch(f func(values.SourceWatch) values.SourceWatch) (values.SourceAdvice, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.watch = f(ss.watch)
	a, _ := ss.watch.Advice()
	if a == ss.advice {
		return a, false
	}
	ss.advice = a
	return a, true
}

func (ss *session) source() values.Source {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.watch.Source()
}

// watching returns the watched source and its generation, for an observation to be taken.
func (ss *session) watching() (values.Source, uint64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.watch.Source(), ss.watchGen
}

// observe feeds o, taken while the watch had generation gen, to the watch. An observation of a source that has been
// switched away since is dropped: it would count the old source's silence against the new one.
func (ss *session) observe(gen uint64, o values.SourceObservation) (values.SourceAdvice, bool) {
	return ss.updateWatch(func(w values.SourceWatch) values.SourceWatch {
		if ss.watchGen != gen {
			return w
		}
		return w.Observe(o)
	})
}

// rewatch starts a new watch of source, a new generation.
func (ss *session) rewatch(source values.Source) (values.SourceAdvice, bool) {
	return ss.updateWatch(func(values.SourceWatch) values.SourceWatch {
		ss.watchGen++
		return values.NewSourceWatch(source)
	})
}

// Start creates a recording and starts the capture of req.Source into recordings/<id>. It returns once the capture
// runs; the session then goes on until Stop, the hard cap or the end of the capture. Only one recording is in
// progress at a time (limits.ErrRecordingInProgress). A failed or aborted start leaves a FAILED recording and returns
// its Info with ErrStartFailed or ErrStartAborted; a chosen app that does not run wraps recording.ErrSourceNotRunning.
func (s *Service) Start(ctx context.Context, req StartRequest) (Info, error) {
	rec, err := recording.New(recording.Spec{
		ID: s.ids.NewRecordingID(), LessonID: req.LessonID, MaxDuration: s.policy.MaxDuration(),
	}, s.clock.Now())
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", opStart, err)
	}

	sess, err := s.reserve(rec, req.Source)
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", opStart, err)
	}

	if err := s.recordings.Save(ctx, rec); err != nil {
		s.finish(sess, rec, err)
		return Info{}, fmt.Errorf("%s %s: %w", opStart, rec.ID(), err)
	}

	// From here the capture may run, so the state is saved even if the caller gives up (detached saves).
	if serr := s.startCapture(ctx, sess, rec, req.Source); serr != nil {
		aborted := ctx.Err() != nil || s.life.Err() != nil || sess.stopRequested()
		problem, kind := problemStartFailed+": "+serr.Error(), ErrStartFailed
		if aborted {
			problem, kind = problemStartAborted, ErrStartAborted
		}

		if err := rec.Fail(s.clock.Now(), problem); err != nil {
			s.finish(sess, rec, err)
			return infoOf(rec), fmt.Errorf("%s %s: %w", opStart, rec.ID(), err)
		}
		err := s.persistFinal(ctx, sess, rec)
		s.finish(sess, rec, err)
		return infoOf(rec), fmt.Errorf("%s %s: %w: %w", opStart, rec.ID(), kind, serr)
	}

	if err := rec.Begin(s.clock.Now()); err != nil {
		s.haltCapture(ctx, rec.ID())
		s.finish(sess, rec, err)
		return infoOf(rec), fmt.Errorf("%s %s: %w", opStart, rec.ID(), err)
	}

	// A capture whose RECORDING state is not stored would look like a failed start to the recovery: retry, then give up.
	sess.publish(rec)
	if err := s.saveRetrying(ctx, rec); err != nil {
		s.log.Error("saving the started recording failed; the capture is stopped",
			slog.String("recording", rec.ID().String()), slog.Any("error", err))
		s.haltCapture(ctx, rec.ID())
		s.finish(sess, rec, err)
		return infoOf(rec), fmt.Errorf("%s %s: %w: %w", opStart, rec.ID(), ErrAbandoned, err)
	}

	info := infoOf(rec) // the session goroutine owns rec from here
	go s.run(sess, rec)
	return info, nil
}

// reserve takes the single slot of a recording in progress.
func (s *Service) reserve(rec *recording.Recording, source values.Source) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.recovery != nil && s.recovery.marking {
		return nil, ErrRecovering
	}

	inProgress := 0
	if s.active != nil {
		inProgress = 1
	}
	if err := s.policy.AllowStart(inProgress); err != nil {
		return nil, fmt.Errorf("%w: %s", err, s.active.id)
	}

	sess := &session{
		id: rec.ID(), stopCh: make(chan struct{}), done: make(chan struct{}), info: infoOf(rec),
		watch: values.NewSourceWatch(source),
	}
	s.active = sess
	s.wg.Add(1)
	return sess, nil
}

// finish publishes the last state, frees the slot and wakes the waiters of the session. An advice shown is withdrawn.
func (s *Service) finish(sess *session, rec *recording.Recording, err error) {
	sess.mu.Lock()
	sess.info, sess.err = infoOf(rec), err
	shown := !sess.advice.IsZero()
	sess.advice = values.SourceAdvice{}
	sess.mu.Unlock()
	if shown {
		s.tellAdvice(rec.ID(), values.SourceAdvice{})
	}

	s.mu.Lock()
	if s.active == sess {
		s.active = nil
	}
	s.mu.Unlock()

	close(sess.done)
	s.wg.Done()
}

// startCapture runs Recorder.Start under a context that a stop, the caller or a forced shutdown cancels.
func (s *Service) startCapture(ctx context.Context, sess *session, rec *recording.Recording, source values.Source) error {
	startCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	unlink := context.AfterFunc(s.life, func() { cancel(errShutdown) })
	defer unlink()

	// Register first, then look: a stop that came earlier is seen here, a later one finds the cancel function.
	sess.mu.Lock()
	sess.cancelStart = cancel
	sess.mu.Unlock()
	if sess.stopRequested() {
		cancel(errStopRequested)
	}

	err := s.recorder.Start(startCtx, rec.ID(), rec.ChunksDir(), source)

	sess.mu.Lock()
	sess.cancelStart = nil
	sess.mu.Unlock()
	return err
}

// run follows a running capture until it ends and the recording is finalized.
func (s *Service) run(sess *session, rec *recording.Recording) {
	ctx := s.life
	watchCtx, stopWatching := context.WithCancel(ctx)
	var bg sync.WaitGroup

	ended := make(chan error, 1)
	bg.Go(func() { ended <- s.recorder.Wait(watchCtx, rec.ID()) })

	// The advice ends with the capture, before it is stopped and assembled (quiet).
	adviceCtx, stopAdvice := context.WithCancel(watchCtx)
	defer stopAdvice()
	advised := make(chan struct{})
	if s.apps != nil {
		bg.Go(func() {
			defer close(advised)
			s.advise(adviceCtx, sess)
		})
	} else {
		close(advised)
	}
	quiet := func() {
		stopAdvice()
		<-advised
	}

	deadline := rec.Deadline()
	beat := make(chan struct{}, 1)
	arm := func() <-chan struct{} {
		reached := make(chan struct{})
		bg.Go(func() { s.watch(watchCtx, deadline, reached, beat) })
		return reached
	}

	err := s.follow(ctx, sess, rec, ended, beat, arm, quiet)
	stopWatching()
	bg.Wait()
	s.finish(sess, rec, err)
}

// watch closes reached once the clock shows the deadline and until then offers a tick on beat every heartbeat
// interval. A tick the session has not taken yet is not doubled. One sleeper serves both, so the session waits on a
// single timer.
func (s *Service) watch(ctx context.Context, deadline time.Time, reached chan<- struct{}, beat chan<- struct{}) {
	next := s.clock.Now().Add(s.heartbeat)
	for {
		now := s.clock.Now()
		if !now.Before(deadline) {
			close(reached)
			return
		}
		switch {
		case !now.Before(next):
			select {
			case beat <- struct{}{}:
			default:
			}
			next = now.Add(s.heartbeat)
		case next.Sub(now) > s.heartbeat:
			next = now.Add(s.heartbeat) // the wall clock went back
		}
		if s.sleeper.Sleep(ctx, min(deadline.Sub(now), next.Sub(now))) != nil {
			return
		}
	}
}

// follow waits for what ends the capture and finalizes the recording. It returns nil when the recording reached a
// final status that was saved, otherwise why not. quiet ends the silent-source advice and waits until it has.
func (s *Service) follow(ctx context.Context, sess *session, rec *recording.Recording, ended <-chan error,
	beat <-chan struct{}, arm func() <-chan struct{}, quiet func()) error {
	atLimit := arm()
	var noted bool // the capture fallback is on the recording
	for {
		select {
		case <-beat:
			if sess.stopRequested() {
				continue // the pending stop goes first
			}
			s.noteFallback(ctx, rec, &noted) // saved with the heartbeat
			if err := rec.Heartbeat(s.clock.Now()); err != nil {
				s.haltCapture(ctx, rec.ID())
				return err
			}
			if err := s.persist(ctx, sess, rec); err != nil {
				s.haltCapture(ctx, rec.ID())
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}

		case <-sess.stopCh:
			quiet()
			s.noteFallback(ctx, rec, &noted) // a fallback since the last heartbeat
			if err := rec.Stop(s.clock.Now()); err != nil {
				s.haltCapture(ctx, rec.ID())
				return err
			}
			if err := s.persist(ctx, sess, rec); err != nil {
				s.haltCapture(ctx, rec.ID())
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			if !s.endCapture(ctx, rec.ID(), ended) {
				return ErrAbandoned
			}
			if err := rec.Finalize(s.clock.Now()); err != nil {
				return err
			}
			if err := s.persist(ctx, sess, rec); err != nil {
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			return s.assemble(ctx, sess, rec)

		case <-atLimit:
			s.noteFallback(ctx, rec, &noted)
			err := rec.ReachLimit(s.clock.Now())
			if errors.Is(err, recording.ErrLimitNotReached) {
				atLimit = arm() // the wall clock went back; wait again
				continue
			}
			if err != nil {
				s.haltCapture(ctx, rec.ID())
				return err
			}
			quiet()
			s.log.Info("recording reached its hard cap", slog.String("recording", rec.ID().String()))
			if err := s.persist(ctx, sess, rec); err != nil {
				s.haltCapture(ctx, rec.ID())
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			if !s.endCapture(ctx, rec.ID(), ended) {
				return ErrAbandoned
			}
			return s.assemble(ctx, sess, rec)

		case cerr := <-ended:
			if ctx.Err() != nil {
				s.haltCapture(ctx, rec.ID())
				return ErrAbandoned
			}
			quiet()
			problem := problemCaptureEnded
			if cerr != nil {
				problem += ": " + cerr.Error()
			}
			s.log.Warn("capture ended by itself", slog.String("recording", rec.ID().String()), slog.Any("error", cerr))
			s.noteFallback(ctx, rec, &noted) // the recorder keeps the diagnostics of an ended capture
			if err := rec.Interrupt(s.clock.Now(), problem); err != nil {
				return err
			}
			if err := s.persist(ctx, sess, rec); err != nil {
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			return s.assemble(ctx, sess, rec)

		case <-ctx.Done():
			s.haltCapture(ctx, rec.ID())
			return ErrAbandoned
		}
	}
}

// noteFallback puts on a RECORDING the note of a capture that fell back to classic loopback (ADR-0002 Decision 3),
// once per recording: the fallback is one-way. The caller saves the recording with its next change. A note the
// aggregate refuses is logged and not tried again.
func (s *Service) noteFallback(ctx context.Context, rec *recording.Recording, noted *bool) {
	if *noted {
		return
	}
	d, ok := s.recorder.Diagnostics(ctx, rec.ID())
	if !ok {
		return
	}
	sw, ok := d.Fallback()
	if !ok {
		return
	}
	*noted = true
	if err := rec.NoteCaptureFallback(s.clock.Now(), sw); err != nil {
		s.log.Warn("the capture fallback could not be noted on the recording", slog.String("recording", rec.ID().String()),
			slog.Any("error", err))
		return
	}
	s.log.Info("capture fell back to the system output; noted on the recording", slog.String("recording", rec.ID().String()),
		slog.String("from", string(sw.From)), slog.String("cause", string(sw.Cause)))
}

// detached returns a context that keeps the values of ctx but not its cancellation and ends after detachedTimeout
// on the Sleeper's time. release must be called; it also waits for the timer goroutine.
func (s *Service) detached(ctx context.Context) (context.Context, func()) {
	dctx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	timer := make(chan struct{})
	go func() {
		defer close(timer)
		if s.sleeper.Sleep(dctx, detachedTimeout) == nil {
			cancel(errDetachedTimeout)
		}
	}()

	release := func() {
		cancel(nil)
		<-timer
	}
	return dctx, release
}

// endCapture stops the capture and waits until it has ended. It reports false if the service shut down meanwhile.
// When Stop fails or the capture does not end in time it may hang, so it is not waited for: the chunks on disk are
// assembled.
func (s *Service) endCapture(ctx context.Context, id shared.RecordingID, ended <-chan error) bool {
	dctx, release := s.detached(ctx)
	defer release()

	if err := s.recorder.Stop(dctx, id); err != nil {
		s.log.Warn("stopping the capture failed", slog.String("recording", id.String()), slog.Any("error", err))
		return ctx.Err() == nil
	}

	select {
	case err := <-ended:
		if err != nil {
			s.log.Warn("capture ended with an error", slog.String("recording", id.String()), slog.Any("error", err))
		}
		return ctx.Err() == nil
	case <-dctx.Done():
		s.log.Warn("capture did not end after the stop", slog.String("recording", id.String()))
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// haltCapture releases the audio device of a session that will not finalize.
func (s *Service) haltCapture(ctx context.Context, id shared.RecordingID) {
	dctx, release := s.detached(ctx)
	defer release()

	if err := s.recorder.Stop(dctx, id); err != nil {
		s.log.Warn("stopping the capture failed", slog.String("recording", id.String()), slog.Any("error", err))
	}
}

// assemble turns the chunks of a FINALIZING recording into the final track and saves COMPLETED or FAILED. The chunks
// stay on disk either way. A shutdown during the assembly leaves FINALIZING for the recovery.
func (s *Service) assemble(ctx context.Context, sess *session, rec *recording.Recording) error {
	attempts := finalizeAttempts
	if n, err := s.countChunks(ctx, rec.ChunksDir()); err != nil {
		s.log.Warn("listing the chunks failed", slog.String("recording", rec.ID().String()), slog.Any("error", err))
	} else if err := rec.ObserveChunks(n, s.clock.Now()); err != nil {
		return err
	} else if n == 0 {
		attempts = 1 // nothing to assemble is not a transient error
	}

	track, err := rec.TrackRef(s.trackExt)
	if err != nil {
		return err
	}
	assembled, ferr := s.finalizeTrack(ctx, rec.ID(), rec.ChunksDir(), track, attempts)
	if ferr != nil && ctx.Err() != nil {
		return ErrAbandoned
	}

	now := s.clock.Now()
	if ferr == nil {
		ferr = rec.CompleteTrack(now, track, assembled)
	}
	if ferr == nil {
		s.logSilent(rec, assembled)
	}
	if ferr != nil {
		s.log.Warn("assembling the recording failed", slog.String("recording", rec.ID().String()), slog.Any("error", ferr))
		if err := rec.Fail(now, problemFinalizeFailed+": "+ferr.Error()); err != nil {
			return err
		}
	}
	if err := s.persistFinal(ctx, sess, rec); err != nil {
		return err
	}
	s.announceCompleted(ctx, rec)
	return nil
}

// logSilent tells that a recording is silent and is not transcribed (TASK-082).
func (s *Service) logSilent(rec *recording.Recording, track values.AssembledTrack) {
	if rec.Silent() {
		s.log.Warn("the recording is silent; it is not transcribed", slog.String("recording", rec.ID().String()),
			slog.Int("seconds", track.Seconds), slog.Int("audible_seconds", track.AudibleSeconds))
	}
}

// announceCompleted tells the listener about a saved recording that can be transcribed (the aggregate decides). It
// runs detached, so a shutdown does not cut the listener's transaction in half; its error is only logged.
func (s *Service) announceCompleted(ctx context.Context, rec *recording.Recording) {
	if _, ok := rec.TranscribableAudio(); !ok {
		return
	}
	dctx, release := s.detached(ctx)
	defer release()

	if err := s.completions.RecordingCompleted(dctx, rec.ID()); err != nil {
		s.log.Warn("handing over the completed recording failed; the next start tries again",
			slog.String("recording", rec.ID().String()), slog.Any("error", err))
	}
}

// finalizeTrack runs Recorder.Finalize up to attempts times with a doubling delay; a shutdown and missing audio are
// not retried.
func (s *Service) finalizeTrack(ctx context.Context, id shared.RecordingID, chunks, track shared.ArtifactRef, attempts int) (values.AssembledTrack, error) {
	delay := finalizeRetryBase
	for attempt := 1; ; attempt++ {
		d, err := s.recorder.Finalize(ctx, chunks, track)
		if err == nil || ctx.Err() != nil || attempt >= attempts || errors.Is(err, recording.ErrNoAudio) {
			return d, err
		}

		s.log.Warn("assembling the recording failed; retrying", slog.String("recording", id.String()),
			slog.Int("attempt", attempt), slog.Any("error", err))
		if serr := s.sleeper.Sleep(ctx, delay); serr != nil {
			return values.AssembledTrack{}, err
		}
		delay *= 2
	}
}

func (s *Service) countChunks(ctx context.Context, dir shared.ArtifactRef) (int, error) {
	chunks, err := s.artifacts.List(ctx, dir)
	if errors.Is(err, shared.ErrNotFound) {
		return 0, nil
	}
	return len(chunks), err
}

// lostOwnership reports a save error after which the session must stop writing: the recording was changed or
// removed elsewhere.
func lostOwnership(err error) bool {
	return errors.Is(err, shared.ErrConflict) || errors.Is(err, shared.ErrNotFound)
}

// save stores rec under a detached context, so neither the caller nor a shutdown cuts a write in half.
func (s *Service) save(ctx context.Context, rec *recording.Recording) error {
	dctx, release := s.detached(ctx)
	defer release()

	return s.recordings.Save(dctx, rec)
}

// saveRetrying saves rec, retrying errors other than a lost ownership a few times; it returns the last error. The
// version is unchanged after a failed save, so the same aggregate can be saved again.
func (s *Service) saveRetrying(ctx context.Context, rec *recording.Recording) error {
	delay := saveRetryBase
	for attempt := 1; ; attempt++ {
		err := s.save(ctx, rec)
		if err == nil || lostOwnership(err) || attempt == saveAttempts {
			return err
		}

		if s.sleeper.Sleep(s.life, delay) != nil {
			return err
		}
		delay *= 2
	}
}

// persist publishes and saves an intermediate state. Only a lost ownership is returned; other failures are logged,
// as the next save carries the full state.
func (s *Service) persist(ctx context.Context, sess *session, rec *recording.Recording) error {
	sess.publish(rec)
	err := s.save(ctx, rec)
	switch {
	case err == nil:
		return nil
	case lostOwnership(err):
		s.log.Warn("recording was changed elsewhere; the session stops", slog.String("recording", rec.ID().String()), slog.Any("error", err))
		return err
	}

	s.log.Error("saving the recording failed", slog.String("recording", rec.ID().String()),
		slog.String("status", rec.Status().String()), slog.Any("error", err))
	return nil
}

// persistFinal publishes and saves a final status with retries.
func (s *Service) persistFinal(ctx context.Context, sess *session, rec *recording.Recording) error {
	sess.publish(rec)
	if err := s.saveRetrying(ctx, rec); err != nil {
		s.log.Error("saving the final state failed; the recovery will find the recording",
			slog.String("recording", rec.ID().String()), slog.String("status", rec.Status().String()), slog.Any("error", err))
		return fmt.Errorf("%w: %w", ErrAbandoned, err)
	}

	s.log.Info("recording finished", slog.String("recording", rec.ID().String()), slog.String("status", rec.Status().String()))
	return nil
}

// Stop ends the recording id and waits until it is COMPLETED or FAILED, then returns its Info. A stop during the
// start aborts the start. Stopping a finished recording returns its Info again. An unfinished recording that no
// session of this process captures yields ErrNotRunning. If ctx ends first, Stop returns the current Info and the
// context error while the session goes on.
func (s *Service) Stop(ctx context.Context, id shared.RecordingID) (Info, error) {
	s.mu.Lock()
	sess := s.active
	s.mu.Unlock()

	if sess != nil && sess.id == id {
		sess.requestStop()
		select {
		case <-sess.done:
		case <-ctx.Done():
			info, _ := sess.snapshot()
			return info, ctx.Err()
		}
		info, err := sess.snapshot()
		if err != nil {
			return info, fmt.Errorf("%s %s: %w", opStop, id, err)
		}
		return info, nil
	}

	rec, err := s.recordings.Get(ctx, id)
	if err != nil {
		return Info{}, fmt.Errorf("%s %s: %w", opStop, id, err)
	}
	if !rec.Status().IsTerminal() {
		return infoOf(rec), fmt.Errorf("%s %s: %w", opStop, id, ErrNotRunning)
	}
	return infoOf(rec), nil
}

// Get returns the stored state of a recording.
func (s *Service) Get(ctx context.Context, id shared.RecordingID) (Info, error) {
	rec, err := s.recordings.Get(ctx, id)
	if err != nil {
		return Info{}, fmt.Errorf("%s %s: %w", opGet, id, err)
	}
	return infoOf(rec), nil
}

// Active returns the recording in progress, as last seen by its session.
func (s *Service) Active() (Info, bool) {
	s.mu.Lock()
	sess := s.active
	s.mu.Unlock()
	if sess == nil {
		return Info{}, false
	}
	info, _ := sess.snapshot()
	return info, true
}

// Delete removes a finished recording with its files and transcripts (PRD section 13.1) in the order of ADR-0004(a):
// the aggregate decides first, the files go next, then both rows in one transaction where the aggregate is asked
// again. A failure after the files were removed is repaired by repeating the request. A recording that the running
// Recover has not finished yet is refused with ErrRecovering, before the files and again in the transaction, so the
// recovery cannot write a track for a deleted row.
func (s *Service) Delete(ctx context.Context, id shared.RecordingID) error {
	rec, err := s.recordings.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	if err := rec.CheckDeletable(); err != nil {
		return fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	if err := s.checkNotRecovering(id); err != nil {
		return fmt.Errorf("%s %s: %w", opDelete, id, err)
	}

	files, err := s.transcripts.ListArtifactsByRecording(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	files = append(files, rec.ChunksDir())
	if !rec.Audio().IsZero() {
		files = append(files, rec.Audio())
	}
	// A track written just before a crash may exist without being recorded on the aggregate.
	if track, err := rec.TrackRef(s.trackExt); err == nil && track != rec.Audio() {
		files = append(files, track)
	}
	for _, f := range files {
		if err := s.artifacts.Delete(ctx, f); err != nil {
			return fmt.Errorf("%s %s: %w", opDelete, id, err)
		}
	}

	err = s.tx.Do(ctx, func(ctx context.Context) error {
		cur, err := s.recordings.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := cur.CheckDeletable(); err != nil {
			return err
		}
		if err := s.checkNotRecovering(id); err != nil {
			return err
		}
		if err := s.transcripts.DeleteByRecording(ctx, id); err != nil {
			return err
		}
		return s.recordings.Delete(ctx, cur)
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	return nil
}

// Close refuses new recordings and stops the one in progress like a user stop, so that it is finalized. If ctx ends
// first, the session is abandoned (its state stays for the recovery after a restart), Close still waits for it to
// return and reports ctx's error. A running Recover is waited for the same way. It is safe to call more than once.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.markingDone() // a Recover that never ran must not keep a waiter waiting: Start now fails with ErrClosed
	sess := s.active
	s.mu.Unlock()

	if sess != nil {
		sess.requestStop()
	}

	idle := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(idle)
	}()

	var err error
	select {
	case <-idle:
	case <-ctx.Done():
		err = ctx.Err()
		s.shutdown()
		<-idle
	}
	s.shutdown()
	return err
}
