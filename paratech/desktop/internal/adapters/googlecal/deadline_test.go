package googlecal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/testenv"
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

// TASK-095: the deadline event of a deleted assignment is removed; a removed or never written one is success.
func TestDeleteDeadline(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if err := r.c.UpsertDeadline(ctx, deadline(t, "a1", "Map of rivers")); err != nil {
		t.Fatal(err)
	}
	cal := r.api.Calendars()[0].ID
	if err := r.c.UpsertLesson(ctx, cal, newLesson(t, "a1", "Алгебра")); err != nil { // same id, other namespace
		t.Fatal(err)
	}
	for i, id := range []shared.AssignmentID{"a1", "a1", "never-written"} {
		if err := r.c.DeleteDeadline(ctx, cal, id); err != nil {
			t.Fatalf("delete %d: %v", i, err)
		}
	}
	if e, ok := r.api.Event(cal, googlecal.DeadlineEventID("a1")); ok && e.Status != testenv.FakeEventCancelled {
		t.Fatalf("deadline event still there: %+v", e)
	}
	if e, ok := r.api.Event(cal, googlecal.LessonEventID("a1")); !ok || e.Status != testenv.FakeEventConfirmed {
		t.Fatalf("the lesson event with the same id was touched: %+v, %v", e, ok)
	}
	if err := r.c.DeleteDeadline(ctx, cal, "a/b"); !errors.Is(err, googlecal.ErrInvalidRequest) {
		t.Fatalf("DeleteDeadline of a bad id = %v, want ErrInvalidRequest", err)
	}
}
