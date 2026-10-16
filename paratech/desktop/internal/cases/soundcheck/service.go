package soundcheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
)

// Service runs the sound check.
type Service struct {
	checker    SoundChecker
	settings   SettingsReader
	recordings Recordings
	log        *slog.Logger

	running sync.Mutex
}

// New builds the Service; log may be nil.
func New(checker SoundChecker, settings SettingsReader, recordings Recordings, log *slog.Logger) (*Service, error) {
	if checker == nil || settings == nil || recordings == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{checker: checker, settings: settings, recordings: recordings, log: log}, nil
}

// Check plays the tone, captures it and returns the verdict for the remembered source (the whole computer when the
// settings cannot be read). A failure of the check is the verdict failed, a build without capture unsupported; only a
// second check at the same time (ErrBusy) and an ended ctx are errors.
func (s *Service) Check(ctx context.Context) (Result, error) {
	if !s.running.TryLock() {
		return Result{}, ErrBusy
	}
	defer s.running.Unlock()

	source := s.source(ctx)
	check := values.SoundCheck{Busy: true}
	if !s.recordings.Recording() {
		var err error
		check, err = s.checker.CheckSound(ctx)
		switch {
		case ctx.Err() != nil:
			return Result{}, ctx.Err()
		case errors.Is(err, recording.ErrCaptureUnavailable):
			s.log.Info("sound check: this build cannot capture")
			return Result{Verdict: values.SoundUnsupported, Source: source}, nil
		case err != nil:
			s.log.Error("sound check failed", "err", err)
			return Result{Verdict: values.SoundFailed, Source: source}, nil
		}
	}
	r := Result{Verdict: check.Verdict(source), Source: source, Check: check}
	s.log.Info("sound check", "source", source.String(), "verdict", string(r.Verdict))
	return r, nil
}

func (s *Service) source(ctx context.Context) values.Source {
	st, err := s.settings.Load(ctx)
	if err != nil {
		s.log.Warn("sound check: the settings cannot be read; checking for the whole computer", "err", err)
		return values.WholeComputer()
	}
	src, err := values.SourceOf(st.RecordingSource())
	if err != nil {
		return values.WholeComputer()
	}
	return src
}
