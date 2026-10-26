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

// SetRecognitionLoad sets how hard speech recognition may load the computer (TASK-084) and returns the stored
// toggles. It applies from the next recognition run; a running one keeps its mode.
func (s *Service) SetRecognitionLoad(ctx context.Context, load shared.RecognitionLoad) (Toggles, error) {
	return s.change(ctx, "set recognition load", func(st *settings.Settings, now time.Time) error {
		return st.SetRecognitionLoad(load, now)
	})
}

// SetFreeWorkNotice turns the free-work card of the agent on or off (TASK-188): off is «Больше не показывать» on the
// card, on is the toggle in «Настройки». It returns the stored toggles.
func (s *Service) SetFreeWorkNotice(ctx context.Context, on bool) (Toggles, error) {
	return s.change(ctx, "set free work notice", func(st *settings.Settings, now time.Time) error {
		return st.SetFreeWorkNotice(on, now)
	})
}

// Profile returns the student profile (TASK-094); every field is empty while none is set.
func (s *Service) Profile(ctx context.Context) (settings.ProfileFields, error) {
	st, err := s.repo.Load(ctx)
	if err != nil {
		return settings.ProfileFields{}, fmt.Errorf("preferences: get profile: %w", err)
	}
	return st.Profile().Fields(), nil
}

// SaveProfile replaces the student profile with f and returns it as stored (trimmed). A field the profile refuses
// wraps settings.ErrInvalidSettings and nothing is stored.
func (s *Service) SaveProfile(ctx context.Context, f settings.ProfileFields) (settings.ProfileFields, error) {
	p, err := settings.NewProfile(f)
	if err != nil {
		return settings.ProfileFields{}, fmt.Errorf("preferences: save profile: %w", err)
	}
	st, err := s.changeSettings(ctx, "save profile", func(st *settings.Settings, now time.Time) error {
		return st.SetProfile(p, now)
	})
	if err != nil {
		return settings.ProfileFields{}, err
	}
	return st.Profile().Fields(), nil
}

// change reloads the settings and applies fn again while the save meets a stale version (ADR-0004(d)).
func (s *Service) change(ctx context.Context, op string, fn func(*settings.Settings, time.Time) error) (Toggles, error) {
	st, err := s.changeSettings(ctx, op, fn)
	if err != nil {
		return Toggles{}, err
	}
	return toggles(st), nil
}

func (s *Service) changeSettings(ctx context.Context, op string, fn func(*settings.Settings, time.Time) error) (*settings.Settings, error) {
	var err error
	for range conflictAttempts {
		var st *settings.Settings
		st, err = s.changeOnce(ctx, fn)
		if err == nil {
			return st, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			break
		}
	}
	return nil, fmt.Errorf("preferences: %s: %w", op, err)
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
		RecognitionLoad:         st.RecognitionLoad(),
		FreeWorkNotice:          st.FreeWorkNotice(),
	}
}
