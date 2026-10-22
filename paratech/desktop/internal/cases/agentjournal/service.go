package agentjournal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names, for error texts and the log.
const (
	opNew         = "agentjournal: new"
	opFind        = "agentjournal: find"
	opBegin       = "agentjournal: begin"
	opFinish      = "agentjournal: finish"
	opPropose     = "agentjournal: propose"
	opBeginUndo   = "agentjournal: begin undo"
	opFinishUndo  = "agentjournal: finish undo"
	opUndoRequest = "agentjournal: undo the request"
	opList        = "agentjournal: list"
	opRecover     = "agentjournal: recover"
	opLineSubject = "agentjournal: subject of a journal line unknown"
	opObserve     = "agentjournal: pending entry not settled"
	opUndoRetry   = "agentjournal: undo retry failed"
)

// MaxList bounds the entries List and ListThread return.
const MaxList = 200

// Service is the action journal. Each method is one short transaction on one entry (a single CAS save); the use case
// that acts runs between Begin and Finish, idempotent by Entry.Key (ADR-0021, ARCH-23).
type Service struct {
	actions    ActionRepository
	comp       Compensator
	resolver   Resolver
	threads    Threads
	processing Processing
	clock      Clock
	ids        IDGenerator
	log        *slog.Logger
}

// New checks the ports.
func New(d Deps) (*Service, error) {
	if d.Actions == nil || d.Clock == nil || d.IDs == nil {
		return nil, fmt.Errorf("%s: %w: Actions, Clock and IDs are required", opNew, ErrInvalidConfig)
	}
	s := &Service{
		actions: d.Actions, comp: d.Compensator, resolver: d.Resolver, threads: d.Threads, processing: d.Processing,
		clock: d.Clock, ids: d.IDs, log: d.Log,
	}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	return s, nil
}

func spec(id shared.ActionID, in BeginInput) action.Spec {
	return action.Spec{ID: id, Thread: in.Thread, Turn: in.Turn, CallID: in.CallID, Tool: in.Tool, Intent: in.Intent}
}

// Find returns the entry of a call of a request, so a call the Driver runs again (the same call ID) does not act
// twice; none wraps shared.ErrNotFound.
func (s *Service) Find(ctx context.Context, th shared.ThreadID, turn int, callID string) (Entry, error) {
	a, err := s.actions.GetByCall(ctx, th, turn, callID)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFind, err)
	}
	return entryOf(a), nil
}

// Begin records a U call PENDING before its use case runs (first transaction). The use case then runs with
// Entry.Key as its idempotency key. A second entry for the same call is shared.ErrVersionConflict: use Find.
func (s *Service) Begin(ctx context.Context, in BeginInput) (Entry, error) {
	a, err := action.New(spec(s.ids.NewActionID(), in), s.clock.Now())
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBegin, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBegin, err)
	}
	return entryOf(a), nil
}

// Finish records the outcome of the use case (second transaction): DONE with its compensation, or FAILED.
func (s *Service) Finish(ctx context.Context, id shared.ActionID, out Outcome) (Entry, error) {
	return s.update(ctx, id, opFinish, func(a *action.Action) error {
		if out.Failed {
			return a.Fail(s.clock.Now())
		}
		return a.Complete(out.Compensation, out.TargetVersion, s.clock.Now())
	})
}

// Propose records a C call that created a proposal or a preview: PROPOSED. Nothing ran; the student decides on the
// confirm card.
func (s *Service) Propose(ctx context.Context, in BeginInput, proposal values.Ref) (Entry, error) {
	a, err := action.NewProposed(spec(s.ids.NewActionID(), in), proposal, s.clock.Now())
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opPropose, err)
	}
	return entryOf(a), nil
}

// BeginUndo is «Отменить»: DONE -> UNDOING, saved before the compensation runs.
func (s *Service) BeginUndo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opBeginUndo, ErrUndoUnavailable)
	}
	return s.update(ctx, id, opBeginUndo, func(a *action.Action) error { return a.BeginUndo(s.clock.Now()) })
}

// FinishUndo runs the compensation of an UNDOING entry: UNDONE when it applied or the target is already compensated
// or gone; UNDO_REFUSED with its reason when the target changed since the action («Изменено после — отменить
// нельзя»), a paid result is ready or its job cannot stop. A failed compensation leaves the entry UNDOING; Recover
// retries it at the next start.
func (s *Service) FinishUndo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, ErrUndoUnavailable)
	}
	a, err := s.actions.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	if err := s.compensate(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	return entryOf(a), nil
}

func (s *Service) compensate(ctx context.Context, a *action.Action) error {
	if a.Status() != action.StatusUndoing {
		return fmt.Errorf("%w: %s is not UNDOING", action.ErrInvalidTransition, a.Status())
	}
	if err := s.undo(ctx, a); err != nil {
		return err
	}
	return s.actions.Save(ctx, a)
}

// undo decides and applies the compensation of a; the caller saves the new status. A target already compensated
// (a crash after the compensation, before UNDONE was saved) is UNDONE, not refused.
func (s *Service) undo(ctx context.Context, a *action.Action) error {
	c := a.Compensation()
	t, err := s.comp.Check(ctx, c)
	if errors.Is(err, shared.ErrNotFound) {
		return a.FinishUndo(s.clock.Now())
	}
	if err != nil {
		return err
	}
	apply, err := a.PlanUndo(t)
	if err != nil {
		return s.refuse(a, err)
	}
	if apply {
		// The check above is advisory; Apply's compare-and-swap decides when the target changes in between.
		if err := s.comp.Apply(ctx, c, a.TargetVersion()); err != nil && !errors.Is(err, shared.ErrNotFound) {
			return s.refuse(a, err)
		}
	}
	return a.FinishUndo(s.clock.Now())
}

// refuse records the refusal err means; another error is returned as it is.
func (s *Service) refuse(a *action.Action, err error) error {
	why, ok := action.UndoRefusalOf(err)
	if !ok {
		return err
	}
	return a.RefuseUndo(why, s.clock.Now())
}

// Undo is BeginUndo and FinishUndo.
func (s *Service) Undo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if _, err := s.BeginUndo(ctx, id); err != nil {
		return Entry{}, err
	}
	return s.FinishUndo(ctx, id)
}

// UndoRequest is «Отменить всё»: every action of a request that can still be undone, the newest first (the reverse
// of the order they ran in). A refused one does not stop the others; failures are returned joined, with the entries
// that went through.
func (s *Service) UndoRequest(ctx context.Context, th shared.ThreadID, turn int) ([]Entry, error) {
	as, err := s.actions.ListByThread(ctx, th, MaxList)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opUndoRequest, err)
	}
	var (
		out  []Entry
		errs []error
	)
	for _, a := range as {
		if a.Turn() != turn || !a.CanUndo() {
			continue
		}
		e, err := s.Undo(ctx, a.ID())
		if err != nil {
			errs = append(errs, fmt.Errorf("action %s: %w", a.ID(), err))
			continue
		}
		out = append(out, e)
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("%s: %w", opUndoRequest, errors.Join(errs...))
	}
	return out, nil
}

// ListThread returns the entries of a thread, newest first, at most limit (1..MaxList).
func (s *Service) ListThread(ctx context.Context, thread shared.ThreadID, limit int) ([]Entry, error) {
	as, err := s.actions.ListByThread(ctx, thread, clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	out := make([]Entry, len(as))
	for i, a := range as {
		out[i] = entryOf(a)
	}
	return out, nil
}

// List is «Журнал»: one list of the actions of paratech (undone ones included) and the processing history, newest
// first, at most limit (1..MaxList).
func (s *Service) List(ctx context.Context, limit int) ([]Line, error) {
	limit = clampLimit(limit)
	as, err := s.actions.ListRecent(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	lines := make([]Line, 0, len(as))
	subjects := map[shared.ThreadID]shared.SubjectID{}
	for _, a := range as {
		can, why := a.Undoable()
		st := a.State()
		lines = append(lines, Line{
			Kind: LineAction, At: st.CreatedAt, UpdatedAt: st.UpdatedAt, Subject: s.subjectOf(ctx, subjects, st.Thread),
			Action: st.ID, Thread: st.Thread, Turn: st.Turn, Tool: st.Tool, Status: st.Status, CanUndo: can, NoUndo: why,
		})
	}
	if s.processing != nil {
		jobs, unreadable, err := s.processing.ListActivity(ctx, limit, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", opList, err)
		}
		if len(unreadable) > 0 {
			s.log.WarnContext(ctx, opList, "unreadable_jobs", len(unreadable))
		}
		for _, j := range jobs {
			lines = append(lines, Line{
				Kind: LineProcessing, At: j.CreatedAt(), UpdatedAt: j.UpdatedAt(), Job: j.ID(), JobType: j.Type(),
				JobStatus: j.Status(),
			})
		}
	}
	slices.SortStableFunc(lines, func(a, b Line) int { return b.At.Compare(a.At) })
	return lines[:min(len(lines), limit)], nil
}

// subjectOf is the subject of a thread's key, cached per List; a thread that cannot be read gives none.
func (s *Service) subjectOf(ctx context.Context, cache map[shared.ThreadID]shared.SubjectID, id shared.ThreadID) shared.SubjectID {
	if sub, ok := cache[id]; ok || s.threads == nil {
		return sub
	}
	var sub shared.SubjectID
	th, err := s.threads.Get(ctx, id)
	if err != nil {
		s.log.WarnContext(ctx, opLineSubject, "thread", id.String(), "err", err)
	} else if r, ok := th.Key().Subject(); ok {
		sub = shared.SubjectID(r.ID())
	}
	cache[id] = sub
	return sub
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > MaxList {
		return MaxList
	}
	return limit
}

// Recover runs at start. A PENDING entry is settled from its intent (action.Settle): the effect is there -> DONE,
// nothing changed -> FAILED, else UNKNOWN (no Undo); without a resolver it is UNKNOWN. An UNDOING one has its
// compensation retried. An entry whose target cannot be read now is left for the next start.
func (s *Service) Recover(ctx context.Context) (RecoverReport, error) {
	var rep RecoverReport
	as, err := s.actions.ListUnfinished(ctx)
	if err != nil {
		return rep, fmt.Errorf("%s: %w", opRecover, err)
	}
	var errs []error
	for _, a := range as {
		switch a.Status() {
		case action.StatusPending:
			if err := s.settle(ctx, a, &rep); err != nil {
				errs = append(errs, fmt.Errorf("action %s: %w", a.ID(), err))
			}
		case action.StatusUndoing:
			s.retryUndo(ctx, a, &rep)
		}
	}
	if len(errs) > 0 {
		return rep, fmt.Errorf("%s: %w", opRecover, errors.Join(errs...))
	}
	return rep, nil
}

func (s *Service) settle(ctx context.Context, a *action.Action, rep *RecoverReport) error {
	if s.resolver == nil {
		if _, err := a.Recover(s.clock.Now()); err != nil {
			return err
		}
	} else {
		obs, err := s.resolver.Observe(ctx, a.Tool(), a.Intent(), a.CreatedAt())
		if err != nil {
			s.log.WarnContext(ctx, opObserve, "action", a.ID().String(), "err", err)
			rep.Left++
			return nil
		}
		if err := a.Settle(obs, s.clock.Now()); err != nil {
			return err
		}
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return err
	}
	switch a.Status() {
	case action.StatusDone:
		rep.Done++
	case action.StatusFailed:
		rep.Failed++
	default:
		rep.Unknown++
	}
	return nil
}

func (s *Service) retryUndo(ctx context.Context, a *action.Action, rep *RecoverReport) {
	if s.comp == nil {
		rep.Left++
		return
	}
	if err := s.compensate(ctx, a); err != nil {
		s.log.WarnContext(ctx, opUndoRetry, "action", a.ID().String(), "err", err)
		rep.Left++
		return
	}
	if a.Status() == action.StatusUndone {
		rep.Undone++
	} else {
		rep.Refused++
	}
}

// update loads an entry, applies change and saves it.
func (s *Service) update(ctx context.Context, id shared.ActionID, op string, change func(*action.Action) error) (Entry, error) {
	a, err := s.actions.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := change(a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", op, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", op, err)
	}
	return entryOf(a), nil
}
