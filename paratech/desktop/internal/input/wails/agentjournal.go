package wails

import (
	"context"
	"errors"
	"time"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/delivery/delivery"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opAgentHistory     = "ui: agent journal"
	opAgentUndo        = "ui: agent undo"
	opAgentUndoRequest = "ui: agent undo the request"
)

// AgentHistoryDays is how far back «Журнал» and the panel history read (D6).
const AgentHistoryDays = 90

// Refusals of Undo (AgentUndoView.Refusal): the entry stays as it was, the page words why.
const (
	AgentUndoChanged     = "changed"      // «Изменено после — отменить нельзя»
	AgentUndoResultReady = "result_ready" // the paid result is ready, the previous one is not kept
	AgentUndoCannotStop  = "cannot_stop"  // the job runs and cannot be stopped now
	AgentUndoNameTaken   = "name_taken"   // restoring would clash with a subject or folder of the same name
	AgentUndoNotFound    = "not_found"    // no such journal entry
	AgentUndoNotUndoable = "not_undoable" // the entry offers no «Отменить» (already undone, running, failed, ...)
)

// Kinds of a journal line (AgentJournalLineView.Kind and AgentHistoryFilter.Kind).
const (
	AgentLineAction     = string(agentjournal.LineAction)
	AgentLineProcessing = string(agentjournal.LineProcessing)
)

// AgentNoUndoSent is the NoUndo of a processing line that sent an email: «Отменить нельзя».
const AgentNoUndoSent = "sent"

// AgentJournal is the action journal (cases/agentjournal) as the panel and «Журнал» use it; satisfied by
// agentjournal.Service.
type AgentJournal interface {
	List(ctx context.Context, limit int) ([]agentjournal.Line, error)
	Undo(ctx context.Context, id shared.ActionID) (agentjournal.Entry, error)
	UndoRequest(ctx context.Context, th shared.ThreadID, turn int) ([]agentjournal.Entry, error)
}

// AgentClock gives the time the history window ends at.
type AgentClock interface {
	Now() time.Time
}

// AgentHistoryFilter narrows «Журнал». Empty fields match every line. Kind is action or processing; Status is an
// action status (DONE, UNDONE, UNDO_REFUSED, ...) or a job status (QUEUED, COMPLETED, ...); SubjectID matches the
// subject of an action's thread (processing lines have none); Days is 1..90, 0 means 90; Undoable keeps only the
// lines that offer «Отменить».
type AgentHistoryFilter struct {
	Kind      string `json:"kind"`
	SubjectID string `json:"subjectId"`
	Status    string `json:"status"`
	Days      int    `json:"days"`
	Undoable  bool   `json:"undoable"`
}

// AgentHistoryView is «Журнал»: the actions of paratech and the processing jobs, newest first, at most
// agentjournal.MaxList lines of the last 90 days. Problem is "", invalid (a malformed filter) or failed.
type AgentHistoryView struct {
	Problem string                 `json:"problem"`
	Lines   []AgentJournalLineView `json:"lines"`
}

// AgentJournalLineView is one line of «Журнал»: ids and enums only, the page words them. Times are RFC 3339 in UTC.
// An action line has actionId, threadId, turn and tool; a processing line jobId and jobType. Status is the action or
// job status. Undoable: «Отменить» is offered; CannotUndo: «Отменить нельзя» (a proposal, a sent email); NoUndo is
// why there is no «Отменить» (running, failed, unknown, undone, irreversible, changed, result_ready, cannot_stop,
// sent), "" when it is offered or there is nothing to say.
type AgentJournalLineView struct {
	Kind       string `json:"kind"`
	At         string `json:"at"`
	UpdatedAt  string `json:"updatedAt"`
	SubjectID  string `json:"subjectId"`
	ActionID   string `json:"actionId"`
	ThreadID   string `json:"threadId"`
	Turn       int    `json:"turn"`
	Tool       string `json:"tool"`
	JobID      string `json:"jobId"`
	JobType    string `json:"jobType"`
	Status     string `json:"status"`
	Undoable   bool   `json:"undoable"`
	CannotUndo bool   `json:"cannotUndo"`
	NoUndo     string `json:"noUndo"`
}

// AgentUndoView is the outcome of «Отменить» of one action. Problem is "", invalid, unavailable, closed or failed;
// when it is "", Status is the entry's status after the call (UNDONE, or UNDO_REFUSED with Refusal) or Refusal says
// why nothing was done.
type AgentUndoView struct {
	Problem  string `json:"problem"`
	ActionID string `json:"actionId"`
	Status   string `json:"status"`
	Refusal  string `json:"refusal"`
}

// AgentUndoRequestView is the outcome of «Отменить всё» of a request: the actions it went through, newest first.
// Problem is "", invalid, unavailable, closed, or failed when some action could not be undone (the others are listed).
type AgentUndoRequestView struct {
	Problem string          `json:"problem"`
	Actions []AgentUndoView `json:"actions"`
}

// History returns «Журнал» filtered as asked; it reads only the local database.
func (s *AgentService) History(ctx context.Context, f AgentHistoryFilter) AgentHistoryView {
	match, ok := historyMatcher(f, s.clock.Now())
	if !ok {
		return AgentHistoryView{Problem: AgentProblemInvalid, Lines: []AgentJournalLineView{}}
	}
	lines, err := s.journal.List(ctx, agentjournal.MaxList)
	if err != nil {
		s.log.Warn(opAgentHistory, "err", err)
		return AgentHistoryView{Problem: AgentProblemFailed, Lines: []AgentJournalLineView{}}
	}
	out := make([]AgentJournalLineView, 0, len(lines))
	for _, l := range lines {
		if v := journalLineView(l); match(l, v) {
			out = append(out, v)
		}
	}
	return AgentHistoryView{Lines: out}
}

// Undo is «Отменить» of an action line; it runs on the app-lifetime ctx, so a closed page does not leave the entry
// half undone.
func (s *AgentService) Undo(_ context.Context, actionID string) AgentUndoView {
	id := shared.ActionID(actionID)
	if id.Validate() != nil {
		return AgentUndoView{Problem: AgentProblemInvalid, ActionID: actionID}
	}
	if s.life.Err() != nil {
		return AgentUndoView{Problem: AgentProblemClosed, ActionID: actionID}
	}
	e, err := s.journal.Undo(s.life, id)
	if err != nil {
		v := s.undoFailed(opAgentUndo, err)
		v.ActionID = actionID
		return v
	}
	return undoView(e)
}

// UndoRequest is «Отменить всё» of a request: every action of it that can still be undone, newest first.
func (s *AgentService) UndoRequest(_ context.Context, ref AgentTurnRef) AgentUndoRequestView {
	th := shared.ThreadID(ref.ThreadID)
	if th.Validate() != nil || ref.Turn < 1 {
		return AgentUndoRequestView{Problem: AgentProblemInvalid, Actions: []AgentUndoView{}}
	}
	if s.life.Err() != nil {
		return AgentUndoRequestView{Problem: AgentProblemClosed, Actions: []AgentUndoView{}}
	}
	entries, err := s.journal.UndoRequest(s.life, th, ref.Turn)
	out := AgentUndoRequestView{Actions: make([]AgentUndoView, 0, len(entries))}
	for _, e := range entries {
		out.Actions = append(out.Actions, undoView(e))
	}
	if err != nil {
		switch {
		case errors.Is(err, agentjournal.ErrUndoUnavailable):
			out.Problem = AgentProblemUnavailable
		case s.life.Err() != nil:
			out.Problem = AgentProblemClosed
		default:
			s.log.Warn(opAgentUndoRequest, "err", err)
			out.Problem = AgentProblemFailed
		}
	}
	return out
}

// undoFailed maps an error of Undo: a refusal the page words, or a problem.
func (s *AgentService) undoFailed(op string, err error) AgentUndoView {
	if why, ok := action.UndoRefusalOf(err); ok {
		return AgentUndoView{Refusal: string(why)}
	}
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return AgentUndoView{Refusal: AgentUndoNotFound}
	case errors.Is(err, action.ErrInvalidTransition):
		return AgentUndoView{Refusal: AgentUndoNotUndoable}
	case errors.Is(err, library.ErrNameTaken), errors.Is(err, library.ErrFolderTaken):
		return AgentUndoView{Refusal: AgentUndoNameTaken}
	case errors.Is(err, agentjournal.ErrUndoUnavailable):
		return AgentUndoView{Problem: AgentProblemUnavailable}
	case s.life.Err() != nil:
		return AgentUndoView{Problem: AgentProblemClosed}
	}
	s.log.Warn(op, "err", err)
	return AgentUndoView{Problem: AgentProblemFailed}
}

func undoView(e agentjournal.Entry) AgentUndoView {
	v := AgentUndoView{ActionID: string(e.ID), Status: string(e.Status)}
	if e.Status == action.StatusUndoRefused {
		v.Refusal = string(e.NoUndo)
	}
	return v
}

// historyMatcher checks the filter and returns its test; the window ends at now.
func historyMatcher(f AgentHistoryFilter, now time.Time) (func(agentjournal.Line, AgentJournalLineView) bool, bool) {
	if f.Days < 0 || f.Days > AgentHistoryDays {
		return nil, false
	}
	if f.Kind != "" && f.Kind != AgentLineAction && f.Kind != AgentLineProcessing {
		return nil, false
	}
	if f.SubjectID != "" && shared.SubjectID(f.SubjectID).Validate() != nil {
		return nil, false
	}
	if f.Status != "" && !action.Status(f.Status).Valid() && !job.Status(f.Status).Valid() {
		return nil, false
	}
	days := f.Days
	if days == 0 {
		days = AgentHistoryDays
	}
	since := now.AddDate(0, 0, -days)
	return func(l agentjournal.Line, v AgentJournalLineView) bool {
		return !l.At.Before(since) && (f.Kind == "" || v.Kind == f.Kind) &&
			(f.SubjectID == "" || v.SubjectID == f.SubjectID) && (f.Status == "" || v.Status == f.Status) &&
			(!f.Undoable || v.Undoable)
	}, true
}

func journalLineView(l agentjournal.Line) AgentJournalLineView {
	v := AgentJournalLineView{
		Kind: string(l.Kind), At: timeText(l.At), UpdatedAt: timeText(l.UpdatedAt), SubjectID: string(l.Subject),
	}
	if l.Kind == agentjournal.LineProcessing {
		v.JobID, v.JobType, v.Status = string(l.Job), string(l.JobType), string(l.JobStatus)
		if l.JobType == delivery.JobType {
			v.CannotUndo, v.NoUndo = true, AgentNoUndoSent
		}
		return v
	}
	v.ActionID, v.ThreadID, v.Turn, v.Tool, v.Status = string(l.Action), string(l.Thread), l.Turn, string(l.Tool), string(l.Status)
	v.Undoable, v.NoUndo = l.CanUndo, string(l.NoUndo)
	v.CannotUndo = l.NoUndo == action.NoUndoIrreversible
	return v
}
