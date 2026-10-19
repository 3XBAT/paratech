package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

// TASK-088, the owner's bug of 2026-10-19 on the wired core: a recording of an imported lesson keeps its title after
// the next import of a file without that lesson soft-deletes it, in the recordings list and in the transcript viewer.
func TestRecordingKeepsTheLessonTitleAfterAnImport(t *testing.T) {
	ctx := context.Background()
	e, _ := testEnv(t)
	c, _, b := startPromptCore(t, e, func(c *core) {
		start := time.Now().Add(-10 * time.Minute)
		l, err := lesson.New(lesson.Spec{ID: "lesson-1", SourceUID: "csv:id:test-1", Content: lesson.Content{
			Title: "Тестовый урок", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		}}, start)
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlite.NewLessonRepository(c.db).Save(ctx, l); err != nil {
			t.Fatal(err)
		}
	})

	r := startRecordingNow(t, b, "")
	if !r.Started || !r.Linked {
		t.Fatalf("StartRecording = %+v, want started and linked", r)
	}
	id := shared.RecordingID(r.RecordingID)

	// Another test.csv: the lesson of the recording is not in it, so the import removes it.
	path := filepath.Join(t.TempDir(), "test.csv")
	csv := "title,date,start,end,timezone,uid\nMath,2026-10-14,09:00,10:30,Europe/Moscow,math-1\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	page := wails.NewScheduleImportService(c.ui.Schedule, c.ui.Files, pathPicker(path), slog.New(slog.DiscardHandler))
	p, err := page.ChooseFile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := page.Commit(ctx, p.SelectionID); err != nil || res != (wails.ImportResult{Added: 1, Removed: 1}) {
		t.Fatalf("Commit = %+v, %v; want the lesson of the recording removed", res, err)
	}
	if _, err := sqlite.NewLessonRepository(c.db).Get(ctx, "lesson-1"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("lesson after the import: %v; want it removed", err)
	}

	rows, err := c.ui.Activity.ListRecordings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == id {
			found = true
			if row.LessonTitle != "Тестовый урок" {
				t.Fatalf("recordings row titled %q; want the lesson title kept", row.LessonTitle)
			}
		}
	}
	if !found {
		t.Fatalf("recording %s not listed in %+v", id, rows)
	}

	st, err := c.ui.Transcript.Status(ctx, id)
	if err != nil || st.Title != "Тестовый урок" {
		t.Fatalf("transcript viewer = %+v, %v; want the lesson title kept", st, err)
	}
}
