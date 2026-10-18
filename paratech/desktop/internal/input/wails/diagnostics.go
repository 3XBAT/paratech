package wails

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"path/filepath"
	"time"

	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/domain/recording/values"
)

// Operation names of the log.
const (
	opStartSound = "ui: start the sound check"
	opSaveBundle = "ui: save support bundle"
	opPickBundle = "ui: choose where to save the support bundle"
)

// Statuses and fixed texts of "Сохранить пакет для поддержки".
const (
	bundleSaved     = "saved"
	bundleCancelled = "cancelled"
	bundleFailed    = "failed"

	msgBundleSaved     = "Пакет для поддержки сохранён. Приложение ничего не отправляет: передайте файл разработчикам сами."
	msgBundleCancelled = "Сохранение отменено."
	msgBundleFailed    = "Не удалось сохранить пакет для поддержки. Выберите другую папку и попробуйте ещё раз."
)

// stopSoundWait bounds how long "stop" waits for the check to end; the page polls the status afterwards anyway.
const stopSoundWait = 5 * time.Second

// Fixed texts of "Проверить звук" (Russian UI, TASK-075).
const (
	msgPlaySomething    = "Включите любой звук на компьютере (видео, музыку, звонок)."
	msgHeard            = "Звук слышен."
	sourceWholeComputer = "Весь звук компьютера"
)

// soundTexts are the message and the hints of each verdict; silence has its hints per kind of source.
var soundTexts = map[values.SoundCause]struct {
	message string
	hints   []string
}{
	values.SoundOK: {msgHeard, nil},
	values.SoundSilence: {"Звук не слышен.", []string{
		"Проверьте, что сейчас что-то звучит и звук не выключен в микшере громкости Windows.",
	}},
	values.SoundNotRunning: {"Выбранное приложение не запущено.", []string{
		"Откройте приложение урока и повторите проверку или выберите другой источник в окне записи.",
	}},
	values.SoundNoDevice: {"Нет устройства вывода звука.", []string{
		"Подключите динамики или наушники и повторите проверку.",
	}},
	values.SoundUnsupported: {"Запись звука не работает в этой системе.", []string{
		"Сохраните пакет для поддержки и передайте его разработчикам.",
	}},
	values.SoundBusy: {"Идёт запись урока: проверка звука сейчас недоступна.", nil},
	values.SoundFailed: {"Проверка звука не удалась.", []string{
		"Попробуйте ещё раз; если не поможет, сохраните пакет для поддержки.",
	}},
}

// silenceHintsApp are the hints of silence when an app is checked.
var silenceHintsApp = []string{
	"Проверьте, что в выбранном приложении сейчас идёт звук.",
	"Проверьте, что звук этого приложения не выключен в микшере громкости Windows.",
	"Если урок идёт в другом приложении, выберите его в окне записи.",
}

// SoundCheckUseCase is the use case of "Проверить звук" (ARCH-42).
type SoundCheckUseCase interface {
	Start(ctx context.Context) (soundcheck.Status, error)
	Status() soundcheck.Status
	Stop(ctx context.Context) soundcheck.Status
}

// SoundCheck is the state of "Проверить звук" for the page. State is "idle", "running" or "done". Level is the
// latest level and Peak the loudest, both 0..1 (Level is 0 once done); Heard reports a level that counts as sound.
// Verdict is set once done: "ok", "silence", "not_running", "no_device", "unsupported", "busy" or "failed". Message
// and Hints are fixed texts; Source is the display name of what is checked: sourceWholeComputer or the executable
// name of the app (untrusted, render as text).
type SoundCheck struct {
	State         string   `json:"state"`
	Level         float64  `json:"level"`
	Peak          float64  `json:"peak"`
	Heard         bool     `json:"heard"`
	ElapsedMs     int64    `json:"elapsedMs"`
	RemainingMs   int64    `json:"remainingMs"`
	DurationMs    int64    `json:"durationMs"`
	Verdict       string   `json:"verdict"`
	Message       string   `json:"message"`
	Hints         []string `json:"hints"`
	Source        string   `json:"source"`
	WholeComputer bool     `json:"wholeComputer"`
}

// soundCheck maps a status to the page's answer.
func soundCheck(st soundcheck.Status) SoundCheck {
	out := SoundCheck{
		State: string(st.State), Level: roundLevel(st.Check.Level), Peak: roundLevel(st.Check.Peak), Heard: st.Heard,
		ElapsedMs: st.Elapsed.Milliseconds(), RemainingMs: max(st.Duration-st.Elapsed, 0).Milliseconds(),
		DurationMs: st.Duration.Milliseconds(), Hints: []string{}, WholeComputer: st.Source.IsWholeComputer(),
		Source: sourceWholeComputer,
	}
	if !out.WholeComputer {
		out.Source = st.Source.App().String()
	}
	switch st.State {
	case soundcheck.StateIdle:
		out.Source, out.WholeComputer = "", false
	case soundcheck.StateRunning:
		out.Message = msgPlaySomething
		if st.Heard {
			out.Message = msgHeard
		}
	default:
		out.State = string(soundcheck.StateDone)
		v := st.Verdict
		if _, ok := soundTexts[v]; !ok {
			v = values.SoundFailed
		}
		t := soundTexts[v]
		out.Verdict, out.Message, out.Hints = string(v), t.message, append(out.Hints, t.hints...)
		if v == values.SoundSilence && !out.WholeComputer {
			out.Hints = append([]string{}, silenceHintsApp...)
		}
	}
	return out
}

// roundLevel keeps three decimals: enough for a bar, and a stable JSON.
func roundLevel(l values.SoundLevel) float64 { return math.Round(float64(l)*1000) / 1000 }

// SupportBundleUseCase is the use case of "Сохранить пакет для поддержки" (ARCH-42).
type SupportBundleUseCase interface {
	SuggestedName() string
	Create(ctx context.Context, path string) error
}

// BundleFilePicker shows the native save dialog of the support bundle with name proposed. It returns the chosen
// path, or "" when the user cancels; the path never comes from the page.
type BundleFilePicker interface {
	PickBundleFile(ctx context.Context, name string) (string, error)
}

// bundlePickerFunc adapts a function to BundleFilePicker.
type bundlePickerFunc func(ctx context.Context, name string) (string, error)

// PickBundleFile implements BundleFilePicker.
func (f bundlePickerFunc) PickBundleFile(ctx context.Context, name string) (string, error) {
	return f(ctx, name)
}

// BundleResult answers "Сохранить пакет для поддержки": Status "saved", "cancelled" or "failed", a fixed text, and
// the base name of the saved file ("" unless saved).
type BundleResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Name    string `json:"name"`
}

// DiagnosticsDeps are the parts of the diagnostics page; all are required.
type DiagnosticsDeps struct {
	Sound  SoundCheckUseCase
	Bundle SupportBundleUseCase
	Picker BundleFilePicker
}

// DiagnosticsService is bound to the UI (the «Диагностика» page): every exported method is callable from the page by
// its full name "<package path>.DiagnosticsService.<Method>". Keep the method set to what the page may call.
type DiagnosticsService struct {
	deps DiagnosticsDeps
	log  *slog.Logger
}

// NewDiagnosticsService returns the binding of the diagnostics page.
func NewDiagnosticsService(deps DiagnosticsDeps, log *slog.Logger) *DiagnosticsService {
	return &DiagnosticsService{deps: deps, log: log}
}

// SaveSupportBundle asks where to save the support bundle in the native save dialog and writes it there. Only this
// explicit action creates a bundle, and it is never uploaded. Neither the path nor an error text reaches the page;
// the page gets the base name of the file.
func (s *DiagnosticsService) SaveSupportBundle(ctx context.Context) (BundleResult, error) {
	path, err := s.deps.Picker.PickBundleFile(ctx, s.deps.Bundle.SuggestedName())
	switch {
	case err != nil:
		s.log.Warn(opPickBundle, "err", err)
		return BundleResult{Status: bundleFailed, Message: msgBundleFailed}, nil
	case path == "":
		return BundleResult{Status: bundleCancelled, Message: msgBundleCancelled}, nil
	}
	if err := s.deps.Bundle.Create(ctx, path); err != nil {
		s.log.Error(opSaveBundle, "err", err) // the adapters' errors carry no path
		return BundleResult{Status: bundleFailed, Message: msgBundleFailed}, nil
	}
	return BundleResult{Status: bundleSaved, Message: msgBundleSaved, Name: filepath.Base(path)}, nil
}

// StartSoundCheck starts "Проверить звук": for about 15 s the source a recording would use is captured, without
// recording, and SoundCheckStatus shows its level. A check that runs already is returned as it is. A failure is a
// normal answer with a fixed text; no error text reaches the page.
func (s *DiagnosticsService) StartSoundCheck(ctx context.Context) (SoundCheck, error) {
	st, err := s.deps.Sound.Start(ctx)
	switch {
	case errors.Is(err, soundcheck.ErrBusy): // the running check is the answer
	case err != nil:
		s.log.Warn(opStartSound, "err", err)
		return soundCheck(soundcheck.Status{State: soundcheck.StateDone, Verdict: values.SoundFailed}), nil
	}
	return soundCheck(st), nil
}

// SoundCheckStatus returns the state of the last check; the page polls it while the check runs.
func (s *DiagnosticsService) SoundCheckStatus(context.Context) (SoundCheck, error) {
	return soundCheck(s.deps.Sound.Status()), nil
}

// StopSoundCheck ends a running check early and returns its state, done unless it did not end within a few seconds.
func (s *DiagnosticsService) StopSoundCheck(ctx context.Context) (SoundCheck, error) {
	ctx, cancel := context.WithTimeout(ctx, stopSoundWait)
	defer cancel()
	return soundCheck(s.deps.Sound.Stop(ctx)), nil
}
