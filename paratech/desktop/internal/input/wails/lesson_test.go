package wails

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/cases/homeworkflow"
	"paratech/desktop/internal/cases/summaryview"
	"paratech/desktop/internal/domain/coursework/proposal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// TASK-092: the page reads the lesson of an assignment under these names.
func TestAssignmentItemCarriesTheLesson(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	e := newHWEnv()
	e.proposals.list = []homeworkflow.AssignmentView{{
		ID: "a1", Title: "Rivers", UpdatedAt: hwT0, Subject: "Матанализ", SubjectSource: values.SourceSchedule,
		Kind: "lecture", Teacher: "Петров", TeacherSource: values.SourceTranscript,
		LessonDate: time.Date(2026, 10, 19, 12, 0, 0, 0, msk), CorrectedTeacher: "",
	}}
	got, err := e.svc.ListAssignments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"subject": "Матанализ", "subjectSource": "schedule", "kind": "lecture", "teacher": "Петров",
		"teacherSource": "transcript", "lessonDate": "2026-10-19T09:00:00Z", "correctedSubject": "", "correctedTeacher": "",
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
}

func TestCorrectLesson(t *testing.T) {
	e := newHWEnv()
	got, err := e.svc.CorrectLesson(context.Background(), "a1", "Физика", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.proposals.corrected != (homeworkflow.LessonCorrection{ID: "a1", Subject: "Физика"}) ||
		got.Subject != "Физика" || got.SubjectSource != "user" || got.CorrectedSubject != "Физика" {
		t.Fatalf("CorrectLesson = %+v, sent %+v", got, e.proposals.corrected)
	}

	e = newHWEnv()
	e.proposals.err = shared.ErrNotFound
	if _, err := e.svc.CorrectLesson(context.Background(), "a1", "", ""); !errors.Is(err, ErrHomeworkNotFound) {
		t.Fatalf("missing assignment: %v", err)
	}
}

// The dialog shows the subject the core built for the email; the configured default only when there is none.
func TestPrepareSendShowsTheSubjectOfTheAssignment(t *testing.T) {
	e := newHWEnv()
	e.proposals.proposal = homeworkflow.ProposalView{
		ID: "p1", Kind: proposal.KindSendHomework, Status: proposal.StatusValidated, AssignmentID: "a1",
		Recipient: "teacher@school.example", Subject: "Матанализ: домашнее задание к 27.10",
	}
	got, err := e.svc.PrepareSend(context.Background(), "a1")
	if err != nil || got.Subject != "Матанализ: домашнее задание к 27.10" {
		t.Fatalf("PrepareSend = %+v, %v", got, err)
	}
}

func TestSummaryAssignmentCarriesTheLesson(t *testing.T) {
	v := summaryview.Assignment{
		ID: "a1", Title: "Rivers", Subject: "Алгебра", SubjectSource: values.SourceTranscript, Kind: "seminar",
		LessonDate: time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC),
	}
	got := summaryAssignment(v)
	if got.Subject != "Алгебра" || got.SubjectSource != "transcript" || got.Kind != "seminar" || got.Teacher != "" ||
		got.TeacherSource != "" || got.LessonDate != "2026-10-19T09:00:00Z" {
		t.Fatalf("summary assignment = %+v", got)
	}
}
