package transcribe

import (
	"context"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// liveWriter stores the segments of one engine run as live parts (TASK-079). The batch rules are the domain's
// (transcript.LiveBatch); a failed store is only logged, it never fails the transcription. The engine calls segment
// and progress on its own goroutines, so the lock orders them.
type liveWriter struct {
	s     *Service
	snap  job.Snapshot
	rec   shared.RecordingID
	mu    sync.Mutex
	batch *transcript.LiveBatch
}

func (s *Service) newLiveWriter(snap job.Snapshot, rec shared.RecordingID) *liveWriter {
	return &liveWriter{s: s, snap: snap, rec: rec, batch: transcript.NewLiveBatch()}
}

// segment returns the engine's segment callback, nil without the LiveParts port.
func (w *liveWriter) segment(ctx context.Context) func(transcript.Segment) {
	if w.s.live == nil {
		return nil
	}
	return func(seg transcript.Segment) {
		w.mu.Lock()
		defer w.mu.Unlock()

		// A refused segment is left out of the preview; its text is never logged.
		due, err := w.batch.Add(seg, w.s.clock.Now())
		if err == nil && due {
			w.store(ctx)
		}
	}
}

// flush stores the pending parts when they are due, or all of them when final is set.
func (w *liveWriter) flush(ctx context.Context, final bool) {
	if w.s.live == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.batch.Pending() > 0 && (final || w.batch.Due(w.s.clock.Now())) {
		w.store(ctx)
	}
}

// store writes the pending parts; on a failure they wait for the next store. Called with mu held.
func (w *liveWriter) store(ctx context.Context) {
	parts := w.batch.Take()
	if err := w.s.live.SaveParts(ctx, w.rec, parts); err != nil {
		w.batch.Retain(parts, w.s.clock.Now())
		w.s.log.Warn("stt job: live transcript not stored", slog.String("job", w.snap.ID().String()),
			slog.String("step", stepLiveSave), slog.Int("parts", len(parts)), slog.Any("cause", err))
	}
}

// deleteLive removes the live parts of the recording once its transcript is stored: the viewer reads the document
// from then on. A failure is only logged; a later attempt or the next transcript removes them.
func (s *Service) deleteLive(ctx context.Context, snap job.Snapshot, rec shared.RecordingID) {
	if s.live == nil {
		return
	}
	if err := s.live.DeleteParts(ctx, rec); err != nil {
		s.log.Warn("stt job: live transcript not removed", slog.String("job", snap.ID().String()),
			slog.String("step", stepLiveDelete), slog.Any("cause", err))
	}
}
