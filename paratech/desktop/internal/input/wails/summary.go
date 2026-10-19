package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/shared"
)

// summaryError is an error of the summary viewer: a fixed Russian text the page shows as it is.
type summaryError string

func (e summaryError) Error() string { return string(e) }

// Errors of the summary viewer (the page shows them as they are). Causes are logged, never shown, and neither paths
// nor summary text are logged.
const (
	ErrSummaryInvalidID   = summaryError("Неверный идентификатор записи.")
	ErrSummaryNotFound    = summaryError("Запись не найдена: обновите список.")
	ErrSummaryEmpty       = summaryError("Конспекта пока нет.")
	ErrSummaryFormat      = summaryError("Неизвестный формат файла.")
	ErrSummaryUnavailable = summaryError("Не удалось прочитать конспект. Попробуйте ещё раз.")
	ErrSummaryClipboard   = summaryError("Не удалось скопировать в буфер обмена. Попробуйте ещё раз.")
)

// Operation names of the log.
const (
	opSummaryView = "ui: summary view"
	opSummaryCopy = "ui: copy summary"
	opSummaryPick = "ui: choose where to save the summary"
	opSummarySave = "ui: save summary"
)

// Statuses and fixed texts of «Сохранить как…»; the statuses are those of the transcript.
const (
	msgSummarySaved     = "Конспект сохранён."
	msgSummaryCancelled = "Сохранение отменено."
	msgSummaryFailed    = "Не удалось сохранить файл. Выберите другую папку и попробуйте ещё раз."
)

// Fixed texts of the states (Russian UI).
const (
	msgSummaryNone        = "Конспекта пока нет: запись ещё не распознана."
	msgSummaryMissing     = "Конспекта нет."
	msgSummaryWaiting     = "Конспект ждёт очереди."
	msgSummaryRunning     = "Конспект составляется…"
	msgSummaryDone        = "Конспект готов."
	msgSummaryFailedText  = "Не удалось составить конспект."
	msgHomeworkNone       = "Домашние задания будут найдены после распознавания."
	msgHomeworkWaiting    = "Поиск домашних заданий ждёт очереди."
	msgHomeworkRunning    = "Ищем домашние задания…"
	msgHomeworkDone       = "Домашние задания найдены."
	msgHomeworkDoneNone   = "Домашних заданий в этом занятии не найдено."
	msgHomeworkFailedText = "Не удалось найти домашние задания."
	msgWaitRetry          = "Не получилось с первого раза, повторится само."
	msgWaitPower          = "Ноутбук на батарее: продолжится после подключения питания."
	msgWaitNetwork        = "Нет сети: продолжится, как только она появится."
	msgWaitQuota          = "Лимит обработки исчерпан: продолжится, когда он пополнится."
	msgWaitNotConfigured  = "Сервис конспектов не настроен: укажите адрес и ключ в «Подключения» → «Конспекты и задания»."
	msgWaitUnavailable    = "Сервис конспектов сейчас недоступен, повторим автоматически."
)

// SummaryUseCase is the summary viewer (ARCH-42); satisfied by summaryview.Service.
type SummaryUseCase interface {
	View(ctx context.Context, id shared.RecordingID) (summaryview.View, error)
	Text(ctx context.Context, id shared.RecordingID) (string, error)
	SuggestedName(ctx context.Context, id shared.RecordingID, f summaryexport.Format) (string, error)
	Export(ctx context.Context, id shared.RecordingID, f summaryexport.Format, path string) error
}

// SummaryFilePicker shows the native save dialog of a summary with name proposed, filtered to format ("txt" or "md").
// It returns the chosen path, or "" when the user cancels; the path never comes from the page.
type SummaryFilePicker interface {
	PickSummaryFile(ctx context.Context, name, format string) (string, error)
}

// summaryPickerFunc adapts a function to SummaryFilePicker.
type summaryPickerFunc func(ctx context.Context, name, format string) (string, error)

// PickSummaryFile implements SummaryFilePicker.
func (f summaryPickerFunc) PickSummaryFile(ctx context.Context, name, format string) (string, error) {
	return f(ctx, name, format)
}

// SummaryStep is where the summary or the homework search stands. State is "none", "waiting", "running", "done" or
// "failed"; WaitReason ("inLine", "retry", "power", "network", "quota", "serviceNotConfigured" or
// "serviceUnavailable") is set while waiting; Message is the fixed text of the state; JobError is the core's last
// error of a failed or retrying job ("" otherwise), never model text.
type SummaryStep struct {
	State      string `json:"state"`
	WaitReason string `json:"waitReason"`
	Message    string `json:"message"`
	JobError   string `json:"jobError"`
}

// SummaryContent is the summary and its state. Ready: Text holds a summary (it may be ready while a new run waits,
// runs or failed). Text and KeyPoints are model output: the page shows them as text only, never as HTML.
type SummaryContent struct {
	SummaryStep
	Ready     bool     `json:"ready"`
	Text      string   `json:"text"`
	KeyPoints []string `json:"keyPoints"`
	UpdatedAt string   `json:"updatedAt"` // RFC 3339 in UTC, "" unless ready
}

// SummaryAssignment is an assignment found in the lesson; ID opens it in «Домашние задания». Title and Description
// may be model output: text only. Due is RFC 3339 in UTC, "" without a deadline.
type SummaryAssignment struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Due         string `json:"due"`
	UserEdited  bool   `json:"userEdited"`
}

// SummaryHomework is the homework found in the lesson and the state of its search.
type SummaryHomework struct {
	SummaryStep
	Assignments []SummaryAssignment `json:"assignments"`
}

// SummaryView is the summary viewer of one recording. StartedAt is RFC 3339 in UTC; DurationSeconds is valid when
// HasDuration. Transcribed: the recording has a transcript.
type SummaryView struct {
	RecordingID     string          `json:"recordingId"`
	LessonID        string          `json:"lessonId"`
	Title           string          `json:"title"`
	StartedAt       string          `json:"startedAt"`
	DurationSeconds int64           `json:"durationSeconds"`
	HasDuration     bool            `json:"hasDuration"`
	Transcribed     bool            `json:"transcribed"`
	Summary         SummaryContent  `json:"summary"`
	Homework        SummaryHomework `json:"homework"`
}

// SummarySaveResult answers «Сохранить как…»: Status "saved", "cancelled" or "failed", a fixed text and the base name
// of the saved file ("" unless saved).
type SummarySaveResult struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Name    string `json:"name"`
}

// SummaryService is bound to the UI (the summary viewer): every exported method is callable from the page by its full
// name "<package path>.SummaryService.<Method>". Keep the method set to what the page may call. Nothing here calls
// the Hub: it reads the local results.
type SummaryService struct {
	uc        SummaryUseCase
	picker    SummaryFilePicker
	clipboard Clipboard
	log       *slog.Logger
}

// NewSummaryService returns the binding of the summary viewer; all arguments are required.
func NewSummaryService(uc SummaryUseCase, picker SummaryFilePicker, clipboard Clipboard, log *slog.Logger) *SummaryService {
	return &SummaryService{uc: uc, picker: picker, clipboard: clipboard, log: log}
}

// View returns the lesson, the summary with its key points, the homework found and where both stand. Works offline.
func (s *SummaryService) View(ctx context.Context, recordingID string) (SummaryView, error) {
	id, err := summaryRecordingID(recordingID)
	if err != nil {
		return SummaryView{}, err
	}
	v, err := s.uc.View(ctx, id)
	if err != nil {
		return SummaryView{}, s.fail(opSummaryView, err)
	}

	out := SummaryView{
		RecordingID: string(v.Recording), LessonID: string(v.Lesson), Title: v.Title, StartedAt: timeText(v.StartedAt),
		HasDuration: v.HasDuration, Transcribed: v.Transcribed,
		Summary: SummaryContent{
			SummaryStep: summaryStep(v.Summary.Step, summaryMessage(v.Summary.State, v.Summary.Wait, v.Transcribed)),
			Ready:       v.Summary.Ready, KeyPoints: []string{},
		},
		Homework: SummaryHomework{
			SummaryStep: summaryStep(v.Homework.Step, homeworkMessage(v.Homework)),
			Assignments: make([]SummaryAssignment, len(v.Homework.Assignments)),
		},
	}
	if v.HasDuration {
		out.DurationSeconds = int64(v.Duration.Seconds())
	}
	if v.Summary.Ready {
		out.Summary.Text, out.Summary.UpdatedAt = v.Summary.Content.Text(), timeText(v.Summary.UpdatedAt)
		out.Summary.KeyPoints = append(out.Summary.KeyPoints, v.Summary.Content.KeyPoints()...)
	}
	for i, a := range v.Homework.Assignments {
		out.Homework.Assignments[i] = SummaryAssignment{
			ID: string(a.ID), Title: a.Title, Description: a.Description, Due: timeText(a.Due), UserEdited: a.UserEdited,
		}
	}
	return out, nil
}

// CopyAll puts the summary with its key points and the homework found on the clipboard as plain text. Nothing leaves
// the computer.
func (s *SummaryService) CopyAll(ctx context.Context, recordingID string) error {
	id, err := summaryRecordingID(recordingID)
	if err != nil {
		return err
	}
	text, err := s.uc.Text(ctx, id)
	if err != nil {
		return s.fail(opSummaryCopy, err)
	}
	if err := s.clipboard.SetText(ctx, text); err != nil {
		s.log.Warn(opSummaryCopy, "err", err)
		return ErrSummaryClipboard
	}
	return nil
}

// SaveAs asks in the native save dialog where to save the summary as format ("txt" or "md") and writes it there. The
// dialog proposes "Конспект", the lesson title and the date as the name. Neither the path nor an error text reaches
// the page; the page gets the base name of the file.
func (s *SummaryService) SaveAs(ctx context.Context, recordingID, format string) (SummarySaveResult, error) {
	id, err := summaryRecordingID(recordingID)
	if err != nil {
		return SummarySaveResult{}, err
	}
	f := summaryexport.Format(format)
	if f.Validate() != nil {
		return SummarySaveResult{}, ErrSummaryFormat
	}

	// Checked before the dialog: nothing to save is an error of the page, not an empty file.
	if _, err := s.uc.Text(ctx, id); err != nil {
		return SummarySaveResult{}, s.fail(opSummarySave, err)
	}
	name, err := s.uc.SuggestedName(ctx, id, f)
	if err != nil {
		return SummarySaveResult{}, s.fail(opSummarySave, err)
	}
	path, err := s.picker.PickSummaryFile(ctx, name, format)
	switch {
	case err != nil:
		s.log.Warn(opSummaryPick, "err", err)
		return SummarySaveResult{Status: transcriptFailed, Message: msgSummaryFailed}, nil
	case path == "":
		return SummarySaveResult{Status: transcriptCancelled, Message: msgSummaryCancelled}, nil
	}

	err = s.uc.Export(ctx, id, f, path)
	switch {
	case errors.Is(err, summaryview.ErrNoSummary), errors.Is(err, summaryview.ErrRecordingNotFound):
		return SummarySaveResult{}, s.fail(opSummarySave, err)
	case err != nil:
		s.log.Error(opSummarySave, "err", err) // the adapters' errors carry neither the path nor the text
		return SummarySaveResult{Status: transcriptFailed, Message: msgSummaryFailed}, nil
	}
	return SummarySaveResult{Status: transcriptSaved, Message: msgSummarySaved, Name: baseName(path)}, nil
}

// fail maps a use case error to the page's text and logs the cause.
func (s *SummaryService) fail(op string, err error) error {
	switch {
	case errors.Is(err, summaryview.ErrRecordingNotFound):
		s.log.Info(op, "result", "not found")
		return ErrSummaryNotFound
	case errors.Is(err, summaryview.ErrNoSummary):
		return ErrSummaryEmpty
	case errors.Is(err, summaryview.ErrInvalidRequest):
		return ErrSummaryInvalidID
	}
	s.log.Error(op, "err", err)
	return ErrSummaryUnavailable
}

func summaryStep(st summaryview.Step, msg string) SummaryStep {
	return SummaryStep{State: string(st.State), WaitReason: string(st.Wait), Message: msg, JobError: st.JobError}
}

func summaryMessage(st summaryview.State, w summaryview.WaitReason, transcribed bool) string {
	switch st {
	case summaryview.StateWaiting:
		return waitMessage(w, msgSummaryWaiting)
	case summaryview.StateRunning:
		return msgSummaryRunning
	case summaryview.StateDone:
		return msgSummaryDone
	case summaryview.StateFailed:
		return msgSummaryFailedText
	}
	if transcribed {
		return msgSummaryMissing
	}
	return msgSummaryNone
}

func homeworkMessage(hw summaryview.Homework) string {
	switch hw.State {
	case summaryview.StateWaiting:
		return waitMessage(hw.Wait, msgHomeworkWaiting)
	case summaryview.StateRunning:
		return msgHomeworkRunning
	case summaryview.StateDone:
		if len(hw.Assignments) == 0 {
			return msgHomeworkDoneNone
		}
		return msgHomeworkDone
	case summaryview.StateFailed:
		return msgHomeworkFailedText
	}
	return msgHomeworkNone
}

// waitMessage explains a wait; waiting in line has the text of its step.
func waitMessage(w summaryview.WaitReason, inLine string) string {
	switch w {
	case summaryview.WaitRetry:
		return msgWaitRetry
	case summaryview.WaitPower:
		return msgWaitPower
	case summaryview.WaitNetwork:
		return msgWaitNetwork
	case summaryview.WaitQuota:
		return msgWaitQuota
	case summaryview.WaitServiceNotConfigured:
		return msgWaitNotConfigured
	case summaryview.WaitServiceUnavailable:
		return msgWaitUnavailable
	}
	return inLine
}

func summaryRecordingID(id string) (shared.RecordingID, error) {
	rid := shared.RecordingID(id)
	if rid.Validate() != nil {
		return "", ErrSummaryInvalidID
	}
	return rid, nil
}
