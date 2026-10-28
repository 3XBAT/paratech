package app

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// lecturer is the teachers of a subject s1 with only its lecturer, at address.
func lecturer(address string) proposal.Teachers {
	return proposal.Teachers{Subject: "s1", Lecture: proposal.Teacher{Name: "Иванова И. И.", Email: address}}
}

// The recipient bridge maps the library's teachers of a subject by role and resolves a subject name as the library
// does (ADR-0024); an unknown subject or name is none.
func TestSubjectTeachersBridge(t *testing.T) {
	ctx := context.Background()
	a := newTestApp(t, t.TempDir())
	ui, err := a.openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lib := ui.Library
	sub, err := lib.CreateSubject(ctx, library.NewSubject{Name: "Матанализ", Aliases: []string{"Матан"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err = lib.SetTeacher(ctx, sub.ID(), subject.TeacherLecture, "Лектор", "lecture@school.example", sub.Version())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.SetTeacher(ctx, sub.ID(), subject.TeacherLab, "Лаборант", "lab@school.example", sub.Version()); err != nil {
		t.Fatal(err)
	}

	svc, ok := lib.(*library.Service)
	if !ok {
		t.Fatalf("the library binding is %T", lib)
	}
	bridge := subjectTeachers{svc}
	ts, err := bridge.Teachers(ctx, sub.ID())
	want := proposal.Teachers{
		Subject: sub.ID(), Lecture: proposal.Teacher{Name: "Лектор", Email: "lecture@school.example"},
		Lab: proposal.Teacher{Name: "Лаборант", Email: "lab@school.example"},
	}
	if err != nil || ts != want {
		t.Fatalf("Teachers = %v, %v", ts.Subject, err)
	}
	for _, id := range []shared.SubjectID{"", "s-unknown"} {
		if ts, err := bridge.Teachers(ctx, id); err != nil || ts != (proposal.Teachers{}) {
			t.Fatalf("subject %q: %v, %v", id, ts.Subject, err)
		}
	}

	for name, wantID := range map[string]bool{"Матанализ": true, " матан ": true, "Физика": false, "": false} {
		id, err := bridge.SubjectNamed(ctx, name)
		if err != nil || (id == sub.ID()) != wantID || (!wantID && id != "") {
			t.Fatalf("SubjectNamed(%q) = %q, %v", name, id, err)
		}
	}
}

// The settings address is never a fallback (ADR-0030 B-1): with it set and no teacher of the subject, the send is
// refused with no_teacher_email.
func TestSettingsRecipientIsNoFallback(t *testing.T) {
	ctx := context.Background()
	a, _ := proposalCore(t, "")
	repo := sqlite.NewSettingsRepository(a.core.db)
	s, err := repo.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHomeworkRecipient(teacher, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	exportHomework(t, a)
	v, err := a.core.proposals.ProposeSend(ctx, "a1", proposal.FormatText)
	if err != nil || v.Status != proposal.StatusRejected || v.Reason != proposal.ReasonNoTeacherEmail || v.Recipient != "" {
		t.Fatalf("ProposeSend = %+v, %v", v, err)
	}
}
