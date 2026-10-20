package wails

import (
	"context"
	"errors"
	"log/slog"

	"paratech/desktop/internal/cases/knowledgebase"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/shared"
)

// knowledgeError is an error of the knowledge base: a fixed Russian text the page shows as it is.
type knowledgeError string

func (e knowledgeError) Error() string { return string(e) }

// Errors of the knowledge base (the page shows them as they are). Causes are logged, never shown, and neither paths
// nor lesson text are logged.
const (
	ErrKnowledgeInvalidID       = knowledgeError("Неверный идентификатор записи.")
	ErrKnowledgeNotFound        = knowledgeError("Запись не найдена: обновите список.")
	ErrKnowledgeSubjectNotFound = knowledgeError("В этом предмете больше нет занятий: обновите список.")
	ErrKnowledgeQuery           = knowledgeError("Введите для поиска от 2 до 100 символов.")
	ErrKnowledgeLessonInfo      = knowledgeError("Слишком длинное название предмета или неизвестный тип занятия.")
	ErrKnowledgeNotFinished     = knowledgeError("Запись ещё идёт: поправить её можно после окончания.")
	ErrKnowledgeBusy            = knowledgeError("Запись сейчас меняется. Попробуйте ещё раз.")
	ErrKnowledgeUnavailable     = knowledgeError("Не удалось прочитать базу знаний. Попробуйте ещё раз.")
)

// Operation names of the log.
const (
	opKnowledgeSubjects = "ui: knowledge base subjects"
	opKnowledgeLessons  = "ui: knowledge base lessons"
	opKnowledgeLesson   = "ui: knowledge base lesson"
	opKnowledgeSearch   = "ui: knowledge base search"
	opKnowledgePick     = "ui: choose where to save the subject"
	opKnowledgeExport   = "ui: export subject"
	opKnowledgeCorrect  = "ui: set the subject and kind of a recording"
)

// Fixed texts of «Экспорт предмета» (Russian UI); the statuses are those of the transcript.
const (
	msgSubjectSaved     = "Предмет сохранён."
	msgSubjectCancelled = "Сохранение отменено."
	msgSubjectFailed    = "Не удалось сохранить файл. Выберите другую папку и попробуйте ещё раз."
)

// KnowledgeUseCase is the knowledge base (ARCH-42); satisfied by knowledgebase.Service.
type KnowledgeUseCase interface {
	Subjects(ctx context.Context) ([]kb.Subject, error)
	Lessons(ctx context.Context, key string) ([]kb.Lesson, error)
	Lesson(ctx context.Context, id shared.RecordingID) (kb.Lesson, error)
	Search(ctx context.Context, query string) (knowledgebase.SearchResult, error)
	SubjectFileName(ctx context.Context, key string) (string, error)
	ExportSubject(ctx context.Context, key, path string) error
	SetLessonInfo(ctx context.Context, id shared.RecordingID, subject, kind string) (kb.Lesson, error)
}

// KnowledgeFilePicker shows the native save dialog of an exported subject (Markdown) with name proposed. It returns
// the chosen path, or "" when the user cancels; the path never comes from the page.
type KnowledgeFilePicker interface {
	PickKnowledgeFile(ctx context.Context, name string) (string, error)
}

// knowledgePickerFunc adapts a function to KnowledgeFilePicker.
type knowledgePickerFunc func(ctx context.Context, name string) (string, error)

// PickKnowledgeFile implements KnowledgeFilePicker.
func (f knowledgePickerFunc) PickKnowledgeFile(ctx context.Context, name string) (string, error) {
	return f(ctx, name)
}

// KnowledgeSubject is one subject of the knowledge base. Key identifies it in Lessons and ExportSubject; "" with
// NoSubject is the group «Без предмета» (Name holds that text). Latest is the start of its newest lesson, RFC 3339 in
// UTC. Name is imported or typed text: plain text only.
type KnowledgeSubject struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Lessons   int    `json:"lessons"`
	Latest    string `json:"latest"`
	NoSubject bool   `json:"noSubject"`
}

// KnowledgeLesson is one lesson (recording) of the knowledge base; RecordingID opens it in the summary and transcript
// viewers. StartedAt is RFC 3339 in UTC (the page shows «19.10»); Title is "" for an ad-hoc recording (the page names
// it «Запись <дата>»); Subject is "" without one. Kind is "lecture", "seminar", "practice", "lab" or "". The *Edited
// flags mark the user's correction. The *Ready flags say what is stored for the newest transcript. Texts are imported,
// typed or model text: plain text only.
type KnowledgeLesson struct {
	RecordingID      string `json:"recordingId"`
	StartedAt        string `json:"startedAt"`
	DurationSeconds  int64  `json:"durationSeconds"`
	Title            string `json:"title"`
	Subject          string `json:"subject"`
	SubjectKey       string `json:"subjectKey"`
	Kind             string `json:"kind"`
	SubjectEdited    bool   `json:"subjectEdited"`
	KindEdited       bool   `json:"kindEdited"`
	TranscriptReady  bool   `json:"transcriptReady"`
	SummaryReady     bool   `json:"summaryReady"`
	LectureTextReady bool   `json:"lectureTextReady"`
}

// KnowledgeHit is one place the query was found. Section is "title", "summary", "lectureText" or "transcript". Index
// is the block index of the lecture text (SummaryView.lectureText.blocks), the segment index of the transcript
// (TranscriptService.Segments from it), for the summary i for section i (SummaryView.summary.sections) and n+j for
// key point j of a summary of n sections (so 0 for the text of a summary made before TASK-101), 0 for the title.
// StartMs is where the block or segment starts ("time": "12:05"), 0 and "" otherwise. Before, Match and After are the
// snippet: one plain line, "…" where it was cut; the page marks Match, as text only, never HTML.
type KnowledgeHit struct {
	RecordingID string          `json:"recordingId"`
	Section     string          `json:"section"`
	Index       int             `json:"index"`
	StartMs     int64           `json:"startMs"`
	Time        string          `json:"time"`
	Before      string          `json:"before"`
	Match       string          `json:"match"`
	After       string          `json:"after"`
	Lesson      KnowledgeLesson `json:"lesson"`
}

// KnowledgeSearchResult is what a search found, newest lessons first. Truncated: more places may exist than are
// listed (at most 3 per section of a lesson and 50 in all); the page asks for a narrower query.
type KnowledgeSearchResult struct {
	Hits      []KnowledgeHit `json:"hits"`
	Truncated bool           `json:"truncated"`
}

// KnowledgeBaseService is bound to the UI (the «База знаний» section): every exported method is callable from the page
// by its full name "<package path>.KnowledgeBaseService.<Method>". Keep the method set to what the page may call.
// Nothing here calls the network.
type KnowledgeBaseService struct {
	uc     KnowledgeUseCase
	picker KnowledgeFilePicker
	log    *slog.Logger
}

// NewKnowledgeBaseService returns the binding of the knowledge base; all arguments are required.
func NewKnowledgeBaseService(uc KnowledgeUseCase, picker KnowledgeFilePicker, log *slog.Logger) *KnowledgeBaseService {
	return &KnowledgeBaseService{uc: uc, picker: picker, log: log}
}

// Subjects lists the subjects with their lesson counts; «Без предмета» comes last.
func (s *KnowledgeBaseService) Subjects(ctx context.Context) ([]KnowledgeSubject, error) {
	list, err := s.uc.Subjects(ctx)
	if err != nil {
		return nil, s.fail(opKnowledgeSubjects, err)
	}
	out := make([]KnowledgeSubject, 0, len(list))
	for _, sub := range list {
		out = append(out, KnowledgeSubject{
			Key: sub.Key, Name: sub.Name, Lessons: sub.Lessons, Latest: timeText(sub.Latest), NoSubject: sub.NoSubject,
		})
	}
	return out, nil
}

// Lessons lists the lessons of the subject with key (KnowledgeSubject.Key), newest first; none is an empty list.
func (s *KnowledgeBaseService) Lessons(ctx context.Context, subjectKey string) ([]KnowledgeLesson, error) {
	list, err := s.uc.Lessons(ctx, subjectKey)
	if err != nil {
		return nil, s.fail(opKnowledgeLessons, err)
	}
	out := make([]KnowledgeLesson, 0, len(list))
	for _, l := range list {
		out = append(out, knowledgeLesson(l))
	}
	return out, nil
}

// Lesson returns one lesson, for the lesson page opened from a search or a link.
func (s *KnowledgeBaseService) Lesson(ctx context.Context, recordingID string) (KnowledgeLesson, error) {
	id, err := knowledgeRecordingID(recordingID)
	if err != nil {
		return KnowledgeLesson{}, err
	}
	l, err := s.uc.Lesson(ctx, id)
	if err != nil {
		return KnowledgeLesson{}, s.fail(opKnowledgeLesson, err)
	}
	return knowledgeLesson(l), nil
}

// Search finds query in the titles, summaries, lecture texts and transcripts of the whole base. Works offline.
func (s *KnowledgeBaseService) Search(ctx context.Context, query string) (KnowledgeSearchResult, error) {
	res, err := s.uc.Search(ctx, query)
	if err != nil {
		return KnowledgeSearchResult{}, s.fail(opKnowledgeSearch, err)
	}
	out := KnowledgeSearchResult{Hits: make([]KnowledgeHit, 0, len(res.Hits)), Truncated: res.Truncated}
	for _, h := range res.Hits {
		hit := KnowledgeHit{
			RecordingID: string(h.Recording), Section: string(h.Section), Index: h.Index, StartMs: h.StartMs,
			Before: h.Snippet.Before, Match: h.Snippet.Match, After: h.Snippet.After, Lesson: knowledgeLesson(h.Lesson),
		}
		if h.Section == kb.SectionLectureText || h.Section == kb.SectionTranscript {
			hit.Time = summaryexport.BlockTime(h.StartMs)
		}
		out.Hits = append(out.Hits, hit)
	}
	return out, nil
}

// ExportSubject asks in the native save dialog where to save the whole subject with key as one Markdown file and
// writes it there: its lessons by date with the summary and the lecture text. The dialog proposes the subject as the
// name. Neither the path nor an error text reaches the page; the page gets the base name of the file.
func (s *KnowledgeBaseService) ExportSubject(ctx context.Context, subjectKey string) (SummarySaveResult, error) {
	name, err := s.uc.SubjectFileName(ctx, subjectKey)
	if err != nil {
		return SummarySaveResult{}, s.fail(opKnowledgeExport, err)
	}
	path, err := s.picker.PickKnowledgeFile(ctx, name)
	switch {
	case err != nil:
		s.log.Warn(opKnowledgePick, "err", err)
		return SummarySaveResult{Status: transcriptFailed, Message: msgSubjectFailed}, nil
	case path == "":
		return SummarySaveResult{Status: transcriptCancelled, Message: msgSubjectCancelled}, nil
	}

	err = s.uc.ExportSubject(ctx, subjectKey, path)
	switch {
	case errors.Is(err, knowledgebase.ErrSubjectNotFound):
		return SummarySaveResult{}, s.fail(opKnowledgeExport, err)
	case err != nil:
		s.log.Error(opKnowledgeExport, "err", err) // the adapters' errors carry neither the path nor the text
		return SummarySaveResult{Status: transcriptFailed, Message: msgSubjectFailed}, nil
	}
	return SummarySaveResult{Status: transcriptSaved, Message: msgSubjectSaved, Name: baseName(path)}, nil
}

// SetLessonInfo sets the subject and kind ("lecture", "seminar", "practice", "lab" or "") of a recording; empty ones
// clear the correction. It returns the lesson as the base now shows it.
func (s *KnowledgeBaseService) SetLessonInfo(ctx context.Context, recordingID, subject, kind string) (KnowledgeLesson, error) {
	id, err := knowledgeRecordingID(recordingID)
	if err != nil {
		return KnowledgeLesson{}, err
	}
	l, err := s.uc.SetLessonInfo(ctx, id, subject, kind)
	if err != nil {
		return KnowledgeLesson{}, s.fail(opKnowledgeCorrect, err)
	}
	return knowledgeLesson(l), nil
}

// fail maps a use case error to the page's text and logs the cause.
func (s *KnowledgeBaseService) fail(op string, err error) error {
	switch {
	case errors.Is(err, knowledgebase.ErrRecordingNotFound):
		s.log.Info(op, "result", "not found")
		return ErrKnowledgeNotFound
	case errors.Is(err, knowledgebase.ErrSubjectNotFound):
		s.log.Info(op, "result", "subject not found")
		return ErrKnowledgeSubjectNotFound
	case errors.Is(err, knowledgebase.ErrInvalidQuery):
		return ErrKnowledgeQuery
	case errors.Is(err, knowledgebase.ErrInvalidLessonInfo):
		return ErrKnowledgeLessonInfo
	case errors.Is(err, knowledgebase.ErrNotFinished):
		return ErrKnowledgeNotFinished
	case errors.Is(err, knowledgebase.ErrBusy):
		return ErrKnowledgeBusy
	case errors.Is(err, knowledgebase.ErrInvalidRequest):
		return ErrKnowledgeInvalidID
	}
	s.log.Error(op, "err", err)
	return ErrKnowledgeUnavailable
}

func knowledgeLesson(l kb.Lesson) KnowledgeLesson {
	return KnowledgeLesson{
		RecordingID: string(l.Recording), StartedAt: timeText(l.StartedAt), DurationSeconds: int64(l.Duration.Seconds()),
		Title: l.Title, Subject: l.Subject, SubjectKey: l.SubjectKey(), Kind: l.Kind,
		SubjectEdited: l.SubjectEdited, KindEdited: l.KindEdited,
		TranscriptReady: l.Ready.Transcribed(), SummaryReady: l.Ready.Summary, LectureTextReady: l.Ready.LectureText,
	}
}

func knowledgeRecordingID(id string) (shared.RecordingID, error) {
	rid := shared.RecordingID(id)
	if rid.Validate() != nil {
		return "", ErrKnowledgeInvalidID
	}
	return rid, nil
}
