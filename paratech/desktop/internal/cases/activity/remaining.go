package activity

import (
	"context"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/transcription/services/speedestimate"
	transcription "paratech/desktop/internal/domain/transcription/values"
)

// WithRemaining makes ListRecordings fill the approximate time left of a queued or running transcription
// (Recording.Remaining). Call it once, before the Service is used; without it no row has a time left.
func (s *Service) WithRemaining(times TranscriptionTimes, clock Clock) *Service {
	s.times, s.clock = times, clock
	return s
}

// remaining is the time left of the transcription of r, when its chain waits for or runs the transcription of its
// newest STT job and there is an estimate for the pinned model.
func (s *Service) remaining(ctx context.Context, r *recording.Recording, jobs []*job.Job, c Chain) (time.Duration, bool) {
	running := c.Stage == StageTranscribing
	queued := c.Stage == StageQueued && c.Step == StepTranscription
	if s.times == nil || s.clock == nil || (!running && !queued) {
		return 0, false
	}
	stt := newest(jobs, transcription.STTJobType)
	audio, hasAudio := r.Duration()
	if stt == nil || stt.PinnedModelID() == "" || !hasAudio {
		return 0, false
	}

	total, ok, err := s.times.TranscriptionTime(ctx, stt.PinnedModelID(), audio)
	if err != nil {
		// The page polls every few seconds; a failed estimate only leaves the time out.
		s.log.Debug("no time left for a transcription", slog.String("recording", r.ID().String()), slog.Any("err", err))
		return 0, false
	}
	if !ok {
		return 0, false
	}
	// Before the first progress the job's last update is its start (Claim). The job percent includes storing the
	// document; the estimate covers only the engine run.
	percent := transcription.STTEnginePercent(c.Percent)
	left := speedestimate.New().Remaining(total, running, percent, s.clock.Now().Sub(stt.UpdatedAt()))
	return left, left > 0
}
