package agent

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the proposal tools, for the log.
const (
	opPropose        = "agent: proposal"
	opProposeJournal = "agent: proposal journal"
)

// Proposals makes the proposals of the C tools (cases/homeworkflow). It has no method that confirms, executes or
// sends: «Да» reaches the use case only from input/wails, on the student's click (ADR-0021, ADR-0022).
type Proposals interface {
	ProposeDeadline(ctx context.Context, a shared.AssignmentID) (homeworkflow.ProposalView, error)
	ProposeEmailOfVersion(ctx context.Context, a shared.AssignmentID, n int) (homeworkflow.ProposalView, error)
}

// DeletionPreviews is the recording deletion (cases/recordingdelete) as propose_delete_recording uses it: the
// preview only, nothing is deleted.
type DeletionPreviews interface {
	Preview(ctx context.Context, id shared.RecordingID) (recordingdelete.Preview, error)
}

// DraftVersions lists the draft versions of an assignment (cases/homeworkflow), so list_assignments shows the version
// propose_send may name.
type DraftVersions interface {
	DraftVersions(ctx context.Context, a shared.AssignmentID) ([]homeworkflow.DraftVersionView, error)
}

// propose runs a C call: the use case makes a proposal or a deletion preview, the journal records it PROPOSED and the
// panel gets a confirm card bound to it. Nothing irreversible runs here; the recipient, the file and the date come
// from local state, never from the arguments (the catalog has no such argument). A call run again under the same ID
// answers from its journal entry and proposes nothing new.
func (t tools) propose(ctx context.Context, st agentturn.Step, c agentturn.Call) ran {
	id, tool := c.Call.ID(), c.Call.Tool()
	refuse := func(why values.Refusal) ran {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, why)}, write: true}
	}
	if t.journal == nil || !t.canPropose(tool) {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseUnavailable)}}
	}
	ctx = context.WithoutCancel(ctx) // a proposal once made is real; its card must reach the panel

	if e, err := t.journal.Find(ctx, st.Thread, st.Turn, id); err == nil {
		return t.replayProposal(c, e)
	} else if !errors.Is(err, shared.ErrNotFound) {
		t.log.WarnContext(ctx, opProposeJournal, "tool", tool.String(), "err", err)
		return refuse(values.RefuseFailed)
	}

	target, why := t.makeProposal(ctx, c.Call)
	if why != "" {
		return refuse(why)
	}
	in := agentjournal.BeginInput{Thread: st.Thread, Turn: st.Turn, CallID: id, Tool: tool}
	if _, err := t.journal.Propose(ctx, in, target); err != nil {
		if e, ferr := t.journal.Find(ctx, st.Thread, st.Turn, id); ferr == nil {
			return t.replayProposal(c, e) // the same call was journaled meanwhile
		}
		// The proposal exists and waits in «Ждёт вашего «Да»»; only its journal line is missing.
		t.log.WarnContext(ctx, opProposeJournal, "tool", tool.String(), "err", err)
	}
	res, seen, err := values.ProposalResult(id, target)
	if err != nil {
		t.log.Warn(opPropose, "tool", tool.String(), "err", err)
		return refuse(values.RefuseFailed)
	}
	return ran{out: agentturn.Outcome{Result: res, Seen: seen}, write: true, cards: []values.Card{values.ConfirmCard{Target: target}}}
}

// canPropose reports whether the use case of a C tool is wired.
func (t tools) canPropose(tool values.Tool) bool {
	switch tool {
	case values.ToolProposeDeadline, values.ToolProposeSend:
		return t.proposals != nil
	case values.ToolProposeDeleteRecording:
		return t.deletions != nil
	}
	return false
}

// makeProposal calls the use case of a C tool and returns the proposal or preview the card is bound to, or why
// nothing was proposed.
func (t tools) makeProposal(ctx context.Context, call values.Call) (values.Ref, values.Refusal) {
	args := call.Args()
	var (
		view homeworkflow.ProposalView
		err  error
	)
	switch call.Tool() {
	case values.ToolProposeDeadline:
		a, _ := args.ID("assignment")
		view, err = t.proposals.ProposeDeadline(ctx, shared.AssignmentID(a.ID()))
	case values.ToolProposeSend:
		a, _ := args.ID("assignment")
		v, _ := args.ID("version")
		n, verr := values.VersionNumber(v)
		if verr != nil {
			return values.Ref{}, values.RefuseInvalidArgs
		}
		view, err = t.proposals.ProposeEmailOfVersion(ctx, shared.AssignmentID(a.ID()), n)
	case values.ToolProposeDeleteRecording:
		return t.previewDeletion(ctx, call)
	default:
		return values.Ref{}, values.RefuseUnavailable
	}
	if err != nil {
		return values.Ref{}, t.proposalRefusal(ctx, call.Tool(), err)
	}
	if view.Status != proposal.StatusValidated {
		return values.Ref{}, rejected(view.Reason)
	}
	target, ok := refOfID(values.KindProposal, view.ID.String())
	if !ok {
		t.log.WarnContext(ctx, opPropose, "tool", call.Tool().String(), "err", "proposal id not addressable")
		return values.Ref{}, values.RefuseFailed
	}
	return target, ""
}

// previewDeletion checks that the recording can be deleted now; the preview is the recording itself, read again by
// the confirm card, so the student sees what goes at the moment of the click.
func (t tools) previewDeletion(ctx context.Context, call values.Call) (values.Ref, values.Refusal) {
	r, _ := call.Args().ID("recording")
	p, err := t.deletions.Preview(ctx, shared.RecordingID(r.ID()))
	if err != nil {
		return values.Ref{}, t.proposalRefusal(ctx, call.Tool(), err)
	}
	if p.Refusal != recordingdelete.RefusalNone {
		return values.Ref{}, values.RefuseNotDeletable
	}
	target, ok := refOfID(values.KindPreview, string(p.Recording))
	if !ok {
		return values.Ref{}, values.RefuseFailed
	}
	return target, ""
}

// rejected maps why a proposal was rejected to the enum the planner gets.
func rejected(r proposal.RejectReason) values.Refusal {
	switch r {
	case proposal.ReasonUnknownAssignment:
		return values.RefuseNotFound
	case proposal.ReasonNoRecipient:
		return values.RefuseNoRecipient
	case proposal.ReasonNoArtifact:
		return values.RefuseNotExported
	case proposal.ReasonDueOutOfRange:
		return values.RefuseDueOutOfRange
	}
	return values.RefuseFailed
}

// proposalRefusal maps an error of a proposal use case to the enum the planner gets (ADR-0022: never the text).
func (t tools) proposalRefusal(ctx context.Context, tool values.Tool, err error) values.Refusal {
	switch {
	case errors.Is(err, shared.ErrNotFound), errors.Is(err, recordingdelete.ErrNotFound):
		return values.RefuseNotFound
	case errors.Is(err, homeworkflow.ErrNoDue):
		return values.RefuseNoDue
	case errors.Is(err, homeworkflow.ErrVersionNotExported):
		return values.RefuseNotExported
	case errors.Is(err, shared.ErrInvalidID), errors.Is(err, recordingdelete.ErrInvalidRequest):
		return values.RefuseInvalidArgs
	}
	t.log.WarnContext(ctx, opPropose, "tool", tool.String(), "err", err)
	return values.RefuseFailed
}

// replayProposal answers a C call that has a journal entry: the proposal it made, without a second card. A call ID
// reused for another tool is refused.
func (t tools) replayProposal(c agentturn.Call, e agentjournal.Entry) ran {
	id := c.Call.ID()
	if e.Tool != c.Call.Tool() || e.Status != action.StatusProposed || e.Proposal.IsZero() {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseInvalidArgs)}, write: true}
	}
	res, seen, err := values.ProposalResult(id, e.Proposal)
	if err != nil {
		return ran{out: agentturn.Outcome{Result: values.RefusalResult(id, values.RefuseFailed)}, write: true}
	}
	return ran{out: agentturn.Outcome{Result: res, Seen: seen}, write: true}
}
