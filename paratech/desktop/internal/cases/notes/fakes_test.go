package notes

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// msk is the local zone of the tests: a lecture at 23:30 UTC is on the next day there.
var msk = time.FixedZone("MSK", 3*60*60)

var t0 = time.Date(2026, time.October, 27, 10, 0, 0, 0, msk)

// errSubjectGone stands for the library's ErrSubjectNotFound, which passes through the port as it is.
var errSubjectGone = errors.New("library: subject not found")

// fakeNotes keeps note states with the version protocol, in the order made.
type fakeNotes struct {
	mu    sync.Mutex
	order []shared.NoteID
	rows  map[shared.NoteID]note.State
	// saveErrs are returned by the next saves, one each, before the save is tried.
	saveErrs []error
	saves    int
}

func newFakeNotes() *fakeNotes { return &fakeNotes{rows: map[shared.NoteID]note.State{}} }

func (f *fakeNotes) Get(_ context.Context, id shared.NoteID) (*note.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return note.Restore(st)
}

func (f *fakeNotes) Save(_ context.Context, n *note.Note) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.saveErrs) > 0 {
		err := f.saveErrs[0]
		f.saveErrs = f.saveErrs[1:]
		return err
	}
	st := n.State()
	cur, ok := f.rows[st.ID]
	if (st.Version == 0 && ok) || (st.Version > 0 && (!ok || cur.Version != st.Version)) {
		return shared.ErrVersionConflict
	}
	if st.Subject == "gone" {
		return fmt.Errorf("fake: subject: %w", shared.ErrConflict)
	}
	if !ok {
		f.order = append(f.order, st.ID)
	}
	st.Version++
	f.rows[st.ID] = st
	n.Persisted(st.Version)
	f.saves++
	return nil
}

func (f *fakeNotes) list(keep func(note.State) bool) ([]*note.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*note.Note
	for _, id := range f.order {
		if st := f.rows[id]; keep(st) {
			n, err := note.Restore(st)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
	}
	return out, nil
}

func (f *fakeNotes) ListBySubject(_ context.Context, sub shared.SubjectID) ([]*note.Note, error) {
	return f.list(func(st note.State) bool { return st.Subject == sub })
}

func (f *fakeNotes) ListByRecording(_ context.Context, rec shared.RecordingID) ([]*note.Note, error) {
	return f.list(func(st note.State) bool { return st.Anchor.Recording() == rec })
}

func (f *fakeNotes) ListByLesson(_ context.Context, l shared.LessonID) ([]*note.Note, error) {
	return f.list(func(st note.State) bool { return st.Anchor.Lesson() == l })
}

func (f *fakeNotes) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// fakeMaterials is the library: one material per content, a new one filed under the subject in a folder named by the
// lesson kind.
type fakeMaterials struct {
	mu    sync.Mutex
	rows  map[shared.MaterialID]*material.Material
	n     int
	kinds []string // lesson kinds FileNotePart was called with
	err   error    // returned by FileNotePart when set
}

func newFakeMaterials() *fakeMaterials {
	return &fakeMaterials{rows: map[shared.MaterialID]*material.Material{}}
}

func folderOf(kind string) shared.FolderID { return shared.FolderID("folder-" + cmpKind(kind)) }

func cmpKind(kind string) string {
	if kind == "" {
		return "none"
	}
	return kind
}

func (f *fakeMaterials) FileNotePart(_ context.Context, sub shared.SubjectID, kind, name string, data []byte) (*material.Material, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kinds = append(f.kinds, kind)
	if f.err != nil {
		return nil, false, f.err
	}
	if sub == "nosuch" {
		return nil, false, errSubjectGone
	}
	d := material.DigestOf(data)
	for _, m := range f.rows {
		if m.Digest() == d {
			return m, true, nil
		}
	}
	typ, err := material.DetectType(data)
	if err != nil {
		return nil, false, err
	}
	m, err := f.add(d, int64(len(data)), typ, name)
	if err != nil {
		return nil, false, err
	}
	if _, err := m.Place(sub, folderOf(kind), t0); err != nil {
		return nil, false, err
	}
	return m, false, nil
}

func (f *fakeMaterials) add(d material.Digest, size int64, typ material.Type, name string) (*material.Material, error) {
	f.n++
	m, err := material.New(material.Spec{ID: shared.MaterialID(fmt.Sprintf("m%d", f.n)), Digest: d, Size: size, Type: typ, Name: name}, t0)
	if err != nil {
		return nil, err
	}
	f.rows[m.ID()] = m
	return m, nil
}

// filed stores content as a material filed under sub, as the library would have it from before.
func (f *fakeMaterials) filed(t *testing.T, sub shared.SubjectID, content string) *material.Material {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.add(material.DigestOf([]byte(content)), int64(len(content)), material.TypeTXT, "old.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Place(sub, "folder-own", t0); err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fakeMaterials) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("fake: %w", shared.ErrNotFound)
	}
	return m, nil
}

func (f *fakeMaterials) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.kinds)
}

type fakeRecordings struct {
	mu   sync.Mutex
	rows map[shared.RecordingID]*recording.Recording
}

func (f *fakeRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return r, nil
}

func (f *fakeRecordings) purge(id shared.RecordingID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
}

type fakeLessons struct {
	mu   sync.Mutex
	rows map[shared.LessonID]*lesson.Lesson
}

func (f *fakeLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.rows[id]
	if !ok {
		return nil, shared.ErrNotFound
	}
	return l, nil
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (g *seqIDs) NewNoteID() shared.NoteID {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return shared.NoteID(fmt.Sprintf("n%d", g.n))
}

type env struct {
	notes      *fakeNotes
	materials  *fakeMaterials
	recordings *fakeRecordings
	lessons    *fakeLessons
	svc        *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		notes: newFakeNotes(), materials: newFakeMaterials(),
		recordings: &fakeRecordings{rows: map[shared.RecordingID]*recording.Recording{}},
		lessons:    &fakeLessons{rows: map[shared.LessonID]*lesson.Lesson{}},
	}
	svc, err := New(Deps{
		Notes: e.notes, Materials: e.materials, Recordings: e.recordings, Lessons: e.lessons,
		Clock: &fakeClock{now: t0}, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// recordingSpec describes a stored recording for addRecording.
type recordingSpec struct {
	id       shared.RecordingID
	status   recording.Status
	started  time.Time
	kind     string // of the snapshot
	fixed    string // the kind the student set; "" none
	deleting bool
}

func (e *env) addRecording(t *testing.T, spec recordingSpec) {
	t.Helper()
	dir, err := shared.NewArtifactRef(shared.ArtifactRecordings, string(spec.id))
	if err != nil {
		t.Fatal(err)
	}
	st := recording.State{
		Version: 1, ID: spec.id, LessonID: "l-rec", Status: spec.status, MaxDuration: 2 * time.Hour, ChunksDir: dir,
		Lesson:    values.NewLessonSnapshot(values.LessonSnapshotSpec{Title: "Механика", Subject: "Физика", Kind: spec.kind}),
		StartedAt: spec.started, CreatedAt: spec.started, UpdatedAt: spec.started,
	}
	if !spec.status.InProgress() {
		st.StoppedAt = spec.started.Add(time.Hour)
	}
	if spec.status == recording.StatusCompleted {
		st.Audio, err = shared.NewArtifactRef(shared.ArtifactRecordings, string(spec.id)+".wav")
		if err != nil {
			t.Fatal(err)
		}
	}
	if spec.fixed != "" {
		st.Correction, err = values.NewLessonCorrection("", spec.fixed)
		if err != nil {
			t.Fatal(err)
		}
	}
	if spec.deleting {
		st.DeletingAt = spec.started.Add(2 * time.Hour)
	}
	r, err := recording.Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	e.recordings.mu.Lock()
	e.recordings.rows[spec.id] = r
	e.recordings.mu.Unlock()
}

func (e *env) addLesson(t *testing.T, id shared.LessonID, start time.Time, kind lesson.Kind) *lesson.Lesson {
	t.Helper()
	l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
		Title: "Семинар по механике", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow",
		Subject: "Физика", Kind: kind,
	}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	e.lessons.mu.Lock()
	e.lessons.rows[id] = l
	e.lessons.mu.Unlock()
	return l
}

func date(t *testing.T, y int, m time.Month, d int) note.Date {
	t.Helper()
	nd, err := note.NewDate(y, m, d)
	if err != nil {
		t.Fatal(err)
	}
	return nd
}

func uploads(names ...string) []Upload {
	out := make([]Upload, 0, len(names))
	for _, n := range names {
		out = append(out, Upload{Name: n, Data: []byte("содержимое " + n)})
	}
	return out
}

func partIDs(v View) []shared.MaterialID { return slices.Clone(v.Note.Parts()) }
