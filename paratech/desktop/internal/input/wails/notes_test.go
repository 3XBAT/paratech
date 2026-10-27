package wails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// fakeNotesUC records the requests and answers with one note whose parts are the materials it was given.
type fakeNotesUC struct {
	t        testing.TB
	adds     []notes.AddRequest
	pastes   []notes.PasteRequest
	calls    []string
	addErrs  map[string]error // by upload name
	err      error            // returned by every call when set
	views    []notes.View
	detached bool // the view reads as the day of a gone recording
}

func testNote(t testing.TB, id shared.NoteID, kind note.AnchorKind, parts ...shared.MaterialID) *note.Note {
	t.Helper()
	spec := note.AnchorSpec{Kind: kind}
	switch kind {
	case note.AnchorRecording:
		spec.Recording = "r1"
		spec.Date, _ = note.NewDate(2026, 9, 12)
		spec.LessonKind, spec.Title = "lecture", "Механика"
	case note.AnchorDay:
		spec.Date, _ = note.NewDate(2026, 9, 12)
	}
	a, err := note.NewAnchor(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) == 0 {
		parts = []shared.MaterialID{"m1"}
	}
	n, err := note.New(note.Spec{ID: id, Subject: "s1", Anchor: a, Title: "Конспект", Parts: parts}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func testPart(t testing.TB, id shared.MaterialID, name string) *material.Material {
	t.Helper()
	m, err := material.New(material.Spec{ID: id, Digest: material.DigestOf([]byte(id)), Size: 1, Type: material.TypeTXT, Name: name}, libT0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fakeNotesUC) viewOf(n *note.Note) notes.View {
	v := notes.View{Note: n, Anchor: n.Anchor()}
	if f.detached {
		v.Anchor = n.Anchor().FallBackToDay()
	}
	for _, id := range n.Parts() {
		v.Parts = append(v.Parts, testPart(f.t, id, string(id)+".txt"))
	}
	return v
}

func (f *fakeNotesUC) Add(_ context.Context, req notes.AddRequest) (notes.Added, error) {
	f.adds = append(f.adds, req)
	if f.err != nil {
		return notes.Added{}, f.err
	}
	up := req.Uploads[0]
	if err := f.addErrs[up.Name]; err != nil {
		return notes.Added{Parts: []notes.PartResult{{Name: up.Name, Err: err}}}, err
	}
	id := shared.MaterialID(fmt.Sprintf("m%d", len(f.adds)))
	n := testNote(f.t, cmpNote(req.Note), note.AnchorRecording, id)
	return notes.Added{
		View: f.viewOf(n), Created: req.Note == "",
		Parts: []notes.PartResult{{Name: up.Name, Material: testPart(f.t, id, up.Name), AlreadyAdded: up.Name == "dup.txt"}},
	}, nil
}

func cmpNote(id shared.NoteID) shared.NoteID {
	if id == "" {
		return "n1"
	}
	return id
}

func (f *fakeNotesUC) Paste(_ context.Context, req notes.PasteRequest) (notes.Added, error) {
	f.pastes = append(f.pastes, req)
	if f.err != nil {
		return notes.Added{}, f.err
	}
	n := testNote(f.t, "n1", note.AnchorUndated)
	return notes.Added{View: f.viewOf(n), Created: true, Parts: []notes.PartResult{{Name: "Текст.txt", Material: testPart(f.t, "m1", "Текст.txt")}}}, nil
}

func (f *fakeNotesUC) replaced(call string, old, next shared.MaterialID) (notes.Replaced, error) {
	f.calls = append(f.calls, call)
	if f.err != nil {
		return notes.Replaced{}, f.err
	}
	n := testNote(f.t, "n1", note.AnchorUndated, next)
	return notes.Replaced{View: f.viewOf(n), Material: testPart(f.t, next, "new.txt"), Previous: old}, nil
}

func (f *fakeNotesUC) ReplacePart(_ context.Context, id shared.NoteID, old shared.MaterialID, up notes.Upload) (notes.Replaced, error) {
	return f.replaced(fmt.Sprintf("ReplacePart %s %s %s %q", id, old, up.Name, up.Data), old, "m9")
}

func (f *fakeNotesUC) ReplacePartWith(_ context.Context, id shared.NoteID, old, next shared.MaterialID) (notes.Replaced, error) {
	return f.replaced(fmt.Sprintf("ReplacePartWith %s %s %s", id, old, next), old, next)
}

func (f *fakeNotesUC) one(call string) (notes.View, error) {
	f.calls = append(f.calls, call)
	if f.err != nil {
		return notes.View{}, f.err
	}
	return f.viewOf(testNote(f.t, "n1", note.AnchorRecording)), nil
}

func (f *fakeNotesUC) Reorder(_ context.Context, id shared.NoteID, order []shared.MaterialID) (notes.View, error) {
	return f.one(fmt.Sprintf("Reorder %s %v", id, order))
}

func (f *fakeNotesUC) Reanchor(_ context.Context, id shared.NoteID, ref notes.AnchorRef) (notes.View, error) {
	return f.one(fmt.Sprintf("Reanchor %s %s %s", id, ref.Kind, ref.Date))
}

func (f *fakeNotesUC) Retitle(_ context.Context, id shared.NoteID, title string) (notes.View, error) {
	return f.one(fmt.Sprintf("Retitle %s %s", id, title))
}

func (f *fakeNotesUC) Remove(_ context.Context, id shared.NoteID) (notes.View, error) {
	return f.one("Remove " + string(id))
}

func (f *fakeNotesUC) Restore(_ context.Context, id shared.NoteID) (notes.View, error) {
	return f.one("Restore " + string(id))
}

func (f *fakeNotesUC) ForLesson(_ context.Context, l notes.Lecture) ([]notes.View, error) {
	f.calls = append(f.calls, fmt.Sprintf("ForLesson %q %q %q %q", l.Recording, l.Lesson, l.Subject, l.Date))
	return f.views, f.err
}

func (f *fakeNotesUC) ForSubject(_ context.Context, sub shared.SubjectID) ([]notes.View, error) {
	f.calls = append(f.calls, "ForSubject "+string(sub))
	return f.views, f.err
}

type fakeNotePicker struct {
	paths    []string
	err      error
	multiple []bool
}

func (p *fakeNotePicker) PickNoteFiles(_ context.Context, multiple bool) ([]string, error) {
	p.multiple = append(p.multiple, multiple)
	return p.paths, p.err
}

func newNotesBinding(t *testing.T, files map[string][]byte, picked ...string) (*NotesService, *fakeNotesUC, *fakeNotePicker, *bytes.Buffer) {
	t.Helper()
	uc := &fakeNotesUC{t: t, addErrs: map[string]error{}}
	picker := &fakeNotePicker{paths: picked}
	logs := &bytes.Buffer{}
	return NewNotesService(uc, picker, &memFiles{files: files}, slog.New(slog.NewTextHandler(logs, nil))), uc, picker, logs
}

// assertNoNotePath fails if the JSON of v has any part of the folder of the files.
func assertNoNotePath(t *testing.T, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"Users", "student", "Документы", `\\`, ":/"} {
		if strings.Contains(string(b), p) {
			t.Fatalf("payload %s has %q of the path", b, p)
		}
	}
}

var recTarget = NoteTarget{SubjectID: "s1", Kind: "recording", RecordingID: "r1"}

func TestNotesAddFiles(t *testing.T) {
	ctx := context.Background()
	a, b := dropDir+`\Лекция 1.docx`, dropDir+`\dup.txt`
	s, uc, picker, logs := newNotesBinding(t, map[string][]byte{a: []byte("A"), b: []byte("B")}, a, b)

	out, err := s.AddFiles(ctx, recTarget)
	if err != nil || out.Cancelled || !out.Created || out.Note == nil || len(out.Files) != 2 {
		t.Fatalf("AddFiles = %+v, %v", out, err)
	}
	if !reflect.DeepEqual(picker.multiple, []bool{true}) {
		t.Fatalf("picker asked with %v, want several files", picker.multiple)
	}
	// One file per call; the second goes to the note the first made.
	if len(uc.adds) != 2 || uc.adds[0].Note != "" || uc.adds[1].Note != "n1" || uc.adds[0].Subject != "s1" ||
		uc.adds[0].Anchor.Kind != note.AnchorRecording || uc.adds[0].Anchor.Recording != "r1" ||
		uc.adds[0].Uploads[0].Name != "Лекция 1.docx" || string(uc.adds[1].Uploads[0].Data) != "B" {
		t.Fatalf("requests %+v", uc.adds)
	}
	if out.Files[0].Name != "Лекция 1.docx" || out.Files[0].MaterialID != "m1" || !out.Files[1].AlreadyAdded {
		t.Fatalf("files %+v", out.Files)
	}
	if n := out.Note; n.ID != "n1" || n.Anchor.Kind != "recording" || n.Anchor.Date != "2026-09-12" || n.Anchor.Detached ||
		len(n.Parts) != 1 || n.Parts[0].Name != "m2.txt" {
		t.Fatalf("note %+v", n)
	}
	assertNoNotePath(t, out)
	if strings.Contains(logs.String(), "student") || strings.Contains(logs.String(), "Лекция") {
		t.Fatalf("the log has the path or the file name: %s", logs.String())
	}

	// A file refused on its own is reported; the next one is still added.
	s, uc, _, _ = newNotesBinding(t, map[string][]byte{a: []byte("A"), b: []byte("B")}, b, a)
	uc.addErrs["dup.txt"] = fmt.Errorf("w: %w", library.ErrUnsupportedFile)
	out, err = s.AddFiles(ctx, recTarget)
	if err != nil || out.Files[0].Error != ErrLibraryUnsupported.Error() || out.Files[1].Error != "" || out.Note == nil {
		t.Fatalf("AddFiles(one refused) = %+v, %v", out, err)
	}

	// A refusal of the lecture ends the call.
	s, uc, _, _ = newNotesBinding(t, map[string][]byte{a: []byte("A"), b: []byte("B")}, a, b)
	uc.err = fmt.Errorf("w: %w", notes.ErrRecordingUnavailable)
	if _, err := s.AddFiles(ctx, recTarget); !errors.Is(err, ErrNotesRecording) || len(uc.adds) != 1 {
		t.Fatalf("AddFiles(recording not ready) = %v after %d calls", err, len(uc.adds))
	}

	// An unexpected error after a file was added keeps that file's result; the error is the failing file's, the call
	// stops there.
	c := dropDir + `\c.txt`
	for _, tt := range []struct {
		name string
		err  error
		want libraryError
	}{
		{"unexpected", errors.New("disk at C:/Users/student"), ErrNotesUnavailable},
		{"busy", fmt.Errorf("w: %w", notes.ErrBusy), ErrLibraryBusy},
	} {
		s, uc, _, _ = newNotesBinding(t, map[string][]byte{a: []byte("A"), b: []byte("B"), c: []byte("C")}, a, b, c)
		uc.addErrs["dup.txt"] = tt.err
		out, err = s.AddFiles(ctx, recTarget)
		if err != nil || out.Note == nil || out.Note.ID != "n1" || !out.Created || len(out.Files) != 2 || len(uc.adds) != 2 ||
			out.Files[0].MaterialID != "m1" || out.Files[1].Name != "dup.txt" || out.Files[1].Error != tt.want.Error() {
			t.Fatalf("%s: AddFiles(fails after one) = %+v, %v after %d calls", tt.name, out, err, len(uc.adds))
		}
		assertNoNotePath(t, out)
	}

	// An unreadable file.
	s, _, _, _ = newNotesBinding(t, map[string][]byte{}, a)
	if out, err := s.AddFiles(ctx, recTarget); err != nil || out.Files[0].Error != ErrLibraryFileUnreadable.Error() || out.Note != nil {
		t.Fatalf("AddFiles(unreadable) = %+v, %v", out, err)
	}
}

func TestNotesAddFilesDialog(t *testing.T) {
	ctx := context.Background()
	s, uc, _, _ := newNotesBinding(t, nil)
	if out, err := s.AddFiles(ctx, recTarget); err != nil || !out.Cancelled || len(uc.adds) != 0 {
		t.Fatalf("AddFiles(cancelled) = %+v, %v", out, err)
	}
	s, _, picker, _ := newNotesBinding(t, nil)
	picker.err = errors.New("no window")
	if _, err := s.AddFiles(ctx, recTarget); !errors.Is(err, ErrLibraryFileUnreadable) {
		t.Fatalf("AddFiles(dialog failed) = %v", err)
	}
	s, _, picker, _ = newNotesBinding(t, nil)
	if _, err := s.AddFiles(ctx, NoteTarget{SubjectID: "s1", Kind: "day", Date: "12.09.2026"}); !errors.Is(err, ErrNotesInvalidDate) ||
		len(picker.multiple) != 0 {
		t.Fatalf("AddFiles(bad date) = %v; dialog shown %v", err, picker.multiple)
	}
	s, _, picker, _ = newNotesBinding(t, nil)
	picker.paths = make([]string, note.MaxParts+1)
	if _, err := s.AddFiles(ctx, recTarget); !errors.Is(err, ErrNotesTooManyPicked) {
		t.Fatalf("AddFiles(too many) = %v", err)
	}
}

func TestNotesCalls(t *testing.T) {
	ctx := context.Background()
	path := dropDir + `\новый.txt`
	s, uc, picker, _ := newNotesBinding(t, map[string][]byte{path: []byte("новое")}, path)

	out, err := s.Paste(ctx, NoteTarget{SubjectID: "s1", Kind: "day", Date: "2026-09-12"}, "текст")
	if err != nil || !out.Created || out.Note == nil || out.Files[0].MaterialID != "m1" {
		t.Fatalf("Paste = %+v, %v", out, err)
	}
	if p := uc.pastes[0]; p.Subject != "s1" || p.Anchor.Kind != note.AnchorDay || p.Anchor.Date.String() != "2026-09-12" || p.Text != "текст" {
		t.Fatalf("paste request %+v", p)
	}

	rep, err := s.ReplaceFile(ctx, "n1", "m1")
	if err != nil || rep.MaterialID != "m9" || rep.PreviousID != "m1" || rep.Note == nil {
		t.Fatalf("ReplaceFile = %+v, %v", rep, err)
	}
	if !reflect.DeepEqual(picker.multiple, []bool{false}) {
		t.Fatalf("picker asked with %v, want one file", picker.multiple)
	}
	assertNoNotePath(t, rep)
	if _, err := s.UndoReplace(ctx, "n1", "m9", "m1"); err != nil {
		t.Fatal(err)
	}
	picker.paths = nil
	if rep, err := s.ReplaceFile(ctx, "n1", "m1"); err != nil || !rep.Cancelled {
		t.Fatalf("ReplaceFile(cancelled) = %+v, %v", rep, err)
	}

	views := []func() (NoteView, error){
		func() (NoteView, error) { return s.Reorder(ctx, "n1", []string{"m2", "m1"}) },
		func() (NoteView, error) {
			return s.Reanchor(ctx, "n1", NoteTarget{Kind: "day", Date: "2026-09-01"})
		},
		func() (NoteView, error) { return s.Retitle(ctx, "n1", "Ряды") },
		func() (NoteView, error) { return s.Remove(ctx, "n1") },
		func() (NoteView, error) { return s.Restore(ctx, "n1") },
	}
	for _, c := range views {
		if v, err := c(); err != nil || v.ID != "n1" {
			t.Fatalf("call = %+v, %v", v, err)
		}
	}
	if _, err := s.Reanchor(ctx, "n1", NoteTarget{Kind: "day", Date: "2026-13-01"}); !errors.Is(err, ErrNotesInvalidDate) {
		t.Fatalf("Reanchor(bad date) = %v", err)
	}

	uc.views = []notes.View{uc.viewOf(testNote(t, "n1", note.AnchorRecording))}
	uc.detached = true
	uc.views = append(uc.views, uc.viewOf(testNote(t, "n2", note.AnchorRecording)))
	list, err := s.ForLesson(ctx, NoteLecture{SubjectID: "s1", Date: "2026-09-12"})
	if err != nil || len(list) != 2 || list[0].Anchor.Detached || !list[1].Anchor.Detached || list[1].Anchor.Kind != "day" ||
		list[1].Anchor.RecordingID != "" || list[1].Anchor.Date != "2026-09-12" {
		t.Fatalf("ForLesson = %+v, %v", list, err)
	}
	if _, err := s.ForLesson(ctx, NoteLecture{SubjectID: "s1", Date: "вчера"}); !errors.Is(err, ErrNotesInvalidDate) {
		t.Fatalf("ForLesson(bad date) = %v", err)
	}
	if list, err := s.ForSubject(ctx, "s1"); err != nil || len(list) != 2 {
		t.Fatalf("ForSubject = %+v, %v", list, err)
	}

	want := []string{
		`ReplacePart n1 m1 новый.txt "новое"`, "ReplacePartWith n1 m9 m1", "Reorder n1 [m2 m1]", "Reanchor n1 day 2026-09-01",
		"Retitle n1 Ряды", "Remove n1", "Restore n1", `ForLesson "" "" "s1" "2026-09-12"`, "ForSubject s1",
	}
	if !reflect.DeepEqual(uc.calls, want) {
		t.Fatalf("calls = %q", uc.calls)
	}
}

func TestNotesErrors(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{notes.ErrInvalidRequest, ErrNotesInvalidID},
		{notes.ErrNoteNotFound, ErrNotesNotFound},
		{notes.ErrRecordingUnavailable, ErrNotesRecording},
		{notes.ErrLessonNotFound, ErrLibraryLessonNotFound},
		{notes.ErrOtherSubject, ErrNotesOtherSubject},
		{notes.ErrMaterialNotFound, ErrLibraryFileNotFound},
		{notes.ErrMaterialRemoved, ErrLibraryFileRemoved},
		{notes.ErrRemoved, ErrNotesRemoved},
		{notes.ErrEmptyTitle, ErrNotesEmptyTitle},
		{notes.ErrNoFiles, ErrNotesNoFiles},
		{notes.ErrTooManyParts, ErrNotesTooManyParts},
		{notes.ErrAlreadyAdded, ErrNotesAlreadyAdded},
		{notes.ErrPartNotFound, ErrNotesPartNotFound},
		{notes.ErrInvalidOrder, ErrNotesInvalidOrder},
		{notes.ErrEmptyText, ErrNotesEmptyText},
		{notes.ErrTextTooLarge, ErrNotesTextTooLarge},
		{notes.ErrInvalidText, ErrNotesInvalidText},
		{notes.ErrChanged, ErrNotesChanged},
		{notes.ErrBusy, ErrLibraryBusy},
		{library.ErrSubjectNotFound, ErrLibrarySubjectNotFound},
		{library.ErrEmptyFile, ErrLibraryEmptyFile},
		{library.ErrFileTooLarge, ErrLibraryFileTooLarge},
		{library.ErrUnsupportedFile, ErrLibraryUnsupported},
		{library.ErrBusy, ErrLibraryBusy},
		{fmt.Errorf("%w: disk", notes.ErrUnavailable), ErrNotesUnavailable},
		{errors.New("other"), ErrNotesUnavailable},
	}
	ctx := context.Background()
	for _, tt := range tests {
		s, uc, _, _ := newNotesBinding(t, nil)
		uc.err = fmt.Errorf("wrapped: %w", tt.err)
		checks := map[string]error{}
		_, checks["Paste"] = s.Paste(ctx, recTarget, "x")
		_, checks["Retitle"] = s.Retitle(ctx, "n1", "x")
		_, checks["UndoReplace"] = s.UndoReplace(ctx, "n1", "m1", "m2")
		_, checks["ForSubject"] = s.ForSubject(ctx, "s1")
		for name, err := range checks {
			if !errors.Is(err, tt.want) {
				t.Errorf("%s with %v = %v, want %v", name, tt.err, err, tt.want)
			}
		}
	}
}

// notesDropped returns the one NotesAdd emitted.
func notesDropped(t *testing.T, em *fakeEmitter) NotesAdd {
	t.Helper()
	if len(em.events) != 1 || em.events[0].name != NotesDropEvent || len(em.events[0].data) != 1 {
		t.Fatalf("events = %+v, want one %s", em.events, NotesDropEvent)
	}
	d, ok := em.events[0].data[0].(NotesAdd)
	if !ok {
		t.Fatalf("payload %T, want NotesAdd", em.events[0].data[0])
	}
	return d
}

func TestNotesDrop(t *testing.T) {
	ctx := context.Background()
	good := dropDir + `\Конспект.TXT`
	attrs := map[string]string{
		"data-file-drop-target": "", dropNoteKind: "recording", dropNoteSubject: "s1", dropNoteRecording: "r1",
	}

	lib, luc, _, _ := newDropBinding(t, map[string][]byte{good: []byte("A")})
	nts, uc, _, _ := newNotesBinding(t, map[string][]byte{good: []byte("A")})
	em := &fakeEmitter{}
	routeDrop(ctx, lib, nts, attrs, []string{good, `\\server\share\a.txt`, dropDir + `\x.exe`}, em)

	d := notesDropped(t, em)
	if d.Error != "" || len(d.Files) != 3 || d.Note == nil || d.Target.RecordingID != "r1" {
		t.Fatalf("drop = %+v", d)
	}
	if d.Files[0].Name != "Конспект.TXT" || d.Files[0].Error != "" ||
		d.Files[1].Name != "" || d.Files[1].Error != ErrLibraryDroppedPath.Error() ||
		d.Files[2].Name != "x.exe" || d.Files[2].Error != ErrLibraryUnsupported.Error() {
		t.Fatalf("files %+v", d.Files)
	}
	if len(uc.adds) != 1 || uc.adds[0].Anchor.Recording != "r1" || luc.upload.Data != nil {
		t.Fatalf("notes calls %d, library upload %q: the drop went to the wrong place", len(uc.adds), luc.upload.Data)
	}
	assertNoNotePath(t, d)

	// Not a lecture: «Входящие».
	em = &fakeEmitter{}
	routeDrop(ctx, lib, nts, map[string]string{"data-file-drop-target": ""}, []string{good}, em)
	if len(em.events) != 1 || em.events[0].name != LibraryDropEvent {
		t.Fatalf("events %+v, want the library's", em.events)
	}
	em = &fakeEmitter{}
	routeDrop(ctx, lib, nts, nil, []string{good}, em)
	if len(em.events) != 1 || em.events[0].name != LibraryDropEvent {
		t.Fatalf("events %+v without a target, want the library's", em.events)
	}

	// Too many files: refused whole, nothing read.
	em = &fakeEmitter{}
	nts, uc, _, _ = newNotesBinding(t, nil)
	nts.addDropped(ctx, recTarget, make([]string, maxDroppedFiles+1), em)
	if d := notesDropped(t, em); d.Error != ErrLibraryTooManyDropped.Error() || len(uc.adds) != 0 {
		t.Fatalf("drop of too many = %+v", d)
	}

	// A refused lecture: the drop's error.
	em = &fakeEmitter{}
	nts, uc, _, _ = newNotesBinding(t, map[string][]byte{good: []byte("A")})
	uc.err = notes.ErrLessonNotFound
	nts.addDropped(ctx, NoteTarget{SubjectID: "s1", Kind: "lesson", LessonID: "l1"}, []string{good}, em)
	if d := notesDropped(t, em); d.Error != ErrLibraryLessonNotFound.Error() {
		t.Fatalf("drop on a gone lesson = %+v", d)
	}

	// Nothing dropped, or the app shutting down: no event.
	em = &fakeEmitter{}
	nts.addDropped(ctx, recTarget, nil, em)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	nts.addDropped(cancelled, recTarget, []string{good}, em)
	if len(em.events) != 0 {
		t.Fatalf("events %+v", em.events)
	}
}

func TestNoteDropTarget(t *testing.T) {
	if _, ok := noteDropTarget(nil); ok {
		t.Fatal("no attributes is a lecture")
	}
	got, ok := noteDropTarget(map[string]string{dropNoteID: "n1"})
	if !ok || got != (NoteTarget{NoteID: "n1"}) {
		t.Fatalf("target %+v, %v", got, ok)
	}
	got, ok = noteDropTarget(map[string]string{dropNoteKind: "day", dropNoteSubject: "s1", dropNoteDate: "2026-09-12", "class": "x"})
	if !ok || got != (NoteTarget{Kind: "day", SubjectID: "s1", Date: "2026-09-12"}) {
		t.Fatalf("target %+v, %v", got, ok)
	}
}

func TestPickedPaths(t *testing.T) {
	if got := pickedPaths([]string{"", ".", `C:\a.txt`}); !reflect.DeepEqual(got, []string{`C:\a.txt`}) {
		t.Fatalf("pickedPaths = %q", got)
	}
}

// TestFrontendKnowsNotesErrors keeps the error texts of frontend/src/bindings/notes.ts equal to the binding's.
func TestFrontendKnowsNotesErrors(t *testing.T) {
	src, err := os.ReadFile("../../../frontend/src/bindings/notes.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []libraryError{
		ErrNotesInvalidID, ErrNotesInvalidDate, ErrNotesNotFound, ErrNotesRecording, ErrNotesOtherSubject, ErrNotesRemoved,
		ErrNotesEmptyTitle, ErrNotesNoFiles, ErrNotesTooManyParts, ErrNotesAlreadyAdded, ErrNotesPartNotFound,
		ErrNotesInvalidOrder, ErrNotesEmptyText, ErrNotesTextTooLarge, ErrNotesInvalidText, ErrNotesChanged,
		ErrNotesUnavailable, ErrNotesTooManyPicked, ErrLibraryLessonNotFound, ErrLibraryFileNotFound, ErrLibraryFileRemoved,
		ErrLibraryBusy, ErrLibrarySubjectNotFound, ErrLibraryEmptyFile, ErrLibraryFileTooLarge, ErrLibraryUnsupported,
		ErrLibraryFileUnreadable, ErrLibraryDroppedPath, ErrLibraryTooManyDropped,
	} {
		if !strings.Contains(string(src), `"`+e.Error()+`"`) {
			t.Errorf("frontend/src/bindings/notes.ts does not list the error %q", e)
		}
	}
	lib, err := os.ReadFile("../../../frontend/src/bindings/library.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lib), `"`+ErrLibraryHasNotes.Error()+`"`) {
		t.Errorf("frontend/src/bindings/library.ts does not list the error %q", ErrLibraryHasNotes)
	}
}
