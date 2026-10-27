package wails

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"paratech/desktop/internal/cases/recordingimport"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/shared"
)

// recordingImportError is an error of the import of an old recording: a fixed Russian text the page shows as it is.
type recordingImportError string

func (e recordingImportError) Error() string { return string(e) }

// Errors of the import of an old recording (TASK-161); the page shows them as they are. Causes are logged, never
// shown; neither paths nor file names are logged.
const (
	ErrRecordingImportInvalid     = recordingImportError("Неверные данные лекции: обновите страницу.")
	ErrRecordingImportInvalidDate = recordingImportError("Неверные дата и время лекции.")
	ErrRecordingImportFuture      = recordingImportError("Эта лекция ещё не началась: выберите прошедшую.")
	ErrRecordingImportNoLesson    = recordingImportError("Этой пары больше нет в расписании: обновите страницу.")
	ErrRecordingImportUnreadable  = recordingImportError("Не удалось прочитать файл.")
	ErrRecordingImportEmpty       = recordingImportError("Файл пустой.")
	ErrRecordingImportTooLarge    = recordingImportError("Файл больше 2 ГБ.")
	ErrRecordingImportTooLong     = recordingImportError("Запись длиннее 4 часов.")
	ErrRecordingImportNoDuration  = recordingImportError("Не удалось узнать длительность записи по файлу. Сохраните её заново в WAV или MP3 и попробуйте ещё раз.")
	ErrRecordingImportFormat      = recordingImportError("Подходят только аудиофайлы WAV, FLAC, MP3 и OGG (Vorbis). Видео, M4A, AAC, WMA и Opus пока не подходят.")
	ErrRecordingImportNoSpace     = recordingImportError("На диске не хватает места для копии записи.")
	ErrRecordingImportChanged     = recordingImportError("Файл изменился во время копирования. Попробуйте ещё раз.")
	ErrRecordingImportDeleting    = recordingImportError("Запись этого файла сейчас удаляется. Попробуйте через минуту.")
	ErrRecordingImportUnavailable = recordingImportError("Не удалось импортировать запись. Попробуйте ещё раз.")
	msgRecordingImportAdded       = "Эта запись уже добавлена."
)

// Operation names of the log.
const (
	opRecordingImportPick = "ui: choose an old recording"
	opRecordingImport     = "ui: import an old recording"
)

// RecordingImportUseCase is the import of old recordings (ARCH-42); satisfied by recordingimport.Service.
type RecordingImportUseCase interface {
	Import(ctx context.Context, req recordingimport.Request) (recordingimport.Result, error)
}

// RecordingFilePicker shows the native open dialog for an audio file of a lecture; "" when the user cancels. The path
// never comes from the page and never goes back to it.
type RecordingFilePicker interface {
	PickRecordingFile(ctx context.Context) (string, error)
}

// recordingPickerFunc adapts a function to RecordingFilePicker.
type recordingPickerFunc func(ctx context.Context) (string, error)

// PickRecordingFile implements RecordingFilePicker.
func (f recordingPickerFunc) PickRecordingFile(ctx context.Context) (string, error) { return f(ctx) }

// RecordingImportLecture is the lecture the file is a recording of: a past lesson (LessonID), or StartedAt (RFC 3339 with the
// offset) with the Subject, Kind ("lecture", "seminar", ...) and Title the student named, plain text.
type RecordingImportLecture struct {
	LessonID  string `json:"lessonId"`
	StartedAt string `json:"startedAt"`
	Subject   string `json:"subject"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
}

// RecordingImportResult answers Import. Cancelled: the user closed the dialog. Existing: the same file was imported before and
// RecordingID is that recording; Message is then «Эта запись уже добавлена.». Format is the format told from the
// content ("wav", "flac", "mp3", "ogg"); DurationSeconds is the duration read from the file (0 only for an Existing
// recording imported before TASK-162 that did not know it). Name is the base name of the file, plain text.
type RecordingImportResult struct {
	Cancelled       bool   `json:"cancelled"`
	RecordingID     string `json:"recordingId"`
	Existing        bool   `json:"existing"`
	Message         string `json:"message"`
	Name            string `json:"name"`
	Format          string `json:"format"`
	DurationSeconds int64  `json:"durationSeconds"`
}

// RecordingImportService is bound to the UI («Импортировать запись лекции», TASK-162): every exported method is
// callable from the page by its full name "<package path>.RecordingImportService.<Method>". The file is chosen in the
// native open dialog only, copied and recognized locally: the sound never leaves the computer.
type RecordingImportService struct {
	uc     RecordingImportUseCase
	picker RecordingFilePicker
	log    *slog.Logger
}

// NewRecordingImportService returns the binding of the import; all arguments are required.
func NewRecordingImportService(uc RecordingImportUseCase, picker RecordingFilePicker, log *slog.Logger) *RecordingImportService {
	return &RecordingImportService{uc: uc, picker: picker, log: log}
}

// Import checks the lecture, asks in the native open dialog for one audio file and imports it as a recording of the
// lecture; its transcription is queued like that of a captured recording.
func (s *RecordingImportService) Import(ctx context.Context, lecture RecordingImportLecture) (RecordingImportResult, error) {
	req, err := lecture.request()
	if err != nil {
		return RecordingImportResult{}, err
	}
	path, err := s.picker.PickRecordingFile(ctx)
	switch {
	case err != nil:
		s.log.Warn(opRecordingImportPick, "err", err)
		return RecordingImportResult{}, ErrRecordingImportUnreadable
	case path == "":
		return RecordingImportResult{Cancelled: true}, nil
	}
	req.Path = path

	res, err := s.uc.Import(ctx, req)
	if err != nil {
		return RecordingImportResult{}, s.fail(err)
	}
	r := res.Recording
	out := RecordingImportResult{RecordingID: string(r.ID()), Existing: res.Existing, Name: baseName(path)}
	if res.Existing {
		out.Message = msgRecordingImportAdded
	}
	if audio := r.Audio(); !audio.IsZero() {
		if ext := extOf(audio.Path()); recording.AudioFormat(ext).Valid() {
			out.Format = ext
		}
	}
	if d, ok := r.Duration(); ok {
		out.DurationSeconds = int64(d.Round(time.Second) / time.Second)
	}
	s.log.Info(opRecordingImport, "recording", out.RecordingID, "existing", out.Existing)
	return out, nil
}

// request reads the lecture; a bad id or time is refused before the dialog opens.
func (l RecordingImportLecture) request() (recordingimport.Request, error) {
	req := recordingimport.Request{LessonID: shared.LessonID(l.LessonID), Subject: l.Subject, Kind: l.Kind, Title: l.Title}
	if l.LessonID != "" {
		if err := req.LessonID.Validate(); err != nil {
			return recordingimport.Request{}, ErrRecordingImportInvalid
		}
		return req, nil
	}
	at, err := time.Parse(time.RFC3339, l.StartedAt)
	if err != nil {
		return recordingimport.Request{}, ErrRecordingImportInvalidDate
	}
	req.StartedAt = at
	return req, nil
}

// fail maps a use case error to the page's text and logs the cause.
func (s *RecordingImportService) fail(err error) error {
	if errors.Is(err, context.Canceled) {
		return ErrRecordingImportUnavailable
	}
	known := []struct {
		err  error
		text recordingImportError
	}{
		{recording.ErrFutureLecture, ErrRecordingImportFuture},
		{recording.ErrEmptyAudio, ErrRecordingImportEmpty},
		{recording.ErrAudioTooLarge, ErrRecordingImportTooLarge},
		{recording.ErrAudioTooLong, ErrRecordingImportTooLong},
		{recording.ErrAudioDurationUnknown, ErrRecordingImportNoDuration},
		{recording.ErrUnsupportedAudio, ErrRecordingImportFormat},
		{recording.ErrNoDiskSpace, ErrRecordingImportNoSpace},
		{recordingimport.ErrInvalidRequest, ErrRecordingImportInvalid},
		{recordingimport.ErrLessonNotFound, ErrRecordingImportNoLesson},
		{recordingimport.ErrUnreadable, ErrRecordingImportUnreadable},
		{recordingimport.ErrChanged, ErrRecordingImportChanged},
		{recordingimport.ErrDeleting, ErrRecordingImportDeleting},
		{recording.ErrInvalidRecording, ErrRecordingImportInvalid},
	}
	for _, k := range known {
		if errors.Is(err, k.err) {
			s.log.Info(opRecordingImport, "result", k.err.Error())
			return k.text
		}
	}
	s.log.Error(opRecordingImport, "err", err)
	return ErrRecordingImportUnavailable
}

// extOf is the extension of a reference path without the dot; "" without one.
func extOf(p string) string {
	for i := len(p) - 1; i >= 0 && p[i] != '/'; i-- {
		if p[i] == '.' {
			return p[i+1:]
		}
	}
	return ""
}

// recordingPicker is the part of the platform shell that shows the dialog; only the Windows shell has it.
type recordingPicker interface {
	pickRecordingFile() (string, error)
}

// pickRecordingFile shows the open dialog of an old recording over the main window; "" when the user cancels.
func (s *Shell) pickRecordingFile(context.Context) (string, error) {
	u, err := s.current()
	if err != nil {
		return "", err
	}
	p, ok := u.(recordingPicker)
	if !ok {
		return "", ErrUnsupported
	}
	return p.pickRecordingFile()
}
