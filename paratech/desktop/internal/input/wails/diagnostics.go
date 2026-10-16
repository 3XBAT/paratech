package wails

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"

	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/domain/recording/values"
)

// Operation names of the log.
const (
	opCheckSound = "ui: check sound"
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

// Fixed texts of "Проверить звук" (Russian UI), one per verdict.
var soundMessages = map[values.SoundCause]string{
	values.SoundOK:          "Звук записывается: проверочный сигнал слышен.",
	values.SoundNoDevice:    "Нет устройства вывода звука. Подключите динамики или наушники и повторите проверку.",
	values.SoundSilence:     "Проверочный сигнал не слышен. Проверьте, что устройство вывода включено, и повторите проверку.",
	values.SoundUnsupported: "Этот способ захвата звука не работает в этой системе. Запись сама перейдёт на весь звук компьютера, если он доступен.",
	values.SoundHandsFree:   "Bluetooth-гарнитура в режиме разговора: звук записывается плохо или не записывается. Завершите звонок или выберите в Windows вывод «Наушники» (стерео).",
	values.SoundBusy:        "Идёт запись урока: проверка сейчас недоступна, её сигнал помешал бы записи.",
	values.SoundFailed:      "Проверка звука не удалась. Попробуйте ещё раз; если не поможет, сохраните пакет для поддержки.",
}

const (
	msgCheckRunning  = "Проверка звука уже идёт."
	msgHandsFreeHint = "Похоже, вывод идёт на Bluetooth-гарнитуру в режиме разговора: качество звука низкое."
)

// SoundCheckUseCase is the use case of "Проверить звук" (ARCH-42).
type SoundCheckUseCase interface {
	Check(ctx context.Context) (soundcheck.Result, error)
}

// SoundCheck answers "Проверить звук". Verdict is "ok" or the cause: "no_device", "silence", "unsupported",
// "hands_free", "busy" or "failed"; Message is its fixed text. Hint is a fixed text when the output device looks like
// a Bluetooth headset in call mode, "" otherwise.
type SoundCheck struct {
	Verdict   string      `json:"verdict"`
	Message   string      `json:"message"`
	Device    string      `json:"device"` // the default output device; "" when unknown
	HandsFree bool        `json:"handsFree"`
	Hint      string      `json:"hint"`
	Modes     []SoundMode `json:"modes"`
}

// SoundMode is the result of one capture mode: "include" (capture of an app) or "system" (the output device).
type SoundMode struct {
	Mode  string `json:"mode"`
	OK    bool   `json:"ok"`
	Cause string `json:"cause"`
}

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

// CheckSound plays a short tone on the default output device and tells whether the capture hears it. A failure is a
// normal answer with a fixed text; no error text reaches the page.
func (s *DiagnosticsService) CheckSound(ctx context.Context) (SoundCheck, error) {
	r, err := s.deps.Sound.Check(ctx)
	switch {
	case errors.Is(err, soundcheck.ErrBusy):
		return SoundCheck{Verdict: string(values.SoundBusy), Message: msgCheckRunning, Modes: []SoundMode{}}, nil
	case err != nil:
		s.log.Warn(opCheckSound, "err", err)
		r = soundcheck.Result{Verdict: values.SoundFailed}
	}
	out := SoundCheck{
		Verdict: string(r.Verdict), Message: soundMessages[r.Verdict], Device: r.Check.Device, HandsFree: r.Check.HandsFree,
		Modes: make([]SoundMode, 0, len(r.Check.Modes)),
	}
	if out.Message == "" {
		out.Verdict, out.Message = string(values.SoundFailed), soundMessages[values.SoundFailed]
	}
	if r.Check.HandsFree && r.Verdict != values.SoundHandsFree {
		out.Hint = msgHandsFreeHint
	}
	for _, m := range r.Check.Modes {
		out.Modes = append(out.Modes, SoundMode{Mode: string(m.Mode), OK: m.Cause == values.SoundOK, Cause: string(m.Cause)})
	}
	return out, nil
}
