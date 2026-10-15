package recordingsession

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/services/limits"
	"paratech/desktop/internal/domain/shared"
)

// Problem texts of the recovery; an error text may follow after a colon.
const (
	problemInterrupted     = "the app stopped while the recording was in progress"
	problemRecoveryFailed  = "the interrupted recording could not be recovered"
	problemRecoveryPending = "the next start tries again"
)

// recoveryRun is the state of a running Recover, guarded by Service.mu.
type recoveryRun struct {
	marking bool                            // the interrupted recordings are being marked; Start is refused
	pending map[shared.RecordingID]struct{} // listed recordings this Recover has not finished; Delete refuses them
}

// Recover finishes the recordings that an earlier run of the app left unfinished (PRD section 6.2 watchdog, section
// 14 case 1, AC-05). It is called once at startup, before the first Start; it refuses while a recording is in
// progress (limits.ErrRecordingInProgress) or another Recover runs (ErrRecovering).
//
// The app runs as a single instance, so every unfinished recording that no session of this process owns was cut off
// by the end of an earlier process: its heartbeat predates this process and is stale. Recover works in two passes:
//  1. For each one the aggregate decides (recording.MarkInterrupted): a STARTING recording is FAILED; a RECORDING one
//     is marked RECOVERY_REQUIRED. Every decision is stored before any chunk is touched, so a crash during the
//     assembly leaves the mark for the next start. Start is refused with ErrRecovering during this short pass only.
//  2. The chunks of the listed recordings that await assembly are assembled into the final track: the recording
//     becomes COMPLETED, or FAILED if no chunk is readable (recording.ErrNoAudio). A transient assembly error (after
//     the usual retries) keeps the status with a problem text, and the next Recover tries again; offering such a
//     recording to the user is a later UI task. A new recording may start meanwhile: its ID is never in the list, so
//     every recording keeps a single writer. Delete refuses a listed recording until this pass is done with it.
//
// Every step can be repeated, so Recover is idempotent and safe after a crash in its middle. It returns the Info of
// every recording it looked at, as left in memory; when a save failed, the error names the recording and its stored
// state may be older. A shutdown (forced Close) or ctx ending stops it; what is not finished stays for the next start.
func (s *Service) Recover(ctx context.Context) ([]Info, error) {
	if err := s.beginRecovery(); err != nil {
		return nil, fmt.Errorf("%s: %w", opRecover, err)
	}
	defer s.endRecovery()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(s.life, cancel)()

	unfinished, err := s.recordings.ListByStatus(ctx, recording.UnfinishedStatuses()...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opRecover, err)
	}
	slices.SortFunc(unfinished, func(a, b *recording.Recording) int {
		return cmp.Or(a.CreatedAt().Compare(b.CreatedAt()), cmp.Compare(a.ID(), b.ID()))
	})
	s.listRecovery(unfinished)

	var errs []error
	fail := func(rec *recording.Recording, err error) {
		s.log.Error("recovering a recording failed", slog.String("recording", rec.ID().String()), slog.Any("error", err))
		errs = append(errs, fmt.Errorf("%s: %w", rec.ID(), err))
	}

	looked := make([]*recording.Recording, 0, len(unfinished))
	var assemble []*recording.Recording
	for _, rec := range unfinished {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		looked = append(looked, rec)
		if err := s.markInterrupted(ctx, rec); err != nil {
			fail(rec, err) // not assembled: the stored state may be older than rec
			continue
		}
		if rec.AwaitsAssembly() {
			assemble = append(assemble, rec)
		}
	}
	s.endMarking(assemble)

	for _, rec := range assemble {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := s.assembleRecovered(ctx, rec); err != nil {
			fail(rec, err)
		}
		s.recovered(rec.ID())
	}

	infos := make([]Info, 0, len(looked))
	for _, rec := range looked {
		infos = append(infos, infoOf(rec))
	}
	if err := errors.Join(errs...); err != nil {
		return infos, fmt.Errorf("%s: %w", opRecover, err)
	}
	return infos, nil
}

// RecoveryMarked returns a channel that is closed once the first Recover has stored its decisions on the interrupted
// recordings, or has ended without getting that far (for example the list failed), or the service is closed (Start
// then fails with ErrClosed). From then on Start is not refused with ErrRecovering by that Recover. It stays open until
// a Recover runs: the app calls Recover once at startup, after the startup cleanups, so a caller that waits for it
// never starts a recording before both (ADR-0009).
func (s *Service) RecoveryMarked() <-chan struct{} { return s.marked }

// markingDone closes the RecoveryMarked channel; s.mu is held.
func (s *Service) markingDone() { s.markedOnce.Do(func() { close(s.marked) }) }

func (s *Service) beginRecovery() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.recovery != nil:
		return ErrRecovering
	case s.active != nil:
		return fmt.Errorf("%w: %s", limits.ErrRecordingInProgress, s.active.id)
	}
	s.recovery = &recoveryRun{marking: true, pending: map[shared.RecordingID]struct{}{}}
	s.wg.Add(1)
	return nil
}

// listRecovery records the recordings this Recover is to finish.
func (s *Service) listRecovery(recs []*recording.Recording) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range recs {
		s.recovery.pending[rec.ID()] = struct{}{}
	}
}

// endMarking lets Start in again and keeps only the recordings that are still to be assembled.
func (s *Service) endMarking(assemble []*recording.Recording) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovery.marking = false
	s.markingDone()
	clear(s.recovery.pending)
	for _, rec := range assemble {
		s.recovery.pending[rec.ID()] = struct{}{}
	}
}

// recovered marks id as done by this Recover.
func (s *Service) recovered(id shared.RecordingID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.recovery.pending, id)
}

func (s *Service) endRecovery() {
	s.mu.Lock()
	s.recovery = nil
	s.markingDone()
	s.mu.Unlock()
	s.wg.Done()
}

// checkNotRecovering refuses a change of a recording that the running Recover has not finished yet.
func (s *Service) checkNotRecovering(id shared.RecordingID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recovery == nil {
		return nil
	}
	if _, ok := s.recovery.pending[id]; ok {
		return ErrRecovering
	}
	return nil
}

// markInterrupted applies and stores the startup decision for one interrupted recording.
func (s *Service) markInterrupted(ctx context.Context, rec *recording.Recording) error {
	from := rec.Status()
	if err := rec.MarkInterrupted(s.clock.Now(), problemInterrupted); err != nil {
		return err
	}
	if err := s.saveRetrying(ctx, rec); err != nil {
		return err
	}
	s.log.Warn("found an interrupted recording", slog.String("recording", rec.ID().String()),
		slog.String("was", from.String()), slog.String("status", rec.Status().String()))
	return nil
}

// assembleRecovered assembles the chunks of a stored, marked recording that awaits assembly and stores the outcome.
func (s *Service) assembleRecovered(ctx context.Context, rec *recording.Recording) error {
	attempts := finalizeAttempts
	if n, err := s.countChunks(ctx, rec.ChunksDir()); err != nil {
		s.log.Warn("listing the chunks failed", slog.String("recording", rec.ID().String()), slog.Any("error", err))
	} else if err := rec.ObserveChunks(n, s.clock.Now()); err != nil {
		return err
	} else if n == 0 {
		attempts = 1
	}

	track, err := rec.TrackRef(s.trackExt)
	if err != nil {
		return err
	}
	duration, ferr := s.finalizeTrack(ctx, rec.ID(), rec.ChunksDir(), track, attempts)
	if ferr != nil && ctx.Err() != nil {
		return ctx.Err() // the stored mark stays for the next start
	}

	now := s.clock.Now()
	if ferr == nil {
		ferr = rec.CompleteRecovery(now, track, duration) // refuses an unusable result without changing rec
	}
	switch {
	case ferr == nil:
	case errors.Is(ferr, recording.ErrNoAudio) || errors.Is(ferr, recording.ErrInvalidRecording):
		s.log.Warn("the interrupted recording has no usable audio", slog.String("recording", rec.ID().String()), slog.Any("error", ferr))
		err = rec.FailRecovery(now, problemRecoveryFailed+": "+ferr.Error())
	default:
		s.log.Warn("assembling the interrupted recording failed; it is retried at the next start",
			slog.String("recording", rec.ID().String()), slog.Any("error", ferr))
		err = rec.DeferRecovery(now, problemRecoveryPending+": "+ferr.Error())
	}
	if err != nil {
		return err
	}
	if err := s.saveRetrying(ctx, rec); err != nil {
		return err
	}
	s.announceCompleted(ctx, rec)
	return nil
}
