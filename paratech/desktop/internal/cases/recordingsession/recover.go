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
)

// Problem texts of the recovery; an error text may follow after a colon.
const (
	problemInterrupted     = "the app stopped while the recording was in progress"
	problemRecoveryFailed  = "the interrupted recording could not be recovered"
	problemRecoveryPending = "the interrupted recording could not be assembled yet; the next start tries again"
)

// Recover finishes the recordings that an earlier run of the app left unfinished (PRD section 6.2 watchdog, section
// 14 case 1, AC-05). It is called once at startup, before the first Start; it refuses while a recording is in
// progress (limits.ErrRecordingInProgress) or another Recover runs (ErrRecovering), and Start is refused with
// ErrRecovering until it returns.
//
// The app runs as a single instance, so every unfinished recording that no session of this process owns was cut off
// by the end of an earlier process: its heartbeat predates this process and is stale. For each one the aggregate
// decides (recording.MarkInterrupted): a STARTING recording is FAILED; a RECORDING one is marked RECOVERY_REQUIRED and
// that mark is stored before anything else, so a crash during the assembly leaves it for the next start. Then the
// readable chunks are assembled into the final track: the recording becomes COMPLETED, or FAILED if no chunk is
// readable (recording.ErrNoAudio). A transient assembly error (after the usual retries) keeps the status with a
// problem text, and the next Recover tries again; offering such a recording to the user is a later UI task.
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

	infos := make([]Info, 0, len(unfinished))
	var errs []error
	for _, rec := range unfinished {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := s.recoverOne(ctx, rec); err != nil {
			s.log.Error("recovering a recording failed", slog.String("recording", rec.ID().String()), slog.Any("error", err))
			errs = append(errs, fmt.Errorf("%s: %w", rec.ID(), err))
		}
		infos = append(infos, infoOf(rec))
	}
	if err := errors.Join(errs...); err != nil {
		return infos, fmt.Errorf("%s: %w", opRecover, err)
	}
	return infos, nil
}

func (s *Service) beginRecovery() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.recovering:
		return ErrRecovering
	case s.active != nil:
		return fmt.Errorf("%w: %s", limits.ErrRecordingInProgress, s.active.id)
	}
	s.recovering = true
	s.wg.Add(1)
	return nil
}

func (s *Service) endRecovery() {
	s.mu.Lock()
	s.recovering = false
	s.mu.Unlock()
	s.wg.Done()
}

// recoverOne applies the startup decision to one interrupted recording and assembles its chunks if it says so.
func (s *Service) recoverOne(ctx context.Context, rec *recording.Recording) error {
	from := rec.Status()
	if err := rec.MarkInterrupted(s.clock.Now(), problemInterrupted); err != nil {
		return err
	}
	if err := s.saveRetrying(ctx, rec); err != nil {
		return err
	}
	s.log.Warn("found an interrupted recording", slog.String("recording", rec.ID().String()),
		slog.String("was", from.String()), slog.String("status", rec.Status().String()))
	if !rec.AwaitsAssembly() {
		return nil
	}

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
	return s.saveRetrying(ctx, rec)
}
