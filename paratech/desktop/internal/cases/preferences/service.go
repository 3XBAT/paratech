package preferences

import (
	"context"
	"errors"
	"fmt"
	"time"

	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// conflictAttempts bounds the reload-and-retry loop on a stale settings version (another writer, e.g. the model
// catalog choosing the default model, saved in between).
const conflictAttempts = 3

// Service reads and changes the toggles of the settings page. Every change is one load, one aggregate method and one
// compare-and-swap save of the settings singleton (ARCH-23).
type Service struct {
	repo  SettingsRepository
	clock Clock
}

// New builds the Service.
func New(repo SettingsRepository, clock Clock) (*Service, error) {
	if repo == nil || clock == nil {
		return nil, fmt.Errorf("%w: every port is required", ErrInvalidConfig)
	}
	return &Service{repo: repo, clock: clock}, nil
}

// Get returns the current toggles, the defaults while nothing is stored.
func (s *Service) Get(ctx context.Context) (Toggles, error) {
	st, err := s.repo.Load(ctx)
	if err != nil {
		return Toggles{}, fmt.Errorf("preferences: get: %w", err)
	}
	return toggles(st), nil
}

// SetRecordingPromptsEnabled turns the automatic recording prompts on or off (AC-02) and returns the stored toggles.
func (s *Service) SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (Toggles, error) {
	return s.change(ctx, "set recording prompts", func(st *settings.Settings, now time.Time) error {
		return st.SetRecordingPromptsEnabled(enabled, now)
	})
}

// SetProcessOnlyOnACPower turns the AC-power restriction of heavy jobs on or off (AC-22) and returns the stored
// toggles.
func (s *Service) SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (Toggles, error) {
	return s.change(ctx, "set process only on AC power", func(st *settings.Settings, now time.Time) error {
		return st.SetProcessOnlyOnACPower(enabled, now)
	})
}

// SetNotifyOnProcessingComplete turns the notification about finished processing on or off (PRD section 15) and
// returns the stored toggles.
func (s *Service) SetNotifyOnProcessingComplete(ctx context.Context, enabled bool) (Toggles, error) {
	return s.change(ctx, "set notify on processing complete", func(st *settings.Settings, now time.Time) error {
		return st.SetNotifyOnProcessingComplete(enabled, now)
	})
}

// change reloads the settings and applies fn again while the save meets a stale version (ADR-0004(d)).
func (s *Service) change(ctx context.Context, op string, fn func(*settings.Settings, time.Time) error) (Toggles, error) {
	var err error
	for range conflictAttempts {
		var st *settings.Settings
		st, err = s.changeOnce(ctx, fn)
		if err == nil {
			return toggles(st), nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			break
		}
	}
	return Toggles{}, fmt.Errorf("preferences: %s: %w", op, err)
}

func (s *Service) changeOnce(ctx context.Context, fn func(*settings.Settings, time.Time) error) (*settings.Settings, error) {
	st, err := s.repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	if err := fn(st, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

func toggles(st *settings.Settings) Toggles {
	return Toggles{
		RecordingPromptsEnabled: st.RecordingPromptsEnabled(),
		ProcessOnlyOnACPower:    st.ProcessOnlyOnACPower(),

		NotifyOnProcessingComplete: st.NotifyOnProcessingComplete(),
	}
}
