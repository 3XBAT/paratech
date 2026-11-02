package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// newTestNote is an undated note of sub with parts.
func newTestNote(t *testing.T, id shared.NoteID, sub shared.SubjectID, parts ...shared.MaterialID) *note.Note {
	t.Helper()
	a, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorUndated, Title: "Лекция без даты"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := note.New(note.Spec{ID: id, Subject: sub, Anchor: a, Title: "Конспект", Parts: parts}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// discardFixture is a subject with a note of two parts, a removed note, two more files in «Материалы» (one removed)
// and one in an own folder.
type discardFixture struct {
	e      *env
	sub    *subject.Subject
	nested shared.MaterialID // the file in the own folder
}

func newDiscardFixture(t *testing.T) discardFixture {
	t.Helper()
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика", "Физ")
	folder, err := e.svc.CreateFolder(ctx, phys.ID(), "Своя папка")
	if err != nil {
		t.Fatal(err)
	}
	place := func(text string, f shared.FolderID) shared.MaterialID {
		m, err := e.svc.Place(ctx, importText(t, e, text).ID(), phys.ID(), f)
		if err != nil {
			t.Fatal(err)
		}
		return m.ID()
	}
	part1, part2 := place("часть 1", ""), place("часть 2", "")
	place("файл", "")
	nested := place("во вложенной папке", folder.ID())
	if _, err := e.svc.RemoveMaterial(ctx, place("удалённый файл", "")); err != nil {
		t.Fatal(err)
	}

	gone := newTestNote(t, "n2", phys.ID(), part1)
	if _, err := gone.Remove(t0); err != nil {
		t.Fatal(err)
	}
	withNotes(t, e, &fakeNotes{list: map[shared.SubjectID][]*note.Note{
		phys.ID(): {newTestNote(t, "n1", phys.ID(), part1, part2), gone},
	}})

	sub, err := e.svc.subject(ctx, phys.ID())
	if err != nil {
		t.Fatal(err)
	}
	return discardFixture{e: e, sub: sub, nested: nested}
}

func TestDiscardSubjectHidesEverythingAndRestoreBringsItBack(t *testing.T) {
	f, ctx := newDiscardFixture(t), context.Background()
	e, id := f.e, f.sub.ID()
	before, err := e.svc.Materials(ctx, id)
	if err != nil || len(before) != 5 {
		t.Fatalf("Materials before = %d, %v", len(before), err)
	}

	e.clock.now = t0.Add(time.Hour)
	got, err := e.svc.DiscardSubject(ctx, id, f.sub.Version())
	if err != nil {
		t.Fatal(err)
	}
	// One active note; the files are the active ones not in it: «файл» and the one in the own folder (a part of the
	// removed note counts as a file, the removed file does not).
	if got.Notes != 1 || got.Files != 2 || got.Subject.Active() || got.Subject.Version() != f.sub.Version()+1 {
		t.Fatalf("DiscardSubject = notes %d, files %d, subject %v v%d", got.Notes, got.Files, got.Subject.Status(),
			got.Subject.Version())
	}

	// Hidden: the subject, its files (also the one in the own folder) and its name in the matching.
	if list, err := e.svc.Subjects(ctx); err != nil || len(list) != 0 {
		t.Fatalf("Subjects after discard = %v, %v", list, err)
	}
	if list, err := e.svc.Materials(ctx, id); err != nil || len(list) != 0 {
		t.Fatalf("Materials after discard = %d, %v", len(list), err)
	}
	if _, err := e.svc.Material(ctx, f.nested); !errors.Is(err, ErrMaterialNotFound) {
		t.Fatalf("Material(nested) after discard = %v, want ErrMaterialNotFound", err)
	}
	if res, err := e.svc.ResolveSubjects(ctx, []string{"Физика", "физ"}); err != nil || len(res) != 0 {
		t.Fatalf("ResolveSubjects after discard = %v, %v", res, err)
	}
	if _, err := e.svc.CreateFolder(ctx, id, "Ещё"); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("CreateFolder in a removed subject = %v, want ErrSubjectNotFound", err)
	}
	if _, err := e.svc.Place(ctx, importText(t, e, "новый").ID(), id, ""); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("Place into a removed subject = %v, want ErrSubjectNotFound", err)
	}
	if _, err := e.svc.DiscardSubject(ctx, id, got.Subject.Version()); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("DiscardSubject again = %v, want ErrSubjectNotFound", err)
	}

	if _, err := e.svc.RestoreSubject(ctx, id, f.sub.Version()); !errors.Is(err, ErrSubjectChanged) {
		t.Fatalf("RestoreSubject(old version) = %v, want ErrSubjectChanged", err)
	}
	e.clock.now = t0.Add(time.Hour + subject.RestoreWindow - time.Minute)
	back, err := e.svc.RestoreSubject(ctx, id, got.Subject.Version())
	if err != nil || !back.Active() {
		t.Fatalf("RestoreSubject = %v, %v", back, err)
	}
	after, err := e.svc.Materials(ctx, id)
	if err != nil || len(after) != len(before) {
		t.Fatalf("Materials after restore = %d, %v; want %d", len(after), err, len(before))
	}
	m, err := e.svc.Material(ctx, f.nested)
	if err != nil || m.Subject() != id {
		t.Fatalf("Material(nested) after restore = %v, %v", m, err)
	}
	if res, err := e.svc.ResolveSubjects(ctx, []string{"физ"}); err != nil || res["физ"].ID != id {
		t.Fatalf("ResolveSubjects after restore = %v, %v", res, err)
	}
	if again, err := e.svc.RestoreSubject(ctx, id, back.Version()); err != nil || again.Version() != back.Version() {
		t.Fatalf("RestoreSubject of an active subject = %v, %v; want it unchanged", again, err)
	}
}

// A file of the subject that is a part of an active note of another subject stays visible there: it does not go, so
// it is not counted; a part of a removed note of another subject is a file that goes.
func TestDiscardSubjectKeepsPartsOfOtherNotes(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys, chem := createSubject(t, e, "Физика"), createSubject(t, e, "Химия")
	place := func(text string) shared.MaterialID {
		m, err := e.svc.Place(ctx, importText(t, e, text).ID(), phys.ID(), "")
		if err != nil {
			t.Fatal(err)
		}
		return m.ID()
	}
	inOther, inGone := place("общая часть"), place("часть удалённого конспекта")
	place("свой файл")
	gone := newTestNote(t, "n2", chem.ID(), inGone)
	if _, err := gone.Remove(t0); err != nil {
		t.Fatal(err)
	}
	withNotes(t, e, &fakeNotes{list: map[shared.SubjectID][]*note.Note{
		chem.ID(): {newTestNote(t, "n1", chem.ID(), inOther), gone},
		phys.ID(): {},
	}})

	sub, err := e.svc.subject(ctx, phys.ID())
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.DiscardSubject(ctx, phys.ID(), sub.Version())
	if err != nil || got.Notes != 0 || got.Files != 2 {
		t.Fatalf("DiscardSubject = notes %d, files %d, %v; want 0 notes, 2 files", got.Notes, got.Files, err)
	}
}

func TestRestoreSubjectAfterTheWindow(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	got, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version())
	if err != nil || got.Notes != 0 || got.Files != 0 {
		t.Fatalf("DiscardSubject(empty) = %+v, %v", got, err)
	}

	e.clock.now = t0.Add(subject.RestoreWindow)
	if _, err := e.svc.RestoreSubject(ctx, phys.ID(), got.Subject.Version()); !errors.Is(err, ErrRestoreExpired) {
		t.Fatalf("RestoreSubject after the window = %v, want ErrRestoreExpired", err)
	}
	if !e.subjects.removed(phys.ID()) {
		t.Fatal("an expired restore brought the subject back")
	}
}

func TestDiscardAndRestoreRefusals(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"discard: bad id", func() error { _, err := e.svc.DiscardSubject(ctx, "", 1); return err }, ErrInvalidRequest},
		{"discard: unknown", func() error { _, err := e.svc.DiscardSubject(ctx, "nope", 1); return err }, ErrSubjectNotFound},
		{"discard: other version", func() error {
			_, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version()+1)
			return err
		}, ErrSubjectChanged},
		{"restore: bad id", func() error { _, err := e.svc.RestoreSubject(ctx, "", 1); return err }, ErrInvalidRequest},
		{"restore: unknown", func() error { _, err := e.svc.RestoreSubject(ctx, "nope", 1); return err }, ErrSubjectNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, tt.want) {
				t.Fatalf("= %v, want %v", err, tt.want)
			}
		})
	}

	// A change saved between the read and the save of the student's click is a change not seen.
	e.subjects.beforeSave = func() {
		if _, err := e.svc.AddAlias(ctx, phys.ID(), "Физ"); err != nil {
			t.Error(err)
		}
	}
	if _, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version()); !errors.Is(err, ErrSubjectChanged) {
		t.Fatalf("DiscardSubject losing its save = %v, want ErrSubjectChanged", err)
	}
	if e.subjects.removed(phys.ID()) {
		t.Fatal("the subject was removed although its save lost")
	}

	e.subjects.err = errors.New("disk")
	if _, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version()+1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("DiscardSubject(store down) = %v, want ErrUnavailable", err)
	}
	if _, err := e.svc.RestoreSubject(ctx, phys.ID(), phys.Version()+1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RestoreSubject(store down) = %v, want ErrUnavailable", err)
	}
}

// The agent's undo of create_subject never takes the student's notes: RemoveSubject still refuses, DiscardSubject is
// the student's click.
func TestAgentUndoKeepsNotesWhileDiscardTakesThem(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	withNotes(t, e, &fakeNotes{list: map[shared.SubjectID][]*note.Note{phys.ID(): {newTestNote(t, "n1", phys.ID(), "m-x")}}})

	if err := e.svc.RemoveSubject(ctx, phys.ID()); !errors.Is(err, ErrHasNotes) {
		t.Fatalf("RemoveSubject(with a note) = %v, want ErrHasNotes", err)
	}
	if e.subjects.removed(phys.ID()) {
		t.Fatal("the agent's undo removed a subject with notes")
	}
	got, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version())
	if err != nil || got.Notes != 1 {
		t.Fatalf("DiscardSubject = %+v, %v", got, err)
	}
	if err := e.svc.RemoveSubject(ctx, phys.ID()); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("RemoveSubject of a removed subject = %v, want ErrSubjectNotFound", err)
	}
}

func TestRemovedSubjectKeepsItsNames(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика", "Физ")
	chem := createSubject(t, e, "Химия")
	if _, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version()); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"ФИЗИКА", "физ"} {
		_, err := e.svc.CreateSubject(ctx, NewSubject{Name: name})
		var removed *RemovedNameError
		if !errors.As(err, &removed) || removed.Subject != phys.ID() || !errors.Is(err, ErrRemovedNameTaken) ||
			!errors.Is(err, ErrNameTaken) {
			t.Fatalf("CreateSubject(%q) = %v, want a RemovedNameError of %s", name, err, phys.ID())
		}
	}
	if _, err := e.svc.CreateSubject(ctx, NewSubject{Name: "Общая химия", Aliases: []string{"Физ"}}); !errors.Is(err, ErrRemovedNameTaken) {
		t.Fatalf("CreateSubject(alias of a removed subject) = %v, want ErrRemovedNameTaken", err)
	}
	if _, err := e.svc.AddAlias(ctx, chem.ID(), "физика"); !errors.Is(err, ErrRemovedNameTaken) {
		t.Fatalf("AddAlias(name of a removed subject) = %v, want ErrRemovedNameTaken", err)
	}
	if _, err := e.svc.CreateSubject(ctx, NewSubject{Name: "химия"}); err == nil || errors.Is(err, ErrRemovedNameTaken) ||
		!errors.Is(err, ErrNameTaken) {
		t.Fatalf("CreateSubject(name of an active subject) = %v, want plain ErrNameTaken", err)
	}
}

// The same bytes as a file hidden under a removed subject come back in «Входящие»: the student finds what was added.
func TestImportOfAFileHiddenUnderARemovedSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	m, err := e.svc.Place(ctx, importText(t, e, "лекция").ID(), phys.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.DiscardSubject(ctx, phys.ID(), phys.Version()); err != nil {
		t.Fatal(err)
	}

	im, err := e.svc.Import(ctx, Upload{Name: "lecture.txt", Data: []byte("лекция")})
	if err != nil || !im.Existing || im.Material.ID() != m.ID() || !im.Material.InInbox() {
		t.Fatalf("Import(hidden bytes) = %+v, %v; want the material back in the inbox", im, err)
	}
	if _, err := e.svc.Material(ctx, m.ID()); err != nil {
		t.Fatalf("Material after the import = %v", err)
	}
}
