package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/recordingdelete"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/shared"
)

var confirmDue = time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)

// confirmProposals keeps proposal views and acts like the pipeline on them: a confirmed send or deadline is executed
// once, a second click returns it as it is.
type confirmProposals struct {
	views       map[proposal.ID]homeworkflow.ProposalView
	assignments map[shared.AssignmentID]homeworkflow.AssignmentView
	sends       int
	events      int
	unresolved  bool                       // an earlier email of the assignment has an unknown outcome
	err         error                      // returned by the confirmations when set
	chosen      homeworkflow.ChosenTeacher // the last ConfirmSendTo or ConfirmSendWorkTo
}

func newConfirmProposals() *confirmProposals {
	return &confirmProposals{
		views: map[proposal.ID]homeworkflow.ProposalView{
			"p-send": {
				ID: "p-send", Kind: proposal.KindSendHomework, Status: proposal.StatusValidated, AssignmentID: "a1",
				Recipient: "teacher@school.example", Subject: "ДЗ · Физика · до 24.10", AttachmentName: "homework.md",
				AttachmentSize: 42, AttachmentFormat: proposal.FormatText, DraftVersion: 2,
			},
			"p-due": {
				ID: "p-due", Kind: proposal.KindDeadlineEvent, Status: proposal.StatusValidated, AssignmentID: "a1",
				Title: "Задачи 1-5", Due: confirmDue,
			},
			"p-export": {ID: "p-export", Kind: proposal.KindExportHomework, Status: proposal.StatusValidated, AssignmentID: "a1"},
		},
		assignments: map[shared.AssignmentID]homeworkflow.AssignmentView{
			"a1": {ID: "a1", Title: "Задачи 1-5", Subject: "Физика", MaterialID: "m-mail"},
		},
	}
}

func (f *confirmProposals) ProposeDeadline(_ context.Context, a shared.AssignmentID) (homeworkflow.ProposalView, error) {
	switch a {
	case "a1":
		return f.views["p-due"], nil
	case "a2":
		return homeworkflow.ProposalView{}, fmt.Errorf("propose: %w", homeworkflow.ErrNoDue)
	case "a3":
		return homeworkflow.ProposalView{
			ID: "p-old", Kind: proposal.KindDeadlineEvent, Status: proposal.StatusRejected, AssignmentID: a,
			Reason: proposal.ReasonDueOutOfRange,
		}, nil
	}
	return homeworkflow.ProposalView{}, fmt.Errorf("assignment %s: %w", a, shared.ErrNotFound)
}

func (f *confirmProposals) Get(_ context.Context, id proposal.ID) (homeworkflow.ProposalView, error) {
	v, ok := f.views[id]
	if !ok {
		return v, fmt.Errorf("proposal %s: %w", id, shared.ErrNotFound)
	}
	return v, nil
}

func (f *confirmProposals) Assignment(_ context.Context, id shared.AssignmentID) (homeworkflow.AssignmentView, error) {
	a, ok := f.assignments[id]
	if !ok {
		return a, fmt.Errorf("assignment %s: %w", id, shared.ErrNotFound)
	}
	return a, nil
}

func (f *confirmProposals) ConfirmSendTo(_ context.Context, id proposal.ID, subject string, format proposal.Format,
	chosen homeworkflow.ChosenTeacher) (homeworkflow.ProposalView, error) {
	f.chosen = chosen
	v := f.views[id]
	switch {
	case f.err != nil:
		return v, f.err
	case v.Kind != proposal.KindSendHomework:
		return v, homeworkflow.ErrNotASend
	case v.Status == proposal.StatusExecuted:
		return v, nil
	case v.Status != proposal.StatusValidated && v.Status != proposal.StatusConfirmed: // ConfirmOnce
		return v, proposal.ErrInvalidTransition
	case subject != v.Subject || format != v.AttachmentFormat:
		v.Status = proposal.StatusStale
		f.views[id] = v
		return v, proposal.ErrStale
	case f.unresolved:
		v.Status = proposal.StatusConfirmed
		f.views[id] = v
		return v, senddelivery.ErrUnresolvedDelivery
	}
	f.sends++
	v.Status = proposal.StatusExecuted
	f.views[id] = v
	return v, nil
}

func (f *confirmProposals) ConfirmDeadline(_ context.Context, id proposal.ID) (homeworkflow.ProposalView, error) {
	v := f.views[id]
	switch {
	case f.err != nil:
		return v, f.err
	case v.Kind != proposal.KindDeadlineEvent:
		return v, homeworkflow.ErrNotADeadline
	case v.Status == proposal.StatusExecuted:
		return v, nil
	}
	f.events++
	v.Status = proposal.StatusExecuted
	f.views[id] = v
	return v, nil
}

func (f *confirmProposals) Decline(_ context.Context, id proposal.ID) (homeworkflow.ProposalView, error) {
	v, ok := f.views[id]
	if !ok {
		return v, shared.ErrNotFound
	}
	if v.Status != proposal.StatusValidated {
		return v, proposal.ErrInvalidTransition
	}
	v.Status = proposal.StatusDeclined
	f.views[id] = v
	return v, nil
}

// fakeMaterials: the reader saw an address in m-mail only.
type fakeMaterials struct{ err error }

func (f fakeMaterials) MentionsEmail(_ context.Context, id shared.MaterialID) (bool, error) {
	return id == "m-mail", f.err
}

func newConfirmService(p *confirmProposals) *AgentConfirmService {
	return NewAgentConfirmService(AgentConfirmCore{
		Proposals: p, Materials: fakeMaterials{},
		Deletions: &fakeDeletion{previews: map[shared.RecordingID]recordingdelete.Preview{
			"rec1": {Recording: "rec1", Title: "Физика", StartedAt: confirmDue},
			"rec2": {Recording: "rec2", Refusal: recordingdelete.RefusalInProgress},
		}},
	}, discard())
}

// The card is read from the proposal: the recipient of the settings, the exact version and file, the subject; the
// reader's mentions_email adds the notice, never an address.
func TestAgentConfirmCardOfASend(t *testing.T) {
	s := newConfirmService(newConfirmProposals())
	got, err := s.Card(t.Context(), "proposal", "p-send")
	if err != nil {
		t.Fatal(err)
	}
	want := AgentConfirmCard{
		Kind: "send", TargetKind: "proposal", TargetID: "p-send", State: "waiting", AssignmentID: "a1", Title: "Задачи 1-5",
		Subject: "Физика", Recipient: "teacher@school.example", EmailSubject: "ДЗ · Физика · до 24.10",
		AttachmentName: "homework.md", AttachmentSize: 42, AttachmentFormat: "txt", DraftVersion: 2,
		Notice: "В файле указан другой адрес — не используется",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("card = %+v\nwant %+v", got, want)
	}
	raw, _ := json.Marshal(got)
	for _, key := range []string{`"recipient":"teacher@school.example"`, `"draftVersion":2`, `"notice":"В файле`, `"deletion":null`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON %s lacks %s", raw, key)
		}
	}

	// No material, or one without an address: no notice.
	p := newConfirmProposals()
	p.assignments["a1"] = homeworkflow.AssignmentView{ID: "a1", Title: "Задачи 1-5"}
	if got, _ := newConfirmService(p).Card(t.Context(), "proposal", "p-send"); got.Notice != "" {
		t.Fatalf("notice without a material = %q", got.Notice)
	}
}

func TestAgentConfirmCardOfADeadlineAndADeletion(t *testing.T) {
	s := newConfirmService(newConfirmProposals())
	due, err := s.Card(t.Context(), "proposal", "p-due")
	if err != nil || due.Kind != "deadline" || due.Title != "Задачи 1-5" || due.Due != "2026-10-24T18:00:00Z" ||
		due.Subject != "Физика" || due.Recipient != "" || due.Notice != "" || due.State != "waiting" {
		t.Fatalf("deadline card = %+v, %v", due, err)
	}

	del, err := s.Card(t.Context(), "preview", "rec1")
	if err != nil || del.Kind != "delete_recording" || del.State != "waiting" || del.Deletion == nil || del.Deletion.ID != "rec1" {
		t.Fatalf("deletion card = %+v, %v", del, err)
	}
	busy, err := s.Card(t.Context(), "preview", "rec2")
	if err != nil || busy.State != "rejected" || busy.Message == "" {
		t.Fatalf("busy recording card = %+v, %v", busy, err)
	}
}

func TestAgentConfirmCardErrors(t *testing.T) {
	s := newConfirmService(newConfirmProposals())
	tests := []struct {
		name, kind, id string
		want           error
	}{
		{"unknown kind", "assignment", "a1", ErrAgentConfirmInvalid},
		{"bad proposal id", "proposal", "p 1", ErrAgentConfirmInvalid},
		{"bad recording id", "preview", "../x", ErrAgentConfirmInvalid},
		{"gone proposal", "proposal", "p-404", ErrAgentConfirmNotFound},
		{"gone recording", "preview", "rec9", ErrAgentConfirmNotFound},
		{"an export proposal", "proposal", "p-export", ErrAgentConfirmInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Card(t.Context(), tt.kind, tt.id); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// «Да» twice: one email, one event (hard rule 7).
func TestAgentConfirmIsIdempotent(t *testing.T) {
	p := newConfirmProposals()
	s := newConfirmService(p)
	for click := range 2 {
		got, err := s.Confirm(t.Context(), "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt", "", "")
		if err != nil || got.State != "done" {
			t.Fatalf("send click %d = %+v, %v", click+1, got, err)
		}
		if got, err := s.Confirm(t.Context(), "proposal", "p-due", "", "", "", ""); err != nil || got.State != "done" {
			t.Fatalf("deadline click %d = %+v, %v", click+1, got, err)
		}
	}
	if p.sends != 1 || p.events != 1 {
		t.Fatalf("sends %d, events %d", p.sends, p.events)
	}
}

func TestAgentConfirmErrors(t *testing.T) {
	tests := []struct {
		name                string
		kind, id, subj, fmt string
		setup               func(p *confirmProposals)
		want                error
	}{
		{"subject changed since the card", "proposal", "p-send", "другая тема", "txt", nil, ErrSendStale},
		{"format changed since the card", "proposal", "p-send", "ДЗ · Физика · до 24.10", "docx", nil, ErrSendStale},
		{"unknown format", "proposal", "p-send", "ДЗ · Физика · до 24.10", "pdf", nil, ErrSendFormat},
		{"unknown outcome of an earlier email", "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt",
			func(p *confirmProposals) { p.unresolved = true }, ErrSendUnresolved},
		{"calendar not connected", "proposal", "p-due", "", "",
			func(p *confirmProposals) { p.err = homeworkflow.ErrCalendarNotConnected }, ErrAgentConfirmCalendar},
		{"declined before", "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt", func(p *confirmProposals) {
			v := p.views["p-send"]
			v.Status = proposal.StatusDeclined
			p.views["p-send"] = v
		}, ErrAgentConfirmDecided},
		{"failure", "proposal", "p-due", "", "", func(p *confirmProposals) { p.err = errors.New("smtp: 451") }, ErrAgentConfirmUnavailable},
		{"an export is not confirmed here", "proposal", "p-export", "", "", nil, ErrAgentConfirmInvalid},
		{"a deletion is confirmed in its window", "preview", "rec1", "", "", nil, ErrAgentConfirmDeleteHere},
		{"gone", "proposal", "p-404", "", "", nil, ErrAgentConfirmNotFound},
		{"bad id", "proposal", "", "", "", nil, ErrAgentConfirmInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newConfirmProposals()
			if tt.setup != nil {
				tt.setup(p)
			}
			if _, err := newConfirmService(p).Confirm(t.Context(), tt.kind, tt.id, tt.subj, tt.fmt, "", ""); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if p.sends != 0 {
				t.Fatal("an email went out")
			}
		})
	}
}

// After an email with an unknown outcome the card never sends again: «Да» once more gets the same refusal and the
// email is resolved in the list of emails (hard rule 7).
func TestAgentConfirmNeverResendsAnUnknownEmail(t *testing.T) {
	p := newConfirmProposals()
	p.unresolved = true
	s := newConfirmService(p)
	for range 2 {
		if _, err := s.Confirm(t.Context(), "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt", "", ""); !errors.Is(err, ErrSendUnresolved) {
			t.Fatalf("err = %v", err)
		}
	}
	if p.sends != 0 {
		t.Fatalf("sends = %d", p.sends)
	}
}

func TestAgentDecline(t *testing.T) {
	p := newConfirmProposals()
	s := newConfirmService(p)
	got, err := s.Decline(t.Context(), "proposal", "p-send")
	if err != nil || got.State != "declined" {
		t.Fatalf("Decline = %+v, %v", got, err)
	}
	if _, err := s.Decline(t.Context(), "proposal", "p-send"); !errors.Is(err, ErrAgentConfirmDecided) {
		t.Fatalf("second Decline = %v", err)
	}
	if _, err := s.Confirm(t.Context(), "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt", "", ""); !errors.Is(err, ErrAgentConfirmDecided) {
		t.Fatalf("Confirm after Decline = %v", err)
	}
	if got, err := s.Decline(t.Context(), "preview", "rec1"); err != nil || got.State != "declined" {
		t.Fatalf("Decline of a preview = %+v, %v", got, err)
	}
	if _, err := s.Decline(t.Context(), "job", "j1"); !errors.Is(err, ErrAgentConfirmInvalid) {
		t.Fatalf("Decline of another kind = %v", err)
	}
	if p.sends != 0 {
		t.Fatal("an email went out")
	}
}

// «Да, в календарь» of «Сегодня» makes the card of the deadline; only Confirm puts it into the calendar.
func TestAgentProposeDeadlineFromToday(t *testing.T) {
	p := newConfirmProposals()
	s := newConfirmService(p)
	got, err := s.ProposeDeadline(t.Context(), "a1")
	if err != nil || got.Kind != "deadline" || got.TargetID != "p-due" || got.State != "waiting" || p.events != 0 {
		t.Fatalf("ProposeDeadline = %+v, %v; events %d", got, err, p.events)
	}
	old, err := s.ProposeDeadline(t.Context(), "a3")
	if err != nil || old.State != "rejected" || old.Message != msgDueOutOfRange {
		t.Fatalf("past deadline = %+v, %v", old, err)
	}
	for id, want := range map[string]error{"a2": ErrAgentConfirmNoDue, "a9": ErrAgentConfirmNotFound, "a/1": ErrAgentConfirmInvalid} {
		if _, err := s.ProposeDeadline(t.Context(), id); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", id, err, want)
		}
	}
}
