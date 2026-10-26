package agent

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/knowledgebase"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/turnkey"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the write tools, for the log.
const (
	opJournal = "agent: action journal"
	opAction  = "agent: action"
)

// Journal is the action journal (cases/agentjournal) as the write tools use it: every action is PENDING before its
// use case runs and DONE or FAILED after (ADR-0021), a proposal of a C tool is PROPOSED; Find makes a call run again
// under the same ID act once.
type Journal interface {
	Find(ctx context.Context, th shared.ThreadID, turn int, callID string) (agentjournal.Entry, error)
	Begin(ctx context.Context, in agentjournal.BeginInput) (agentjournal.Entry, error)
	Finish(ctx context.Context, id shared.ActionID, out agentjournal.Outcome) (agentjournal.Entry, error)
	Propose(ctx context.Context, in agentjournal.BeginInput, proposal values.Ref) (agentjournal.Entry, error)
}

// ran is what running one call gave: the outcome for the step use case, whether it was an action (its outcome is
// real even when the Driver's context ended) and the cards it adds (the action line with «Отменить», the job).
type ran struct {
	out   agentturn.Outcome
	write bool
	cards []values.Card
}

// write runs an action (class U): journal PENDING, the use case, journal DONE or FAILED. Once started it runs to its
// end without the Driver's context: the use cases are local and short, and a half-known outcome would need Recover.
func (t tools) write(ctx context.Context, st agentturn.Step, c agentturn.Call) ran {
	id, tool := c.Call.ID(), c.Call.Tool()
	refuse := func(why values.Refusal) ran {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, why)}, write: true}
	}
	if t.journal == nil || t.actions == nil {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnavailable)}}
	}
	ctx = context.WithoutCancel(ctx)

	// The same call again (the step was replayed): its entry tells what happened, nothing runs twice.
	if e, err := t.journal.Find(ctx, st.Thread, st.Turn, id); err == nil {
		return t.replay(ctx, c, e)
	} else if !errors.Is(err, shared.ErrNotFound) {
		t.log.WarnContext(ctx, opJournal, "tool", tool.String(), "err", err)
		return refuse(values.RefuseFailed)
	}

	p, ok, err := t.actions.prepare(ctx, originOf(st), c.Call)
	switch {
	case !ok:
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnavailable)}}
	case err != nil:
		return refuse(t.refusalOf(ctx, tool, err))
	case p.noop != nil:
		return t.result(c, *p.noop, values.Ref{}, nil)
	}

	entry, err := t.journal.Begin(ctx, agentjournal.BeginInput{
		Thread: st.Thread, Turn: st.Turn, CallID: id, Tool: tool, Intent: p.intent,
	})
	if err != nil {
		if e, ferr := t.journal.Find(ctx, st.Thread, st.Turn, id); ferr == nil {
			return t.replay(ctx, c, e) // the same call began meanwhile
		}
		t.log.WarnContext(ctx, opJournal, "tool", tool.String(), "err", err)
		return refuse(values.RefuseFailed)
	}
	eff, err := p.run(ctx)
	if err != nil || eff.failed {
		if _, ferr := t.journal.Finish(ctx, entry.ID, agentjournal.Outcome{Failed: true}); ferr != nil {
			t.log.WarnContext(ctx, opJournal, "action", entry.ID.String(), "err", ferr)
		}
		if err != nil {
			return refuse(t.refusalOf(ctx, tool, err))
		}
		eff.created = values.Ref{}
		return t.outcome(c, eff, eff.cards) // not this action's: no «Отменить»
	}
	done := agentjournal.Outcome{Compensation: eff.comp, TargetVersion: eff.version}
	if _, err := t.journal.Finish(ctx, entry.ID, done); err != nil {
		// The action is done; the entry stays PENDING and Recover settles it from its intent at the next start.
		t.log.WarnContext(ctx, opJournal, "action", entry.ID.String(), "err", err)
	}
	line := values.ActionLine{Action: entry.ID}
	if eff.created.Kind() == values.KindAssignment {
		// The Driver fills its title, subject and deadline from the local data when it publishes the line.
		line.Assignment = values.AssignmentLine{Assignment: eff.created}
	}
	cards := []values.Card{line}
	if !eff.row.Job.IsZero() {
		cards = append(cards, values.ProgressCard{Job: shared.JobID(eff.row.Job.ID())})
	}
	return t.outcome(c, eff, append(cards, eff.cards...))
}

// outcome is the result of an action that went through: the triage row of triage_assignment, else its write row.
func (t tools) outcome(c agentturn.Call, eff effect, cards []values.Card) ran {
	if eff.triage == nil {
		return t.result(c, eff.row, eff.created, cards)
	}
	res, seen, err := values.TriageResult(c.Call.ID(), *eff.triage)
	if err != nil {
		t.log.Warn(opAction, "tool", c.Call.Tool().String(), "err", err)
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(c.Call.ID(), values.RefuseFailed)}, write: true}
	}
	return ran{out: agentturn.Outcome{Result: res, Seen: seen}, write: true, cards: cards}
}

// result is the outcome of a call that went through.
func (t tools) result(c agentturn.Call, row values.WriteRow, created values.Ref, cards []values.Card) ran {
	res, seen, err := values.WriteResult(c.Call.ID(), row)
	if err != nil {
		t.log.Warn(opAction, "tool", c.Call.Tool().String(), "err", err)
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(c.Call.ID(), values.RefuseFailed)}, write: true}
	}
	return ran{out: agentturn.Outcome{Result: res, Seen: seen, Created: created}, write: true, cards: cards}
}

// replay is the outcome of a call that has an entry already: the result it had, rebuilt from the compensation (a
// triage from its work as it is now). A call ID reused for another tool is refused, never answered with the other
// tool's result.
func (t tools) replay(ctx context.Context, c agentturn.Call, e agentjournal.Entry) ran {
	id := c.Call.ID()
	if e.Tool != c.Call.Tool() {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseInvalidArgs)}, write: true}
	}
	switch e.Status {
	case action.StatusFailed:
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseFailed)}, write: true}
	case action.StatusPending, action.StatusUnknown, action.StatusProposed:
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnknown)}, write: true}
	}
	row := values.WriteRow{Changed: true}
	var created values.Ref
	switch v := e.Compensation.(type) {
	case action.RemoveSubject:
		row.ID, created = v.Subject, v.Subject
	case action.RemoveFolder:
		row.ID, created = v.Folder, v.Folder
	case action.ReturnToInbox:
		row.ID = v.Material
	case action.CancelJob:
		row.Job = v.Job
	case action.StopWork:
		row.Job = v.Job
	case action.RemoveAssignment:
		row.ID, created = v.Assignment, v.Assignment
	case action.RemoveWork:
		tr, err := t.actions.triageReplay(ctx, v.Work)
		if err != nil {
			return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, t.refusalOf(ctx, c.Call.Tool(), err))}, write: true}
		}
		return t.outcome(c, effect{triage: &tr}, nil)
	}
	return t.result(c, row, created, nil)
}

// refusalOf maps an error of an action's use case to the enum the planner gets (ADR-0022: never the text).
func (t tools) refusalOf(ctx context.Context, tool values.Tool, err error) values.Refusal {
	// The subject or lesson of a new assignment the use case does not know: an ID that passed provenance but is gone.
	if tool == values.ToolCreateAssignment &&
		(errors.Is(err, homeworkflow.ErrSubjectNotFound) || errors.Is(err, shared.ErrNotFound)) {
		return values.RefuseUnknownID
	}
	switch {
	case errors.Is(err, library.ErrNameTaken), errors.Is(err, library.ErrFolderTaken):
		return values.RefuseNameTaken
	case errors.Is(err, library.ErrSubjectNotFound), errors.Is(err, library.ErrFolderNotFound),
		errors.Is(err, library.ErrMaterialNotFound), errors.Is(err, library.ErrLessonNotFound),
		errors.Is(err, knowledgebase.ErrRecordingNotFound), errors.Is(err, shared.ErrNotFound),
		errors.Is(err, turnkey.ErrSourceNotFound), errors.Is(err, turnkey.ErrNotFound):
		return values.RefuseNotFound
	case errors.Is(err, turnkey.ErrNoText):
		return values.RefuseNoText
	case errors.Is(err, turnkey.ErrNotWritable):
		return values.RefuseNotNow // the student confirms the requirements first
	case errors.Is(err, turnkey.ErrNotConfigured):
		return values.RefuseUnavailable
	case errors.Is(err, library.ErrInvalidName), errors.Is(err, library.ErrInvalidFolderName),
		errors.Is(err, library.ErrInvalidRequest), errors.Is(err, knowledgebase.ErrInvalidLessonInfo),
		errors.Is(err, assignment.ErrInvalidDetails), errors.Is(err, turnkey.ErrInvalidRequest):
		return values.RefuseInvalidArgs
	}
	t.log.WarnContext(ctx, opAction, "tool", tool.String(), "err", err)
	return values.RefuseFailed
}
