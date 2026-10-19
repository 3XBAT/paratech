package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/domain/recording/services/deletion"
	"paratech/desktop/internal/domain/shared"
)

// deleteError is an error of the recording deletion: a fixed Russian text the page shows as it is.
type deleteError string

func (e deleteError) Error() string { return string(e) }

// Errors of the recording deletion (the page shows them as they are). Causes are logged, never shown.
const (
	ErrDeleteInvalidID   = deleteError("Неверный идентификатор записи.")
	ErrDeleteNotFound    = deleteError("Запись не найдена: обновите список.")
	ErrDeleteInProgress  = deleteError("Запись ещё идёт: сначала остановите её.")
	ErrDeleteRecovering  = deleteError("Запись восстанавливается после сбоя. Попробуйте через минуту.")
	ErrDeleteChoice      = deleteError("Список найденных заданий изменился: откройте удаление заново.")
	ErrDeleteTooMany     = deleteError("Слишком много записей сразу: выберите не больше 100.")
	ErrDeleteUnavailable = deleteError("Не удалось удалить запись. Попробуйте ещё раз.")
)

// Operation names of the log.
const (
	opDeletePreview = "ui: preview recording deletion"
	opDelete        = "ui: delete recordings"
)

// Fixed texts of the deletion (Russian UI).
const (
	msgDeleteSound       = "Звук записи"
	msgDeleteTranscript  = "Расшифровка"
	msgDeleteSummary     = "Краткое содержание"
	msgDeleteLectureText = "Текст лекции"
	msgDeleteJobs        = "Незавершённые задачи обработки: %d"
	msgDeleted           = "Удалено %d"
	msgDeletePending     = "Удалено %d. Часть файлов удалится при следующем запуске."
	msgRefusalInProgress = "Запись ещё идёт: сначала остановите её."
	msgRefusalRecovering = "Запись восстанавливается после сбоя. Попробуйте через минуту."
)

// RecordingDeleteUseCase is the deletion of recordings (ARCH-42); satisfied by recordingdelete.Service.
type RecordingDeleteUseCase interface {
	Preview(ctx context.Context, id shared.RecordingID) (recordingdelete.Preview, error)
	DeleteMany(ctx context.Context, reqs []recordingdelete.Request) (recordingdelete.Batch, error)
}

// DeleteAssignment is a homework assignment found in a recording. Class is "plain" (offered checked), "draft" (has a
// draft: shown apart, deleted only when checked) or "emails" (emails were sent: it stays in «Домашние задания»,
// never deleted). Title is model output: plain text only.
type DeleteAssignment struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Class string `json:"class"`
}

// DeleteRecording is one recording of the confirmation. Title is the lesson title (imported text, plain text only),
// "" for an ad-hoc recording (the page titles it «Запись <дата, время>» from startedAt). Deletable is false while it
// is in progress or recovered; Refusal is then "in_progress" or "recovering" and RefusalMessage its text. Deletes
// lists what goes, in Russian («без возможности восстановления» is the page's).
type DeleteRecording struct {
	ID             string             `json:"id"`
	Title          string             `json:"title"`
	StartedAt      string             `json:"startedAt"` // RFC 3339 in UTC
	Deletable      bool               `json:"deletable"`
	Refusal        string             `json:"refusal"`
	RefusalMessage string             `json:"refusalMessage"`
	Sound          bool               `json:"sound"`
	Transcript     bool               `json:"transcript"`
	Summary        bool               `json:"summary"`
	LectureText    bool               `json:"lectureText"`
	Jobs           int                `json:"jobs"`
	Deletes        []string           `json:"deletes"`
	Assignments    []DeleteAssignment `json:"assignments"`
}

// DeletePreview answers Preview: the recordings found and the ids that are gone (deleted meanwhile).
type DeletePreview struct {
	Recordings []DeleteRecording `json:"recordings"`
	Missing    []string          `json:"missing"`
}

// DeleteRequest deletes one recording with the found assignments the user checked.
type DeleteRequest struct {
	RecordingID   string   `json:"recordingId"`
	AssignmentIDs []string `json:"assignmentIds"`
}

// DeleteFailure is a recording that was not deleted, with the text to show.
type DeleteFailure struct {
	RecordingID string `json:"recordingId"`
	Message     string `json:"message"`
}

// DeleteResult answers Delete. Deleted recordings are gone; Pending ones are gone from the lists and the next start
// finishes them (a job did not stop in time, a file was busy). Removed = Deleted + Pending and Message is «Удалено N».
// KeptAssignments are the found assignments that stay in «Домашние задания».
type DeleteResult struct {
	Removed         int             `json:"removed"`
	Deleted         int             `json:"deleted"`
	Pending         int             `json:"pending"`
	Message         string          `json:"message"`
	Failures        []DeleteFailure `json:"failures"`
	KeptAssignments []string        `json:"keptAssignments"`
}

// RecordingDeleteService is bound to the UI (the «⋯» menu of a recording row, the summary screen and «Удалить
// выбранные»): every exported method is callable from the page by its full name
// "<package path>.RecordingDeleteService.<Method>". Keep the method set to what the page may call.
type RecordingDeleteService struct {
	uc  RecordingDeleteUseCase
	log *slog.Logger
}

// NewRecordingDeleteService returns the binding of the recording deletion; both arguments are required.
func NewRecordingDeleteService(uc RecordingDeleteUseCase, log *slog.Logger) *RecordingDeleteService {
	return &RecordingDeleteService{uc: uc, log: log}
}

// Preview returns, for the confirmation, what deleting each recording removes and the found homework. Works offline.
func (s *RecordingDeleteService) Preview(ctx context.Context, recordingIDs []string) (DeletePreview, error) {
	ids, err := deleteIDs(recordingIDs)
	if err != nil {
		return DeletePreview{}, err
	}
	out := DeletePreview{Recordings: []DeleteRecording{}, Missing: []string{}}
	for _, id := range ids {
		p, err := s.uc.Preview(ctx, id)
		switch {
		case errors.Is(err, recordingdelete.ErrNotFound):
			out.Missing = append(out.Missing, string(id))
			continue
		case err != nil:
			return DeletePreview{}, s.fail(opDeletePreview, err)
		}
		out.Recordings = append(out.Recordings, deleteRecordingView(p))
	}
	return out, nil
}

// Delete deletes the recordings with the checked assignments, one confirmation for all. A recording that cannot be
// deleted is listed in Failures; the others go on.
func (s *RecordingDeleteService) Delete(ctx context.Context, requests []DeleteRequest) (DeleteResult, error) {
	if len(requests) > recordingdelete.MaxBatch {
		return DeleteResult{}, ErrDeleteTooMany
	}
	reqs := make([]recordingdelete.Request, 0, len(requests))
	for _, r := range requests {
		id := shared.RecordingID(r.RecordingID)
		if id.Validate() != nil {
			return DeleteResult{}, ErrDeleteInvalidID
		}
		req := recordingdelete.Request{Recording: id}
		for _, a := range r.AssignmentIDs {
			if shared.AssignmentID(a).Validate() != nil {
				return DeleteResult{}, ErrDeleteChoice
			}
			req.Assignments = append(req.Assignments, shared.AssignmentID(a))
		}
		reqs = append(reqs, req)
	}

	b, err := s.uc.DeleteMany(ctx, reqs)
	if err != nil {
		return DeleteResult{}, s.fail(opDelete, err)
	}
	out := DeleteResult{Failures: []DeleteFailure{}, KeptAssignments: []string{}}
	for _, r := range b.Results {
		switch r.Outcome {
		case recordingdelete.OutcomeDeleted:
			out.Deleted++
		case recordingdelete.OutcomePending:
			out.Pending++
		}
		for _, a := range r.Kept {
			out.KeptAssignments = append(out.KeptAssignments, string(a))
		}
	}
	for _, f := range b.Failures {
		out.Failures = append(out.Failures, DeleteFailure{
			RecordingID: string(f.Recording), Message: s.fail(opDelete, f.Err).Error(),
		})
	}
	out.Removed = out.Deleted + out.Pending
	out.Message = fmt.Sprintf(msgDeleted, out.Removed)
	if out.Pending > 0 {
		out.Message = fmt.Sprintf(msgDeletePending, out.Removed)
	}
	return out, nil
}

// deleteIDs validates the ids of a preview, at most recordingdelete.MaxBatch.
func deleteIDs(raw []string) ([]shared.RecordingID, error) {
	if len(raw) > recordingdelete.MaxBatch {
		return nil, ErrDeleteTooMany
	}
	ids := make([]shared.RecordingID, 0, len(raw))
	for _, r := range raw {
		id := shared.RecordingID(r)
		if id.Validate() != nil {
			return nil, ErrDeleteInvalidID
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func deleteRecordingView(p recordingdelete.Preview) DeleteRecording {
	c := p.Contents
	out := DeleteRecording{
		ID: string(p.Recording), Title: p.Title, StartedAt: timeText(p.StartedAt), Deletable: p.Refusal == recordingdelete.RefusalNone,
		Refusal: string(p.Refusal), Sound: c.Sound, Transcript: c.Transcripts > 0 || c.Parts > 0, Summary: c.Summaries > 0,
		LectureText: c.LectureTexts > 0, Jobs: c.Jobs, Deletes: []string{}, Assignments: make([]DeleteAssignment, 0, len(c.Assignments)),
	}
	switch p.Refusal {
	case recordingdelete.RefusalInProgress:
		out.RefusalMessage = msgRefusalInProgress
	case recordingdelete.RefusalRecovering:
		out.RefusalMessage = msgRefusalRecovering
	}
	for _, item := range []struct {
		on   bool
		text string
	}{
		{out.Sound, msgDeleteSound}, {out.Transcript, msgDeleteTranscript}, {out.Summary, msgDeleteSummary},
		{out.LectureText, msgDeleteLectureText}, {out.Jobs > 0, fmt.Sprintf(msgDeleteJobs, out.Jobs)},
	} {
		if item.on {
			out.Deletes = append(out.Deletes, item.text)
		}
	}
	for _, a := range c.Assignments {
		out.Assignments = append(out.Assignments, DeleteAssignment{ID: string(a.ID), Title: a.Title, Class: string(a.Class())})
	}
	return out
}

// fail logs the cause and returns the text for the page.
func (s *RecordingDeleteService) fail(op string, err error) error {
	var out deleteError
	switch {
	case errors.Is(err, recordingdelete.ErrNotFound):
		out = ErrDeleteNotFound
	case errors.Is(err, recordingdelete.ErrInProgress):
		out = ErrDeleteInProgress
	case errors.Is(err, recordingdelete.ErrRecovering):
		out = ErrDeleteRecovering
	case errors.Is(err, deletion.ErrUnknownAssignment), errors.Is(err, recordingdelete.ErrInvalidRequest):
		out = ErrDeleteChoice
	default:
		out = ErrDeleteUnavailable
	}
	s.log.Warn(op, "err", err)
	return out
}
