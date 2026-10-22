package library

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"paratech/desktop/internal/domain/library/inspection"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/services/subjectmatch"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)

// fakeSubjects keeps states with the version protocol and the uniqueness of name and alias keys of the sqlite adapter.
type fakeSubjects struct {
	mu   sync.Mutex
	rows map[shared.SubjectID]subject.State
	err  error // returned by every call when set
	// beforeSave runs once before the next Save, to simulate a concurrent change.
	beforeSave func()
	// holding names subjects whose folders the database refuses to delete (a file was filed meanwhile).
	holding map[shared.FolderID]bool
}

func newFakeSubjects() *fakeSubjects {
	return &fakeSubjects{rows: map[shared.SubjectID]subject.State{}, holding: map[shared.FolderID]bool{}}
}

func (f *fakeSubjects) List(context.Context) ([]*subject.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []*subject.Subject
	for _, st := range f.rows {
		s, err := subject.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *subject.Subject) int {
		if a.Name().Key() < b.Name().Key() {
			return -1
		}
		return 1
	})
	return out, nil
}

func (f *fakeSubjects) Get(_ context.Context, id shared.SubjectID) (*subject.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	st, ok := f.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return subject.Restore(st)
}

func (f *fakeSubjects) Save(_ context.Context, s *subject.Subject) error {
	if hook := f.beforeSave; hook != nil {
		f.beforeSave = nil
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	st := s.State()
	cur, ok := f.rows[st.ID]
	switch {
	case st.Version == 0 && ok:
		return shared.ErrVersionConflict
	case st.Version > 0 && (!ok || cur.Version != st.Version):
		return shared.ErrVersionConflict
	}
	keys := map[string]bool{values.Key(st.Name): true}
	for _, a := range st.Aliases {
		keys[values.Key(a)] = true
	}
	for id, other := range f.rows {
		if id == st.ID {
			continue
		}
		if keys[values.Key(other.Name)] {
			return shared.ErrDuplicate
		}
		for _, a := range other.Aliases {
			if keys[values.Key(a)] {
				return shared.ErrDuplicate
			}
		}
	}
	if ok {
		for _, old := range cur.Folders {
			if f.holding[old.ID] && !slices.ContainsFunc(st.Folders, func(n subject.FolderState) bool { return n.ID == old.ID }) {
				return fmt.Errorf("folder holds a file: %w", shared.ErrConflict)
			}
		}
	}
	st.Version++
	f.rows[st.ID] = st
	s.Persisted(st.Version)
	return nil
}

func (f *fakeSubjects) Delete(_ context.Context, s *subject.Subject) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.rows[s.ID()]
	if !ok || cur.Version != s.Version() {
		return shared.ErrVersionConflict
	}
	delete(f.rows, s.ID())
	return nil
}

// fakeMaterials keeps states with the version protocol and one material per sha256.
type fakeMaterials struct {
	mu   sync.Mutex
	rows map[shared.MaterialID]material.State
	err  error
	// saveErrs are returned by the next saves, one each, before the save is tried.
	saveErrs []error
	// beforeSave runs once before the next Save.
	beforeSave func()
}

func newFakeMaterials() *fakeMaterials {
	return &fakeMaterials{rows: map[shared.MaterialID]material.State{}}
}

func (f *fakeMaterials) Get(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	st, ok := f.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return material.Restore(st)
}

func (f *fakeMaterials) GetByDigest(_ context.Context, d material.Digest) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	for _, st := range f.rows {
		if st.Digest == d {
			return material.Restore(st)
		}
	}
	return nil, shared.ErrNotFound
}

func (f *fakeMaterials) ListIn(_ context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*material.Material
	for _, st := range f.rows {
		if st.Subject == sub {
			m, err := material.Restore(st)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b *material.Material) int {
		if a.ID() < b.ID() {
			return -1
		}
		return 1
	})
	return out, nil
}

func (f *fakeMaterials) Count(_ context.Context, sub shared.SubjectID, folder shared.FolderID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	n := 0
	for _, st := range f.rows {
		if st.Subject == sub && (folder == "" || st.Folder == folder) {
			n++
		}
	}
	return n, nil
}

func (f *fakeMaterials) Save(_ context.Context, m *material.Material) error {
	if hook := f.beforeSave; hook != nil {
		f.beforeSave = nil
		hook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.saveErrs) > 0 {
		err := f.saveErrs[0]
		f.saveErrs = f.saveErrs[1:]
		return err
	}
	if f.err != nil {
		return f.err
	}
	st := m.State()
	cur, ok := f.rows[st.ID]
	switch {
	case st.Version == 0 && ok:
		return shared.ErrVersionConflict
	case st.Version > 0 && (!ok || cur.Version != st.Version):
		return shared.ErrVersionConflict
	}
	for id, other := range f.rows {
		if id != st.ID && other.Digest == st.Digest {
			return shared.ErrDuplicate
		}
	}
	st.Version++
	f.rows[st.ID] = st
	m.Persisted(st.Version)
	return nil
}

// fakeBlobs is the artifact store.
type fakeBlobs struct {
	mu     sync.Mutex
	files  map[string][]byte
	writes int
	err    error
}

func newFakeBlobs() *fakeBlobs { return &fakeBlobs{files: map[string][]byte{}} }

func (f *fakeBlobs) Size(_ context.Context, ref shared.ArtifactRef) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[ref.String()]
	if !ok {
		return 0, shared.ErrNotFound
	}
	return int64(len(data)), nil
}

func (f *fakeBlobs) Open(_ context.Context, ref shared.ArtifactRef) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[ref.String()]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeBlobs) Write(_ context.Context, ref shared.ArtifactRef, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.files[ref.String()] = data
	f.writes++
	return nil
}

// fakeTexts reads a TXT file as lines; any other type, and text starting with "BROKEN", cannot be read.
type fakeTexts struct{ calls int }

func (f *fakeTexts) Extract(_ context.Context, typ material.Type, data []byte) (material.Text, error) {
	f.calls++
	switch {
	case typ == material.TypePDF:
		return material.Text{}, material.ErrTextNotExtracted
	case typ != material.TypeTXT, bytes.HasPrefix(data, []byte("BROKEN")):
		return material.Text{}, fmt.Errorf("fake: %w", material.ErrTextUnreadable)
	}
	return material.NewText(material.FragmentsOf(strings.Split(string(data), "\n")), false)
}

// fakeReader is the Hub's reader: it answers the next scripted output or error and records the requests.
type fakeReader struct {
	mu      sync.Mutex
	outputs []string
	errs    []error
	reqs    []inspection.Request
}

func (f *fakeReader) InspectMaterial(_ context.Context, req inspection.Request) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return "", err
		}
	}
	if len(f.outputs) == 0 {
		return "", fmt.Errorf("fake reader: no output scripted")
	}
	out := f.outputs[0]
	if len(f.outputs) > 1 {
		f.outputs = f.outputs[1:]
	}
	return out, nil
}

func (f *fakeReader) requests() []inspection.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reqs)
}

// fakeInspections keeps states with the version protocol.
type fakeInspections struct {
	mu   sync.Mutex
	rows map[shared.MaterialID]inspection.State
	err  error
}

func (f *fakeInspections) Get(_ context.Context, m shared.MaterialID) (*inspection.Inspection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	st, ok := f.rows[m]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return inspection.Restore(st)
}

func (f *fakeInspections) Save(_ context.Context, i *inspection.Inspection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := i.State()
	cur, ok := f.rows[st.Material]
	if (st.Version == 0 && ok) || (st.Version > 0 && (!ok || cur.Version != st.Version)) {
		return shared.ErrVersionConflict
	}
	st.Version++
	f.rows[st.Material] = st
	i.Persisted(st.Version)
	return nil
}

// fakeLessons knows the live lessons by id.
type fakeLessons struct{ live map[shared.LessonID]bool }

func (f fakeLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if !f.live[id] {
		return nil, shared.ErrNotFound
	}
	return lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: "Лекция", Start: t0, End: t0.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
	}}, t0)
}

type fakeStrings struct {
	list []subjectmatch.Candidate
	err  error
}

func (f fakeStrings) ListSubjectStrings(context.Context) ([]subjectmatch.Candidate, error) {
	return f.list, f.err
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// seqIDs hands out numbered ids.
type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (g *seqIDs) next(prefix string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

func (g *seqIDs) NewSubjectID() shared.SubjectID   { return shared.SubjectID(g.next("s")) }
func (g *seqIDs) NewFolderID() shared.FolderID     { return shared.FolderID(g.next("f")) }
func (g *seqIDs) NewMaterialID() shared.MaterialID { return shared.MaterialID(g.next("m")) }

type env struct {
	subjects    *fakeSubjects
	materials   *fakeMaterials
	blobs       *fakeBlobs
	lessons     fakeLessons
	strings     *fakeStrings
	clock       *fakeClock
	texts       *fakeTexts
	reader      *fakeReader
	inspections *fakeInspections
	svc         *Service
}

func newEnv(t interface {
	Helper()
	Fatal(...any)
}) *env {
	t.Helper()
	e := &env{
		subjects: newFakeSubjects(), materials: newFakeMaterials(), blobs: newFakeBlobs(),
		lessons: fakeLessons{live: map[shared.LessonID]bool{"l1": true, "l2": true}}, strings: &fakeStrings{},
		clock: &fakeClock{now: t0}, texts: &fakeTexts{}, reader: &fakeReader{},
		inspections: &fakeInspections{rows: map[shared.MaterialID]inspection.State{}},
	}
	svc, err := New(Deps{
		Subjects: e.subjects, Materials: e.materials, Blobs: e.blobs, Lessons: e.lessons, Strings: e.strings,
		Clock: e.clock, IDs: &seqIDs{}, Texts: e.texts, Reader: e.reader, Inspections: e.inspections,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// setStrings sets the free subject strings the store returns.
func (e *env) setStrings(list []subjectmatch.Candidate) { e.strings.list = list }
