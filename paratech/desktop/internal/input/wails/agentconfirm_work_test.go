package wails

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/senddelivery"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/shared"
)

const workSubject = "ДЗ · История · до 24.10"

// withWorkSend adds the send of work w1's paper, waiting for «Да».
func withWorkSend(p *confirmProposals) *confirmProposals {
	p.views["p-work"] = homeworkflow.ProposalView{
		ID: "p-work", Kind: proposal.KindSendWork, Status: proposal.StatusValidated, AssignmentID: "a1", WorkID: "w1",
		Recipient: "teacher@school.example", Subject: workSubject, AttachmentName: "Реферат.docx", AttachmentSize: 2048,
		AttachmentFormat: proposal.FormatDocx,
	}
	return p
}

// ProposeSendWork of the fake: w1 has its send, w-topic has no assignment, w-moved left «ДЗ».
func (f *confirmProposals) ProposeSendWork(_ context.Context, w shared.WorkID) (homeworkflow.ProposalView, error) {
	switch w {
	case "w1":
		return f.views["p-work"], nil
	case "w-topic":
		return homeworkflow.ProposalView{}, fmt.Errorf("propose: %w", work.ErrNoAssignment)
	case "w-moved":
		return homeworkflow.ProposalView{
			ID: "p-moved", Kind: proposal.KindSendWork, Status: proposal.StatusRejected, AssignmentID: "a1", WorkID: w,
			Reason: proposal.ReasonFileMoved, AttachmentFormat: proposal.FormatDocx,
		}, nil
	}
	return homeworkflow.ProposalView{}, fmt.Errorf("work %s: %w", w, shared.ErrNotFound)
}

// ConfirmSendWorkTo of the fake acts like ConfirmSendTo on the send of a work.
func (f *confirmProposals) ConfirmSendWorkTo(_ context.Context, id proposal.ID, subject string, format proposal.Format,
	chosen homeworkflow.ChosenTeacher) (homeworkflow.ProposalView, error) {
	f.chosen = chosen
	v := f.views[id]
	switch {
	case f.err != nil:
		return v, f.err
	case v.Kind != proposal.KindSendWork:
		return v, homeworkflow.ErrNotASend
	case v.Status == proposal.StatusExecuted:
		return v, nil
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

// «Отправить преподавателю» makes the send card of the work's file; only «Да» sends, once.
func TestAgentProposeSendWork(t *testing.T) {
	p := withWorkSend(newConfirmProposals())
	s := newConfirmService(p)
	got, err := s.ProposeSendWork(t.Context(), "w1")
	want := AgentConfirmCard{
		Kind: "send", TargetKind: "proposal", TargetID: "p-work", State: "waiting", AssignmentID: "a1", Title: "Задачи 1-5",
		Subject: "Физика", Recipient: "teacher@school.example", EmailSubject: workSubject, AttachmentName: "Реферат.docx",
		AttachmentSize: 2048, AttachmentFormat: "docx", Notice: "В файле указан другой адрес — не используется",
	}
	if err != nil || fmt.Sprint(got) != fmt.Sprint(want) || p.sends != 0 {
		t.Fatalf("ProposeSendWork = %+v, %v; sends %d\nwant %+v", got, err, p.sends, want)
	}
	for click := range 2 {
		done, err := s.Confirm(t.Context(), "proposal", "p-work", workSubject, "docx", "", "")
		if err != nil || done.State != "done" {
			t.Fatalf("click %d = %+v, %v", click+1, done, err)
		}
	}
	if p.sends != 1 {
		t.Fatalf("sends = %d after two «Да», want 1", p.sends)
	}
}

func TestAgentProposeSendWorkRefusals(t *testing.T) {
	s := newConfirmService(withWorkSend(newConfirmProposals()))
	for id, want := range map[string]error{
		"w-topic": ErrAgentConfirmNoAssignment, "w-404": ErrAgentConfirmNotFound, "../w": ErrAgentConfirmInvalid, "": ErrAgentConfirmInvalid,
	} {
		if _, err := s.ProposeSendWork(t.Context(), id); !errors.Is(err, want) {
			t.Errorf("%q: err = %v, want %v", id, err, want)
		}
	}
	moved, err := s.ProposeSendWork(t.Context(), "w-moved")
	if err != nil || moved.State != "rejected" || moved.Message != workRejectMessages[proposal.ReasonFileMoved] {
		t.Fatalf("moved file = %+v, %v", moved, err)
	}
}

func TestAgentConfirmSendWorkErrors(t *testing.T) {
	tests := []struct {
		name, subj, format string
		setup              func(p *confirmProposals)
		want               error
	}{
		{"subject changed since the card", "другая тема", "docx", nil, ErrSendStale},
		{"format changed since the card", workSubject, "pptx", nil, ErrSendStale},
		{"a homework format", workSubject, "txt", nil, ErrAgentConfirmInvalid},
		{"unknown outcome of an earlier email", workSubject, "docx", func(p *confirmProposals) { p.unresolved = true }, ErrSendUnresolved},
		{"failure", workSubject, "docx", func(p *confirmProposals) { p.err = errors.New("disk I/O") }, ErrAgentConfirmUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := withWorkSend(newConfirmProposals())
			if tt.setup != nil {
				tt.setup(p)
			}
			if _, err := newConfirmService(p).Confirm(t.Context(), "proposal", "p-work", tt.subj, tt.format, "", ""); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if p.sends != 0 {
				t.Fatal("an email went out")
			}
		})
	}
}
