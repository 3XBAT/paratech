package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/speechcomponent"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/values"
)

// SpeechStatusReader tells whether speech recognition works now; the models page reads it on every List.
type SpeechStatusReader interface {
	Status() speechcomponent.Status
}

// SpeechComponentUseCase is the speech recognition component as the pages need it (ARCH-42); speechcomponent.Service
// satisfies it. StartInstall runs in the background; Status shows its progress.
type SpeechComponentUseCase interface {
	SpeechStatusReader
	StartInstall() error
	CancelInstall()
}

// speechText is a fixed Russian text of the speech component, shown as is. It never names whisper-cli, an .exe or
// a DLL: those stay in the log.
type speechText string

func (t speechText) Error() string { return string(t) }

// Texts of the speech component.
const (
	SpeechMissing        speechText = "Компонент распознавания речи не установлен"
	SpeechDamaged        speechText = "Компонент распознавания речи повреждён: установите его заново"
	ErrSpeechOffline     speechText = "Не удалось скачать компонент: проверьте подключение к интернету"
	ErrSpeechCorrupted   speechText = "Скачанный файл повреждён, попробуйте ещё раз"
	ErrSpeechRefused     speechText = "Сервер с компонентом ответил не так, как ожидалось: попробуйте позже"
	ErrSpeechNoAccess    speechText = "Нет доступа к папке с данными программы"
	ErrSpeechNotStarted  speechText = "Компонент распознавания установлен, но не запустился. Перезапустите приложение; если не поможет — сохраните пакет для поддержки в разделе «Диагностика»"
	ErrSpeechFailed      speechText = "Не удалось установить компонент распознавания речи"
	ErrSpeechInstalling  speechText = "Компонент распознавания речи уже устанавливается"
	ErrSpeechAlreadyWork speechText = "Распознавание речи уже работает"
)

// SpeechStatusView is the JSON form of the speech component for the models page and the recordings page.
//
// State is "ready", "missing", "damaged", "installing" or "failed". Stage is set while installing: "downloading",
// "verifying", "unpacking" or "activating"; DoneBytes and TotalBytes are the download progress. Message is the text to
// show ("" when ready or installing). CanInstall offers the «Установить компонент распознавания» button (not when
// verified files failed to start), CanCancel the cancel of a running install.
type SpeechStatusView struct {
	State      string `json:"state"`
	Stage      string `json:"stage"`
	DoneBytes  int64  `json:"doneBytes"`
	TotalBytes int64  `json:"totalBytes"`
	Message    string `json:"message"`
	CanInstall bool   `json:"canInstall"`
	CanCancel  bool   `json:"canCancel"`
}

// SpeechService is bound to the pages like SettingsService ("<package path>.SpeechService.<Method>"). The page never
// passes a URL or a path: the source and its hosts come from the pinned manifest.
type SpeechService struct {
	uc  SpeechComponentUseCase
	log *slog.Logger
}

// NewSpeechService returns the binding of the speech component; OpenServices checks the use case before calling it.
func NewSpeechService(uc SpeechComponentUseCase, log *slog.Logger) *SpeechService {
	return &SpeechService{uc: uc, log: log}
}

// Status returns the state of speech recognition and of the current or last install.
func (s *SpeechService) Status(context.Context) SpeechStatusView { return speechView(s.uc.Status()) }

// StartInstall starts downloading and installing the component in the background; Status shows the progress.
func (s *SpeechService) StartInstall(context.Context) error {
	err := s.uc.StartInstall()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, speechcomponent.ErrInstallInProgress):
		return ErrSpeechInstalling
	case errors.Is(err, speechcomponent.ErrAlreadyActive):
		return ErrSpeechAlreadyWork
	}
	s.log.Error("ui: start the speech component install", "err", err)
	return ErrSpeechFailed
}

// CancelInstall stops the running install; the component stays as it was.
func (s *SpeechService) CancelInstall() { s.uc.CancelInstall() }

func speechView(st speechcomponent.Status) SpeechStatusView {
	v := SpeechStatusView{State: string(st.State)}
	switch st.State {
	case speechcomponent.StateInstalling:
		v.Stage, v.DoneBytes, v.TotalBytes, v.CanCancel = string(st.Stage), st.DoneBytes, st.TotalBytes, true
	case speechcomponent.StateMissing:
		v.Message, v.CanInstall = SpeechMissing.Error(), true
	case speechcomponent.StateDamaged:
		v.Message, v.CanInstall = SpeechDamaged.Error(), true
	case speechcomponent.StateFailed:
		// A verified set that did not start is not fixed by downloading it again: no install button then.
		v.Message, v.CanInstall = speechErrorText(st.Err), !errors.Is(st.Err, speechcomponent.ErrActivationFailed)
	}
	return v
}

// speechErrorText maps why an install failed to the page's text; the narrow causes come first.
func speechErrorText(err error) string {
	var space *values.InsufficientSpaceError
	if errors.As(err, &space) {
		return spaceError{required: space.Required, available: space.Available}.Error()
	}
	for _, m := range []struct {
		err  error
		text speechText
	}{
		{speechcomponent.ErrActivationFailed, ErrSpeechNotStarted},
		{shared.ErrStorageFull, speechText(ErrNotEnoughSpace)},
		{speechcomponent.ErrDownloadFailed, ErrSpeechOffline},
		{values.ErrHostNotAllowed, ErrSpeechRefused},
		{values.ErrInvalidSourceURL, ErrSpeechRefused},
		{values.ErrUnexpectedRange, ErrSpeechRefused},
		{values.ErrChecksumMismatch, ErrSpeechCorrupted},
		{values.ErrSizeMismatch, ErrSpeechCorrupted},
		{values.ErrInvalidComponentArchive, ErrSpeechCorrupted},
		{speechcomponent.ErrNotVerified, ErrSpeechCorrupted},
		{shared.ErrStoragePermission, ErrSpeechNoAccess},
	} {
		if errors.Is(err, m.err) {
			return m.text.Error()
		}
	}
	return ErrSpeechFailed.Error()
}
