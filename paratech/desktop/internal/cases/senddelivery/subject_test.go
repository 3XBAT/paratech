package senddelivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/delivery/delivery"
)

// TASK-092: the delivery keeps the subject the core built for the assignment, also from a hostile transcript hint.
func TestSendHomeworkUsesTheSubjectOfTheCommand(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	due := time.Date(2026, 10, 27, 23, 59, 0, 0, msk)
	cases := []struct {
		name    string
		subject string // the subject or hint of the assignment
		due     time.Time
		want    string
	}{
		{"subject and due", "Матанализ", due, "Матанализ: домашнее задание к 27.10"},
		{"no subject", "", due, "Домашнее задание к 27.10"},
		{"no due", "Матанализ", time.Time{}, "Матанализ: домашнее задание"},
		{"neither", "", time.Time{}, "Домашнее задание"},
		{"CRLF header injection in a hint", values.NewLessonNames("Физика\r\nBcc: evil@example.com", "").Subject(), time.Time{},
			"Физика Bcc: evil@example.com: домашнее задание"},
		{"very long hint", strings.Repeat("я", 5000), due, strings.Repeat("я", delivery.MaxSubjectRunes-26) + ": домашнее задание к 27.10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			cmd, err := command(t, "p1", "a1", homework).WithSubject(values.HomeworkEmailSubject(tc.subject, tc.due))
			if err != nil {
				t.Fatal(err)
			}
			if err := e.svc.SendHomework(context.Background(), cmd); err != nil {
				t.Fatal(err)
			}
			if st := e.st.only(); st.Subject != tc.want {
				t.Fatalf("subject = %q, want %q", st.Subject, tc.want)
			}
		})
	}
}

// The email subject of the core fits the delivery limit.
func TestEmailSubjectLimitIsTheDeliveryLimit(t *testing.T) {
	if values.MaxEmailSubjectRunes != delivery.MaxSubjectRunes {
		t.Fatalf("values.MaxEmailSubjectRunes = %d, delivery.MaxSubjectRunes = %d", values.MaxEmailSubjectRunes, delivery.MaxSubjectRunes)
	}
}
