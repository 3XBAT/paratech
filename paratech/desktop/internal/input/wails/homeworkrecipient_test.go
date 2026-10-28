package wails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/domain/coursework/proposal"
)

// choosingView is a send whose card asks «Кому отправить?».
var choosingView = homeworkflow.ProposalView{
	ID: "p1", Kind: proposal.KindSendHomework, Status: proposal.StatusValidated, AssignmentID: "a1", Subject: "ДЗ",
	RecipientReason: proposal.ReasonChooseTeacher, SubjectID: "s1", AttachmentFormat: proposal.FormatText,
	TeacherOptions: []homeworkflow.TeacherOptionView{
		{Role: proposal.RoleLecture, Name: "Лектор", Email: "lecture@school.example"},
		{Role: proposal.RoleLab, Name: "Лаборант", Email: "lab@school.example"},
	},
}

func TestPrepareSendRecipient(t *testing.T) {
	ctx := context.Background()
	t.Run("pinned", func(t *testing.T) {
		e := newHWEnv()
		e.proposals.proposal.RecipientName, e.proposals.proposal.RecipientRole = "Иванова И. И.", proposal.RoleLecture
		e.proposals.proposal.SubjectID = "s1"
		got, err := e.svc.PrepareSend(ctx, "a1", "txt")
		if err != nil || got.Status != "ready" || got.Reason != "" || got.RecipientName != "Иванова И. И." ||
			got.Role != "lecture" || got.SubjectID != "s1" || got.Options != nil {
			t.Fatalf("PrepareSend = %+v, %v", got, err)
		}
	})
	t.Run("choose", func(t *testing.T) {
		e := newHWEnv()
		e.proposals.proposal = choosingView
		got, err := e.svc.PrepareSend(ctx, "a1", "txt")
		want := []TeacherOption{
			{Role: "lecture", Name: "Лектор", Email: "lecture@school.example"}, {Role: "lab", Name: "Лаборант", Email: "lab@school.example"},
		}
		if err != nil || got.Status != "ready" || got.Reason != "choose_teacher" || got.Recipient != "" ||
			fmt.Sprint(got.Options) != fmt.Sprint(want) {
			t.Fatalf("PrepareSend = %+v, %v", got, err)
		}
		raw, _ := json.Marshal(got.Options[0])
		if string(raw) != `{"role":"lecture","name":"Лектор","email":"lecture@school.example"}` {
			t.Fatalf("option JSON %s", raw)
		}
	})
	for _, tc := range []struct {
		reason proposal.RejectReason
		role   proposal.TeacherRole
		text   string
	}{
		{proposal.ReasonNoTeacherEmail, proposal.RoleLab, "Нет почты преподавателя (лабораторные)"},
		{proposal.ReasonNoTeacherEmail, proposal.RolePractice, "(семинарист)"},
		{proposal.ReasonNoTeacherEmail, "", "Нет почты преподавателя"},
		{proposal.ReasonNoSubject, "", "У задания не указан предмет"},
	} {
		t.Run(string(tc.reason)+" "+string(tc.role), func(t *testing.T) {
			e := newHWEnv()
			e.proposals.proposal = homeworkflow.ProposalView{
				ID: "p2", Kind: proposal.KindSendHomework, Status: proposal.StatusRejected, Reason: tc.reason,
				RecipientReason: tc.reason, RecipientRole: tc.role, SubjectID: "s1",
			}
			got, err := e.svc.PrepareSend(ctx, "a1", "txt")
			if err != nil || got.Status != "rejected" || got.Reason != string(tc.reason) || got.Role != string(tc.role) ||
				!strings.Contains(got.Message, tc.text) {
				t.Fatalf("PrepareSend = %+v, %v", got, err)
			}
		})
	}
}

// «Да» carries the clicked option to the core; a refusal for the missing choice reads as such.
func TestConfirmSendChosenTeacher(t *testing.T) {
	ctx := context.Background()
	e := newHWEnv()
	if err := e.svc.ConfirmSend(ctx, "p1", "ДЗ", "txt", "lab", "lab@school.example"); err != nil {
		t.Fatal(err)
	}
	if e.proposals.chosen != (homeworkflow.ChosenTeacher{Role: proposal.RoleLab, Address: "lab@school.example"}) {
		t.Fatalf("chosen = %+v", e.proposals.chosen)
	}

	for cause, want := range map[error]error{
		fmt.Errorf("confirm: %w", proposal.ErrChooseTeacher):    ErrSendChooseTeacher,
		fmt.Errorf("confirm: %w", proposal.ErrInvalidRecipient): ErrSendStale,
	} {
		e := newHWEnv()
		e.proposals.confirmErr = cause
		if err := e.svc.ConfirmSend(ctx, "p1", "ДЗ", "txt", "", ""); !errors.Is(err, want) {
			t.Fatalf("%v: ConfirmSend = %v, want %v", cause, err, want)
		}
	}
}

func TestAgentCardRecipient(t *testing.T) {
	p := newConfirmProposals()
	p.views["p1"] = choosingView
	s := newConfirmService(p)
	card, err := s.Card(t.Context(), "proposal", "p1")
	if err != nil || card.State != "waiting" || card.Reason != "choose_teacher" || len(card.Options) != 2 ||
		card.SubjectID != "s1" || card.Recipient != "" {
		t.Fatalf("Card = %+v, %v", card, err)
	}

	if _, err := s.Confirm(t.Context(), "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt", "lab", "lab@school.example"); err != nil {
		t.Fatal(err)
	}
	if p.chosen != (homeworkflow.ChosenTeacher{Role: proposal.RoleLab, Address: "lab@school.example"}) {
		t.Fatalf("chosen = %+v", p.chosen)
	}

	p.views["p2"] = homeworkflow.ProposalView{
		ID: "p2", Kind: proposal.KindSendHomework, Status: proposal.StatusRejected, AssignmentID: "a1",
		Reason: proposal.ReasonNoTeacherEmail, RecipientReason: proposal.ReasonNoTeacherEmail, RecipientRole: proposal.RoleLab,
	}
	card, err = s.Card(t.Context(), "proposal", "p2")
	if err != nil || card.State != "rejected" || card.Role != "lab" ||
		card.Message != "Нет почты преподавателя (лабораторные) предмета «Физика»: укажите её на странице предмета." {
		t.Fatalf("Card = %+v, %v", card, err)
	}

	p.err = fmt.Errorf("confirm: %w", proposal.ErrChooseTeacher)
	if _, err := s.Confirm(t.Context(), "proposal", "p-send", "ДЗ · Физика · до 24.10", "txt", "", ""); !errors.Is(err, ErrSendChooseTeacher) {
		t.Fatalf("Confirm without a choice = %v", err)
	}
}

func TestAgentCardJSONNames(t *testing.T) {
	raw, err := json.Marshal(AgentConfirmCard{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`"recipient":`, `"recipientName":`, `"role":`, `"reason":`, `"subjectId":`, `"options":`} {
		if !strings.Contains(string(raw), name) {
			t.Errorf("the card has no %s: %s", name, raw)
		}
	}
}
