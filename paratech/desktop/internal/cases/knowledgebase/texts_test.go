package knowledgebase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/shared"
)

// Texts gives the lessons of a library subject (or of the whole base) with what the search reads of them, for the
// answers with citations (TASK-118).
func TestTexts(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.svc.library = &fakeResolver{table: map[string]subjectmatch.Match{"Физика": {ID: "phys", Name: "Физика"}}}
	e.add(t, lessonSpec{id: "old", title: "Кванты", subject: "Физика"})
	e.add(t, lessonSpec{id: "new", start: 24 * time.Hour, title: "Оптика", subject: "Физика"})
	e.add(t, lessonSpec{id: "chem", start: 48 * time.Hour, title: "Реакции", subject: "Химия"})
	e.addTranscript(t, "old", " Сегодня про кванты.")
	e.addSummary(t, "old", "Про кванты и поля.", "Кванты дискретны")
	e.addLectureText(t, "old", block(t, "Кванты", 60_000, "Энергия квантуется."))
	e.addTranscript(t, "new", " Свет и линзы.")

	got, err := e.svc.Texts(ctx, "phys")
	if err != nil {
		t.Fatal(err)
	}
	var recs []shared.RecordingID
	for _, lt := range got {
		recs = append(recs, lt.Lesson.Recording)
		if lt.Document.Recording != lt.Lesson.Recording {
			t.Fatalf("document of %s is %s", lt.Lesson.Recording, lt.Document.Recording)
		}
	}
	if want := []shared.RecordingID{"new", "old"}; !reflect.DeepEqual(recs, want) {
		t.Fatalf("lessons %v, want %v", recs, want)
	}
	old := got[1].Document
	if len(old.Summary) == 0 || len(old.Blocks) != 1 || old.Blocks[0].StartMs != 60_000 || len(old.Segments) != 1 {
		t.Fatalf("texts of old = %+v", old)
	}

	all, err := e.svc.Texts(ctx, "")
	if err != nil || len(all) != 3 || all[0].Lesson.Recording != "chem" {
		t.Fatalf("Texts(all) = %d lessons, %v", len(all), err)
	}
	if none, err := e.svc.Texts(ctx, "bio"); err != nil || len(none) != 0 {
		t.Fatalf("Texts(no lessons) = %v, %v", none, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.svc.Texts(cancelled, "phys"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
