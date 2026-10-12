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
	"paratech/desktop/internal/domain/shared"
)

// Operation names used in errors.
const (
	opStart  = "start recording"
	opStop   = "stop recording"
	opGet    = "get recording"
	opDelete = "delete recording"
)

// Problem texts stored on a recording; an error text may follow after a colon.
const (
	problemStartAborted   = "the start was cancelled before the capture ran"
	problemStartFailed    = "the capture could not start"
	problemCaptureEnded   = "the capture ended unexpectedly"
	problemFinalizeFailed = "the recording could not be assembled"
)

// Bounds of the save of a final status: a transient storage error is retried with a doubling delay, so a finished
// recording does not wait for the recovery after a restart.
const (
	finalSaveAttempts  = 4
	finalSaveRetryBase = 200 * time.Millisecond
)

// Service records classes (PRD section 6): Start and Stop from the popup or the UI (AC-03), chunked capture through
// the Recorder, finalization at the hard cap (AC-04) and deletion with the transcripts (ADR-0004(a)).
//
// One recording is in progress at a time. Its session owns the aggregate from Start until a final status: it is the
// only writer, so a version conflict means someone else changed the recording, and the session then stops capturing
// and writing. A save that fails otherwise is logged and the session goes on: the next save carries the full state,
// and a recording whose last save never succeeded is found by the recovery after a restart (TASK-014).
type Service struct {
	recordings  RecordingRepository
	transcripts RecordingTranscripts
	recorder    Recorder
	artifacts   ArtifactStore
	tx          Tx
	clock       Clock
	sleeper     Sleeper
	ids         IDGenerator
	log         *slog.Logger

	policy   limits.Policy
	trackExt string

	life     context.Context // parent of the session work; ends on a forced Close
	shutdown context.CancelFunc

	mu     sync.Mutex
	active *session
	closed bool
	wg     sync.WaitGroup // one per session, from the reservation in Start until it ends
}

// New validates the configuration and builds the Service.
func New(deps Deps, cfg Config) (*Service, error) {
	if deps.Recordings == nil || deps.Transcripts == nil || deps.Recorder == nil || deps.Artifacts == nil ||
		deps.Tx == nil || deps.Clock == nil || deps.Sleeper == nil || deps.IDs == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}

	policy, err := limits.New(cfg.MaxDuration)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	if err := recording.ValidateTrackExtension(cfg.TrackExtension); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	log := deps.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	s := &Service{
		recordings: deps.Recordings, transcripts: deps.Transcripts, recorder: deps.Recorder, artifacts: deps.Artifacts,
		tx: deps.Tx, clock: deps.Clock, sleeper: deps.Sleeper, ids: deps.IDs, log: log,
		policy: policy, trackExt: cfg.TrackExtension,
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

	mu          sync.Mutex
	info        Info
	err         error                   // why the session ended without a final status
	cancelStart context.CancelCauseFunc // set while the capture starts
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
	return ss.info, ss.err
}

// Start creates a recording and starts the capture into recordings/<id>. It returns once the capture runs; the
// session then goes on until Stop, the hard cap or the end of the capture. Only one recording is in progress at a
// time (limits.ErrRecordingInProgress). A failed or aborted start leaves a FAILED recording and returns its Info
// with ErrStartFailed or ErrStartAborted.
func (s *Service) Start(ctx context.Context, req StartRequest) (Info, error) {
	rec, err := recording.New(recording.Spec{
		ID: s.ids.NewRecordingID(), LessonID: req.LessonID, MaxDuration: s.policy.MaxDuration(),
	}, s.clock.Now())
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", opStart, err)
	}

	sess, err := s.reserve(rec)
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", opStart, err)
	}

	if err := s.recordings.Save(ctx, rec); err != nil {
		s.finish(sess, rec, err)
		return Info{}, fmt.Errorf("%s %s: %w", opStart, rec.ID(), err)
	}

	// From here the capture may run, so the state is saved even if the caller gives up.
	keep := context.WithoutCancel(ctx)

	if serr := s.startCapture(ctx, sess, rec); serr != nil {
		aborted := ctx.Err() != nil || s.life.Err() != nil || sess.stopRequested()
		problem, kind := problemStartFailed+": "+serr.Error(), ErrStartFailed
		if aborted {
			problem, kind = problemStartAborted, ErrStartAborted
		}

		if err := rec.Fail(s.clock.Now(), problem); err != nil {
			s.finish(sess, rec, err)
			return infoOf(rec), fmt.Errorf("%s %s: %w", opStart, rec.ID(), err)
		}
		err := s.persistFinal(keep, sess, rec)
		s.finish(sess, rec, err)
		return infoOf(rec), fmt.Errorf("%s %s: %w: %w", opStart, rec.ID(), kind, serr)
	}

	if err := rec.Begin(s.clock.Now()); err != nil {
		s.haltCapture(keep, rec.ID())
		s.finish(sess, rec, err)
		return infoOf(rec), fmt.Errorf("%s %s: %w", opStart, rec.ID(), err)
	}
	if err := s.persist(keep, sess, rec); err != nil {
		s.haltCapture(keep, rec.ID())
		s.finish(sess, rec, err)
		return infoOf(rec), fmt.Errorf("%s %s: %w: %w", opStart, rec.ID(), ErrAbandoned, err)
	}

	info := infoOf(rec) // the session goroutine owns rec from here
	go s.run(sess, rec)
	return info, nil
}

// reserve takes the single slot of a recording in progress.
func (s *Service) reserve(rec *recording.Recording) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}

	inProgress := 0
	if s.active != nil {
		inProgress = 1
	}
	if err := s.policy.AllowStart(inProgress); err != nil {
		return nil, fmt.Errorf("%w: %s", err, s.active.id)
	}

	sess := &session{id: rec.ID(), stopCh: make(chan struct{}), done: make(chan struct{}), info: infoOf(rec)}
	s.active = sess
	s.wg.Add(1)
	return sess, nil
}

// finish publishes the last state, frees the slot and wakes the waiters of the session.
func (s *Service) finish(sess *session, rec *recording.Recording, err error) {
	sess.mu.Lock()
	sess.info, sess.err = infoOf(rec), err
	sess.mu.Unlock()

	s.mu.Lock()
	if s.active == sess {
		s.active = nil
	}
	s.mu.Unlock()

	close(sess.done)
	s.wg.Done()
}

// startCapture runs Recorder.Start under a context that a stop, the caller or a forced shutdown cancels.
func (s *Service) startCapture(ctx context.Context, sess *session, rec *recording.Recording) error {
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

	err := s.recorder.Start(startCtx, rec.ID(), rec.ChunksDir())

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

	deadline := rec.Deadline()
	arm := func() <-chan struct{} {
		reached := make(chan struct{})
		bg.Go(func() { s.awaitDeadline(watchCtx, deadline, reached) })
		return reached
	}

	err := s.follow(ctx, sess, rec, ended, arm)
	stopWatching()
	bg.Wait()
	s.finish(sess, rec, err)
}

// awaitDeadline closes reached once the clock shows the deadline.
func (s *Service) awaitDeadline(ctx context.Context, deadline time.Time, reached chan<- struct{}) {
	for {
		left := deadline.Sub(s.clock.Now())
		if left <= 0 {
			close(reached)
			return
		}
		if s.sleeper.Sleep(ctx, left) != nil {
			return
		}
	}
}

// follow waits for what ends the capture and finalizes the recording. It returns nil when the recording reached a
// final status that was saved, otherwise why not.
func (s *Service) follow(ctx context.Context, sess *session, rec *recording.Recording, ended <-chan error, arm func() <-chan struct{}) error {
	keep := context.WithoutCancel(ctx)
	atLimit := arm()
	for {
		select {
		case <-sess.stopCh:
			if err := rec.Stop(s.clock.Now()); err != nil {
				s.haltCapture(keep, rec.ID())
				return err
			}
			if err := s.persist(keep, sess, rec); err != nil {
				s.haltCapture(keep, rec.ID())
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			if !s.endCapture(ctx, rec.ID(), ended) {
				return ErrAbandoned
			}
			if err := rec.Finalize(s.clock.Now()); err != nil {
				return err
			}
			if err := s.persist(keep, sess, rec); err != nil {
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			return s.assemble(ctx, sess, rec)

		case <-atLimit:
			err := rec.ReachLimit(s.clock.Now())
			if errors.Is(err, recording.ErrLimitNotReached) {
				atLimit = arm() // the wall clock went back; wait again
				continue
			}
			if err != nil {
				s.haltCapture(keep, rec.ID())
				return err
			}
			s.log.Info("recording reached its hard cap", slog.String("recording", rec.ID().String()))
			if err := s.persist(keep, sess, rec); err != nil {
				s.haltCapture(keep, rec.ID())
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			if !s.endCapture(ctx, rec.ID(), ended) {
				return ErrAbandoned
			}
			return s.assemble(ctx, sess, rec)

		case cerr := <-ended:
			if ctx.Err() != nil {
				s.haltCapture(keep, rec.ID())
				return ErrAbandoned
			}
			problem := problemCaptureEnded
			if cerr != nil {
				problem += ": " + cerr.Error()
			}
			s.log.Warn("capture ended by itself", slog.String("recording", rec.ID().String()), slog.Any("error", cerr))
			if err := rec.Interrupt(s.clock.Now(), problem); err != nil {
				return err
			}
			if err := s.persist(keep, sess, rec); err != nil {
				return fmt.Errorf("%w: %w", ErrAbandoned, err)
			}
			return s.assemble(ctx, sess, rec)

		case <-ctx.Done():
			s.haltCapture(keep, rec.ID())
			return ErrAbandoned
		}
	}
}

// endCapture stops the capture and waits until it has ended. It reports false if the service shut down meanwhile.
// When Stop fails the capture may hang, so it is not waited for: the chunks on disk are assembled.
func (s *Service) endCapture(ctx context.Context, id shared.RecordingID, ended <-chan error) bool {
	if err := s.recorder.Stop(context.WithoutCancel(ctx), id); err != nil {
		s.log.Warn("stopping the capture failed", slog.String("recording", id.String()), slog.Any("error", err))
		return ctx.Err() == nil
	}

	select {
	case err := <-ended:
		if err != nil {
			s.log.Warn("capture ended with an error", slog.String("recording", id.String()), slog.Any("error", err))
		}
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// haltCapture releases the audio device of a session that will not finalize.
func (s *Service) haltCapture(ctx context.Context, id shared.RecordingID) {
	if err := s.recorder.Stop(ctx, id); err != nil {
		s.log.Warn("stopping the capture failed", slog.String("recording", id.String()), slog.Any("error", err))
	}
}

// assemble turns the chunks of a FINALIZING recording into the final track and saves COMPLETED or FAILED. The chunks
// stay on disk either way. A shutdown during the assembly leaves FINALIZING for the recovery.
func (s *Service) assemble(ctx context.Context, sess *session, rec *recording.Recording) error {
	keep := context.WithoutCancel(ctx)

	if n, err := s.countChunks(ctx, rec.ChunksDir()); err != nil {
		s.log.Warn("listing the chunks failed", slog.String("recording", rec.ID().String()), slog.Any("error", err))
	} else if err := rec.ObserveChunks(n, s.clock.Now()); err != nil {
		return err
	}

	track, err := rec.TrackRef(s.trackExt)
	if err != nil {
		return err
	}
	duration, ferr := s.recorder.Finalize(ctx, rec.ChunksDir(), track)
	if ferr != nil && ctx.Err() != nil {
		return ErrAbandoned
	}

	now := s.clock.Now()
	if ferr == nil {
		ferr = rec.Complete(now, track, duration)
	}
	if ferr != nil {
		s.log.Warn("assembling the recording failed", slog.String("recording", rec.ID().String()), slog.Any("error", ferr))
		if err := rec.Fail(now, problemFinalizeFailed+": "+ferr.Error()); err != nil {
			return err
		}
	}
	return s.persistFinal(keep, sess, rec)
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

// persist publishes and saves an intermediate state. Only a lost ownership is returned; other failures are logged,
// as the next save carries the full state.
func (s *Service) persist(ctx context.Context, sess *session, rec *recording.Recording) error {
	sess.publish(rec)
	err := s.recordings.Save(ctx, rec)
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

// persistFinal saves a final status, retrying errors other than a lost ownership a few times. The version is
// unchanged after a failed save, so the same aggregate can be saved again.
func (s *Service) persistFinal(ctx context.Context, sess *session, rec *recording.Recording) error {
	sess.publish(rec)
	delay := finalSaveRetryBase
	for attempt := 1; ; attempt++ {
		err := s.recordings.Save(ctx, rec)
		if err == nil {
			s.log.Info("recording finished", slog.String("recording", rec.ID().String()), slog.String("status", rec.Status().String()))
			return nil
		}
		if lostOwnership(err) || attempt == finalSaveAttempts {
			s.log.Error("saving the final state failed; the recovery will find the recording",
				slog.String("recording", rec.ID().String()), slog.String("status", rec.Status().String()), slog.Any("error", err))
			return fmt.Errorf("%w: %w", ErrAbandoned, err)
		}
		if s.sleeper.Sleep(s.life, delay) != nil {
			return fmt.Errorf("%w: %w", ErrAbandoned, err)
		}
		delay *= 2
	}
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
// again. A failure after the files were removed is repaired by repeating the request.
func (s *Service) Delete(ctx context.Context, id shared.RecordingID) error {
	rec, err := s.recordings.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opDelete, id, err)
	}
	if err := rec.CheckDeletable(); err != nil {
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
// return and reports ctx's error. It is safe to call more than once.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
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
