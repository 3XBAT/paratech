package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
)

// Errors of the lecture text (the page shows them as they are); the others are those of the summary viewer.
const (
	ErrLectureTextEmpty       = summaryError("Текста лекции пока нет.")
	ErrLectureTextUnavailable = summaryError("Не удалось прочитать текст лекции. Попробуйте ещё раз.")
)

// Operation names of the log.
const (
	opLectureCopy = "ui: copy lecture text"
	opLecturePick = "ui: choose where to save the lecture text"
	opLectureSave = "ui: save lecture text"
)

// Fixed texts of the lecture text (Russian UI).
const (
	msgLectureNone     = "Текст лекции появится после распознавания."
	msgLectureMissing  = "Текст лекции ещё не составлялся."
	msgLectureWaiting  = "Текст лекции ждёт очереди."
	msgLectureRunning  = "Текст лекции составляется…"
	msgLectureParts    = "Текст лекции составляется: готово частей %d из %d."
	msgLectureDone     = "Текст лекции готов."
	msgLectureFailed   = "Не удалось составить текст лекции."
	msgLectureSaved    = "Текст лекции сохранён."
	lectureWaitInParts = "Текст лекции: готово частей %d из %d. %s"
)

// SummaryLectureBlock is one block of the lecture text: a heading, where it starts in the recording (seconds and
// "12:05" or "1:02:05") and its paragraphs. Heading and paragraphs are model output: text only, never HTML.
type SummaryLectureBlock struct {
	Heading      string   `json:"heading"`
	StartSeconds int64    `json:"startSeconds"`
	Time         string   `json:"time"`
	Paragraphs   []string `json:"paragraphs"`
}

// SummaryLectureText is the lecture text and the state of its job (TASK-090). Ready: Blocks hold the text (it may be
// ready while a new run waits, runs or failed). PartsDone of Parts are made while the job waits or runs (Parts 0:
// unknown). RetryWork is "lectureText" when ActivityService.RetryPostProcessing may start the lecture text: after a
// failure, or when it was never made (CanStart, a transcript older than the lecture text); "" otherwise.
type SummaryLectureText struct {
	SummaryStep
	Ready     bool                  `json:"ready"`
	Blocks    []SummaryLectureBlock `json:"blocks"`
	UpdatedAt string                `json:"updatedAt"` // RFC 3339 in UTC, "" unless ready
	PartsDone int                   `json:"partsDone"`
	Parts     int                   `json:"parts"`
	CanStart  bool                  `json:"canStart"`
	RetryWork string                `json:"retryWork"`
}

// lectureTextView is the JSON form of the lecture text of a recording.
func lectureTextView(l summaryview.LectureText, transcribed bool) SummaryLectureText {
	out := SummaryLectureText{
		SummaryStep: summaryStep(l.Step, lectureMessage(l, transcribed)), Ready: l.Ready,
		Blocks: make([]SummaryLectureBlock, 0, len(l.Blocks)), PartsDone: l.PartsDone, Parts: l.Parts, CanStart: l.CanStart,
	}
	if l.Ready {
		out.UpdatedAt = timeText(l.UpdatedAt)
	}
	for _, b := range l.Blocks {
		out.Blocks = append(out.Blocks, SummaryLectureBlock{
			Heading: b.Heading(), StartSeconds: b.StartMs() / 1000, Time: summaryexport.BlockTime(b.StartMs()),
			Paragraphs: b.Paragraphs(),
		})
	}
	if l.CanStart || l.State == summaryview.StateFailed {
		out.RetryWork = string(homeworkflow.WorkLectureText)
	}
	return out
}

func lectureMessage(l summaryview.LectureText, transcribed bool) string {
	switch l.State {
	case summaryview.StateWaiting:
		msg := waitMessage(l.Wait, msgLectureWaiting)
		if l.Parts > 0 && l.PartsDone > 0 {
			return fmt.Sprintf(lectureWaitInParts, l.PartsDone, l.Parts, msg)
		}
		return msg
	case summaryview.StateRunning:
		if l.Parts > 0 {
			return fmt.Sprintf(msgLectureParts, l.PartsDone, l.Parts)
		}
		return msgLectureRunning
	case summaryview.StateDone:
		return msgLectureDone
	case summaryview.StateFailed:
		return msgLectureFailed
	case summaryview.StateNoSpeech:
		return msgNoSpeech
	}
	if transcribed {
		return msgLectureMissing
	}
	return msgLectureNone
}

// LectureTextService is bound to the UI (the lecture text on the summary page, TASK-090): copy and «Сохранить как…»
// of the lecture text; the page reads it with SummaryService.View and starts it with
// ActivityService.RetryPostProcessing(recordingId, "lectureText"). Nothing here calls the Hub.
type LectureTextService struct {
	uc        SummaryUseCase
	picker    SummaryFilePicker
	clipboard Clipboard
	log       *slog.Logger
}

// NewLectureTextService returns the binding of the lecture text; all arguments are required.
func NewLectureTextService(uc SummaryUseCase, picker SummaryFilePicker, clipboard Clipboard, log *slog.Logger) *LectureTextService {
	return &LectureTextService{uc: uc, picker: picker, clipboard: clipboard, log: log}
}

// Copy puts the lecture text on the clipboard as plain text; times adds the start of each block ("[12:05]").
func (s *LectureTextService) Copy(ctx context.Context, recordingID string, times bool) error {
	id, err := summaryRecordingID(recordingID)
	if err != nil {
		return err
	}
	text, err := s.uc.LectureText(ctx, id, times)
	if err != nil {
		return s.fail(opLectureCopy, err)
	}
	if err := s.clipboard.SetText(ctx, text); err != nil {
		s.log.Warn(opLectureCopy, "err", err)
		return ErrSummaryClipboard
	}
	return nil
}

// SaveAs asks in the native save dialog where to save the lecture text as format ("txt" or "md") and writes it
// there; times adds the start of each block. The dialog proposes the lesson title, "текст лекции" and the date. Neither
// the path nor an error text reaches the page; the page gets the base name of the file.
func (s *LectureTextService) SaveAs(ctx context.Context, recordingID, format string, times bool) (SummarySaveResult, error) {
	id, err := summaryRecordingID(recordingID)
	if err != nil {
		return SummarySaveResult{}, err
	}
	f := summaryexport.Format(format)
	if f.Validate() != nil {
		return SummarySaveResult{}, ErrSummaryFormat
	}

	// Checked before the dialog: nothing to save is an error of the page, not an empty file.
	if _, err := s.uc.LectureText(ctx, id, times); err != nil {
		return SummarySaveResult{}, s.fail(opLectureSave, err)
	}
	name, err := s.uc.LectureSuggestedName(ctx, id, f)
	if err != nil {
		return SummarySaveResult{}, s.fail(opLectureSave, err)
	}
	path, err := s.picker.PickSummaryFile(ctx, name, format)
	switch {
	case err != nil:
		s.log.Warn(opLecturePick, "err", err)
		return SummarySaveResult{Status: transcriptFailed, Message: msgSummaryFailed}, nil
	case path == "":
		return SummarySaveResult{Status: transcriptCancelled, Message: msgSummaryCancelled}, nil
	}

	err = s.uc.ExportLectureText(ctx, id, f, times, path)
	switch {
	case errors.Is(err, summaryview.ErrNoLectureText), errors.Is(err, summaryview.ErrRecordingNotFound):
		return SummarySaveResult{}, s.fail(opLectureSave, err)
	case err != nil:
		s.log.Error(opLectureSave, "err", err) // the adapters' errors carry neither the path nor the text
		return SummarySaveResult{Status: transcriptFailed, Message: msgSummaryFailed}, nil
	}
	return SummarySaveResult{Status: transcriptSaved, Message: msgLectureSaved, Name: baseName(path)}, nil
}

// fail maps a use case error to the page's text and logs the cause.
func (s *LectureTextService) fail(op string, err error) error {
	switch {
	case errors.Is(err, summaryview.ErrRecordingNotFound):
		s.log.Info(op, "result", "not found")
		return ErrSummaryNotFound
	case errors.Is(err, summaryview.ErrNoLectureText):
		return ErrLectureTextEmpty
	case errors.Is(err, summaryview.ErrInvalidRequest):
		return ErrSummaryInvalidID
	}
	s.log.Error(op, "err", err)
	return ErrLectureTextUnavailable
}
