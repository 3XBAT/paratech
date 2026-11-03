package subjectpurge

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/library/values"
	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, time.October, 27, 10, 0, 0, 0, time.UTC)

type fakeNote struct {
	sub   shared.SubjectID
	parts []shared.MaterialID
}

// store is the library on fakes: it keeps the guards of the sqlite adapter (still removed at or before the cutoff,
// the foreign keys of the subject row, a material held by a note is never deleted).
type store struct {
	mu         sync.Mutex
	subjects   map[shared.SubjectID]subject.State
	notes      map[shared.NoteID]fakeNote
	ticketSets map[shared.TicketSetID]shared.SubjectID
	materials  map[shared.MaterialID]material.State
	pinned     map[material.Digest]bool // the digests a send of a work not yet carried out pinned
	files      map[string]bool

	// fail holds an error for the next call of an operation ("note n1", "blob materials/…", "subject s1", …).
	fail map[string]error
	// calls counts the calls by operation kind.
	calls map[string]int
}

func newStore() *store {
	return &store{
		subjects: map[shared.SubjectID]subject.State{}, notes: map[shared.NoteID]fakeNote{},
		ticketSets: map[shared.TicketSetID]shared.SubjectID{}, materials: map[shared.MaterialID]material.State{},
		pinned: map[material.Digest]bool{}, files: map[string]bool{}, fail: map[string]error{}, calls: map[string]int{},
	}
}

// failOnce makes the next call of op fail with err.
func (s *store) failOnce(op string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail[op] = err
}

// hit counts a call of kind and returns the error set for op, once.
func (s *store) hit(kind, op string) error {
	s.calls[kind]++
	err := s.fail[op]
	delete(s.fail, op)
	return err
}

// due reports whether sub is stored and removed at or before cutoff.
func (s *store) due(sub shared.SubjectID, cutoff time.Time) bool {
	st, ok := s.subjects[sub]
	return ok && !st.RemovedAt.IsZero() && !st.RemovedAt.After(cutoff)
}

// snapshot is a deep copy of the content, to compare runs.
func (s *store) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprint(sortedKeys(s.subjects), sortedKeys(s.notes), sortedKeys(s.ticketSets), s.materials,
		sortedKeys(s.files))
}

func sortedKeys[K ~string, V any](m map[K]V) []K { return slices.Sorted(maps.Keys(m)) }

type fakeSubjects struct{ *store }

func (f fakeSubjects) ListRemoved(context.Context) ([]*subject.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("list", "list subjects"); err != nil {
		return nil, err
	}
	var out []*subject.Subject
	for _, id := range sortedKeys(f.subjects) {
		st := f.subjects[id]
		if st.RemovedAt.IsZero() {
			continue
		}
		s, err := subject.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (f fakeSubjects) PurgeRemoved(_ context.Context, id shared.SubjectID, cutoff time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("subject", "subject "+string(id)); err != nil {
		return false, err
	}
	if !f.due(id, cutoff) {
		return false, nil
	}
	for _, m := range f.materials {
		if m.Subject == id {
			return false, fmt.Errorf("a file is filed under it: %w", shared.ErrConflict)
		}
	}
	for _, n := range f.notes {
		if n.sub == id {
			return false, fmt.Errorf("a note is under it: %w", shared.ErrConflict)
		}
	}
	delete(f.subjects, id)
	return true, nil
}

type fakeNotes struct{ *store }

func (f fakeNotes) ListIDsBySubject(_ context.Context, sub shared.SubjectID) ([]shared.NoteID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("list", "list notes"); err != nil {
		return nil, err
	}
	var out []shared.NoteID
	for _, id := range sortedKeys(f.notes) {
		if f.notes[id].sub == sub {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f fakeNotes) PurgeOfRemovedSubject(_ context.Context, id shared.NoteID, sub shared.SubjectID, cutoff time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("note", "note "+string(id)); err != nil {
		return false, err
	}
	n, ok := f.notes[id]
	if !ok || n.sub != sub || !f.due(sub, cutoff) {
		return false, nil
	}
	delete(f.notes, id)
	return true, nil
}

type fakeTicketSets struct{ *store }

func (f fakeTicketSets) ListOfSubject(_ context.Context, sub shared.SubjectID) ([]shared.TicketSetID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("list", "list ticket sets"); err != nil {
		return nil, err
	}
	var out []shared.TicketSetID
	for _, id := range sortedKeys(f.ticketSets) {
		if f.ticketSets[id] == sub {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f fakeTicketSets) PurgeOfRemovedSubject(_ context.Context, id shared.TicketSetID, sub shared.SubjectID, cutoff time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("ticket set", "ticket set "+string(id)); err != nil {
		return false, err
	}
	if s, ok := f.ticketSets[id]; !ok || s != sub || !f.due(sub, cutoff) {
		return false, nil
	}
	delete(f.ticketSets, id)
	return true, nil
}

type fakeMaterials struct{ *store }

func (f fakeMaterials) ListFiledUnder(_ context.Context, sub shared.SubjectID) ([]*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("list", "list materials"); err != nil {
		return nil, err
	}
	var out []*material.Material
	for _, id := range sortedKeys(f.materials) {
		if st := f.materials[id]; st.Subject == sub {
			m, err := material.Restore(st)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
	}
	return out, nil
}

func (f fakeMaterials) PartOfOtherNote(_ context.Context, id shared.MaterialID, sub shared.SubjectID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("held", "held "+string(id)); err != nil {
		return false, err
	}
	for _, n := range f.notes {
		if n.sub != sub && slices.Contains(n.parts, id) {
			return true, nil
		}
	}
	return false, nil
}

func (f fakeMaterials) DigestInUse(_ context.Context, d material.Digest, except shared.MaterialID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("in use", "in use "+string(except)); err != nil {
		return false, err
	}
	if f.pinned[d] {
		return true, nil
	}
	for id, st := range f.materials {
		if id != except && st.Digest == d {
			return true, nil
		}
	}
	return false, nil
}

func (f fakeMaterials) Save(_ context.Context, m *material.Material) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := m.State()
	if err := f.hit("save", "save "+string(st.ID)); err != nil {
		return err
	}
	cur, ok := f.materials[st.ID]
	if !ok || cur.Version != st.Version {
		return shared.ErrVersionConflict
	}
	st.Version++
	f.materials[st.ID] = st
	m.Persisted(st.Version)
	return nil
}

func (f fakeMaterials) PurgeFiledUnderRemoved(_ context.Context, id shared.MaterialID, sub shared.SubjectID, cutoff time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("material", "material "+string(id)); err != nil {
		return false, err
	}
	st, ok := f.materials[id]
	if !ok || st.Subject != sub || !f.due(sub, cutoff) {
		return false, nil
	}
	for _, n := range f.notes {
		if slices.Contains(n.parts, id) {
			return false, nil
		}
	}
	delete(f.materials, id)
	return true, nil
}

type fakeBlobs struct{ *store }

func (f fakeBlobs) Delete(_ context.Context, ref shared.ArtifactRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.hit("blob", "blob "+ref.String()); err != nil {
		return err
	}
	delete(f.files, ref.String())
	return nil
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// newPurger is a Purger on st with the clock at now.
func newPurger(t *testing.T, st *store, now time.Time) *Purger {
	t.Helper()
	p, err := New(Deps{
		Subjects: fakeSubjects{st}, Notes: fakeNotes{st}, TicketSets: fakeTicketSets{st}, Materials: fakeMaterials{st},
		Blobs: fakeBlobs{st}, Clock: &fakeClock{now: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// addSubject stores a subject named name, removed at removedAt (zero: active), with an alias, an own folder and a
// teacher.
func (s *store) addSubject(t *testing.T, id shared.SubjectID, name string, removedAt time.Time) {
	t.Helper()
	sub, err := subject.New(subject.Spec{ID: id, Name: name, Aliases: []string{name + " (алиас)"}, Semester: values.SemesterOf(t0)}, t0)
	if err != nil {
		t.Fatal(err)
	}
	fn, err := subject.NewFolderName("Своя")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sub.AddFolder(shared.FolderID(string(id)+"-own"), fn, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.SetTeacher(subject.TeacherLecture, "Иванов И. И.", "teacher@example.test", t0); err != nil {
		t.Fatal(err)
	}
	if !removedAt.IsZero() {
		if _, err := sub.Remove(removedAt); err != nil {
			t.Fatal(err)
		}
	}
	st := sub.State()
	st.Version = 1
	s.subjects[id] = st
}

// addMaterial stores a material of content filed in the materials folder of sub ("" the inbox), removed when asked,
// with its stored bytes and cached text.
func (s *store) addMaterial(t *testing.T, id shared.MaterialID, sub shared.SubjectID, removed bool) material.Digest {
	t.Helper()
	data := []byte("content of " + string(id))
	m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf(data), Size: int64(len(data)), Type: material.TypeTXT, Name: string(id) + ".txt"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if sub != "" {
		if _, err := m.Place(sub, subject.SystemFolderID(sub, subject.SystemMaterials), t0); err != nil {
			t.Fatal(err)
		}
	}
	if removed {
		if _, err := m.Remove(t0); err != nil {
			t.Fatal(err)
		}
	}
	st := m.State()
	st.Version = 1
	s.materials[id] = st
	for _, ref := range refsOf(t, st.Digest) {
		s.files[ref] = true
	}
	return st.Digest
}

// refsOf is the stored file and the cached text of d.
func refsOf(t *testing.T, d material.Digest) []string {
	t.Helper()
	blob, err := material.BlobRef(d)
	if err != nil {
		t.Fatal(err)
	}
	text, err := material.TextRef(d)
	if err != nil {
		t.Fatal(err)
	}
	return []string{blob.String(), text.String()}
}

var errDisk = errors.New("disk busy")
