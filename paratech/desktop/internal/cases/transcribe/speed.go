package transcribe

import (
	"context"
	"log/slog"
	"time"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// loadMode reads the load mode for the run that starts now (TASK-084). The setting must not stop a transcription:
// when it cannot be read, the run uses the default, the gentlest mode.
func (s *Service) loadMode(ctx context.Context, snap job.Snapshot) shared.RecognitionLoad {
	if s.settings == nil {
		return shared.DefaultRecognitionLoad
	}
	st, err := s.settings.Load(ctx)
	if err != nil {
		s.log.Warn("stt job runs in the default load mode", slog.String("job", snap.ID().String()),
			slog.String("step", stepLoad), slog.Any("cause", err))
		return shared.DefaultRecognitionLoad
	}
	return st.RecognitionLoad()
}

// measure adds a successful run that took wall to the measured speed of its model in its mode. The domain ignores
// absurd runs; a failure here only leaves the estimate as it was, never the job. STT jobs run one at a time (Heavy),
// so the read and the write do not race.
func (s *Service) measure(ctx context.Context, snap job.Snapshot, key values.SpeedKey, wall time.Duration,
	rec *recording.Recording) {
	audio, ok := rec.Duration()
	if s.speeds == nil || !ok {
		return
	}
	attrs := []any{slog.String("job", snap.ID().String()), slog.String("step", stepSpeed)}

	cur, err := s.speeds.Speed(ctx, key)
	if err != nil {
		s.log.Warn("stt job step failed", append(attrs, slog.Any("cause", err))...)
		return
	}
	next, counted := cur.Observe(wall, audio)
	if !counted {
		s.log.Info("stt run not counted in the measured speed", append(attrs,
			slog.Duration("wall", wall), slog.Duration("audio", audio))...)
		return
	}
	if err := s.speeds.SaveSpeed(ctx, key, next, s.clock.Now()); err != nil {
		s.log.Warn("stt job step failed", append(attrs, slog.Any("cause", err))...)
		return
	}
	s.log.Info("stt speed measured", append(attrs, slog.String("load", key.Load.String()),
		slog.Float64("factor", next.Factor()), slog.Int("runs", next.Samples()))...)
}
