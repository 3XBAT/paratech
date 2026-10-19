package transcriptview

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/domain/transcription/transcript"
)

// addSnapshotRecording stores a completed recording of lesson lid that kept snapshotTitle at its start; the schedule
// holds lid titled liveTitle, or nothing when liveTitle is empty.
func (e *env) addSnapshotRecording(t *testing.T, id shared.RecordingID, lid shared.LessonID, snapshotTitle, liveTitle string) {
	t.Helper()
	if liveTitle != "" {
		l, err := lesson.New(lesson.Spec{ID: lid, Content: lesson.Content{
			Title: liveTitle, Start: t0, End: t0.Add(time.Hour), TimeZone: "Europe/Moscow",
		}}, t0)
		if err != nil {
			t.Fatal(err)
		}
		e.lessons[lid] = l
	}
	r, err := recording.New(recording.Spec{
		ID: id, LessonID: lid, MaxDuration: 3 * time.Hour,
		Lesson: values.NewLessonSnapshot(values.LessonSnapshotSpec{Title: snapshotTitle, Start: t0, End: t0.Add(time.Hour)}),
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	end := t0.Add(time.Hour)
	track, err := r.TrackRef("wav")
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(r.Begin(t0), r.Stop(end), r.Finalize(end), r.Complete(end, track, time.Hour)); err != nil {
		t.Fatal(err)
	}
	e.recordings[id] = r
}

// TASK-088: the title kept at the start wins; the lesson in the schedule only stands in for a recording without one.
func TestTitleFromLessonSnapshot(t *testing.T) {
	e := newEnv(t)
	e.addSnapshotRecording(t, "r-gone", "l-gone", "Тестовый урок", "")             // the import deleted the lesson
	e.addSnapshotRecording(t, "r-renamed", "l-renamed", "Тестовый урок", "Другое") // the import renamed it
	e.addSnapshotRecording(t, "r-old", "l-old", "", "Физика")                      // stored before snapshots
	ctx := context.Background()

	tests := []struct {
		rec       shared.RecordingID
		title     string
		suggested string
	}{
		{"r-gone", "Тестовый урок", "Тестовый урок 2026-10-19.txt"},
		{"r-renamed", "Тестовый урок", "Тестовый урок 2026-10-19.txt"},
		{"r-old", "Физика", "Физика 2026-10-19.txt"},
	}
	for _, tc := range tests {
		t.Run(string(tc.rec), func(t *testing.T) {
			st, err := e.svc.Status(ctx, tc.rec)
			if err != nil || st.Title != tc.title {
				t.Fatalf("Status = %+v, %v; want title %q", st, err, tc.title)
			}
			name, err := e.svc.SuggestedName(ctx, tc.rec, transcript.FormatText)
			if err != nil || name != tc.suggested {
				t.Fatalf("SuggestedName = %q, %v; want %q", name, err, tc.suggested)
			}

			e.addTranscript(t, tc.rec, segsN(1))
			path := "C:/out/" + string(tc.rec) + ".txt"
			if err := e.svc.Export(ctx, tc.rec, transcript.FormatText, false, path); err != nil {
				t.Fatal(err)
			}
			if got := e.writer.content; e.writer.path != path || !strings.HasPrefix(got, tc.title) {
				t.Fatalf("export starts with %q; want the title %q", got, tc.title)
			}
		})
	}
}
