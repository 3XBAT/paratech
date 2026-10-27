package answering

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"paratech/desktop/internal/domain/coursework/answer"
	kb "paratech/desktop/internal/domain/coursework/services/knowledgebase"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log. The log never holds the question, a lesson's text or the reader's answer.
const (
	opAsked     = "answering: answer made"
	opReused    = "answering: answer found by its key"
	opNoPassage = "answering: no passage matches the question"
)

// Service answers questions from the student's lessons.
type Service struct {
	knowledge Knowledge
	reader    Reader
	answers   Repository
	clock     Clock
	ids       IDs
	log       *slog.Logger
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Knowledge == nil || d.Reader == nil || d.Answers == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every port except the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{knowledge: d.Knowledge, reader: d.Reader, answers: d.Answers, clock: d.Clock, ids: d.IDs, log: log}, nil
}

// Ask answers question from the lessons of a library subject, or of the whole base for a zero subject. The passages
// that best match it (kb.Passages) go to the reader as fragments with their places; its answer is parsed strictly, its
// citations are checked (answer.New) and the answer is stored. The same question about the same fragments in the same
// reuse scope (answer.NewRequest: e.g. one agent request) returns the stored answer without asking again; another
// scope asks afresh. No matching passage: Asked without an answer, nothing asked. A non-nil drafts gets the claim texts
// of the output while it is read (throttled, the last one once the read succeeded); the draft is never stored,
// returned or checked, only the whole output is.
//
// Errors: ErrInvalidQuestion, ErrNoConnection (asking again later repeats the same key, free if the Hub answered),
// ErrQuota, ErrReaderRefused, ErrUnavailable (also for an invalid scope: the caller's bug, not the student's input),
// or the error of ctx.
func (s *Service) Ask(ctx context.Context, question string, subject shared.SubjectID, scope string, drafts DraftSink) (Asked, error) {
	q, err := answer.ParseQuestion(question)
	if err != nil {
		return Asked{}, fmt.Errorf("%w: %w", ErrInvalidQuestion, err)
	}
	if subject != "" && subject.Validate() != nil {
		return Asked{}, fmt.Errorf("%w: subject id", ErrInvalidQuestion)
	}
	texts, err := s.knowledge.Texts(ctx, subject)
	if err != nil {
		return Asked{}, s.local(ctx, err)
	}
	passages := kb.Passages(q, texts)
	if len(passages) == 0 {
		s.log.InfoContext(ctx, opNoPassage, slog.String("subject", string(subject)), slog.Int("lessons", len(texts)))
		return Asked{}, nil
	}
	excerpts := make([]answer.Excerpt, 0, len(passages))
	for _, p := range passages {
		excerpts = append(excerpts, excerptOf(p))
	}
	req, err := answer.NewRequest(q, excerpts, scope)
	if err != nil {
		// The passages are cut to the reader's bounds and the scope is the caller's; a refusal here is a broken
		// invariant, not the student's input.
		return Asked{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	if got, ok, err := s.find(ctx, req.Key()); err != nil || ok {
		return got, err
	}
	var partial func(string)
	var dr *drafter
	if drafts != nil {
		dr = &drafter{sink: drafts, clock: s.clock}
		partial = dr.partial
	}
	raw, err := s.reader.AnswerQuestion(ctx, req, partial)
	switch {
	case err == nil:
		if dr != nil {
			dr.flush() // a read without a result discards its text: only a whole output gets the last draft
		}
	case ctx.Err() != nil:
		return Asked{}, ctx.Err()
	case errors.Is(err, shared.ErrQuotaExceeded):
		return Asked{}, fmt.Errorf("%w: %w", ErrQuota, err)
	case errors.Is(err, answer.ErrInvalidAnswer):
		return Asked{}, fmt.Errorf("%w: %w", ErrReaderRefused, err)
	default:
		return Asked{}, fmt.Errorf("%w: %w", ErrNoConnection, err)
	}
	rep, err := req.Parse(raw)
	if err != nil {
		return Asked{}, fmt.Errorf("%w: %w", ErrReaderRefused, err)
	}
	a, err := answer.New(s.ids.NewAnswerID(), subject, req, rep, s.clock.Now())
	if err != nil {
		return Asked{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	err = s.answers.Insert(ctx, a)
	switch {
	case errors.Is(err, shared.ErrDuplicate):
		// The same question was answered meanwhile (a replayed step): that answer stands.
		if got, ok, err := s.find(ctx, req.Key()); err != nil || ok {
			return got, err
		}
		return Asked{}, fmt.Errorf("%w: answer key taken but not found", ErrUnavailable)
	case err != nil:
		// The Hub keeps its answer under the key: asking again is free.
		return Asked{}, s.local(ctx, err)
	}
	c := a.Counts()
	s.log.InfoContext(ctx, opAsked, slog.String("answer", string(a.ID())), slog.Int("fragments", c.Fragments),
		slog.Int("claims", c.Claims), slog.Int("supported", c.Supported), slog.Int("missing", c.Missing))
	return Asked{Answer: a}, nil
}

// Answer returns a stored answer, for its card. Errors: ErrNotFound, ErrUnavailable.
func (s *Service) Answer(ctx context.Context, id shared.AnswerID) (*answer.Answer, error) {
	if id.Validate() != nil {
		return nil, ErrNotFound
	}
	a, err := s.answers.Get(ctx, id)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil, ErrNotFound
	case err != nil:
		return nil, s.local(ctx, err)
	}
	return a, nil
}

// find returns the stored answer of key; ok is false when there is none.
func (s *Service) find(ctx context.Context, key string) (Asked, bool, error) {
	a, err := s.answers.FindByKey(ctx, key)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return Asked{}, false, nil
	case err != nil:
		return Asked{}, false, s.local(ctx, err)
	}
	s.log.InfoContext(ctx, opReused, slog.String("answer", string(a.ID())))
	return Asked{Answer: a, Reused: true}, true, nil
}

// draftEvery bounds how often the partial output is scanned for its draft (answer.Draft reads it whole).
const draftEvery = 100 * time.Millisecond

// drafter turns the partial output into drafts for sink, passing only a changed draft. A scan runs when draftEvery
// has passed since the last one or the output has doubled since, so a stream of many small deltas costs a bounded
// number of whole scans, not one per delta (TASK-147); the output skipped meanwhile waits for flush.
type drafter struct {
	sink    DraftSink
	clock   Clock
	last    []string
	scanned int       // length of the output at the last scan; 0 before the first
	at      time.Time // time of the last scan
	pending string    // the output received since the last scan, "" none
}

func (d *drafter) partial(soFar string) {
	now := d.clock.Now()
	if d.scanned > 0 && len(soFar) < 2*d.scanned && now.Sub(d.at) < draftEvery {
		d.pending = soFar
		return
	}
	d.scan(soFar, now)
}

// flush scans the output skipped since the last scan, so the last draft is the whole output's.
func (d *drafter) flush() {
	if d.pending != "" {
		d.scan(d.pending, d.clock.Now())
	}
}

func (d *drafter) scan(soFar string, now time.Time) {
	d.pending, d.scanned, d.at = "", len(soFar), now
	texts := answer.Draft(soFar)
	if slices.Equal(texts, d.last) {
		return
	}
	d.last = texts
	d.sink.Draft(slices.Clone(texts))
}

// local wraps an error of the local store; a cancelled or timed-out ctx is returned as it is.
func (s *Service) local(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// excerptOf is a passage of the knowledge base as a fragment for the reader, with the place it opens. Only the text
// goes to the reader as data (answer.Request.UntrustedData); the place stays local.
func excerptOf(p kb.Passage) answer.Excerpt {
	src := answer.Source{
		Recording: p.Recording(), Section: sectionOf(p.Section), Index: p.Index, StartMs: p.StartMs,
		Title: p.Lesson.Title, StartedAt: p.Lesson.StartedAt,
	}
	if src.Section == answer.SectionOwnNote {
		src.Recording = "" // the note opens, not a recording the lesson may have
		src.Note, src.Material = p.Note, p.Material
		src.Locator = answer.Locator{Kind: answer.LocatorKind(p.Locator.Kind), N: p.Locator.N}
		if src.Locator.Kind == answer.LocatorSection {
			src.Heading = headingOf(p.Heading)
		}
	}
	return answer.Excerpt{Text: p.Text, Source: src}
}

// headingOf cuts a heading to what a source keeps.
func headingOf(s string) string {
	if r := []rune(s); len(r) > answer.MaxHeadingRunes {
		return strings.TrimSpace(string(r[:answer.MaxHeadingRunes-1])) + "…"
	}
	return s
}

func sectionOf(s kb.Section) answer.Section {
	switch s {
	case kb.SectionSummary:
		return answer.SectionSummary
	case kb.SectionLectureText:
		return answer.SectionLectureText
	case kb.SectionTranscript:
		return answer.SectionTranscript
	case kb.SectionOwnNote:
		return answer.SectionOwnNote
	}
	return "" // kb.Passages makes no other section; answer.NewRequest refuses it
}
