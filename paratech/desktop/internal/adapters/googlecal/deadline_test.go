package googlecal_test

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/domain/coursework/values"
)

// TASK-092: the deadline event carries the lesson lines above the app note; the title stays the assignment's.
func TestUpsertDeadlineNamesTheLesson(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	date := time.Date(2026, 10, 19, 10, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	label := values.NewLessonLabel(values.ScheduledLesson{Subject: "Матанализ", Kind: "seminar", Start: date},
		values.NewLessonNames("", "Петров\r\nBcc: x"), values.LessonNames{})
	if err := r.c.UpsertDeadline(ctx, deadline(t, "a1", "Map of rivers").WithLesson(label)); err != nil {
		t.Fatal(err)
	}
	cals := r.api.Calendars()
	if len(cals) != 1 {
		t.Fatalf("%d calendars", len(cals))
	}
	e, ok := r.api.Event(cals[0].ID, googlecal.DeadlineEventID("a1"))
	want := "Матанализ · семинар · Петров Bcc: x (по записи)\nЗанятие 19.10\n\nДедлайн из paratech. Изменения делайте в приложении."
	if !ok || e.Summary != "Map of rivers" || e.Description != want {
		t.Fatalf("event %q / %q, want description %q", e.Summary, e.Description, want)
	}
}
