package app

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// TASK-370 on the wired core: the popup's «Подключиться» gets the subject of the lesson only while the library subject
// its string resolves to keeps a call link.
func TestPromptCallSubjectOnTheWiredCore(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	c, _, _, b := buildPromptCore(t, e)

	sub, err := c.library.CreateSubjectWithCallLink(ctx, library.NewSubject{Name: "История"}, "https://meet.example.org/j/1?pwd=FakePwd42")
	if err != nil {
		t.Fatal(err)
	}
	lessons := sqlite.NewLessonRepository(c.db)
	announce := func(id shared.LessonID, subject string) string {
		t.Helper()
		start := time.Now()
		l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
			Title: "Лекция", Subject: subject, Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		}}, start)
		if err != nil {
			t.Fatal(err)
		}
		if err := lessons.Save(ctx, l); err != nil {
			t.Fatal(err)
		}
		if err := c.prompts.LessonStarting(ctx, lesson.StartAnnounced{
			LessonID: id, Title: "Лекция", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
			AnnouncedAt: start,
		}); err != nil {
			t.Fatal(err)
		}
		cur := b.Current()
		if cur == nil {
			t.Fatal("no prompt after the announcement")
		}
		return cur.ID
	}

	id := announce("lesson-1", "история") // matched by its key, as on «Сегодня»
	if got := b.CallSubject(ctx, id).SubjectID; got != sub.ID().String() {
		t.Fatalf("linked subject: CallSubject = %q, want %q", got, sub.ID())
	}

	if _, err := c.library.ClearCallLink(ctx, sub.ID()); err != nil {
		t.Fatal(err)
	}
	if got := b.CallSubject(ctx, id).SubjectID; got != "" {
		t.Fatalf("subject without a link: CallSubject = %q, want none", got)
	}

	id = announce("lesson-2", "Химия")
	if got := b.CallSubject(ctx, id).SubjectID; got != "" {
		t.Fatalf("unknown subject: CallSubject = %q, want none", got)
	}
}
