package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/preferences"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the free-work card, for the log.
const (
	opFreeWork       = "agent: free work"
	opFreeWorkNotice = "agent: free work: card setting"
)

// freeWorkHistory bounds the entries read to find the call of a card: the newest of its request and the later ones (a
// card under many later steps is not found, which refuses it, never runs another call).
const freeWorkHistory = 1024

// FreeWorkThreads reads the scope stored with a request of a thread; a missing request wraps shared.ErrNotFound.
// Implemented by the sqlite adapter.
type FreeWorkThreads interface {
	RequestScope(ctx context.Context, id shared.ThreadID, turn int) (values.Scope, error)
}

// FreeWorkHistory reads the stored history of a thread (the agentturn.History port, implemented by the sqlite adapter).
type FreeWorkHistory interface {
	Since(ctx context.Context, id shared.ThreadID, from, limit int) ([]values.HistoryEntry, error)
}

// NoticeSettings reads and changes the free-work card setting (cases/preferences).
type NoticeSettings interface {
	NoticeReader
	SetFreeWorkNotice(ctx context.Context, on bool) (preferences.Toggles, error)
}

// FreeWorkDeps are the dependencies of FreeWorks; Log is optional.
type FreeWorkDeps struct {
	Threads  FreeWorkThreads
	History  FreeWorkHistory
	Journal  Journal
	Actions  *Actions
	Settings NoticeSettings
	Log      *slog.Logger
}

// FreeWorks answers the free-work card «Такого задания нет — могу сделать как свободную работу» (TASK-188). The card
// names a call only; the topic is read from that call in the card's own thread history and the subject from the scope
// of the card's request (a chat spans subjects, ADR-0028), so the page sends no text. «Сделать» is the student's
// consent: the triage runs through the journaled action path of the agent (one entry per thread, request and call ID,
// Undo removes the work), so a second «Сделать» finds the same work. «Не надо» stores nothing and runs nothing.
type FreeWorks struct {
	threads  FreeWorkThreads
	history  FreeWorkHistory
	settings NoticeSettings
	tools    tools
	log      *slog.Logger
}

// NewFreeWorks checks the dependencies.
func NewFreeWorks(d FreeWorkDeps) (*FreeWorks, error) {
	if d.Threads == nil || d.History == nil || d.Journal == nil || d.Actions == nil || d.Settings == nil {
		return nil, fmt.Errorf("agent: free works: %w: a required dependency is missing", ErrInvalidConfig)
	}
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &FreeWorks{
		threads: d.Threads, history: d.History, settings: d.Settings, log: log,
		tools: tools{journal: d.Journal, actions: d.Actions, log: log},
	}, nil
}

// FreeWorkCard returns the state of a card. A card that names no call waiting on it in its thread (another thread's,
// a made-up one) wraps shared.ErrNotFound.
func (f *FreeWorks) FreeWorkCard(ctx context.Context, card values.FreeWorkCard) (values.FreeWorkOutcome, error) {
	if _, _, _, err := f.call(ctx, card); err != nil {
		return values.FreeWorkOutcome{}, err
	}
	return f.state(ctx, card)
}

// FreeWork is the student's answer on a card: accept is «Сделать», dontShowAgain «Больше не показывать» (stored
// first, for either answer). «Сделать» starts the triage of the topic once; a repeated one returns the same work. The
// outcome is the card's state after the answer (FreeWorkCard says the same). Errors wrap shared.ErrNotFound (no such card, or its work is gone), shared.ErrServiceNotConfigured (no turnkey
// work in this build) or are a local failure.
func (f *FreeWorks) FreeWork(ctx context.Context, card values.FreeWorkCard, accept, dontShowAgain bool) (values.FreeWorkOutcome, error) {
	call, adm, requestScope, err := f.call(ctx, card)
	if err != nil {
		return values.FreeWorkOutcome{}, err
	}
	if dontShowAgain {
		if _, err := f.settings.SetFreeWorkNotice(ctx, false); err != nil {
			f.log.WarnContext(ctx, opFreeWorkNotice, "err", err)
			return values.FreeWorkOutcome{}, fmt.Errorf("%s: %w", opFreeWorkNotice, err)
		}
	}
	if !accept {
		return f.state(ctx, card)
	}

	// Only the subject of the request: the triage of a topic reads nothing else of the scope.
	var scope values.Scope
	if sub := requestScope.Subject(); !sub.IsZero() {
		if scope, err = values.NewScope(sub, "", values.Ref{}, values.Ref{}); err != nil {
			return values.FreeWorkOutcome{}, fmt.Errorf("%s: %w", opFreeWork, err)
		}
	}
	// No lock: the journal entry of (thread, request, call ID) makes a second «Сделать» replay the first one (Find, or
	// Begin failing on the same key), and the reader's request key is the same call's.
	st := agentturn.Step{Thread: card.Thread, Turn: card.Turn, Scope: scope}
	r := f.tools.write(ctx, st, agentturn.Call{Call: call, Class: adm.Class, Paid: adm.Paid, PlanStep: adm.PlanStep})
	out, err := f.state(ctx, card)
	if err != nil {
		return values.FreeWorkOutcome{}, err
	}
	// Refused before anything was journaled (no such source, no turnkey work in this build): the card still waits.
	if why, refused := values.RefusalOf(r.out.Result); refused && out.State == values.FreeWorkWaiting {
		return values.FreeWorkOutcome{}, freeWorkError(why)
	}
	return out, nil
}

// call is the call card names, with the scope of its request: the request must be stored and the thread's own
// history must hold that call waiting on the card (thread.FreeWorkCall, which also decides how the call runs), so a
// card of another thread or a made-up one is not found.
func (f *FreeWorks) call(ctx context.Context, card values.FreeWorkCard) (values.Call, thread.Admission, values.Scope, error) {
	fail := func(err error) (values.Call, thread.Admission, values.Scope, error) {
		return values.Call{}, thread.Admission{}, values.Scope{}, err
	}
	if err := card.Validate(); err != nil {
		return fail(fmt.Errorf("%s: %w: %w", opFreeWork, shared.ErrNotFound, err))
	}
	scope, err := f.threads.RequestScope(ctx, card.Thread, card.Turn)
	if err != nil {
		return fail(fmt.Errorf("%s: %w", opFreeWork, err))
	}
	entries, err := f.history.Since(ctx, card.Thread, card.Turn, freeWorkHistory)
	if err != nil {
		return fail(fmt.Errorf("%s: %w", opFreeWork, err))
	}
	call, adm, err := thread.FreeWorkCall(entries, card.Turn, card.CallID)
	if err != nil {
		return fail(fmt.Errorf("%s: %w: %w", opFreeWork, shared.ErrNotFound, err))
	}
	return call, adm, scope, nil
}

// state reads the card's journal entry: none is waiting.
func (f *FreeWorks) state(ctx context.Context, card values.FreeWorkCard) (values.FreeWorkOutcome, error) {
	e, err := f.tools.journal.Find(ctx, card.Thread, card.Turn, card.CallID)
	if errors.Is(err, shared.ErrNotFound) {
		return values.FreeWorkOutcome{State: values.FreeWorkWaiting}, nil
	}
	if err != nil {
		return values.FreeWorkOutcome{}, fmt.Errorf("%s: %w", opFreeWork, err)
	}
	if e.Tool != values.ToolTriageAssignment {
		return values.FreeWorkOutcome{}, fmt.Errorf("%s: %w: the call is not a triage", opFreeWork, shared.ErrNotFound)
	}
	// FAILED, PENDING and UNKNOWN have no work of this call to show (UNKNOWN is settled at the next start).
	c, ok := e.Compensation.(action.RemoveWork)
	if !ok {
		return values.FreeWorkOutcome{State: values.FreeWorkFailed}, nil
	}
	out := values.FreeWorkOutcome{State: values.FreeWorkDone, Work: shared.WorkID(c.Work.ID())}
	switch e.Status {
	case action.StatusUndone:
		out.State = values.FreeWorkUndone
	case action.StatusDone, action.StatusUndoing, action.StatusUndoRefused:
	default:
		out.State = values.FreeWorkFailed
	}
	return out, nil
}

// freeWorkError maps the refusal of the triage to the errors of FreeWork.
func freeWorkError(why values.Refusal) error {
	switch why {
	case values.RefuseNotFound:
		return fmt.Errorf("%s: %w: %s", opFreeWork, shared.ErrNotFound, why)
	case values.RefuseUnavailable:
		return fmt.Errorf("%s: %w: %s", opFreeWork, shared.ErrServiceNotConfigured, why)
	}
	return fmt.Errorf("%s: %s", opFreeWork, why)
}
