package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/preferences"
)

// ErrSettingsUnavailable is what the UI sees when reading or saving the settings fails. The cause is logged, not
// sent to the page: it may name local paths or database details.
var ErrSettingsUnavailable = errors.New("settings are unavailable")

// SettingsUseCase is the part of the preferences use case the settings page needs (ARCH-42).
type SettingsUseCase interface {
	Get(ctx context.Context) (preferences.Toggles, error)
	SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetNotifyOnProcessingComplete(ctx context.Context, enabled bool) (preferences.Toggles, error)
}

// Settings is the JSON form of the toggles on the settings page.
type Settings struct {
	RecordingPromptsEnabled    bool `json:"recordingPromptsEnabled"`
	ProcessOnlyOnACPower       bool `json:"processOnlyOnAcPower"`
	NotifyOnProcessingComplete bool `json:"notifyOnProcessingComplete"`
}

// SettingsService is bound to the UI: every exported method is callable from the page by its full name
// "<package path>.SettingsService.<Method>" (see frontend/src/bindings/settings.ts). Keep the method set to what the
// page may call.
type SettingsService struct {
	uc  SettingsUseCase
	log *slog.Logger
}

// NewSettingsService returns the binding of the settings page.
func NewSettingsService(uc SettingsUseCase, log *slog.Logger) *SettingsService {
	return &SettingsService{uc: uc, log: log}
}

// GetSettings returns the current toggles.
func (s *SettingsService) GetSettings(ctx context.Context) (Settings, error) {
	t, err := s.uc.Get(ctx)
	return s.reply("get settings", t, err)
}

// SetRecordingPromptsEnabled turns the automatic recording prompts on or off (AC-02).
func (s *SettingsService) SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (Settings, error) {
	t, err := s.uc.SetRecordingPromptsEnabled(ctx, enabled)
	return s.reply("set recording prompts", t, err)
}

// SetProcessOnlyOnACPower turns the AC-power restriction of heavy jobs on or off (AC-22).
func (s *SettingsService) SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (Settings, error) {
	t, err := s.uc.SetProcessOnlyOnACPower(ctx, enabled)
	return s.reply("set process only on AC power", t, err)
}

// SetNotifyOnProcessingComplete turns the notification about finished processing on or off (PRD section 15).
func (s *SettingsService) SetNotifyOnProcessingComplete(ctx context.Context, enabled bool) (Settings, error) {
	t, err := s.uc.SetNotifyOnProcessingComplete(ctx, enabled)
	return s.reply("set notify on processing complete", t, err)
}

func (s *SettingsService) reply(op string, t preferences.Toggles, err error) (Settings, error) {
	if err != nil {
		s.log.Error("ui: "+op, "err", err)
		return Settings{}, ErrSettingsUnavailable
	}
	return Settings{
		RecordingPromptsEnabled: t.RecordingPromptsEnabled, ProcessOnlyOnACPower: t.ProcessOnlyOnACPower,
		NotifyOnProcessingComplete: t.NotifyOnProcessingComplete,
	}, nil
}
