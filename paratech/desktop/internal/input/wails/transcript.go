package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"

	"paratech/desktop/internal/cases/transcriptview"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// transcriptError is an error of the transcript viewer: a fixed Russian text the page shows as it is.
type transcriptError string

func (e transcriptError) Error() string { return string(e) }

// Errors of the transcript viewer (the page shows them as they are). Causes are logged, never shown, and neither
// paths nor transcript text are logged.
const (
	ErrTranscriptInvalidID   = transcriptError("Неверный идентификатор записи.")
	ErrTranscriptNotFound    = transcriptError("Запись не найдена: обновите список.")
	ErrTranscriptEmpty       = transcriptError("Распознанного текста пока нет.")
	ErrTranscriptFormat      = transcriptError("Неизвестный формат файла.")
	ErrTranscriptPage        = transcriptError("Неверный номер фрагмента: обновите страницу.")
	ErrTranscriptUnavailable = transcriptError("Не удалось прочитать расшифровку. Попробуйте ещё раз.")
	ErrTranscriptClipboard   = transcriptError("Не удалось скопировать в буфер обмена. Попробуйте ещё раз.")
	ErrTranscriptCopyTooBig  = transcriptError("Выделен слишком большой фрагмент: скопируйте его частями или сохраните файл.")
)

// Operation names of the log.
const (
	opTranscriptStatus   = "ui: transcript status"
	opTranscriptSegments = "ui: transcript segments"
	opTranscriptText     = "ui: transcript text"
	opTranscriptCopy     = "ui: copy transcript"
	opTranscriptPick     = "ui: choose where to save the transcript"
	opTranscriptSave     = "ui: save transcript"
)

// Statuses and fixed texts of «Сохранить как…».
const (
	transcriptSaved     = "saved"
	transcriptCancelled = "cancelled"
	transcriptFailed    = "failed"

	msgTranscriptSaved     = "Расшифровка сохранена."
	msgTranscriptCancelled = "Сохранение отменено."
	msgTranscriptFailed    = "Не удалось сохранить файл. Выберите другую папку и попробуйте ещё раз."
)

// Fixed texts of the states (Russian UI). The running state has the percent filled in.
const (
	msgStateNone        = "Расшифровки пока нет: распознавание ещё не начиналось."
	msgStateWaiting     = "Распознавание ждёт очереди."
	msgStateRecognising = "Распознаётся… %d %%"
	msgStateDone        = "Расшифровка готова."
	msgStateFailed      = "Распознавание не удалось."
	msgStateFailedParts = "Распознавание не удалось. Показано то, что успело распознаться."
)

// maxCopyBytes bounds CopyText: far above a selection, below what the clipboard of a page should carry.
const maxCopyBytes = 16 << 20

// mediaRecordingPath is the path below which the asset server plays a recording: mediaRecordingPath + <id>.
const mediaRecordingPath = "/media/recording/"

// TranscriptUseCase is the transcript viewer (ARCH-42); satisfied by transcriptview.Service.
type TranscriptUseCase interface {
	Status(ctx context.Context, id shared.RecordingID) (transcriptview.Status, error)
	Segments(ctx context.Context, id shared.RecordingID, from, limit int) (transcriptview.Page, error)
	Text(ctx context.Context, id shared.RecordingID, withTimestamps bool) (string, error)
	SuggestedName(ctx context.Context, id shared.RecordingID, f transcript.ExportFormat) (string, error)
	Export(ctx context.Context, id shared.RecordingID, f transcript.ExportFormat, withTimestamps bool, path string) error
	OpenAudio(ctx context.Context, id shared.RecordingID) (transcriptview.Audio, error)
}

// TranscriptFilePicker shows the native save dialog of a transcript with name proposed, filtered to format ("txt" or
// "md"). It returns the chosen path, or "" when the user cancels; the path never comes from the page.
type TranscriptFilePicker interface {
	PickTranscriptFile(ctx context.Context, name, format string) (string, error)
}

// transcriptPickerFunc adapts a function to TranscriptFilePicker.
type transcriptPickerFunc func(ctx context.Context, name, format string) (string, error)

// PickTranscriptFile implements TranscriptFilePicker.
func (f transcriptPickerFunc) PickTranscriptFile(ctx context.Context, name, format string) (string, error) {
	return f(ctx, name, format)
}

// Clipboard puts text on the system clipboard. The shell does it natively: a page's navigator.clipboard needs focus
// and a permission in WebView2, the native call does not.
type Clipboard interface {
	SetText(ctx context.Context, text string) error
}

// clipboardFunc adapts a function to Clipboard.
type clipboardFunc func(ctx context.Context, text string) error

// SetText implements Clipboard.
func (f clipboardFunc) SetText(ctx context.Context, text string) error { return f(ctx, text) }

// TranscriptStatus is the head of the viewer. State is "none", "waiting", "recognising", "done" or "failed";
// Message is its fixed text. AudioURL plays the recording ("" when it has no track); add "#t=<seconds>" or set
// currentTime to start at a timestamp.
type TranscriptStatus struct {
	RecordingID string `json:"recordingId"`
	Title       string `json:"title"`
	StartedAt   string `json:"startedAt"` // RFC 3339 in UTC
	State       string `json:"state"`
	Percent     int    `json:"percent"`
	Message     string `json:"message"`
	Segments    int    `json:"segments"` // how many segments Segments pages through
	Final       bool   `json:"final"`    // the stored transcript; false: what was recognised so far
	AudioURL    string `json:"audioUrl"`
}

// TranscriptSegment is one segment: times in milliseconds from the start of the recording and Start as mm:ss or
// hh:mm:ss. Text is plain text, never HTML.
type TranscriptSegment struct {
	Index   int    `json:"index"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Start   string `json:"start"`
	Text    string `json:"text"`
}

// TranscriptPage is a page of segments from index From of Total.
type TranscriptPage struct {
	From     int                 `json:"from"`
	Total    int                 `json:"total"`
	Final    bool                `json:"final"`
	Segments []TranscriptSegment `json:"segments"`
}

// TranscriptSaveResult answers «Сохранить как…»: Status "saved", "cancelled" or "failed", a fixed text and the base
// name of the saved file ("" unless saved).
type TranscriptSaveResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Name    string `json:"name"`
}

// TranscriptService is bound to the UI (the transcript viewer): every exported method is callable from the page by its
// full name "<package path>.TranscriptService.<Method>". Keep the method set to what the page may call.
type TranscriptService struct {
	uc        TranscriptUseCase
	picker    TranscriptFilePicker
	clipboard Clipboard
	log       *slog.Logger
}

// NewTranscriptService returns the binding of the transcript viewer; all arguments are required.
func NewTranscriptService(uc TranscriptUseCase, picker TranscriptFilePicker, clipboard Clipboard, log *slog.Logger) *TranscriptService {
	return &TranscriptService{uc: uc, picker: picker, clipboard: clipboard, log: log}
}

// Status returns the title, the state of the recognition and how many segments there are. Works offline.
func (s *TranscriptService) Status(ctx context.Context, recordingID string) (TranscriptStatus, error) {
	id, err := transcriptRecordingID(recordingID)
	if err != nil {
		return TranscriptStatus{}, err
	}
	st, err := s.uc.Status(ctx, id)
	if err != nil {
		return TranscriptStatus{}, s.fail(opTranscriptStatus, err)
	}

	out := TranscriptStatus{
		RecordingID: string(st.Recording), Title: st.Title, StartedAt: timeText(st.StartedAt), State: string(st.State),
		Percent: st.Percent, Segments: st.Segments, Final: st.Final, Message: stateMessage(st),
	}
	if st.Playable {
		out.AudioURL = mediaRecordingPath + url.PathEscape(string(st.Recording))
	}
	return out, nil
}

// Segments returns at most limit segments from index from (limit is capped at 1000 by the core).
func (s *TranscriptService) Segments(ctx context.Context, recordingID string, from, limit int) (TranscriptPage, error) {
	id, err := transcriptRecordingID(recordingID)
	if err != nil {
		return TranscriptPage{}, err
	}
	if from < 0 || limit < 1 {
		return TranscriptPage{}, ErrTranscriptPage
	}
	p, err := s.uc.Segments(ctx, id, from, limit)
	if err != nil {
		return TranscriptPage{}, s.fail(opTranscriptSegments, err)
	}

	out := TranscriptPage{From: p.From, Total: p.Total, Final: p.Final, Segments: make([]TranscriptSegment, len(p.Segments))}
	for i, seg := range p.Segments {
		out.Segments[i] = TranscriptSegment{
			Index: p.From + i, StartMs: seg.StartMs, EndMs: seg.EndMs, Start: transcript.FormatOffset(seg.StartMs), Text: seg.Text,
		}
	}
	return out, nil
}

// Text returns the whole transcript as plain text, one segment per line, with "[mm:ss] " in front when
// withTimestamps is set.
func (s *TranscriptService) Text(ctx context.Context, recordingID string, withTimestamps bool) (string, error) {
	id, err := transcriptRecordingID(recordingID)
	if err != nil {
		return "", err
	}
	text, err := s.uc.Text(ctx, id, withTimestamps)
	if err != nil {
		return "", s.fail(opTranscriptText, err)
	}
	return text, nil
}

// CopyAll puts the whole transcript (as Text renders it) on the clipboard. Nothing leaves the computer.
func (s *TranscriptService) CopyAll(ctx context.Context, recordingID string, withTimestamps bool) error {
	text, err := s.Text(ctx, recordingID, withTimestamps)
	if err != nil {
		return err
	}
	return s.copy(ctx, text)
}

// CopyText puts text (the selection of the page) on the clipboard.
func (s *TranscriptService) CopyText(ctx context.Context, text string) error {
	if len(text) > maxCopyBytes {
		return ErrTranscriptCopyTooBig
	}
	return s.copy(ctx, text)
}

func (s *TranscriptService) copy(ctx context.Context, text string) error {
	if err := s.clipboard.SetText(ctx, text); err != nil {
		s.log.Warn(opTranscriptCopy, "err", err)
		return ErrTranscriptClipboard
	}
	return nil
}

// SaveAs asks in the native save dialog where to save the transcript as format ("txt" or "md") and writes it there.
// The dialog proposes the lesson title and date as the name. Neither the path nor an error text reaches the page; the
// page gets the base name of the file.
func (s *TranscriptService) SaveAs(ctx context.Context, recordingID, format string, withTimestamps bool) (TranscriptSaveResult, error) {
	id, err := transcriptRecordingID(recordingID)
	if err != nil {
		return TranscriptSaveResult{}, err
	}
	f := transcript.ExportFormat(format)
	if f.Validate() != nil {
		return TranscriptSaveResult{}, ErrTranscriptFormat
	}

	name, err := s.uc.SuggestedName(ctx, id, f)
	if err != nil {
		return TranscriptSaveResult{}, s.fail(opTranscriptSave, err)
	}
	path, err := s.picker.PickTranscriptFile(ctx, name, format)
	switch {
	case err != nil:
		s.log.Warn(opTranscriptPick, "err", err)
		return TranscriptSaveResult{Status: transcriptFailed, Message: msgTranscriptFailed}, nil
	case path == "":
		return TranscriptSaveResult{Status: transcriptCancelled, Message: msgTranscriptCancelled}, nil
	}

	err = s.uc.Export(ctx, id, f, withTimestamps, path)
	switch {
	case errors.Is(err, transcriptview.ErrNoTranscript), errors.Is(err, transcriptview.ErrRecordingNotFound):
		return TranscriptSaveResult{}, s.fail(opTranscriptSave, err)
	case err != nil:
		s.log.Error(opTranscriptSave, "err", err) // the adapters' errors carry neither the path nor the text
		return TranscriptSaveResult{Status: transcriptFailed, Message: msgTranscriptFailed}, nil
	}
	return TranscriptSaveResult{Status: transcriptSaved, Message: msgTranscriptSaved, Name: filepath.Base(path)}, nil
}

// fail maps a use case error to the page's text and logs the cause.
func (s *TranscriptService) fail(op string, err error) error {
	switch {
	case errors.Is(err, transcriptview.ErrRecordingNotFound):
		s.log.Info(op, "result", "not found")
		return ErrTranscriptNotFound
	case errors.Is(err, transcriptview.ErrNoTranscript):
		return ErrTranscriptEmpty
	case errors.Is(err, transcriptview.ErrInvalidRequest):
		return ErrTranscriptInvalidID
	}
	s.log.Error(op, "err", err)
	return ErrTranscriptUnavailable
}

// audio is what the media handler plays the recordings from.
func (s *TranscriptService) audio() AudioSource { return s.uc }

func stateMessage(st transcriptview.Status) string {
	switch st.State {
	case transcriptview.StateWaiting:
		return msgStateWaiting
	case transcriptview.StateRecognising:
		return fmt.Sprintf(msgStateRecognising, st.Percent)
	case transcriptview.StateDone:
		return msgStateDone
	case transcriptview.StateFailed:
		if st.Segments > 0 {
			return msgStateFailedParts
		}
		return msgStateFailed
	}
	return msgStateNone
}

func transcriptRecordingID(id string) (shared.RecordingID, error) {
	rid := shared.RecordingID(id)
	if rid.Validate() != nil {
		return "", ErrTranscriptInvalidID
	}
	return rid, nil
}
