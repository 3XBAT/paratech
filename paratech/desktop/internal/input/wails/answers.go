package wails

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"paratech/desktop/internal/cases/answering"
	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/coursework/services/summaryexport"
	"paratech/desktop/internal/domain/shared"
)

// answerError is an error of the answer card: a fixed Russian text the page shows as it is.
type answerError string

func (e answerError) Error() string { return string(e) }

// Errors of the answer card. Causes are logged, never shown.
const (
	ErrAnswerNotFound    = answerError("Ответ не найден.")
	ErrAnswerUnavailable = answerError("Не удалось открыть ответ. Попробуйте ещё раз.")
)

const opAnswerCard = "ui: answer card"

// AnswersUseCase reads a stored answer with citations (cases/answering) for its card (ARCH-42).
type AnswersUseCase interface {
	Card(ctx context.Context, id shared.AnswerID) (answering.Card, error)
}

// AnswerView is the AnswerCard of the agent panel (TASK-118): the question, the claims in the reader's order and the
// parts the lessons do not answer. A claim with FromLessons shows its quotes as «из ваших лекций», each opening
// Sources[cite.source]; a claim without is shown apart, never as from the lessons. SubjectID is "" for a question
// about the whole base; CreatedAt is RFC 3339 in UTC. Every text is model or lesson text: plain text only, never HTML.
type AnswerView struct {
	ID        string         `json:"id"`
	Question  string         `json:"question"`
	SubjectID string         `json:"subjectId"`
	CreatedAt string         `json:"createdAt"`
	Claims    []AnswerClaim  `json:"claims"`
	Missing   []string       `json:"missing"`
	Sources   []AnswerSource `json:"sources"`
}

// AnswerClaim is one statement of the answer with the citations that passed the check.
type AnswerClaim struct {
	Text        string       `json:"text"`
	FromLessons bool         `json:"fromLessons"`
	Cites       []AnswerCite `json:"cites"`
}

// AnswerCite is a checked quote and the index of its place in AnswerView.Sources.
type AnswerCite struct {
	Quote  string `json:"quote"`
	Source int    `json:"source"`
}

// AnswerSource is the place a citation opens, as a KnowledgeHit: Section "summary", "lectureText" or "transcript";
// Index the summary text, the lecture text block or the first transcript segment; StartMs and Time ("12:05") where
// it starts in the recording, 0 and "" for the summary. Title ("" for an ad-hoc recording) and StartedAt (RFC 3339 in
// UTC, "" when unknown) name the lesson as it was when the answer was made.
//
// Section "ownNote" is the student's own note (ADR-0027): RecordingID, StartMs and Time are empty, NoteID and
// MaterialID open the note's file at LocatorKind ("page", "slide", "section", "fragment") LocatorN, and Label names
// the place («ваш конспект · стр. 3»); the note fields and Label are "" and 0 for the other sections. Removed is true
// for a note removed since the answer was made: Label is then «источник удалён» and the place does not open.
type AnswerSource struct {
	RecordingID string `json:"recordingId"`
	Section     string `json:"section"`
	Index       int    `json:"index"`
	StartMs     int64  `json:"startMs"`
	Time        string `json:"time"`
	Title       string `json:"title"`
	StartedAt   string `json:"startedAt"`
	NoteID      string `json:"noteId"`
	MaterialID  string `json:"materialId"`
	LocatorKind string `json:"locatorKind"`
	LocatorN    int    `json:"locatorN"`
	Label       string `json:"label"`
	Removed     bool   `json:"removed"`
}

// AnswerService is bound to the answer card of the agent panel; it only reads.
type AnswerService struct {
	core AnswersUseCase
	log  *slog.Logger
}

// NewAnswerService returns the binding; OpenServices checks the use case before calling it.
func NewAnswerService(core AnswersUseCase, log *slog.Logger) *AnswerService {
	return &AnswerService{core: core, log: log}
}

// Answer returns the answer with ID id for its card. Errors: ErrAnswerNotFound, ErrAnswerUnavailable.
func (s *AnswerService) Answer(ctx context.Context, id string) (AnswerView, error) {
	c, err := s.core.Card(ctx, shared.AnswerID(id))
	switch {
	case errors.Is(err, answering.ErrNotFound):
		return AnswerView{}, ErrAnswerNotFound
	case err != nil:
		s.log.Warn(opAnswerCard, "err", err)
		return AnswerView{}, ErrAnswerUnavailable
	}
	return answerView(c), nil
}

// answerView maps an answer; empty lists are [] for the page.
func answerView(c answering.Card) AnswerView {
	a := c.Answer
	out := AnswerView{
		ID: string(a.ID()), Question: a.Question(), SubjectID: string(a.Subject()),
		CreatedAt: a.CreatedAt().UTC().Format(time.RFC3339), Claims: []AnswerClaim{}, Missing: a.Missing(),
		Sources: []AnswerSource{},
	}
	for _, c := range a.Claims() {
		cl := AnswerClaim{Text: c.Text, FromLessons: c.Supported(), Cites: []AnswerCite{}}
		for _, ct := range c.Cites {
			cl.Cites = append(cl.Cites, AnswerCite{Quote: ct.Quote, Source: ct.Source})
		}
		out.Claims = append(out.Claims, cl)
	}
	for i, src := range a.Sources() {
		v := AnswerSource{
			RecordingID: string(src.Recording), Section: string(src.Section), Index: src.Index, StartMs: src.StartMs,
			Title: src.Title, NoteID: string(src.Note), MaterialID: string(src.Material),
			LocatorKind: string(src.Locator.Kind), LocatorN: src.Locator.N, Label: src.OwnNoteLabel(),
		}
		if i < len(c.Removed) && c.Removed[i] {
			v.Removed, v.Label = true, answer.SourceRemovedLabel
		}
		if src.Section == answer.SectionLectureText || src.Section == answer.SectionTranscript {
			v.Time = summaryexport.BlockTime(src.StartMs)
		}
		if !src.StartedAt.IsZero() {
			v.StartedAt = src.StartedAt.UTC().Format(time.RFC3339)
		}
		out.Sources = append(out.Sources, v)
	}
	if out.Missing == nil {
		out.Missing = []string{}
	}
	return out
}
