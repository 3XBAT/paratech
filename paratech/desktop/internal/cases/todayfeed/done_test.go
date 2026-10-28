package todayfeed

import (
	"context"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/agent/services/today"
)

// A done assignment (ADR-0033 §9) leaves «Горит на 7 дней», the burning chip and the calendar hint of its post; the post
// of today and the subject history still count it, marked done.
func TestTodayLeavesDoneAssignmentsOut(t *testing.T) {
	w := day(t)
	if changed, err := w.homework["t-math"][0].MarkDone(tNow); err != nil || !changed {
		t.Fatalf("MarkDone = %v, %v", changed, err)
	}
	for i := range w.deadlines {
		if w.deadlines[i].Assignment == "a-math" || w.deadlines[i].Assignment == "a-essay" {
			w.deadlines[i].Done = true
		}
	}

	got, err := w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Burning) != 0 {
		t.Errorf("burning = %+v, want none: both near deadlines are done, the other is beyond 7 days", got.Burning)
	}
	for _, c := range got.Chips {
		if c.Text == "Что горит на неделе?" {
			t.Errorf("chips = %+v, want no burning chip", got.Chips)
		}
	}
	if len(got.Posts) != 1 {
		t.Fatalf("posts = %+v", got.Posts)
	}
	p := got.Posts[0]
	if p.Homework != 1 || p.HomeworkDone != 1 || len(p.Calendar) != 0 || !p.EarliestDue.IsZero() ||
		p.Headline != "Конспект готов · Найдено ДЗ" {
		t.Errorf("post = %+v, want the done assignment counted and marked, without a deadline or a calendar hint", p)
	}

	thread, err := w.screen().Thread(context.Background(), "subj-math")
	if err != nil || len(thread) != 1 || thread[0].Homework != 1 || thread[0].HomeworkDone != 1 {
		t.Fatalf("thread = %+v, %v; want the post with its done assignment marked", thread, err)
	}

	// «Вернуть в работу»: it burns and is proposed again.
	if changed, err := w.homework["t-math"][0].MarkUndone(tNow); err != nil || !changed {
		t.Fatalf("MarkUndone = %v, %v", changed, err)
	}
	w.deadlines[slices.IndexFunc(w.deadlines, func(d today.Deadline) bool { return d.Assignment == "a-math" })].Done = false
	got, err = w.screen().Today(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Burning) != 1 || got.Burning[0].Assignment != "a-math" || got.Posts[0].HomeworkDone != 0 ||
		len(got.Posts[0].Calendar) != 1 {
		t.Fatalf("after undo: burning %+v, post %+v", got.Burning, got.Posts[0])
	}
}
