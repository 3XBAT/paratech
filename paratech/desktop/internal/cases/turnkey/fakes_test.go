package turnkey

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/assignment"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// fakeWorks keeps works by their stored state with the version protocol of ADR-0004(d).
type fakeWorks struct {
	mu      sync.Mutex
	rows    map[shared.WorkID]work.State
	sources map[string]bool // sources that exist (assignment or material ids)
	saveErr error
	// beforeSave runs once before the next Save (a concurrent writer).
	beforeSave func()
}

func newFakeWorks(sources ...string) *fakeWorks {
	f := &fakeWorks{rows: map[shared.WorkID]work.State{}, sources: map[string]bool{}}
	for _, s := range sources {
		f.sources[s] = true
	}
	return f
}

func (f *fakeWorks) Get(_ context.Context, id shared.WorkID) (*work.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("work %s: %w", id, shared.ErrNotFound)
	}
	return work.Restore(st)
}

func (f *fakeWorks) ActiveOf(_ context.Context, src work.Source) (*work.Work, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, st := range f.rows {
		if sourceKey(st.Source) == sourceKey(src) && st.Status != work.StatusFailed {
			return work.Restore(st)
		}
	}
	return nil, shared.ErrNotFound
}

// sourceKey identifies a source as the store does: its id, or the request key of a topic.
func sourceKey(s work.Source) string {
	if !s.Topic.IsZero() {
		return "topic:" + s.Topic.Request()
	}
	return string(s.Assignment) + string(s.Material)
}

func (f *fakeWorks) Save(_ context.Context, w *work.Work) error {
	if hook := f.beforeSave; hook != nil {
		f.beforeSave = nil
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	st := w.State()
	stored, ok := f.rows[st.ID]
	switch {
	case st.Version == 0 && ok, st.Version > 0 && (!ok || stored.Version != st.Version):
		return shared.ErrVersionConflict
	case st.Version == 0 && st.Source.Topic.IsZero() && !f.sources[sourceKey(st.Source)]:
		return shared.ErrConflict
	}
	if st.Version == 0 {
		for _, other := range f.rows {
			if sourceKey(other.Source) == sourceKey(st.Source) && other.Status != work.StatusFailed {
				return shared.ErrDuplicate
			}
		}
	}
	st.Version++
	f.rows[st.ID] = st
	w.Persisted(st.Version)
	return nil
}

func (f *fakeWorks) Delete(_ context.Context, id shared.WorkID, expected int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.rows[id]
	if !ok || st.Version != expected {
		return shared.ErrVersionConflict
	}
	delete(f.rows, id)
	return nil
}

type fakeAssignments map[shared.AssignmentID]*assignment.Assignment

func (f fakeAssignments) Get(_ context.Context, id shared.AssignmentID) (*assignment.Assignment, error) {
	a, ok := f[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return a, nil
}

func newAssignment(t *testing.T, id shared.AssignmentID, title, description string, m shared.MaterialID) *assignment.Assignment {
	t.Helper()
	a, err := assignment.Restore(assignment.State{
		Version: 1, ID: id, Material: m, SourceIndex: -1, Title: title, Description: description, CreatedAt: t0, UpdatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// fakeMaterials answers MaterialText with a text or an error per material.
type fakeMaterials struct {
	texts map[shared.MaterialID]string
	errs  map[shared.MaterialID]error
}

func (f fakeMaterials) MaterialText(_ context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	if err := f.errs[id]; err != nil {
		return nil, material.Text{}, err
	}
	s, ok := f.texts[id]
	if !ok {
		return nil, material.Text{}, shared.ErrNotFound
	}
	text, err := material.NewText(material.FragmentsOf(strings.Split(s, "\n")), false)
	if s == "" {
		text, err = material.NewText(nil, false)
	}
	if err != nil {
		return nil, material.Text{}, err
	}
	m, err := material.New(material.Spec{
		ID: id, Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypeTXT, Name: string(id) + ".txt",
	}, t0)
	return m, text, err
}

// fakeReader answers the reader calls in order, like the Hub: a key seen before replays its first answer.
type fakeReader struct {
	mu      sync.Mutex
	answers []readerAnswer
	seen    map[string]readerAnswer
	reqs    []work.Request
}

type readerAnswer struct {
	out string
	err error
}

func (f *fakeReader) then(out string) *fakeReader {
	f.answers = append(f.answers, readerAnswer{out: out})
	return f
}

func (f *fakeReader) fail(err error) *fakeReader {
	f.answers = append(f.answers, readerAnswer{err: err})
	return f
}

func (f *fakeReader) TriageAssignment(_ context.Context, req work.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.seen == nil {
		f.seen = map[string]readerAnswer{}
	}
	if a, ok := f.seen[req.Key()]; ok && a.err == nil {
		return a.out, nil
	}
	if len(f.answers) == 0 {
		return "", errors.New("fake reader: nothing scripted")
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	f.seen[req.Key()] = a
	return a.out, a.err
}

// paid is the number of distinct keys the reader was asked with.
func (f *fakeReader) paid() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := map[string]bool{}
	for _, r := range f.reqs {
		keys[r.Key()] = true
	}
	return len(keys)
}

type fakeAdvisor struct {
	out string
	err error
	got []work.StepQuestion
}

func (f *fakeAdvisor) AdviseStep(_ context.Context, q work.StepQuestion) (string, error) {
	f.got = append(f.got, q)
	return f.out, f.err
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return t0 }

type seqIDs struct{ n int }

func (s *seqIDs) NewWorkID() shared.WorkID {
	s.n++
	return shared.WorkID(fmt.Sprintf("w%d", s.n))
}

// env is a service over fakes: assignment a1 (with material m1) and an uploaded file m2.
type env struct {
	svc    *Service
	works  *fakeWorks
	reader *fakeReader
	advice *fakeAdvisor
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{works: newFakeWorks("a1", "m2", "m3"), reader: &fakeReader{}, advice: &fakeAdvisor{}}
	svc, err := New(Deps{
		Works: e.works,
		Assignments: fakeAssignments{
			"a1": newAssignment(t, "a1", "Реферат по истории", "Тема на выбор", "m1"),
		},
		Materials: fakeMaterials{
			texts: map[shared.MaterialID]string{"m1": "Методичка: объём 10 страниц", "m2": "Задание: эссе", "m3": ""},
			errs:  map[shared.MaterialID]error{"m4": material.ErrTextNotExtracted},
		},
		Reader: e.reader, Advisor: e.advice, Clock: fixedClock{}, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}
