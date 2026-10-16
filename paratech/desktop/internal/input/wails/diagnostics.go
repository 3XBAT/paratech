package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/soundcheck"
	"paratech/desktop/internal/domain/recording/values"
)

// Operation names of the log.
const opCheckSound = "ui: check sound"

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

// DiagnosticsService is bound to the UI (the «Диагностика» page): every exported method is callable from the page by
// its full name "<package path>.DiagnosticsService.<Method>". Keep the method set to what the page may call.
type DiagnosticsService struct {
	sound SoundCheckUseCase
	log   *slog.Logger
}

// NewDiagnosticsService returns the binding of the diagnostics page.
func NewDiagnosticsService(sound SoundCheckUseCase, log *slog.Logger) *DiagnosticsService {
	return &DiagnosticsService{sound: sound, log: log}
}

// CheckSound plays a short tone on the default output device and tells whether the capture hears it. A failure is a
// normal answer with a fixed text; no error text reaches the page.
func (s *DiagnosticsService) CheckSound(ctx context.Context) (SoundCheck, error) {
	r, err := s.sound.Check(ctx)
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
