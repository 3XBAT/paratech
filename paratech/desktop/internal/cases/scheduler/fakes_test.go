package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// lessonStore keeps stored states, so every list returns fresh aggregates as a database would; a restarted
// scheduler on the same store sees what the previous one saved.
type lessonStore struct {
	mu      sync.Mutex
	rows    map[shared.LessonID]lesson.State
	listErr error
	saveErr error

	// beforeSave runs once on the next Save, without the lock: a concurrent writer changing the row.
	beforeSave func()
}

func newLessonStore() *lessonStore { return &lessonStore{rows: map[shared.LessonID]lesson.State{}} }

func stateOf(l *lesson.Lesson, version int64) lesson.State {
	return lesson.State{
		Version: version, ID: l.ID(), Content: l.Content(), SourceUID: l.SourceUID(), StartNotifiedAt: l.StartNotifiedAt(),
		ContentRevision: l.ContentRevision(), CalendarSyncedRevision: l.CalendarSyncedRevision(),
		CalendarSyncedAt: l.CalendarSyncedAt(), DeletedAt: l.DeletedAt(), CreatedAt: l.CreatedAt(), UpdatedAt: l.UpdatedAt(),
	}
}

// add stores a new lesson of the given start and duration in zone zone.
func (s *lessonStore) add(t *testing.T, id string, start time.Time, d time.Duration, zone string) {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: shared.LessonID(id), Content: lesson.Content{
		Title: "Lesson " + id, Start: start, End: start.Add(d), TimeZone: zone,
	}}, start.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[l.ID()] = stateOf(l, 1)
}

func (s *lessonStore) get(t *testing.T, id string) *lesson.Lesson {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := lesson.Restore(s.rows[shared.LessonID(id)])
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func (s *lessonStore) update(t *testing.T, id string, fn func(*lesson.Lesson) error) {
	t.Helper()
	l := s.get(t, id)
	if err := fn(l); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[l.ID()] = stateOf(l, l.Version()+1)
}

func (s *lessonStore) ListStartingBetween(_ context.Context, from, to time.Time) ([]*lesson.Lesson, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []*lesson.Lesson
	for _, st := range s.rows {
		if !st.DeletedAt.IsZero() || st.Content.Start.Before(from) || !st.Content.Start.Before(to) {
			continue
		}
		l, err := lesson.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Content().Start.Before(out[j].Content().Start) })
	return out, nil
}

func (s *lessonStore) Save(_ context.Context, l *lesson.Lesson) error {
	s.mu.Lock()
	hook := s.beforeSave
	s.beforeSave = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	if cur, ok := s.rows[l.ID()]; !ok || cur.Version != l.Version() {
		return fmt.Errorf("fake: lesson %s: %w", l.ID(), shared.ErrVersionConflict)
	}
	s.rows[l.ID()] = stateOf(l, l.Version()+1)
	l.Persisted(l.Version() + 1)
	return nil
}

type eventLog struct {
	mu     sync.Mutex
	events []lesson.StartAnnounced
	err    error
}

func (e *eventLog) LessonStarting(_ context.Context, ev lesson.StartAnnounced) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return e.err
}

func (e *eventLog) all() []lesson.StartAnnounced {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]lesson.StartAnnounced(nil), e.events...)
}

func (e *eventLog) ids() []shared.LessonID {
	var out []shared.LessonID
	for _, ev := range e.all() {
		out = append(out, ev.LessonID)
	}
	return out
}
