package schedulesync

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/schedule/services/importplan"
	"paratech/desktop/internal/domain/shared"
)

type txKey struct{}

// lessonStore is the in-memory schedule behind LessonRepository and Tx. Lessons are kept as State and rebuilt with
// Restore, as the sqlite adapter does; ApplyChanges is all-or-nothing with the version compare-and-swap.
type lessonStore struct {
	t *testing.T

	mu       sync.Mutex
	rows     map[shared.LessonID]lesson.State
	applies  int
	applyErr error // returned by the next ApplyChanges when set

	txMu sync.Mutex
}

func newLessonStore(t *testing.T) *lessonStore {
	return &lessonStore{t: t, rows: map[shared.LessonID]lesson.State{}}
}

func stateOf(l *lesson.Lesson, version int64) lesson.State {
	return lesson.State{
		Version: version, ID: l.ID(), Content: l.Content(), SourceUID: l.SourceUID(), StartNotifiedAt: l.StartNotifiedAt(),
		ContentRevision: l.ContentRevision(), CalendarSyncedRevision: l.CalendarSyncedRevision(),
		CalendarSyncedAt: l.CalendarSyncedAt(), DeletedAt: l.DeletedAt(), CreatedAt: l.CreatedAt(), UpdatedAt: l.UpdatedAt(),
	}
}

func (s *lessonStore) put(l *lesson.Lesson) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[l.ID()] = stateOf(l, 1)
	l.Persisted(1)
}

func (s *lessonStore) snapshot() map[shared.LessonID]lesson.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[shared.LessonID]lesson.State, len(s.rows))
	for k, v := range s.rows {
		out[k] = v
	}
	return out
}

func (s *lessonStore) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.rows[id]
	if !ok || !st.DeletedAt.IsZero() {
		return nil, fmt.Errorf("lesson %s: %w", id, shared.ErrNotFound)
	}
	return lesson.Restore(st)
}

func (s *lessonStore) Save(ctx context.Context, l *lesson.Lesson) error {
	return s.ApplyChanges(ctx, []*lesson.Lesson{l}, nil)
}

func (s *lessonStore) ApplyChanges(ctx context.Context, upserts, deleted []*lesson.Lesson) error {
	if ctx.Value(txKey{}) == nil {
		s.t.Error("ApplyChanges outside Tx.Do")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applies++
	if err := s.applyErr; err != nil {
		s.applyErr = nil
		return err
	}
	all := append(append([]*lesson.Lesson(nil), upserts...), deleted...)
	for _, l := range all {
		st, ok := s.rows[l.ID()]
		if (l.Version() == 0 && ok) || (l.Version() > 0 && (!ok || st.Version != l.Version())) {
			return shared.ErrVersionConflict
		}
	}
	for _, l := range all {
		s.rows[l.ID()] = stateOf(l, l.Version()+1)
	}
	for _, l := range all {
		l.Persisted(l.Version() + 1)
	}
	return nil
}

func (s *lessonStore) ListLive(context.Context) ([]*lesson.Lesson, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*lesson.Lesson
	for _, st := range s.rows {
		if !st.DeletedAt.IsZero() {
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

func (s *lessonStore) ListStartingBetween(context.Context, time.Time, time.Time) ([]*lesson.Lesson, error) {
	return nil, errors.New("not used")
}

func (s *lessonStore) ListPendingCalendarSync(context.Context, int) ([]*lesson.Lesson, error) {
	return nil, errors.New("not used")
}

func (s *lessonStore) PurgeDeleted(context.Context, shared.LessonID) error {
	return errors.New("not used")
}

func (s *lessonStore) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(txKey{}) != nil {
		return fn(ctx)
	}
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return fn(context.WithValue(ctx, txKey{}, true))
}

// fakeParser returns the rows registered for the file content.
type fakeParser struct {
	files map[string]importplan.Parsed
	err   error
	calls int
}

func (p *fakeParser) Parse(_ context.Context, data []byte) (importplan.Parsed, error) {
	p.calls++
	if p.err != nil {
		return importplan.Parsed{}, p.err
	}
	parsed, ok := p.files[string(data)]
	if !ok {
		return importplan.Parsed{}, fmt.Errorf("%w: unknown test file", importplan.ErrUnreadableFile)
	}
	return parsed, nil
}

type seqIDs struct{ n int }

func (g *seqIDs) NewLessonID() shared.LessonID {
	g.n++
	return shared.LessonID(fmt.Sprintf("lesson-%d", g.n))
}

func (g *seqIDs) NewJobID() shared.JobID { return "job" }

var (
	_ LessonRepository = (*lessonStore)(nil)
	_ Tx               = (*lessonStore)(nil)
	_ ScheduleParser   = (*fakeParser)(nil)
	_ IDGenerator      = (*seqIDs)(nil)
)
