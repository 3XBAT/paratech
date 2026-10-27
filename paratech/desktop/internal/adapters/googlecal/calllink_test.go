package googlecal_test

import (
	"context"
	"strings"
	"testing"

	"paratech/desktop/internal/adapters/googlecal"
	"paratech/desktop/internal/domain/schedule/lesson"
)

// The call link of a lesson is not projected (ADR-0032): the event of a lesson with a link is the event without it.
func TestLessonCallLinkIsNotWritten(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	cal := r.calendar(t)

	plain := newLesson(t, "l0", "Алгебра")
	l := newLesson(t, "l1", "Алгебра")
	content := l.Content()
	content.CallLink = "https://meet.google.com/abc-defg-hij"
	if _, err := l.ChangeContent(content, t0); err != nil {
		t.Fatal(err)
	}
	for _, x := range []*lesson.Lesson{plain, l} {
		if err := r.c.UpsertLesson(ctx, cal, x); err != nil {
			t.Fatal(err)
		}
	}

	withLink, _ := r.api.Event(cal, googlecal.LessonEventID("l1"))
	without, _ := r.api.Event(cal, googlecal.LessonEventID("l0"))
	for _, s := range []string{withLink.Summary, withLink.Description} {
		if strings.Contains(s, "meet.google.com") || strings.Contains(s, "abc-defg-hij") {
			t.Fatalf("event text holds the link: %q", s)
		}
	}
	for k, v := range withLink.Private {
		if strings.Contains(v, "meet.google.com") {
			t.Fatalf("private property %s holds the link", k)
		}
	}
	if withLink.Description != without.Description || withLink.Summary != without.Summary {
		t.Fatalf("event with a link %+v differs from the one without %+v", withLink, without)
	}
}
