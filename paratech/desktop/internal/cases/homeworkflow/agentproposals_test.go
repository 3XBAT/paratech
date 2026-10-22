package homeworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/homeworkdraft"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

// dueAssignments is assignmentRepo with deadlines: a1 is due in six days, a2 has none.
type dueAssignments struct {
	assignmentRepo
	due map[shared.AssignmentID]time.Time
}

func (r dueAssignments) Get(ctx context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	if _, err := r.assignmentRepo.Get(ctx, id); err != nil {
		return nil, err
	}
	return assignment.Restore(assignment.State{
		Version: 1, ID: id, SourceIndex: -1, Title: fakeAssignmentTitle, Due: r.due[id], CreatedAt: t0, UpdatedAt: t0,
	})
}

// memDraftReader holds the drafts of the agent tests: version 1 «v1 text» and version 2 «v2 text» of a1.
type memDraftReader struct {
	drafts map[shared.AssignmentID]*homeworkdraft.Draft
	err    error
}

func (r *memDraftReader) Get(_ context.Context, a shared.AssignmentID) (*homeworkdraft.Draft, error) {
	if r.err != nil {
		return nil, r.err
	}
	d, ok := r.drafts[a]
	if !ok {
		return nil, fmt.Errorf("draft %s: %w", a, shared.ErrNotFound)
	}
	return d, nil
}

// waitingList lists the stored proposals of one status (the order does not matter to the callers).
type waitingList struct{ s *store }

func (w waitingList) ListByStatus(_ context.Context, st proposal.Status, limit int) ([]*proposal.Proposal, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	var out []*proposal.Proposal
	for _, ps := range w.s.proposals {
		if ps.Status == st && len(out) < limit {
			p, err := proposal.Restore(ps)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
	}
	return out, nil
}

type agentEnv struct {
	*env
	drafts *memDraftReader
}

func newAgentProposalEnv(t *testing.T, withDrafts bool) *agentEnv {
	t.Helper()
	e := &agentEnv{env: newEnv(t).withHomework(t)}
	d, err := homeworkdraft.Restore(homeworkdraft.State{Version: 1, Assignment: "a1", Revisions: []homeworkdraft.RevisionState{
		{Number: 1, Origin: homeworkdraft.OriginEdited, Title: "Задачи", Body: "v1 text", CreatedAt: t0},
		{Number: 2, Origin: homeworkdraft.OriginEdited, Title: "Задачи", Body: "v2 text", CreatedAt: t0},
	}})
	if err != nil {
		t.Fatal(err)
	}
	e.drafts = &memDraftReader{drafts: map[shared.AssignmentID]*homeworkdraft.Draft{"a1": d}}
	deps := Deps{
		Proposals: proposalRepo{e.store}, Assignments: dueAssignments{assignmentRepo{e.store}, map[shared.AssignmentID]time.Time{
			"a1": t0.Add(6 * 24 * time.Hour),
		}},
		AssignmentList: assignmentList{e.store}, Artifacts: e.files, Recipients: recipients{e.store}, Mailer: e.mailer,
		Exporter: e.exporter, Calendar: e.calendar, Recordings: noRecordings{}, Lessons: noLessons{}, Clock: e.clock,
		IDs: ids{e.store}, Tx: tx{e.store}, Waiting: waitingList{e.store},
	}
	if withDrafts {
		deps.Drafts = e.drafts
	}
	if e.svc, err = New(deps); err != nil {
		t.Fatal(err)
	}
	return e
}

// exportVersion writes the text export of draft version n of a1, as Drafts.ExportDraft does.
func (e *agentEnv) exportVersion(t *testing.T, n int) {
	t.Helper()
	r, ok := e.drafts.drafts["a1"].Revision(n)
	if !ok {
		t.Fatalf("no version %d", n)
	}
	ref, _ := proposal.ExportRef("a1")
	if err := e.files.Write(t.Context(), ref, strings.NewReader(r.Markdown())); err != nil {
		t.Fatal(err)
	}
}

func TestProposeDeadlineTakesTheStoredAssignment(t *testing.T) {
	e := newAgentProposalEnv(t, false)
	e.assignments["a2"] = true
	v, err := e.svc.ProposeDeadline(t.Context(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != proposal.KindDeadlineEvent || v.Status != proposal.StatusValidated || v.Title != fakeAssignmentTitle ||
		!v.Due.Equal(t0.Add(6*24*time.Hour)) || v.Recipient != "" {
		t.Fatalf("view = %+v", v)
	}

	// The same deadline again: the waiting proposal, not a second «Да».
	again, err := e.svc.ProposeDeadline(t.Context(), "a1")
	if err != nil || again.ID != v.ID || len(e.proposals) != 1 {
		t.Fatalf("again = %+v, %v; %d proposals", again, err, len(e.proposals))
	}

	for _, tc := range []struct {
		name string
		a    shared.AssignmentID
		want error
	}{
		{"no deadline", "a2", ErrNoDue},
		{"unknown assignment", "a9", shared.ErrNotFound},
		{"invalid id", "a/1", shared.ErrInvalidID},
	} {
		if _, err := e.svc.ProposeDeadline(t.Context(), tc.a); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(e.proposals) != 1 || e.sideEffects() != 0 {
		t.Fatalf("%d proposals, %d side effects", len(e.proposals), e.sideEffects())
	}
}

// A deadline that changed since the waiting proposal is a new proposal; a declined one is never reused.
func TestProposeDeadlineAfterAChange(t *testing.T) {
	e := newAgentProposalEnv(t, false)
	first, err := e.svc.ProposeDeadline(t.Context(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Decline(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.ProposeDeadline(t.Context(), "a1")
	if err != nil || second.ID == first.ID || second.Status != proposal.StatusValidated {
		t.Fatalf("after a decline = %+v, %v", second, err)
	}
}

// «Да» on the deadline puts it into the calendar once; the second click changes nothing (hard rule 7).
func TestConfirmDeadlineIsIdempotent(t *testing.T) {
	e := newAgentProposalEnv(t, false)
	v, err := e.svc.ProposeDeadline(t.Context(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	for click := range 2 {
		got, err := e.svc.ConfirmDeadline(t.Context(), v.ID)
		if err != nil || got.Status != proposal.StatusExecuted {
			t.Fatalf("click %d = %+v, %v", click+1, got, err)
		}
	}
	if n := len(e.calendar.Calls()); n != 1 {
		t.Fatalf("calendar calls = %d, want 1", n)
	}
}

// A failed calendar call leaves the proposal CONFIRMED; the next click executes it, still one event.
func TestConfirmDeadlineRetriesAFailedCalendar(t *testing.T) {
	e := newAgentProposalEnv(t, false)
	v, err := e.svc.ProposeDeadline(t.Context(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	e.calendar.Fail(ErrCalendarNotConnected)
	if _, err := e.svc.ConfirmDeadline(t.Context(), v.ID); !errors.Is(err, ErrCalendarNotConnected) {
		t.Fatalf("first click = %v", err)
	}
	if st := e.proposals[v.ID].Status; st != proposal.StatusConfirmed {
		t.Fatalf("after a failed calendar: %s", st)
	}
	e.calendar.Fail(nil)
	if got, err := e.svc.ConfirmDeadline(t.Context(), v.ID); err != nil || got.Status != proposal.StatusExecuted {
		t.Fatalf("second click = %+v, %v", got, err)
	}
}

func TestConfirmDeadlineRefusesOtherProposals(t *testing.T) {
	e := newAgentProposalEnv(t, false)
	send := propose(t, e.env, outEmail)
	if _, err := e.svc.ConfirmDeadline(t.Context(), send.ID); !errors.Is(err, ErrNotADeadline) {
		t.Fatalf("a send = %v", err)
	}
	if e.proposals[send.ID].Status != proposal.StatusValidated || e.sideEffects() != 0 {
		t.Fatal("a send was confirmed through the deadline path")
	}
	v, _ := e.svc.ProposeDeadline(t.Context(), "a1")
	if _, err := e.svc.Decline(t.Context(), v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ConfirmDeadline(t.Context(), v.ID); !errors.Is(err, proposal.ErrInvalidTransition) {
		t.Fatalf("a declined deadline = %v", err)
	}
	if _, err := e.svc.ConfirmDeadline(t.Context(), "p404"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestProposeEmailOfVersion(t *testing.T) {
	tests := []struct {
		name     string
		exported int // 0: the "my homework" export of withHomework
		version  int
		setup    func(e *agentEnv)
		status   proposal.Status
		reason   proposal.RejectReason
		err      error
	}{
		{"the exported version", 2, 2, nil, proposal.StatusValidated, "", nil},
		{"an older version exported", 1, 2, nil, "", "", ErrVersionNotExported},
		{"another text exported", 0, 1, nil, "", "", ErrVersionNotExported},
		{"no such version", 2, 3, nil, "", "", shared.ErrNotFound},
		{"no recipient", 2, 2, func(e *agentEnv) { e.recipient = "" }, proposal.StatusRejected, proposal.ReasonNoRecipient, nil},
		{"nothing exported", 2, 2, func(e *agentEnv) {
			ref, _ := proposal.ExportRef("a1")
			_ = e.files.Delete(t.Context(), ref)
		}, proposal.StatusRejected, proposal.ReasonNoArtifact, nil},
		{"drafts unreadable", 2, 2, func(e *agentEnv) { e.drafts.err = errors.New("disk") }, "", "", errors.New("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAgentProposalEnv(t, true)
			if tt.exported > 0 {
				e.exportVersion(t, tt.exported)
			}
			if tt.setup != nil {
				tt.setup(e)
			}
			before := len(e.proposals)
			v, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", tt.version)
			switch {
			case tt.err != nil && tt.err.Error() == "":
				if err == nil {
					t.Fatal("want an error")
				}
			case tt.err != nil:
				if !errors.Is(err, tt.err) || len(e.proposals) != before {
					t.Fatalf("err = %v, %d proposals stored", err, len(e.proposals)-before)
				}
			default:
				if err != nil || v.Status != tt.status || v.Reason != tt.reason {
					t.Fatalf("view = %+v, %v", v, err)
				}
			}
			if e.sideEffects() != 0 {
				t.Fatal("a proposal had a side effect")
			}
		})
	}
}

// The confirmation shows the recipient of the settings and the exact version of the attachment, derived from the
// pinned digest; a later export of another version leaves the shown version and makes the send STALE.
func TestEmailOfVersionShowsTheVersionAndGoesStale(t *testing.T) {
	e := newAgentProposalEnv(t, true)
	e.exportVersion(t, 2)
	v, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if v.Recipient != "teacher@school.example" || v.DraftVersion != 2 || v.AttachmentFormat != proposal.FormatText ||
		v.Subject == "" || v.AttachmentSize == 0 {
		t.Fatalf("view = %+v", v)
	}
	if got, err := e.svc.Get(t.Context(), v.ID); err != nil || got.DraftVersion != 2 {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	e.exportVersion(t, 1)
	if got, _ := e.svc.Get(t.Context(), v.ID); got.DraftVersion != 2 {
		t.Fatalf("the shown version follows the pinned file, got %d", got.DraftVersion)
	}
	if _, err := e.svc.ConfirmSend(t.Context(), v.ID, v.Subject, proposal.FormatText); !errors.Is(err, proposal.ErrStale) {
		t.Fatalf("send after another export = %v, want ErrStale", err)
	}
	if len(e.mailer.Calls()) != 0 {
		t.Fatal("an email went out with another version")
	}
}

// A replayed propose_send (crash or journal failure after the save) returns the waiting send, never a second one
// that a second «Да» could email; a declined send or another version is a new proposal.
func TestEmailOfVersionReplayKeepsOneSend(t *testing.T) {
	e := newAgentProposalEnv(t, true)
	e.exportVersion(t, 2)
	first, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 2)
	if err != nil || first.Status != proposal.StatusValidated {
		t.Fatalf("first = %+v, %v", first, err)
	}
	// No journal entry was written: the agent calls the use case again.
	again, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 2)
	if err != nil || again.ID != first.ID || again.DraftVersion != 2 || again.Subject != first.Subject {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	if n := len(e.proposals); n != 1 {
		t.Fatalf("%d proposals after a replay, want 1", n)
	}

	if _, err := e.svc.Decline(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	after, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 2)
	if err != nil || after.ID == first.ID || after.Status != proposal.StatusValidated {
		t.Fatalf("after a decline = %+v, %v", after, err)
	}
	e.exportVersion(t, 1)
	other, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 1)
	if err != nil || other.ID == after.ID || other.DraftVersion != 1 {
		t.Fatalf("another version = %+v, %v", other, err)
	}
	if e.sideEffects() != 0 {
		t.Fatal("a proposal had a side effect")
	}
}

// A failing Waiting port stores nothing.
func TestEmailOfVersionWaitingFails(t *testing.T) {
	e := newAgentProposalEnv(t, true)
	e.exportVersion(t, 2)
	e.svc.waiting = failingWaiting{}
	if _, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 2); err == nil || len(e.proposals) != 0 {
		t.Fatalf("err = %v, %d proposals", err, len(e.proposals))
	}
}

type failingWaiting struct{}

func (failingWaiting) ListByStatus(context.Context, proposal.Status, int) ([]*proposal.Proposal, error) {
	return nil, errors.New("disk")
}

func TestEmailOfVersionWithoutDrafts(t *testing.T) {
	e := newAgentProposalEnv(t, false)
	if _, err := e.svc.ProposeEmailOfVersion(t.Context(), "a1", 1); !errors.Is(err, ErrNoDrafts) {
		t.Fatalf("err = %v", err)
	}
	// The existing send shows no version without the drafts.
	v, err := e.svc.ProposeSend(t.Context(), "a1", proposal.FormatText)
	if err != nil || v.DraftVersion != 0 {
		t.Fatalf("ProposeSend = %+v, %v", v, err)
	}
}
