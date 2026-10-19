package activity

import (
	"context"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/shared"
)

// TASK-088: a row takes the lesson from the snapshot kept at the start, also when the schedule lost or renamed the
// lesson, and the schedule is not read for it; a recording without a snapshot shows the live title as before.
func TestListRecordingsLessonSnapshot(t *testing.T) {
	snap := values.NewLessonSnapshot(values.LessonSnapshotSpec{
		Title: "Тестовый урок", Subject: "Матанализ", Kind: "lecture", Teacher: "Иванов И. И.", Start: t0,
	})
	mk := func(id string, l shared.LessonID, s values.LessonSnapshot) *recording.Recording {
		r, err := recording.New(recording.Spec{ID: shared.RecordingID(id), LessonID: l, Lesson: s, MaxDuration: 3 * time.Hour}, t0)
		must(t, err)
		return r
	}
	recs := &fakeRecordings{list: []*recording.Recording{
		mk("r-gone", "gone", snap), mk("r-renamed", "renamed", snap), mk("r-old", "math", values.LessonSnapshot{}),
	}}
	lessons := &fakeLessons{titles: map[shared.LessonID]string{"renamed": "Другое", "math": "Физика"}}
	s, err := New(recs, &fakeJobs{}, lessons, nil)
	must(t, err)

	got, err := s.ListRecordings(context.Background())
	must(t, err)

	type lessonCols struct{ title, subject, kind, teacher string }
	want := map[shared.RecordingID]lessonCols{
		"r-gone":    {"Тестовый урок", "Матанализ", "lecture", "Иванов И. И."},
		"r-renamed": {"Тестовый урок", "Матанализ", "lecture", "Иванов И. И."},
		"r-old":     {title: "Физика"},
	}
	if len(got) != len(want) {
		t.Fatalf("rows %+v", got)
	}
	for _, r := range got {
		if c := (lessonCols{r.LessonTitle, r.LessonSubject, r.LessonKind, r.LessonTeacher}); c != want[r.ID] {
			t.Errorf("%s: lesson %+v, want %+v", r.ID, c, want[r.ID])
		}
	}
	if lessons.gets != 1 {
		t.Errorf("lesson reads = %d, want 1 (only the recording without a snapshot)", lessons.gets)
	}
}
