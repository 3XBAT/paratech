package library

import (
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// fakeNotes answers the notes of a subject as counted.
type fakeNotes struct {
	of  map[shared.SubjectID]int
	err error
}

func (f *fakeNotes) ListBySubject(_ context.Context, sub shared.SubjectID) ([]*note.Note, error) {
	if f.err != nil {
		return nil, f.err
	}
	return make([]*note.Note, f.of[sub]), nil // only the count is read
}

// withNotes rebuilds the service of e with the notes port.
func withNotes(t *testing.T, e *env, notes SubjectNotes) {
	t.Helper()
	svc, err := New(Deps{
		Subjects: e.subjects, Materials: e.materials, Blobs: e.blobs, Lessons: e.lessons, Strings: e.strings,
		Clock: e.clock, IDs: &seqIDs{n: 100}, Texts: e.texts, Reader: e.reader, Inspections: e.inspections, Notes: notes,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
}

func TestRemoveSubjectWithNotes(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	notes := &fakeNotes{of: map[shared.SubjectID]int{phys.ID(): 1}}
	withNotes(t, e, notes)

	err := e.svc.RemoveSubject(ctx, phys.ID())
	if !errors.Is(err, ErrHasNotes) || !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("RemoveSubject(with a note) = %v, want ErrHasNotes wrapping ErrNotEmpty", err)
	}
	if _, ok := e.subjects.rows[phys.ID()]; !ok {
		t.Fatal("the subject was deleted although it has notes")
	}

	notes.err = errors.New("disk")
	if err := e.svc.RemoveSubject(ctx, phys.ID()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("RemoveSubject(notes unreadable) = %v, want ErrUnavailable", err)
	}

	notes.err, notes.of = nil, map[shared.SubjectID]int{}
	if err := e.svc.RemoveSubject(ctx, phys.ID()); err != nil {
		t.Fatalf("RemoveSubject(no notes) = %v", err)
	}
}

func TestFileNotePart(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	chem := createSubject(t, e, "Химия")
	lectures := subject.SystemFolderID(phys.ID(), subject.SystemLectures)

	tests := []struct {
		name string
		kind string
		data string
		want shared.FolderID
	}{
		{"lecture", "lecture", "лекция", lectures},
		{"seminar", "seminar", "семинар", subject.SystemFolderID(phys.ID(), subject.SystemSeminars)},
		{"practice", "practice", "практика", subject.SystemFolderID(phys.ID(), subject.SystemSeminars)},
		{"lab", "lab", "лабораторная", subject.SystemFolderID(phys.ID(), subject.SystemLabs)},
		{"unknown kind", "", "без вида", lectures},
	}
	for _, tt := range tests {
		m, existing, err := e.svc.FileNotePart(ctx, phys.ID(), tt.kind, "a.txt", []byte(tt.data))
		if err != nil || existing || m.Subject() != phys.ID() || m.Folder() != tt.want {
			t.Fatalf("%s: FileNotePart = %+v, %v, %v; want in %s", tt.name, m, existing, err, tt.want)
		}
	}

	// The same bytes again: the same material, nothing new stored.
	first, _, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "b.txt", []byte("лекция"))
	if err != nil {
		t.Fatal(err)
	}
	again, existing, err := e.svc.FileNotePart(ctx, phys.ID(), "lab", "c.txt", []byte("лекция"))
	if err != nil || !existing || again.ID() != first.ID() || again.Folder() != lectures {
		t.Fatalf("FileNotePart(same bytes) = %+v, %v, %v; want the same material where it was", again, existing, err)
	}

	// Filed in another subject: it stays there, the caller sees the subject.
	other := importText(t, e, "химия")
	if _, err := e.svc.Place(ctx, other.ID(), chem.ID(), ""); err != nil {
		t.Fatal(err)
	}
	got, existing, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "d.txt", []byte("химия"))
	if err != nil || !existing || got.Subject() != chem.ID() {
		t.Fatalf("FileNotePart(filed elsewhere) = %+v, %v, %v", got, existing, err)
	}

	// A removed material comes back and is filed.
	gone := importText(t, e, "удалённый")
	if _, err := e.svc.RemoveMaterial(ctx, gone.ID()); err != nil {
		t.Fatal(err)
	}
	back, _, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "e.txt", []byte("удалённый"))
	if err != nil || !back.Active() || back.Folder() != lectures {
		t.Fatalf("FileNotePart(removed) = %+v, %v", back, err)
	}

	writes := e.blobs.writes
	if _, _, err := e.svc.FileNotePart(ctx, "nope", "lecture", "f.txt", []byte("новый")); !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("FileNotePart(unknown subject) = %v", err)
	}
	if e.blobs.writes != writes {
		t.Fatal("a file was stored for a subject that does not exist")
	}
	if _, _, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "g.bin", []byte{0, 1, 2}); !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("FileNotePart(binary) = %v", err)
	}
	if _, _, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "h.txt", nil); !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("FileNotePart(empty) = %v", err)
	}
	if m, _ := e.svc.Material(ctx, first.ID()); m.Type() != material.TypeTXT {
		t.Fatalf("type %s", m.Type())
	}
}
