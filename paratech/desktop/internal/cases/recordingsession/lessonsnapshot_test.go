package recordingsession

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// memLessons is a LessonReader over a map; err fails every read.
type memLessons struct {
	mu    sync.Mutex
	rows  map[shared.LessonID]*lesson.Lesson
	err   error
	calls int
}

func (m *memLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	l, ok := m.rows[id]
	if !ok {
		return nil, fmt.Errorf("lesson %s: %w", id, shared.ErrNotFound)
	}
	return l, nil
}

func (m *memLessons) put(t *testing.T, id shared.LessonID, title string, start time.Time) {
	t.Helper()
	l, err := lesson.New(lesson.Spec{
		ID: id, SourceUID: "uid-" + string(id),
		Content: lesson.Content{Title: title, Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow"},
	}, t0)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[shared.LessonID]*lesson.Lesson{}
	}
	m.rows[id] = l
}

// TASK-088: a recording keeps the lesson as it was at the start; a lesson that cannot be read never stops the start.
func TestStartKeepsLessonSnapshot(t *testing.T) {
	start := t0.Add(-10 * time.Minute)
	tests := []struct {
		name      string
		lessonID  shared.LessonID
		lessonErr error
		want      values.LessonSnapshot
		reads     int
	}{
		{
			name: "lesson in the schedule", lessonID: "l1", reads: 1,
			want: values.NewLessonSnapshot(values.LessonSnapshotSpec{
				Title: "Тестовый урок", Start: start, End: start.Add(90 * time.Minute),
			}),
		},
		{name: "lesson gone", lessonID: "l-gone", reads: 1},
		{name: "read fails", lessonID: "l1", lessonErr: errors.New("database is locked"), reads: 1},
		{name: "read cancelled", lessonID: "l1", lessonErr: context.Canceled, reads: 1},
		{name: "ad-hoc", lessonID: "", reads: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t, DefaultConfig())
			e.lesson.put(t, "l1", "Тестовый урок", start)
			e.lesson.err = tt.lessonErr

			info, err := e.svc.Start(testCtx(t), StartRequest{LessonID: tt.lessonID})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			r := e.stored(info.ID)
			if r.Status() != recording.StatusRecording || r.LessonID() != tt.lessonID || info.LessonID != tt.lessonID {
				t.Fatalf("stored %s with lesson %q (info %q)", r.Status(), r.LessonID(), info.LessonID)
			}
			if !r.LessonSnapshot().Equal(tt.want) {
				t.Fatalf("snapshot = %+v, want %+v", r.LessonSnapshot(), tt.want)
			}
			if e.lesson.calls != tt.reads {
				t.Fatalf("lesson reads = %d, want %d", e.lesson.calls, tt.reads)
			}

			// The snapshot stays through the end of the recording.
			if _, err := e.svc.Stop(testCtx(t), info.ID); err != nil {
				t.Fatal(err)
			}
			if got := e.stored(info.ID).LessonSnapshot(); !got.Equal(tt.want) {
				t.Fatalf("snapshot after stop = %+v", got)
			}
		})
	}
}

// Without the port wired the recording starts with no snapshot.
func TestStartWithoutLessonReader(t *testing.T) {
	e := newEnv(t, DefaultConfig())
	e.svc.lessons = nil
	info, err := e.svc.Start(testCtx(t), StartRequest{LessonID: "l1"})
	if err != nil {
		t.Fatal(err)
	}
	if r := e.stored(info.ID); !r.LessonSnapshot().IsZero() || r.LessonID() != "l1" {
		t.Fatalf("lesson %q, snapshot %+v", r.LessonID(), r.LessonSnapshot())
	}
}
