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
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log. The log never holds the question, a lesson's text or the reader's answer.
const (
	opAsked     = "answering: answer made"
	opReused    = "answering: answer found by its key"
	opRetried   = "answering: read retried after a transient failure"
	opNoPassage = "answering: no passage matches the question"
	opNoteCheck = "answering: note of a citation not checked, shown as there"
)

// Service answers questions from the student's lessons.
type Service struct {
	knowledge Knowledge
	reader    Reader
	answers   Repository
	notes     Notes // nil: no citation shows as removed
	clock     Clock
	sleeper   Sleeper // nil: AskNow does not retry
	ids       IDs
	log       *slog.Logger
}

// New checks that every port is given.
func New(d Deps) (*Service, error) {
	if d.Knowledge == nil || d.Reader == nil || d.Answers == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every port except Notes, Sleeper and the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{knowledge: d.Knowledge, reader: d.Reader, answers: d.Answers, notes: d.Notes, clock: d.Clock, sleeper: d.Sleeper, ids: d.IDs,
		log: log}, nil
}

// Ask answers question from the lessons of a library subject, or of the whole base for a zero subject. The passages
// that best match it (kb.Passages) go to the reader as fragments with their places; its answer is parsed strictly, its
// citations are checked (answer.New) and the answer is stored. The same question about the same fragments in the same
// reuse scope (answer.NewRequest: e.g. one agent call) returns the stored answer without asking again; another
// scope asks afresh. No matching passage: Asked without an answer, nothing asked. A non-nil drafts gets the claim texts
// of the output while it is read (throttled, the last one once the read succeeded); the draft is never stored,
// returned or checked, only the whole output is.
//
// Ask reads once: a job run (tickets) keeps the job queue's backoff. A question the student waits for uses AskNow.
//
// Errors: ErrInvalidQuestion, ErrNoConnection (asking again later repeats the same key, free if the Hub answered),
// ErrQuota, ErrReaderRefused, ErrUnavailable (also for an invalid scope: the caller's bug, not the student's input),
// or the error of ctx.
func (s *Service) Ask(ctx context.Context, question string, subject shared.SubjectID, scope string, drafts DraftSink) (Asked, error) {
	return s.ask(ctx, question, subject, scope, drafts, false)
}

// AskNow is Ask for a question the student waits for (the agent's answer_question, ADR-0026 addendum 2026-11-08): a
// transient failure to reach the Hub is retried by itself after backoff.Interactive (5 s, 10 s, 20 s) under the SAME
// key, an answer stored meanwhile found first, so a result the Hub kept is replayed free. Never retried: the quota, a
// refused or unusable answer, an invalid request, a Hub that is not set up, a permanent failure, the local store, ctx
// gone; never without Sleeper. Each retry starts a fresh draft: its first draft replaces the shown one. ctx ending
// during a wait returns its error at once. Errors: those of Ask, ErrNoConnection after the last retry.
func (s *Service) AskNow(ctx context.Context, question string, subject shared.SubjectID, scope string, drafts DraftSink) (Asked, error) {
	return s.ask(ctx, question, subject, scope, drafts, true)
}

// ask builds the request once, so every attempt has the same key, and reads it; retry allows the automatic retries
// of AskNow.
func (s *Service) ask(ctx context.Context, question string, subject shared.SubjectID, scope string, drafts DraftSink,
	retry bool,
) (Asked, error) {
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

	for attempt := 1; ; attempt++ {
		got, err := s.once(ctx, req, subject, drafts)
		if err == nil {
			return got, nil
		}
		wait, ok := s.retryable(ctx, retry, err, attempt)
		if !ok {
			return Asked{}, err
		}
		s.log.WarnContext(ctx, opRetried, slog.Int("attempt", attempt), slog.Duration("wait", wait), slog.Any("cause", err))
		if serr := s.sleeper.Sleep(ctx, wait); serr != nil {
			if ctx.Err() != nil {
				return Asked{}, ctx.Err()
			}
			return Asked{}, err
		}
	}
}

// retryable tells whether AskNow retries the failure err of attempt (1 for the first read) and after which wait: only
// a transient ErrNoConnection while ctx lives (see AskNow).
func (s *Service) retryable(ctx context.Context, retry bool, err error, attempt int) (time.Duration, bool) {
	if !retry || s.sleeper == nil || ctx.Err() != nil || !errors.Is(err, ErrNoConnection) ||
		errors.Is(err, shared.ErrServiceNotConfigured) || backoff.Classify(err) != backoff.ClassTransient {
		return 0, false
	}
	return backoff.Interactive(attempt)
}

// once is one attempt at req: the answer stored under its key, else one read with a fresh draft, parsed, checked and
// stored.
func (s *Service) once(ctx context.Context, req answer.Request, subject shared.SubjectID, drafts DraftSink) (Asked, error) {
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
		slog.Int("claims", c.Claims), slog.Int("supported", c.Supported), slog.Int("missing", c.Missing),
		slog.Int("dropped", rep.Dropped))
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

// Card returns a stored answer for its card with the sources of notes removed since it was made (Card.Removed), so
// such a citation shows «источник удалён» (ADR-0027). A note that cannot be checked is logged and shown as there.
// Errors: those of Answer.
func (s *Service) Card(ctx context.Context, id shared.AnswerID) (Card, error) {
	a, err := s.Answer(ctx, id)
	if err != nil {
		return Card{}, err
	}
	sources := a.Sources()
	removed := make([]bool, len(sources))
	if s.notes == nil {
		return Card{Answer: a, Removed: removed}, nil
	}
	active := map[shared.NoteID]bool{}
	for i, src := range sources {
		if src.Section != answer.SectionOwnNote {
			continue
		}
		ok, seen := active[src.Note]
		if !seen {
			if ok, err = s.notes.NoteActive(ctx, src.Note); err != nil {
				s.log.WarnContext(ctx, opNoteCheck, slog.String("answer", string(id)), slog.String("note", string(src.Note)),
					slog.Any("cause", err))
				ok = true
			}
			active[src.Note] = ok
		}
		removed[i] = !ok
	}
	return Card{Answer: a, Removed: removed}, nil
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
