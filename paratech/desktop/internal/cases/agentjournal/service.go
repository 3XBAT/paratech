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
	opNew          = "agentjournal: new"
	opFind         = "agentjournal: find"
	opBegin        = "agentjournal: begin"
	opFinish       = "agentjournal: finish"
	opPropose      = "agentjournal: propose"
	opBeginUndo    = "agentjournal: begin undo"
	opFinishUndo   = "agentjournal: finish undo"
	opUndoRequest  = "agentjournal: undo the request"
	opList         = "agentjournal: list"
	opLive         = "agentjournal: live effects"
	opRecover      = "agentjournal: recover"
	opLineSubject  = "agentjournal: subject of a journal line unknown"
	opObserve      = "agentjournal: pending entry not settled"
	opUndoRetry    = "agentjournal: undo retry failed"
	opTargetUnread = "agentjournal: target of an entry unread"
)

// MaxList bounds the entries List and ListThread return.
const MaxList = 200

// Service is the action journal. Each method is one short transaction on one entry (a single CAS save); the use case
// that acts runs between Begin and Finish. No key is passed to it: the one entry per (thread, turn, call ID) and
// Find make a repeated call act at most once (ADR-0021, ARCH-23).
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
	return entryOf(a, s.clock.Now(), action.Target{}), nil
}

// Begin records a U call PENDING before its use case runs (first transaction). A second entry for the same call is
// shared.ErrVersionConflict: use Find.
func (s *Service) Begin(ctx context.Context, in BeginInput) (Entry, error) {
	a, err := action.New(spec(s.ids.NewActionID(), in), s.clock.Now())
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBegin, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBegin, err)
	}
	return entryOf(a, s.clock.Now(), action.Target{}), nil
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
	return entryOf(a, s.clock.Now(), action.Target{}), nil
}

// BeginUndo is «Отменить»: DONE -> UNDOING (or the retry of a stale UNDOING entry, action.StartUndo), saved before
// the compensation runs.
func (s *Service) BeginUndo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opBeginUndo, ErrUndoUnavailable)
	}
	return s.update(ctx, id, opBeginUndo, func(a *action.Action) error { return a.StartUndo(s.clock.Now()) })
}

// FinishUndo runs the compensation of an UNDOING entry: UNDONE when it applied or the target is already compensated
// or gone; UNDO_REFUSED with its reason when the target changed since the action («Изменено после — отменить
// нельзя»), a paid result is ready or its job cannot stop. A failed compensation leaves the entry UNDOING; «Отменить»
// retries it once it is stale, Recover at the next start.
func (s *Service) FinishUndo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, ErrUndoUnavailable)
	}
	a, err := s.actions.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	if _, _, err := s.compensate(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	return entryOf(a, s.clock.Now(), action.Target{}), nil
}

// compensate runs the compensation of an UNDOING entry and saves its new status. applied tells that Apply changed
// the target, which then has version after.
func (s *Service) compensate(ctx context.Context, a *action.Action) (after int64, applied bool, err error) {
	if a.Status() != action.StatusUndoing {
		return 0, false, fmt.Errorf("%w: %s is not UNDOING", action.ErrInvalidTransition, a.Status())
	}
	if after, applied, err = s.undo(ctx, a); err != nil {
		return 0, false, err
	}
	return after, applied, s.actions.Save(ctx, a)
}

// undo decides and applies the compensation of a; the caller saves the new status. A target already compensated
// (a crash after the compensation, before UNDONE was saved) is UNDONE, not refused.
func (s *Service) undo(ctx context.Context, a *action.Action) (after int64, applied bool, err error) {
	c := a.Compensation()
	t, err := s.comp.Check(ctx, c)
	if errors.Is(err, shared.ErrNotFound) {
		return 0, false, a.FinishUndo(s.clock.Now())
	}
	if err != nil {
		return 0, false, err
	}
	apply, err := a.PlanUndo(t)
	if err != nil {
		return 0, false, s.refuse(a, err)
	}
	if apply {
		// The check above is advisory; Apply's compare-and-swap decides when the target changes in between.
		after, err = s.comp.Apply(ctx, c, a.TargetVersion())
		switch {
		case errors.Is(err, shared.ErrNotFound):
			after = 0
		case err != nil:
			return 0, false, s.refuse(a, err)
		default:
			applied = true
		}
	}
	return after, applied, a.FinishUndo(s.clock.Now())
}

// startUndo is «Отменить» of a loaded entry: StartUndo saved, then the compensation.
func (s *Service) startUndo(ctx context.Context, a *action.Action) (after int64, applied bool, err error) {
	if err := a.StartUndo(s.clock.Now()); err != nil {
		return 0, false, fmt.Errorf("%s: %w", opBeginUndo, err)
	}
	if err := s.actions.Save(ctx, a); err != nil {
		return 0, false, fmt.Errorf("%s: %w", opBeginUndo, err)
	}
	if after, applied, err = s.compensate(ctx, a); err != nil {
		return 0, false, fmt.Errorf("%s: %w", opFinishUndo, err)
	}
	return after, applied, nil
}

// refuse records the refusal err means; another error is returned as it is.
func (s *Service) refuse(a *action.Action, err error) error {
	why, ok := action.UndoRefusalOf(err)
	if !ok {
		return err
	}
	return a.RefuseUndo(why, s.clock.Now())
}

// checkUndo is the check before StartUndo: a DONE paid job whose result is ready is action.ErrResultReady and the
// entry stays DONE, nothing saved. A target that is gone is left to the compensation (UNDONE).
func (s *Service) checkUndo(ctx context.Context, a *action.Action) error {
	if !a.UndoNeedsTarget() {
		return nil
	}
	t, err := s.comp.Check(ctx, a.Compensation())
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	return a.CheckUndo(t)
}

// targetOf is the state of the target of a for «Отменить» in a list (read-only); the zero Target when a does not need
// it or it cannot be read now (then «Отменить» is offered and Undo decides).
func (s *Service) targetOf(ctx context.Context, a *action.Action) action.Target {
	if s.comp == nil || !a.UndoNeedsTarget() {
		return action.Target{}
	}
	t, err := s.comp.Check(ctx, a.Compensation())
	if err != nil {
		if !errors.Is(err, shared.ErrNotFound) {
			s.log.WarnContext(ctx, opTargetUnread, "action", a.ID().String(), "err", err)
		}
		return action.Target{}
	}
	return t
}

// Undo is BeginUndo and FinishUndo; a paid job whose result is already ready is refused before the entry is touched
// (action.ErrResultReady). An entry left UNDOING by a failed compensation is retried once it is stale.
func (s *Service) Undo(ctx context.Context, id shared.ActionID) (Entry, error) {
	if s.comp == nil {
		return Entry{}, fmt.Errorf("%s: %w", opBeginUndo, ErrUndoUnavailable)
	}
	a, err := s.actions.Get(ctx, id)
	if err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBeginUndo, err)
	}
	if err := s.checkUndo(ctx, a); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", opBeginUndo, err)
	}
	if _, _, err := s.startUndo(ctx, a); err != nil {
		return Entry{}, err
	}
	return entryOf(a, s.clock.Now(), action.Target{}), nil
}

// UndoRequest is «Отменить всё»: every action of a request that can still be undone, the newest first (the reverse
// of the order they ran in). An earlier action follows its target through the Undo of a later one of the request
// (action.Rebase), so create_subject + create_folder in it are both undone. A refused one does not stop the others;
// failures are returned joined, with the entries that went through.
func (s *Service) UndoRequest(ctx context.Context, th shared.ThreadID, turn int) ([]Entry, error) {
	if s.comp == nil {
		return nil, fmt.Errorf("%s: %w", opUndoRequest, ErrUndoUnavailable)
	}
	as, err := s.actions.ListByThread(ctx, th, MaxList)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opUndoRequest, err)
	}
	type undone struct {
		a     *action.Action
		after int64
	}
	var (
		out  []Entry
		errs []error
		last = map[values.Ref]undone{} // the last undo applied to each target in this run
	)
	for _, a := range as {
		if a.Turn() != turn || !a.CanUndo(s.clock.Now()) {
			continue
		}
		err := s.checkUndo(ctx, a)
		if errors.Is(err, action.ErrResultReady) {
			continue // the result stays; the others of the request are still undone
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("action %s: %w", a.ID(), err))
			continue
		}
		if u, ok := last[a.TargetRef()]; ok {
			a.Rebase(u.a, u.after)
		}
		after, applied, err := s.startUndo(ctx, a)
		if err != nil {
			errs = append(errs, fmt.Errorf("action %s: %w", a.ID(), err))
			continue
		}
		if applied && !a.TargetRef().IsZero() {
			last[a.TargetRef()] = undone{a: a, after: after}
		}
		out = append(out, entryOf(a, s.clock.Now(), action.Target{}))
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("%s: %w", opUndoRequest, errors.Join(errs...))
	}
	return out, nil
}

// Live reports whether request turn of a thread left effects a repeat would double (agentturn.Effects, ADR-0026): an
// entry that is not FAILED or UNDONE. A proposal counts whatever the student did with it, and so does an action whose
// outcome is unknown or whose undo was refused or has not finished.
func (s *Service) Live(ctx context.Context, th shared.ThreadID, turn int) (bool, error) {
	// The request is the newest of its thread, so its entries are among the newest MaxList (at most a few per request).
	as, err := s.actions.ListByThread(ctx, th, MaxList)
	if err != nil {
		return false, fmt.Errorf("%s: %w", opLive, err)
	}
	for _, a := range as {
		if a.Turn() == turn && a.Status() != action.StatusFailed && a.Status() != action.StatusUndone {
			return true, nil
		}
	}
	return false, nil
}

// ListThread returns the entries of a thread, newest first, at most limit (1..MaxList).
func (s *Service) ListThread(ctx context.Context, thread shared.ThreadID, limit int) ([]Entry, error) {
	as, err := s.actions.ListByThread(ctx, thread, clampLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	out, now := make([]Entry, len(as)), s.clock.Now()
	for i, a := range as {
		out[i] = entryOf(a, now, s.targetOf(ctx, a))
	}
	return out, nil
}

// List is «Журнал»: one list of the actions of paratech (undone ones included) and the processing history, newest
// first, at most limit (1..MaxList).
func (s *Service) List(ctx context.Context, limit int) ([]Line, error) {
	limit, now := clampLimit(limit), s.clock.Now()
	as, err := s.actions.ListRecent(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opList, err)
	}
	lines := make([]Line, 0, len(as))
	subjects := map[requestOf]shared.SubjectID{}
	for _, a := range as {
		can, why := a.Undoable(now, s.targetOf(ctx, a))
		st := a.State()
		lines = append(lines, Line{
			Kind: LineAction, At: st.CreatedAt, UpdatedAt: st.UpdatedAt, Action: st.ID, Thread: st.Thread, Turn: st.Turn,
			Tool: st.Tool, Status: st.Status, CanUndo: can, NoUndo: why, Subject: s.subjectOf(ctx, subjects, requestOf{st.Thread, st.Turn}),
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

// requestOf names the request of a journal line.
type requestOf struct {
	thread shared.ThreadID
	turn   int
}

// subjectOf is the subject of the scope of a line's request, cached per List; a request that cannot be read gives
// none.
func (s *Service) subjectOf(ctx context.Context, cache map[requestOf]shared.SubjectID, r requestOf) shared.SubjectID {
	if sub, ok := cache[r]; ok || s.threads == nil {
		return sub
	}
	scope, err := s.threads.RequestScope(ctx, r.thread, r.turn)
	if err != nil {
		s.log.WarnContext(ctx, opLineSubject, "thread", r.thread.String(), "turn", r.turn, "err", err)
	}
	sub := shared.SubjectID(scope.Subject().ID())
	cache[r] = sub
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
	if _, _, err := s.compensate(ctx, a); err != nil {
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
	return entryOf(a, s.clock.Now(), action.Target{}), nil
}
