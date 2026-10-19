package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/shared"
)

// The hints and the correction of an assignment (TASK-092) survive an insert and an update.
func TestAssignmentLessonNamesRoundTrip(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	a := newExtracted(t, "a1", "t1", 0, asgT0)
	if _, err := a.SetHints(values.NewLessonNames("Матанализ", "Иванов И. И."), asgT0); err != nil {
		t.Fatal(err)
	}
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := e.assignments.Get(ctx, "a1")
	if err != nil || got.State() != a.State() {
		t.Fatalf("after insert: %+v, %v; want %+v", got.State(), err, a.State())
	}

	a.Correct(values.NewLessonNames(strings.Repeat("я", values.MaxNameRunes), "Сидоров"), asgT0)
	if err := e.assignments.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err = e.assignments.Get(ctx, "a1")
	if err != nil || got.State() != a.State() || got.Correction().Teacher() != "Сидоров" {
		t.Fatalf("after update: %+v, %v; want %+v", got.State(), err, a.State())
	}

	// The CHECK bounds the stored text in characters, the limit of the domain.
	if _, err := e.db.sql.ExecContext(ctx, `UPDATE assignments SET user_subject = ? WHERE id = 'a1'`,
		strings.Repeat("я", values.MaxNameRunes+1)); err == nil {
		t.Fatal("a subject over the limit was stored")
	}
}

// RecordingOf finds the recording behind a transcript with its lesson snapshot; an unknown transcript is not found.
func TestRecordingOfTranscript(t *testing.T) {
	e, ctx := newCourseworkEnv(t), context.Background()
	repo := NewRecordingRepository(e.db)

	got, err := repo.RecordingOf(ctx, "t1")
	if err != nil {
		t.Fatal(err)
	}
	want, err := repo.Get(ctx, e.rec.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RecordingOf = %+v, want %+v", got, want)
	}

	if _, err := repo.RecordingOf(ctx, "t-missing"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown transcript: %v, want ErrNotFound", err)
	}
}

func TestRecordingOfTranscriptKeepsTheSnapshot(t *testing.T) {
	e, ctx := newTranscriptEnv(t), context.Background()
	repo := NewRecordingRepository(e.db)
	if err := NewLessonRepository(e.db).Save(ctx, newTestLesson(t, "l1", "", 0)); err != nil {
		t.Fatal(err)
	}
	rec := snapshotRecording(t, "r-snap", "l1", testLessonSnapshot())
	saveRecording(t, repo, rec)
	e.rec = rec
	if err := e.repo.Save(ctx, e.newTranscript(t, "t-snap", "ru")); err != nil {
		t.Fatal(err)
	}

	got, err := repo.RecordingOf(ctx, "t-snap")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LessonSnapshot().Equal(testLessonSnapshot()) || got.ID() != "r-snap" {
		t.Fatalf("snapshot = %+v of %s", got.LessonSnapshot(), got.ID())
	}
}
