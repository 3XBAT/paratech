package turnkey

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log. The log never holds a text of the assignment or of the reader.
const (
	opTriaged  = "turnkey: reader answered"
	opRefused  = "turnkey: reader answer refused"
	opMaterial = "turnkey: material text not read"
	opSaveLost = "turnkey: work changed while saved"
)

// saveAttempts bounds the saves of one change that lose against another save of the same work.
const saveAttempts = 3

// Service is the turnkey work use case.
type Service struct {
	works       WorkRepository
	assignments Assignments
	materials   MaterialTexts
	reader      Reader
	advisor     StepAdvisor
	clock       Clock
	ids         IDs
	log         *slog.Logger
}

// New checks that every required port is given (see Deps).
func New(d Deps) (*Service, error) {
	if d.Works == nil || d.Assignments == nil || d.Materials == nil || d.Reader == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%w: every port except the advisor and the logger is required", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		works: d.Works, assignments: d.Assignments, materials: d.Materials, reader: d.Reader, advisor: d.Advisor,
		clock: d.Clock, ids: d.IDs, log: log,
	}, nil
}

// Triage starts the work of an assignment or an uploaded file and asks the reader (a paid call, round 1). A source
// that has a work already (not failed) gets it back with Created false: its due call is asked again if it has one,
// else nothing is paid. The call is idempotent per work, round and attempt: a repeat after a lost answer is replayed
// by the Hub for free.
//
// A failed reader call keeps the work with its call due: the view comes back with the error. Errors:
// ErrInvalidRequest, ErrSourceNotFound, ErrNoText, ErrReaderUnavailable, ErrQuota, ErrReaderRefused, ErrUnavailable.
func (s *Service) Triage(ctx context.Context, src work.Source) (Started, error) {
	w, created, err := s.workOf(ctx, src)
	if err != nil {
		return Started{}, err
	}
	if w.Status() != work.StatusTriage {
		return Started{View: viewOf(w), Created: created}, nil
	}
	w, err = s.ask(ctx, w)
	return Started{View: viewOf(w), Created: created}, err
}

// Retry asks the due reader call of a work again («Повторить» after no network or a refused answer).
func (s *Service) Retry(ctx context.Context, id shared.WorkID) (View, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	if w.Status() != work.StatusTriage {
		return viewOf(w), fmt.Errorf("%w: no reader call is due", ErrInvalidRequest)
	}
	w, err = s.ask(ctx, w)
	return viewOf(w), err
}

// AnswerQuestions takes the student's answers to the questions of the current round (unanswered ones take their
// default) and asks the reader the next round (paid). The answers are saved before the call: if it fails, the work
// waits with its call due and the view comes back with the error. Errors: those of Triage and ErrNotFound.
func (s *Service) AnswerQuestions(ctx context.Context, id shared.WorkID, replies []work.Reply) (View, error) {
	w, err := s.change(ctx, id, func(w *work.Work) error { return w.Answer(replies, s.clock.Now()) })
	if err != nil {
		return View{}, err
	}
	if w.Status() == work.StatusTriage {
		w, err = s.ask(ctx, w)
	}
	return viewOf(w), err
}

// SkipQuestions is «Пропустить — сделай как считаешь»: every question takes its default, then as AnswerQuestions.
func (s *Service) SkipQuestions(ctx context.Context, id shared.WorkID) (View, error) {
	return s.AnswerQuestions(ctx, id, nil)
}

// EditUnderstanding keeps the student's card «Как я понял задание» (local only). Errors: ErrNotFound,
// ErrInvalidRequest, ErrUnavailable.
func (s *Service) EditUnderstanding(ctx context.Context, id shared.WorkID, u work.Understanding) (View, error) {
	w, err := s.change(ctx, id, func(w *work.Work) error { return w.Edit(u, s.clock.Now()) })
	if err != nil {
		return View{}, err
	}
	return viewOf(w), nil
}

// ConfirmRequirements is «Подтвердить» on the card: the requirements are settled for the writing (TASK-105).
func (s *Service) ConfirmRequirements(ctx context.Context, id shared.WorkID) (View, error) {
	w, err := s.change(ctx, id, func(w *work.Work) error { return w.Confirm(s.clock.Now()) })
	if err != nil {
		return View{}, err
	}
	return viewOf(w), nil
}

// Work returns a work. Errors: ErrNotFound, ErrUnavailable.
func (s *Service) Work(ctx context.Context, id shared.WorkID) (View, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return View{}, err
	}
	return viewOf(w), nil
}

// WorkOf returns the work of a source that has not failed. Errors: ErrNotFound, ErrInvalidRequest, ErrUnavailable.
func (s *Service) WorkOf(ctx context.Context, src work.Source) (View, error) {
	if (src.Assignment == "") == (src.Material == "") {
		return View{}, fmt.Errorf("%w: exactly one source", ErrInvalidRequest)
	}
	w, err := s.works.ActiveOf(ctx, src)
	if err != nil {
		return View{}, s.loadError(err)
	}
	return viewOf(w), nil
}

// AskStep is «Спросить по шагу»: the student's question about one helper step, answered from that step and the
// requirements only (work.StepQuestion), with no tools. The answer is untrusted text for a card. Errors:
// ErrNotConfigured (no advisor yet), ErrNotFound, ErrInvalidRequest, ErrReaderUnavailable, ErrQuota,
// ErrReaderRefused, ErrUnavailable.
func (s *Service) AskStep(ctx context.Context, id shared.WorkID, step int, question string) (string, error) {
	if s.advisor == nil {
		return "", ErrNotConfigured
	}
	w, err := s.get(ctx, id)
	if err != nil {
		return "", err
	}
	q, err := w.AskStep(step, question)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	out, err := s.advisor.AdviseStep(ctx, q)
	if err != nil {
		return "", s.readerError(err)
	}
	advice, err := work.CheckAdvice(out)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrReaderRefused, err)
	}
	return advice, nil
}

// Remove deletes a work if it is still at version expected: the Undo of the agent's triage_assignment (ADR-0021). The
// money of the reader call is not returned. A work gone already is no error. Errors: ErrChanged, ErrUnavailable.
func (s *Service) Remove(ctx context.Context, id shared.WorkID, expected int64) error {
	err := s.works.Delete(ctx, id, expected)
	if !errors.Is(err, shared.ErrVersionConflict) {
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return nil
	}
	if _, gerr := s.works.Get(ctx, id); errors.Is(gerr, shared.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrChanged, err)
}

// --- steps ----------------------------------------------------------------------------------------------

// workOf returns the active work of src, or a new saved one (created).
func (s *Service) workOf(ctx context.Context, src work.Source) (*work.Work, bool, error) {
	w, err := s.works.ActiveOf(ctx, src)
	switch {
	case err == nil:
		return w, false, nil
	case !errors.Is(err, shared.ErrNotFound):
		return nil, false, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	w, err = work.New(s.ids.NewWorkID(), src, s.clock.Now())
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	err = s.works.Save(ctx, w)
	switch {
	case err == nil:
		return w, true, nil
	case errors.Is(err, shared.ErrDuplicate):
		// Another call started the work of this source meanwhile: that one is it.
		got, gerr := s.works.ActiveOf(ctx, src)
		if gerr != nil {
			return nil, false, s.loadError(gerr)
		}
		return got, false, nil
	case errors.Is(err, shared.ErrConflict):
		return nil, false, fmt.Errorf("%w: %w", ErrSourceNotFound, err)
	}
	return nil, false, fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// ask runs the due reader call of w and records its outcome. It returns the work as stored after it (w unchanged when
// nothing was recorded) and the error of the call.
func (s *Service) ask(ctx context.Context, w *work.Work) (*work.Work, error) {
	assignmentText, materialText, err := s.texts(ctx, w.Source())
	if err != nil {
		return w, err
	}
	req, err := w.Request(assignmentText, materialText)
	switch {
	case errors.Is(err, work.ErrInvalidRequest):
		return w, fmt.Errorf("%w: %w", ErrNoText, err)
	case err != nil:
		return w, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	raw, err := s.reader.TriageAssignment(ctx, req)
	if errors.Is(err, work.ErrInvalidAnswer) {
		return s.refused(ctx, w, req.Round(), err)
	}
	if err != nil {
		return w, s.readerError(err)
	}
	answer, err := req.Parse(raw)
	if err != nil {
		return s.refused(ctx, w, req.Round(), err)
	}
	got, err := s.save(ctx, w, func(w *work.Work) error { return w.Record(req.Round(), answer, s.clock.Now()) })
	if err != nil {
		return w, err
	}
	s.log.InfoContext(ctx, opTriaged, slog.String("work", string(w.ID())), slog.Int("round", req.Round()),
		slog.String("status", string(got.Status())))
	return got, nil
}

// refused records a refused answer of round: the attempt is used, after the last one the work fails.
func (s *Service) refused(ctx context.Context, w *work.Work, round int, cause error) (*work.Work, error) {
	s.log.WarnContext(ctx, opRefused, slog.String("work", string(w.ID())), slog.Int("round", round),
		slog.Int("attempt", w.Attempt()))
	got, err := s.save(ctx, w, func(w *work.Work) error { return w.Refuse(round, s.clock.Now()) })
	if err != nil {
		return w, err
	}
	return got, fmt.Errorf("%w: %w", ErrReaderRefused, cause)
}

// save applies fn to w and saves it; when another save won meanwhile, fn is applied to the stored work again. A move
// the stored work no longer allows (another call recorded the round already) returns the stored work as it is.
func (s *Service) save(ctx context.Context, w *work.Work, fn func(*work.Work) error) (*work.Work, error) {
	for range saveAttempts {
		if err := fn(w); err != nil {
			if errors.Is(err, work.ErrInvalidTransition) || errors.Is(err, work.ErrStaleRound) {
				return w, nil
			}
			return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		err := s.works.Save(ctx, w)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		s.log.InfoContext(ctx, opSaveLost, slog.String("work", string(w.ID())))
		if w, err = s.get(ctx, w.ID()); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: the work keeps changing", ErrUnavailable)
}

// change loads a work and applies a student's move to it; a move the work does not allow is ErrInvalidRequest.
func (s *Service) change(ctx context.Context, id shared.WorkID, fn func(*work.Work) error) (*work.Work, error) {
	for range saveAttempts {
		w, err := s.get(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := fn(w); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		err = s.works.Save(ctx, w)
		if err == nil {
			return w, nil
		}
		if !errors.Is(err, shared.ErrVersionConflict) {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		s.log.InfoContext(ctx, opSaveLost, slog.String("work", string(id)))
	}
	return nil, fmt.Errorf("%w: the work keeps changing", ErrUnavailable)
}

func (s *Service) get(ctx context.Context, id shared.WorkID) (*work.Work, error) {
	if id.Validate() != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, shared.ErrNotFound)
	}
	w, err := s.works.Get(ctx, id)
	if err != nil {
		return nil, s.loadError(err)
	}
	return w, nil
}

func (s *Service) loadError(err error) error {
	if errors.Is(err, shared.ErrNotFound) {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// readerError sorts an error of a Hub call that recorded nothing.
func (s *Service) readerError(err error) error {
	switch {
	case errors.Is(err, shared.ErrQuotaExceeded):
		return fmt.Errorf("%w: %w", ErrQuota, err)
	case errors.Is(err, work.ErrInvalidAnswer):
		return fmt.Errorf("%w: %w", ErrReaderRefused, err)
	}
	return fmt.Errorf("%w: %w", ErrReaderUnavailable, err)
}

// texts reads the assignment text and the teacher's material of a source. An assignment's own material is optional
// context: when it cannot be read the assignment is asked about alone. An uploaded file is the assignment itself.
func (s *Service) texts(ctx context.Context, src work.Source) (assignmentText, materialText string, err error) {
	if src.Material != "" {
		_, text, err := s.materials.MaterialText(ctx, src.Material)
		if err != nil {
			return "", "", sourceError(err)
		}
		if text.Empty() {
			return "", "", ErrNoText
		}
		return text.String(), "", nil
	}

	a, err := s.assignments.Get(ctx, src.Assignment)
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return "", "", fmt.Errorf("%w: %w", ErrSourceNotFound, err)
	case err != nil:
		return "", "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	d := a.Details()
	assignmentText = strings.TrimSpace(d.Title() + "\n\n" + d.Description())
	if m := a.MaterialID(); m != "" {
		if _, text, err := s.materials.MaterialText(ctx, m); err == nil {
			materialText = text.String()
		} else {
			s.log.InfoContext(ctx, opMaterial, slog.String("material", string(m)), slog.Any("cause", err))
		}
	}
	return assignmentText, materialText, nil
}

// sourceError sorts an error of reading the text of an uploaded file.
func sourceError(err error) error {
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, material.ErrRemoved):
		return fmt.Errorf("%w: %w", ErrSourceNotFound, err)
	case errors.Is(err, material.ErrTextNotExtracted), errors.Is(err, material.ErrTextUnreadable),
		errors.Is(err, material.ErrDamaged):
		return fmt.Errorf("%w: %w", ErrNoText, err)
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}
