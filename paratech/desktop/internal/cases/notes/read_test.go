package notes

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

func ids(views []View) []shared.NoteID {
	out := []shared.NoteID{}
	for _, v := range views {
		out = append(out, v.Note.ID())
	}
	return out
}

// The repository lists removed notes too: the use case returns the active ones only.
func TestListsSkipRemovedNotes(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.addRecording(t, recordingSpec{id: "r1", status: recording.StatusCompleted, started: t0, kind: "lecture"})
	a, err := e.svc.Add(ctx, AddRequest{Anchor: recAnchor("r1"), Subject: phys, Uploads: uploads("a.txt")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: uploads("b.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Remove(ctx, a.View.Note.ID()); err != nil {
		t.Fatal(err)
	}

	sub, err := e.svc.ForSubject(ctx, phys)
	if err != nil || len(sub) != 1 || sub[0].Note.ID() != b.View.Note.ID() {
		t.Fatalf("ForSubject = %v, %v; want only the active note", ids(sub), err)
	}
	rec, err := e.svc.ForLesson(ctx, Lecture{Recording: "r1"})
	if err != nil || len(rec) != 0 {
		t.Fatalf("ForLesson(recording) = %v, %v; want none", ids(rec), err)
	}
	// A removed note is not extended by a new drop: a new note is made.
	again, err := e.svc.Add(ctx, AddRequest{Anchor: recAnchor("r1"), Subject: phys, Uploads: uploads("a.txt")})
	if err != nil || !again.Created {
		t.Fatalf("Add after Remove = %+v, %v", again, err)
	}

	if _, err := e.svc.Restore(ctx, a.View.Note.ID()); err != nil {
		t.Fatal(err)
	}
	if rec, err = e.svc.ForLesson(ctx, Lecture{Recording: "r1"}); err != nil || len(rec) != 2 {
		t.Fatalf("ForLesson after Restore = %v, %v", ids(rec), err)
	}
}

// A deleted recording or a lesson that left the schedule reads as the day of its snapshot; the note stays.
func TestGoneAnchorsReadAsTheirDay(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	started := time.Date(2026, time.September, 12, 10, 0, 0, 0, msk)
	e.addRecording(t, recordingSpec{id: "r1", status: recording.StatusCompleted, started: started, kind: "lecture"})
	e.addRecording(t, recordingSpec{id: "r2", status: recording.StatusCompleted, started: started, kind: "lecture"})
	l := e.addLesson(t, "l1", started, lesson.KindSeminar)

	add := func(ref AnchorRef) shared.NoteID {
		t.Helper()
		res, err := e.svc.Add(ctx, AddRequest{Anchor: ref, Subject: phys, Uploads: uploads(string(ref.Kind) + string(ref.Recording) + ".txt")})
		if err != nil {
			t.Fatal(err)
		}
		return res.View.Note.ID()
	}
	purged := add(recAnchor("r1"))
	deleting := add(recAnchor("r2"))
	ofLesson := add(AnchorRef{Kind: note.AnchorLesson, Lesson: "l1"})
	sept12 := date(t, 2026, time.September, 12)

	day, err := e.svc.ForLesson(ctx, Lecture{Subject: phys, Date: sept12})
	if err != nil || len(day) != 0 {
		t.Fatalf("ForLesson(day) with the lectures there = %v, %v", ids(day), err)
	}

	e.recordings.purge("r1") // ADR-0018: the recording is gone, the note is not
	e.addRecording(t, recordingSpec{id: "r2", status: recording.StatusCompleted, started: started, kind: "lecture", deleting: true})
	if err := l.MarkDeleted(t0); err != nil {
		t.Fatal(err)
	}

	sub, err := e.svc.ForSubject(ctx, phys)
	if err != nil || len(sub) != 3 {
		t.Fatalf("ForSubject = %v, %v", ids(sub), err)
	}
	for _, v := range sub {
		a := v.Anchor
		if a.Kind() != note.AnchorDay || a.Date() != sept12 || a.Recording() != "" || a.Lesson() != "" {
			t.Errorf("note %s reads as %+v, want the day of its snapshot", v.Note.ID(), a)
		}
		if v.Note.Anchor().Kind() == note.AnchorDay {
			t.Errorf("note %s: the stored anchor was rewritten", v.Note.ID())
		}
	}
	if sub[2].Anchor.LessonKind() != "seminar" || sub[2].Anchor.Title() != "Семинар по механике" {
		t.Fatalf("lesson snapshot %+v", sub[2].Anchor)
	}

	day, err = e.svc.ForLesson(ctx, Lecture{Subject: phys, Date: sept12})
	if err != nil || len(day) != 3 {
		t.Fatalf("ForLesson(day) = %v, %v", ids(day), err)
	}
	if rec, err := e.svc.ForLesson(ctx, Lecture{Recording: "r1"}); err != nil || len(rec) != 1 || rec[0].Note.ID() != purged ||
		rec[0].Anchor.Kind() != note.AnchorDay {
		t.Fatalf("ForLesson(purged recording) = %v, %v", ids(rec), err)
	}
	if les, err := e.svc.ForLesson(ctx, Lecture{Lesson: "l1"}); err != nil || len(les) != 1 || les[0].Note.ID() != ofLesson {
		t.Fatalf("ForLesson(removed lesson) = %v, %v", ids(les), err)
	}

	// A drop on that day extends the first note that reads as it.
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorDay, Date: sept12}, Subject: phys, Uploads: uploads("new.txt")})
	if err != nil || res.Created || res.View.Note.ID() != purged {
		t.Fatalf("Add(day) = %+v, %v", res, err)
	}
	if e.notes.count() != 3 {
		t.Fatalf("%d notes stored", e.notes.count())
	}
	if rec, err := e.svc.ForLesson(ctx, Lecture{Recording: "r2"}); err != nil || len(rec) != 1 || rec[0].Note.ID() != deleting ||
		rec[0].Anchor.Kind() != note.AnchorDay {
		t.Fatalf("ForLesson(recording marked for deletion) = %v, %v", ids(rec), err)
	}
}

func TestReadRequests(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	d := date(t, 2026, 9, 1)
	bad := []Lecture{
		{},
		{Recording: "r1", Lesson: "l1"},
		{Subject: phys},
		{Date: d},
		{Recording: "r1", Subject: phys, Date: d},
		{Recording: "a/b"},
		{Lesson: "a/b"},
		{Subject: "a b", Date: d},
	}
	for _, l := range bad {
		if _, err := e.svc.ForLesson(ctx, l); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("ForLesson(%+v) = %v, want ErrInvalidRequest", l, err)
		}
	}
	if _, err := e.svc.ForSubject(ctx, ""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ForSubject(\"\") = %v", err)
	}
	if got, err := e.svc.ForSubject(ctx, phys); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ForSubject(empty) = %v, %v; want an empty list", got, err)
	}
}
