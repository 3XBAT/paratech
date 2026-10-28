package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// ErrSettingsUnavailable is what the UI sees when reading or saving the settings fails. The cause is logged, not
// sent to the page: it may name local paths or database details.
var ErrSettingsUnavailable = errors.New("settings are unavailable")

// ErrUnknownRecognitionLoad is what the UI sees for a load mode other than gentle, normal or max.
var ErrUnknownRecognitionLoad = errors.New("unknown recognition load")

// ErrUnknownNoteRecognition is what the UI sees for a photo recognition mode other than off or local.
var ErrUnknownNoteRecognition = errors.New("unknown note recognition")

// settingsError is an error of the settings page: a fixed Russian text the page shows as it is (like homeworkError).
type settingsError string

func (e settingsError) Error() string { return string(e) }

// ErrProfileInvalid is what the UI sees for a profile field the core refuses.
const ErrProfileInvalid = settingsError("Каждое поле профиля — одна строка до 200 символов без управляющих символов.")

// SettingsUseCase is the part of the preferences use case the settings page needs (ARCH-42).
type SettingsUseCase interface {
	Get(ctx context.Context) (preferences.Toggles, error)
	SetRecordingPromptsEnabled(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetProcessOnlyOnACPower(ctx context.Context, enabled bool) (preferences.Toggles, error)
	SetRecognitionLoad(ctx context.Context, load shared.RecognitionLoad) (preferences.Toggles, error)
	SetFreeWorkNotice(ctx context.Context, on bool) (preferences.Toggles, error)
	SetNoteRecognition(ctx context.Context, mode settings.NoteRecognition) (preferences.Toggles, error)
	RecognitionLanguage(ctx context.Context) (preferences.LanguageStatus, error)
	Profile(ctx context.Context) (settings.ProfileFields, error)
	SaveProfile(ctx context.Context, f settings.ProfileFields) (settings.ProfileFields, error)
}

// StudentProfile is the JSON form of the student profile («Профиль студента», TASK-094): the title page of a homework
// document in Word names these. Every field is optional ("" not set).
type StudentProfile struct {
	FullName   string `json:"fullName"`
	Group      string `json:"group"`
	University string `json:"university"`
	Faculty    string `json:"faculty"` // faculty or department
	City       string `json:"city"`
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

	// FreeWorkNotice («Предупреждать перед свободной работой», TASK-188): the agent shows «Такого задания нет — могу
	// сделать как свободную работу» before a work on a topic; off after «Больше не показывать» on that card.
	FreeWorkNotice bool `json:"freeWorkNotice"`

	// NoteRecognition («ИИ распознаёт фото конспектов», TASK-294): "off" (the default) or "local" (Windows OCR on this
	// computer, the photos never leave it).
	NoteRecognition string `json:"noteRecognition"`
	// NoteRecognitionLanguage: "available" (Windows has the Russian recognizer), "missing" (it has not: the page says
	// how to add it) or "unknown" (another OS, no OCR, a failure: the page says nothing).
	NoteRecognitionLanguage string `json:"noteRecognitionLanguage"`
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

// SetFreeWorkNotice turns the free-work card of the agent on or off (TASK-188).
func (s *SettingsService) SetFreeWorkNotice(ctx context.Context, on bool) (Settings, error) {
	t, err := s.uc.SetFreeWorkNotice(ctx, on)
	return s.reply(ctx, "set free work notice", t, err)
}

// SetNoteRecognition sets who recognises photo pages of notes: "off" or "local" (TASK-294).
func (s *SettingsService) SetNoteRecognition(ctx context.Context, mode string) (Settings, error) {
	m := settings.NoteRecognition(mode)
	if !m.Valid() {
		return Settings{}, ErrUnknownNoteRecognition
	}
	t, err := s.uc.SetNoteRecognition(ctx, m)
	return s.reply(ctx, "set note recognition", t, err)
}

// GetProfile returns the student profile.
func (s *SettingsService) GetProfile(ctx context.Context) (StudentProfile, error) {
	f, err := s.uc.Profile(ctx)
	if err != nil {
		s.log.Error("ui: get profile", "err", err)
		return StudentProfile{}, ErrSettingsUnavailable
	}
	return studentProfile(f), nil
}

// SaveProfile stores the student profile as the page sends it and returns it as stored (trimmed). A field that is not
// one line of at most 200 characters is ErrProfileInvalid and nothing is stored.
func (s *SettingsService) SaveProfile(ctx context.Context, p StudentProfile) (StudentProfile, error) {
	f, err := s.uc.SaveProfile(ctx, settings.ProfileFields{
		FullName: p.FullName, Group: p.Group, University: p.University, Faculty: p.Faculty, City: p.City,
	})
	switch {
	case errors.Is(err, settings.ErrInvalidSettings):
		return StudentProfile{}, ErrProfileInvalid
	case err != nil:
		s.log.Error("ui: save profile", "err", err)
		return StudentProfile{}, ErrSettingsUnavailable
	}
	return studentProfile(f), nil
}

func studentProfile(f settings.ProfileFields) StudentProfile {
	return StudentProfile{FullName: f.FullName, Group: f.Group, University: f.University, Faculty: f.Faculty, City: f.City}
}

func (s *SettingsService) reply(ctx context.Context, op string, t preferences.Toggles, err error) (Settings, error) {
	if err != nil {
		s.log.Error("ui: "+op, "err", err)
		return Settings{}, ErrSettingsUnavailable
	}
	out := Settings{
		RecordingPromptsEnabled: t.RecordingPromptsEnabled, ProcessOnlyOnACPower: t.ProcessOnlyOnACPower,
		RecognitionLoad: t.RecognitionLoad.String(), FreeWorkNotice: t.FreeWorkNotice,
		NoteRecognition: t.NoteRecognition.String(),
	}
	// The language is extra too: unknown hides the hint, it does not fail the page.
	lang, err := s.uc.RecognitionLanguage(ctx)
	if err != nil {
		s.log.Warn("ui: recognition language", "err", err)
	}
	out.NoteRecognitionLanguage = string(lang)

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
