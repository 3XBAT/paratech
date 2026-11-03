package agent

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"paratech/desktop/internal/cases/agentjournal"
	"paratech/desktop/internal/cases/agentturn"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/agent/services/actionpolicy"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/services/filing"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// The notes of the agent tests run on the real use case (cases/notes) over the library fake and a note store with
// the version protocol of the sqlite repository: every save bumps the version; the notes of a removed subject are
// hidden as the repository hides them (TASK-288).

type memNoteStore struct {
	mu      sync.Mutex
	order   []shared.NoteID
	rows    map[shared.NoteID]note.State
	removed map[shared.SubjectID]bool // removed subjects: their notes are not found
	saveErr error                     // returned by every save when set
}

func (s *memNoteStore) Get(_ context.Context, id shared.NoteID) (*note.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.rows[id]
	if !ok || s.removed[st.Subject] {
		return nil, fmt.Errorf("note %s: %w", id, shared.ErrNotFound)
	}
	return note.Restore(st)
}

func (s *memNoteStore) Save(_ context.Context, n *note.Note) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	st := n.State()
	cur, ok := s.rows[st.ID]
	if (st.Version == 0 && ok) || (st.Version > 0 && (!ok || cur.Version != st.Version)) {
		return shared.ErrVersionConflict
	}
	if !ok {
		s.order = append(s.order, st.ID)
	}
	st.Version++
	s.rows[st.ID] = st
	n.Persisted(st.Version)
	return nil
}

func (s *memNoteStore) list(keep func(note.State) bool) ([]*note.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*note.Note
	for _, id := range s.order {
		st := s.rows[id]
		if s.removed[st.Subject] || !keep(st) {
			continue
		}
		n, err := note.Restore(st)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *memNoteStore) ListBySubject(_ context.Context, sub shared.SubjectID) ([]*note.Note, error) {
	return s.list(func(st note.State) bool { return st.Subject == sub })
}

func (s *memNoteStore) ListByRecording(_ context.Context, rec shared.RecordingID) ([]*note.Note, error) {
	return s.list(func(st note.State) bool { return st.Anchor.Recording() == rec })
}

func (s *memNoteStore) ListByLesson(_ context.Context, l shared.LessonID) ([]*note.Note, error) {
	return s.list(func(st note.State) bool { return st.Anchor.Lesson() == l })
}

// state returns the stored note id.
func (s *memNoteStore) state(t *testing.T, id shared.NoteID) note.State {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.rows[id]
	if !ok {
		t.Fatalf("no note %s", id)
	}
	return st
}

// noteMaterials is the library of the notes over memLibrary: one material per content, a new one filed under the
// subject in the system folder of the lesson kind, as library.FileNotePart does.
type noteMaterials struct {
	lib   *memLibrary
	clock *noteClock // the time a new material is stored at, as the journal reads it
}

func (m noteMaterials) FileNotePart(_ context.Context, sub shared.SubjectID, kind, name string, data []byte) (*material.Material, bool, error) {
	if _, err := m.lib.subject(sub); err != nil {
		return nil, false, err
	}
	d := material.DigestOf(data)
	for _, x := range m.lib.materials {
		if x.Digest() == d {
			return x, true, nil
		}
	}
	m.lib.next++
	x, err := material.New(material.Spec{
		ID: shared.MaterialID(fmt.Sprintf("m-new%d", m.lib.next)), Digest: d, Size: int64(len(data)), Type: material.TypeTXT, Name: name,
	}, m.clock.Now())
	if err != nil {
		return nil, false, err
	}
	if _, err := x.Place(sub, subject.SystemFolderID(sub, filing.NoteFolder(kind)), now); err != nil {
		return nil, false, err
	}
	x.Persisted(1)
	m.lib.materials[x.ID()] = x
	return x, false, nil
}

func (m noteMaterials) Material(_ context.Context, id shared.MaterialID) (*material.Material, error) {
	if x, ok := m.lib.materials[id]; ok {
		return x, nil
	}
	return nil, fmt.Errorf("material %s: %w", id, shared.ErrNotFound)
}

func (m noteMaterials) Place(ctx context.Context, id shared.MaterialID, sub shared.SubjectID, folder shared.FolderID) (*material.Material, error) {
	return m.lib.Place(ctx, id, sub, folder)
}

func (m noteMaterials) MoveToInbox(ctx context.Context, id shared.MaterialID) (*material.Material, error) {
	return m.lib.MoveToInbox(ctx, id)
}

func (noteMaterials) PageImage(_ context.Context, id shared.MaterialID) (*material.Material, []byte, error) {
	return nil, nil, fmt.Errorf("material %s: %w", id, shared.ErrNotFound)
}

func (noteMaterials) MaterialText(_ context.Context, id shared.MaterialID) (*material.Material, material.Text, error) {
	return nil, material.Text{}, fmt.Errorf("material %s: %w", id, material.ErrTextNotExtracted)
}

type noRecordings struct{}

func (noRecordings) Get(_ context.Context, id shared.RecordingID) (*recording.Recording, error) {
	return nil, fmt.Errorf("recording %s: %w", id, shared.ErrNotFound)
}

type noteLessons map[shared.LessonID]*lesson.Lesson

func (l noteLessons) Get(_ context.Context, id shared.LessonID) (*lesson.Lesson, error) {
	if x, ok := l[id]; ok {
		return x, nil
	}
	return nil, fmt.Errorf("lesson %s: %w", id, shared.ErrNotFound)
}

type seqNoteIDs struct{ n int }

func (g *seqNoteIDs) NewNoteID() shared.NoteID {
	g.n++
	return shared.NoteID(fmt.Sprintf("n%d", g.n))
}

// noteClock moves on by a minute every read, so a note made later is made after an earlier journal entry.
type noteClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *noteClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(time.Minute)
	return c.at
}

// notesWorld is newWave10World with «Мой конспект» wired: the subject s1 «Физика», the lesson l1 of «Физика» on
// 2026-10-20, l2 of a subject the library does not know, the uploads up1 and up2 in «Входящие», and the note n1 of s1 on
// the day 2026-10-15 with the part m1.
type notesWorld struct {
	*world
	noteStore *memNoteStore
	notes     *notes.Service
	log       *bytes.Buffer
}

func newNotesWorld(t *testing.T) *notesWorld {
	t.Helper()
	w, _ := newWave10World(t)
	nw := &notesWorld{world: w, noteStore: &memNoteStore{rows: map[shared.NoteID]note.State{}, removed: map[shared.SubjectID]bool{}}, log: &bytes.Buffer{}}
	log := slog.New(slog.NewTextHandler(nw.log, &slog.HandlerOptions{Level: slog.LevelDebug}))

	lessons := noteLessons{}
	for id, subj := range map[shared.LessonID]string{"l1": "Физика", "l2": "Астрология"} {
		start := time.Date(2026, 10, 20, 10, 0, 0, 0, moscow)
		l, err := lesson.New(lesson.Spec{ID: id, Content: lesson.Content{
			Title: "Механика", Start: start, End: start.Add(90 * time.Minute), TimeZone: "Europe/Moscow", Subject: subj, Kind: lesson.KindLecture,
		}}, now)
		if err != nil {
			t.Fatal(err)
		}
		lessons[id] = l
	}
	for _, id := range []shared.MaterialID{"up1", "up2"} {
		m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(id)), Size: 3, Type: material.TypeTXT, Name: "скан.txt"}, now)
		if err != nil {
			t.Fatal(err)
		}
		m.Persisted(1)
		w.lib.materials[id] = m
	}
	// m1 is filed under s1, the part of n1.
	if _, err := w.lib.Place(t.Context(), "m1", "s1", ""); err != nil {
		t.Fatal(err)
	}

	clock := &noteClock{at: now}
	var err error
	nw.notes, err = notes.New(notes.Deps{
		Notes: nw.noteStore, Materials: noteMaterials{lib: w.lib, clock: clock}, Recordings: noRecordings{}, Lessons: lessons, Clock: clock,
		IDs: &seqNoteIDs{n: 1}, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	day, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorDay, Date: mustNoteDate(t, "2026-10-15")})
	if err != nil {
		t.Fatal(err)
	}
	n1, err := note.New(note.Spec{ID: "n1", Subject: "s1", Anchor: day, Title: "Моя лекция", Parts: []shared.MaterialID{"m1"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := nw.noteStore.Save(t.Context(), n1); err != nil {
		t.Fatal(err)
	}

	ch := newMemChanges(w.assignments)
	if w.actions, err = NewActions(ActionDeps{
		Library: w.lib, Lessons: w.lessons, Assignments: w.assignments, NewAssignments: w.added, Rerunner: w.jobs,
		Jobs: w.activity, Canceller: w.jobs, Tx: w.tx, Clock: fixedClock{}, Log: log, Changes: ch, Marks: ch, Events: ch,
		Notes: nw.notes,
	}); err != nil {
		t.Fatal(err)
	}
	if w.journal, err = agentjournal.New(agentjournal.Deps{
		Actions: w.store, Compensator: w.actions, Resolver: w.actions, Clock: clock, IDs: &seqActionIDs{}, Log: log,
	}); err != nil {
		t.Fatal(err)
	}
	w.actions.chat = w.journal
	return nw
}

// tools are the Driver's tools with the world's logger.
func (nw *notesWorld) tools(t *testing.T) tools {
	t.Helper()
	d := nw.deps(&scriptedTurns{}, &chanSink{})
	d.Log = slog.New(slog.NewTextHandler(nw.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	drv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return drv.tools
}

// run runs an admitted call of catalog v10 as request st; it returns what it gave and its journal entry (zero: none).
func (nw *notesWorld) run(t *testing.T, st agentturn.Step, id, args string) (ran, agentjournal.Entry) {
	t.Helper()
	c := call10(t, id, values.ToolNote, args, actionpolicy.ClassU)
	r := nw.tools(t).run(t.Context(), st, c)
	e, err := nw.journal.Find(t.Context(), st.Thread, st.Turn, id)
	if err != nil {
		e = agentjournal.Entry{}
	}
	return r, e
}

// inScope is request req with the subject chip sub.
func inScope(t *testing.T, sub string) agentturn.Step {
	t.Helper()
	sc, err := values.NewScope(mustRef(t, values.KindSubject, sub), "", values.Ref{}, values.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	st := req
	st.Scope = sc
	return st
}

func mustNoteDate(t *testing.T, s string) note.Date {
	t.Helper()
	d, err := note.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// removeSubject removes s from the library as «Удалить предмет» does: it and its notes are no longer found.
func (nw *notesWorld) removeSubject(id shared.SubjectID) {
	delete(nw.lib.subjects, id)
	nw.noteStore.mu.Lock()
	nw.noteStore.removed[id] = true
	nw.noteStore.mu.Unlock()
}
