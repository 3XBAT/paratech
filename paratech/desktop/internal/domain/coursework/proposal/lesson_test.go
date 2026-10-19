package proposal

import (
	"errors"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
)

func TestEmailCommandWithSubject(t *testing.T) {
	email, err := inStatus(t, outEmail, StatusConfirmed).EmailCommand()
	if err != nil {
		t.Fatal(err)
	}
	if email.Subject() != "" {
		t.Fatalf("subject before WithSubject = %q", email.Subject())
	}

	cases := []struct {
		name    string
		subject string
		ok      bool
	}{
		{"built by the core", "Матанализ: домашнее задание к 27.10", true},
		{"at the limit", strings.Repeat("я", values.MaxEmailSubjectRunes), true},
		{"empty", "", false},
		{"blank", "  ", false},
		{"CRLF header injection", "Homework\r\nBcc: evil@example.com", false},
		{"LF", "a\nb", false},
		{"line separator", "a\u2028b", false},
		{"bidi override", "a\u202eb", false},
		{"too long", strings.Repeat("я", values.MaxEmailSubjectRunes+1), false},
		{"invalid utf-8", "a\xffb", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := email.WithSubject(tc.subject)
			if !tc.ok {
				if !errors.Is(err, ErrInvalidProposal) || !got.IsZero() {
					t.Fatalf("WithSubject = %+v, %v; want ErrInvalidProposal and the zero command", got, err)
				}
				return
			}
			if err != nil || got.Subject() != tc.subject || got.IdempotencyKey() != email.IdempotencyKey() {
				t.Fatalf("WithSubject = %+v, %v", got, err)
			}
		})
	}

	if _, err := (EmailCommand{}).WithSubject("Домашнее задание"); !errors.Is(err, ErrInvalidProposal) {
		t.Fatalf("zero command: %v", err)
	}
}

func TestCalendarCommandWithLesson(t *testing.T) {
	cal, err := inStatus(t, outDeadline, StatusConfirmed).CalendarCommand()
	if err != nil {
		t.Fatal(err)
	}
	label := values.NewLessonLabel(values.ScheduledLesson{Subject: "Алгебра", Kind: "seminar", Start: t0},
		values.NewLessonNames("", "Петров"), values.LessonNames{})
	got := cal.WithLesson(label)
	if got.Lesson() != label || got.Title() != cal.Title() || !got.Due().Equal(cal.Due()) || got.IsZero() {
		t.Fatalf("WithLesson = %+v", got)
	}
	if !cal.Lesson().IsZero() {
		t.Fatal("WithLesson changed the original command")
	}
	if !(CalendarCommand{}).WithLesson(label).IsZero() {
		t.Fatal("the zero command is no longer zero")
	}
}
