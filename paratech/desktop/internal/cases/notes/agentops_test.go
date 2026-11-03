package notes

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

// An upload of «Входящие» is filed under the note's subject by the lesson kind, then added like a file; the same one
// again is «уже добавлено»; one filed under another subject, removed or unknown is refused before anything changes.
func TestAddMaterial(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.addLesson(t, "l1", t0.Add(24*time.Hour), lesson.KindSeminar)
	up := e.materials.inbox(t, "фото доски")

	res, err := e.svc.AddMaterial(ctx, MaterialRequest{
		Anchor: AnchorRef{Kind: note.AnchorLesson, Lesson: "l1"}, Subject: phys, Material: up.ID(),
	})
	if err != nil || !res.Created || len(res.Parts) != 1 || res.Parts[0].Material.ID() != up.ID() {
		t.Fatalf("AddMaterial = %+v, %v", res, err)
	}
	n := res.View.Note
	if n.Title() != "загрузка.txt" || !slices.Equal(n.Parts(), []shared.MaterialID{up.ID()}) {
		t.Fatalf("note = %+v", n.State())
	}
	wantFolder := subject.SystemFolderID(phys, subject.SystemSeminars)
	if got := e.materials.rows[up.ID()]; got.Subject() != phys || got.Folder() != wantFolder {
		t.Fatalf("upload filed in %s/%s, want %s", got.Subject(), got.Folder(), wantFolder)
	}

	again, err := e.svc.AddMaterial(ctx, MaterialRequest{Note: n.ID(), Material: up.ID()})
	if err != nil || again.Created || !again.Parts[0].AlreadyAdded || len(again.View.Note.Parts()) != 1 {
		t.Fatalf("AddMaterial(again) = %+v, %v", again, err)
	}

	other := e.materials.filed(t, "s-chem", "химия")
	removed := e.materials.filed(t, phys, "старое")
	if _, err := removed.Remove(t0); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		req  MaterialRequest
		want error
	}{
		{"filed under another subject", MaterialRequest{Note: n.ID(), Material: other.ID()}, ErrOtherSubject},
		{"removed upload", MaterialRequest{Note: n.ID(), Material: removed.ID()}, ErrMaterialRemoved},
		{"unknown upload", MaterialRequest{Note: n.ID(), Material: "m-none"}, ErrMaterialNotFound},
		{"bad id", MaterialRequest{Note: n.ID(), Material: "a b"}, ErrInvalidRequest},
		{"unknown note", MaterialRequest{Note: "n-none", Material: up.ID()}, ErrNoteNotFound},
	}
	saves := e.notes.saves
	for _, tt := range tests {
		if _, err := e.svc.AddMaterial(ctx, tt.req); !errors.Is(err, tt.want) {
			t.Errorf("%s: AddMaterial = %v, want %v", tt.name, err, tt.want)
		}
	}
	if e.notes.saves != saves {
		t.Fatal("a refused AddMaterial saved the note")
	}
}

func TestAddMaterialToAFullNoteFilesNothing(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	parts := make([]shared.MaterialID, 0, note.MaxParts)
	for range note.MaxParts {
		parts = append(parts, e.materials.filed(t, phys, string(rune(0x400+len(parts)))).ID())
	}
	n, err := note.New(note.Spec{ID: "n-full", Subject: phys, Anchor: undated(t), Title: "Полный", Parts: parts}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.notes.Save(ctx, n); err != nil {
		t.Fatal(err)
	}
	up := e.materials.inbox(t, "ещё одна")
	if _, err := e.svc.AddMaterial(ctx, MaterialRequest{Note: "n-full", Material: up.ID()}); !errors.Is(err, ErrTooManyParts) {
		t.Fatalf("AddMaterial(full) = %v", err)
	}
	if len(e.materials.placed) != 0 || !e.materials.rows[up.ID()].InInbox() {
		t.Fatal("the upload was filed for a note that had no room")
	}
}

// An add that fails after AddMaterial filed the upload returns it to «Входящие» (best effort: a failure of that keeps the
// error of the add); an upload that was filed before stays where it was.
func TestAddMaterialThatFailsReturnsTheUpload(t *testing.T) {
	disk := errors.New("disk I/O error")
	conflicts := []error{shared.ErrVersionConflict, shared.ErrVersionConflict, shared.ErrVersionConflict}
	tests := []struct {
		name      string
		into      bool    // into the note n-own, else a new note
		filed     bool    // the upload is filed under the subject already
		saveErrs  []error // of the add's saves
		inboxErr  error
		want      error
		wantInbox bool
	}{
		{name: "a new note not saved", saveErrs: []error{disk}, want: ErrUnavailable, wantInbox: true},
		{name: "a note that kept changing", into: true, saveErrs: conflicts, want: ErrBusy, wantInbox: true},
		{name: "a filed upload stays filed", into: true, filed: true, saveErrs: []error{disk}, want: ErrUnavailable},
		{name: "the return fails too: the add's error", saveErrs: []error{disk}, inboxErr: errors.New("library busy"), want: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, ctx := newEnv(t), context.Background()
			req := MaterialRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys}
			if tt.into {
				n, err := note.New(note.Spec{ID: "n-own", Subject: phys, Anchor: undated(t), Title: "Своё",
					Parts: []shared.MaterialID{e.materials.filed(t, phys, "своё").ID()}}, t0)
				if err != nil {
					t.Fatal(err)
				}
				if err := e.notes.Save(ctx, n); err != nil {
					t.Fatal(err)
				}
				req = MaterialRequest{Note: "n-own"}
			}
			up := e.materials.inbox(t, "фото доски")
			if tt.filed {
				up = e.materials.filed(t, phys, "фото доски 2")
			}
			req.Material = up.ID()
			e.materials.inboxErr = tt.inboxErr
			e.notes.saveErrs = tt.saveErrs

			if _, err := e.svc.AddMaterial(ctx, req); !errors.Is(err, tt.want) {
				t.Fatalf("AddMaterial = %v, want %v", err, tt.want)
			}
			if got := e.materials.rows[up.ID()].InInbox(); got != tt.wantInbox {
				t.Fatalf("upload in «Входящие» = %v, want %v", got, tt.wantInbox)
			}
			if tt.filed && len(e.materials.unplaced) != 0 {
				t.Fatalf("a filed upload was moved: %v", e.materials.unplaced)
			}
		})
	}
}

// RemovePart is the undo of an add into a note that existed: the part goes, the material stays; the last part stays.
func TestRemovePart(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: uploads("a.txt", "b.txt")})
	if err != nil {
		t.Fatal(err)
	}
	id, parts := res.View.Note.ID(), res.View.Note.Parts()
	v, err := e.svc.RemovePart(ctx, id, parts[1])
	if err != nil || !slices.Equal(v.Note.Parts(), parts[:1]) {
		t.Fatalf("RemovePart = %+v, %v", v, err)
	}
	if _, err := e.materials.Material(ctx, parts[1]); err != nil {
		t.Fatalf("the material left the library: %v", err)
	}
	if _, err := e.svc.RemovePart(ctx, id, parts[0]); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("RemovePart(last) = %v", err)
	}
	if _, err := e.svc.Remove(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RemovePart(ctx, id, parts[0]); !errors.Is(err, ErrRemoved) {
		t.Fatalf("RemovePart(removed note) = %v", err)
	}
}

// Note reads a note in any status; Anchor and LessonSubject read what an add would use.
func TestNoteAnchorAndLessonSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	start := time.Date(2026, time.October, 28, 23, 30, 0, 0, time.UTC) // the 29th in the local zone
	e.addLesson(t, "l1", start, lesson.KindLecture)
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorLesson, Lesson: "l1"}, Subject: phys, Uploads: uploads("a.txt")})
	if err != nil {
		t.Fatal(err)
	}
	id := res.View.Note.ID()
	if _, err := e.svc.Remove(ctx, id); err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Note(ctx, id)
	if err != nil || v.Note.Active() || v.Note.ID() != id {
		t.Fatalf("Note(removed) = %+v, %v", v, err)
	}
	if _, err := e.svc.Note(ctx, "n-none"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("Note(none) = %v", err)
	}

	a, err := e.svc.Anchor(ctx, AnchorRef{Kind: note.AnchorLesson, Lesson: "l1"})
	if err != nil || a != res.View.Anchor || a.Date() != date(t, 2026, time.October, 29) {
		t.Fatalf("Anchor = %+v, %v; want %+v", a, err, res.View.Anchor)
	}
	if _, err := e.svc.Anchor(ctx, AnchorRef{Kind: note.AnchorLesson, Lesson: "l-none"}); !errors.Is(err, ErrLessonNotFound) {
		t.Fatalf("Anchor(no lesson) = %v", err)
	}
	if s, err := e.svc.LessonSubject(ctx, "l1"); err != nil || s != "Физика" {
		t.Fatalf("LessonSubject = %q, %v", s, err)
	}
	if _, err := e.svc.LessonSubject(ctx, "l-none"); !errors.Is(err, ErrLessonNotFound) {
		t.Fatalf("LessonSubject(none) = %v", err)
	}
}

func undated(t *testing.T) note.Anchor {
	t.Helper()
	a, err := note.NewAnchor(note.AnchorSpec{Kind: note.AnchorUndated})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
