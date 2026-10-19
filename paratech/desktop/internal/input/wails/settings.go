package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/domain/shared"
)

// ErrSettingsUnavailable is what the UI sees when reading or saving the settings fails. The cause is logged, not
// sent to the page: it may name local paths or database details.
var ErrSettingsUnavailable = errors.New("settings are unavailable")

// ErrUnknownRecognitionLoad is what the UI sees for a load mode other than gentle, normal or max.
var ErrUnknownRecognitionLoad = errors.New("unknown recognition load")

// SettingsUseCase is the part of the preferences use case the settings page needs (ARCH-42).
type SettingsUseCase interface {
	Get(ctx context.Context) (preferences.Toggles, error)
	SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetRecognitionLoad(ctx context.Context, load shared.RecognitionLoad) (preferences.Toggles, error)
}

// Settings is the JSON form of the toggles on the settings page.
type Settings struct {
	RecordingPromptsEnabled bool `json:"recordingPromptsEnabled"`
	ProcessOnlyOnACPower    bool `json:"processOnlyOnAcPower"`

	// RecognitionLoad («Нагрузка при распознавании»): gentle, normal or max. A change applies from the next
	// recognition run; a running one keeps its mode («Применится к следующему распознаванию»).
	RecognitionLoad string `json:"recognitionLoad"`
	// LoadEstimates are the estimates of the model new recordings are recognised with, for the text next to the
	// setting; null when no model is in use or the estimate is unavailable.
	LoadEstimates *ModelLoadEstimates `json:"loadEstimates"`
	// LoadEstimatesModel is the name of that model; "" with null estimates.
	LoadEstimatesModel string `json:"loadEstimatesModel"`
}

// SettingsService is bound to the UI: every exported method is callable from the page by its full name
// "<package path>.SettingsService.<Method>" (see frontend/src/bindings/settings.ts). Keep the method set to what the
// page may call.
type SettingsService struct {
	uc    SettingsUseCase
	speed SpeedEstimates // nil: no estimates
	log   *slog.Logger
}

// NewSettingsService returns the binding of the settings page.
func NewSettingsService(uc SettingsUseCase, log *slog.Logger) *SettingsService {
	return &SettingsService{uc: uc, log: log}
}

// withSpeed adds the time estimates of the model in use (TASK-084).
func (s *SettingsService) withSpeed(speed SpeedEstimates) *SettingsService {
	s.speed = speed
	return s
}

// GetSettings returns the current toggles.
func (s *SettingsService) GetSettings(ctx context.Context) (Settings, error) {
	t, err := s.uc.Get(ctx)
	return s.reply(ctx, "get settings", t, err)
}

// SetRecordingPromptsEnabled turns the automatic recording prompts on or off (AC-02).
func (s *SettingsService) SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (Settings, error) {
	t, err := s.uc.SetRecordingPromptsEnabled(ctx, enabled)
	return s.reply(ctx, "set recording prompts", t, err)
}

// SetProcessOnlyOnACPower turns the AC-power restriction of heavy jobs on or off (AC-22).
func (s *SettingsService) SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (Settings, error) {
	t, err := s.uc.SetProcessOnlyOnACPower(ctx, enabled)
	return s.reply(ctx, "set process only on AC power", t, err)
}

// SetRecognitionLoad sets the load mode of speech recognition: "gentle", "normal" or "max" (TASK-084).
func (s *SettingsService) SetRecognitionLoad(ctx context.Context, load string) (Settings, error) {
	l := shared.RecognitionLoad(load)
	if !l.Valid() {
		return Settings{}, ErrUnknownRecognitionLoad
	}
	t, err := s.uc.SetRecognitionLoad(ctx, l)
	return s.reply(ctx, "set recognition load", t, err)
}

func (s *SettingsService) reply(ctx context.Context, op string, t preferences.Toggles, err error) (Settings, error) {
	if err != nil {
		s.log.Error("ui: "+op, "err", err)
		return Settings{}, ErrSettingsUnavailable
	}
	out := Settings{
		RecordingPromptsEnabled: t.RecordingPromptsEnabled, ProcessOnlyOnACPower: t.ProcessOnlyOnACPower,
		RecognitionLoad: t.RecognitionLoad.String(),
	}
	if s.speed == nil {
		return out, nil
	}
	// The estimate is extra: the toggles are shown without it.
	o, err := s.speed.Overview(ctx)
	if err != nil {
		s.log.Warn("ui: recognition time estimates", "err", err)
		return out, nil
	}
	if e, ok := o.Installed[o.InUse]; ok && o.InUse != "" {
		out.LoadEstimates, out.LoadEstimatesModel = modelLoadEstimates(e), o.InUseName.String()
	}
	return out, nil
}
