package notes

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/recording/recording"
	"paratech/desktop/internal/domain/schedule/lesson"
	"paratech/desktop/internal/domain/shared"
)

const phys shared.SubjectID = "s-phys"

func recAnchor(id shared.RecordingID) AnchorRef { return AnchorRef{Kind: note.AnchorRecording, Recording: id} }

func TestNewRequiresPorts(t *testing.T) {
	if _, err := New(Deps{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(empty) = %v", err)
	}
}

func TestAddToRecording(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	// 23:30 UTC is already the 13th in the local zone.
	e.addRecording(t, recordingSpec{id: "r1", status: recording.StatusCompleted,
		started: time.Date(2026, time.October, 12, 23, 30, 0, 0, time.UTC), kind: "lecture", fixed: "seminar"})

	res, err := e.svc.Add(ctx, AddRequest{Anchor: recAnchor("r1"), Subject: phys, Uploads: uploads("Лекция 1.docx", "схема.txt")})
	if err != nil || !res.Created || len(res.Parts) != 2 {
		t.Fatalf("Add = %+v, %v", res, err)
	}
	n := res.View.Note
	a := res.View.Anchor
	if n.Title() != "Лекция 1.docx" || n.Subject() != phys || len(n.Parts()) != 2 || a.Kind() != note.AnchorRecording ||
		a.Recording() != "r1" || a.Date() != date(t, 2026, time.October, 13) || a.LessonKind() != "seminar" ||
		a.Title() != "Механика" {
		t.Fatalf("note %+v, anchor %+v", n.State(), a)
	}
	if !reflect.DeepEqual(e.materials.kinds, []string{"seminar", "seminar"}) {
		t.Fatalf("filed with kinds %q, want the kind the student set", e.materials.kinds)
	}
	if len(res.View.Parts) != 2 || res.View.Parts[0].Name() != "Лекция 1.docx" {
		t.Fatalf("view parts %+v", res.View.Parts)
	}

	// A second drop on the lecture: the same file is «уже добавлено», a new one is appended to the same note.
	again, err := e.svc.Add(ctx, AddRequest{Anchor: recAnchor("r1"), Subject: phys, Uploads: uploads("схема.txt", "Лекция 2.pdf.txt", "схема.txt")})
	if err != nil || again.Created || again.View.Note.ID() != n.ID() {
		t.Fatalf("second Add = %+v, %v", again, err)
	}
	got := again.Parts
	if !got[0].AlreadyAdded || !got[0].Existing || got[1].AlreadyAdded || !got[2].AlreadyAdded {
		t.Fatalf("results %+v", got)
	}
	if len(again.View.Note.Parts()) != 3 || e.notes.count() != 1 {
		t.Fatalf("parts %v, notes %d", again.View.Note.Parts(), e.notes.count())
	}

	// Only files that are parts already: nothing saved, the note as it is.
	saves := e.notes.saves
	same, err := e.svc.Add(ctx, AddRequest{Note: n.ID(), Uploads: uploads("схема.txt")})
	if err != nil || !same.Parts[0].AlreadyAdded || e.notes.saves != saves {
		t.Fatalf("Add(only a part) = %+v, %v; saves %d -> %d", same, err, saves, e.notes.saves)
	}
}

func TestAddChecksTheAnchor(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.addRecording(t, recordingSpec{id: "r-rec", status: recording.StatusRecording, started: t0})
	e.addRecording(t, recordingSpec{id: "r-failed", status: recording.StatusFailed, started: t0})
	e.addRecording(t, recordingSpec{id: "r-del", status: recording.StatusCompleted, started: t0, deleting: true})
	gone := e.addLesson(t, "l-gone", t0, lesson.KindLecture)
	if err := gone.MarkDeleted(t0); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		ref  AnchorRef
		sub  shared.SubjectID
		want error
	}{
		{"recording in progress", recAnchor("r-rec"), phys, ErrRecordingUnavailable},
		{"failed recording", recAnchor("r-failed"), phys, ErrRecordingUnavailable},
		{"recording marked for deletion", recAnchor("r-del"), phys, ErrRecordingUnavailable},
		{"no such recording", recAnchor("r-none"), phys, ErrRecordingUnavailable},
		{"bad recording id", recAnchor("a/b"), phys, ErrInvalidRequest},
		{"no such lesson", AnchorRef{Kind: note.AnchorLesson, Lesson: "l-none"}, phys, ErrLessonNotFound},
		{"lesson removed from the schedule", AnchorRef{Kind: note.AnchorLesson, Lesson: "l-gone"}, phys, ErrLessonNotFound},
		{"day without a date", AnchorRef{Kind: note.AnchorDay}, phys, ErrInvalidRequest},
		{"undated with a date", AnchorRef{Kind: note.AnchorUndated, Date: date(t, 2026, 9, 1)}, phys, ErrInvalidRequest},
		{"unknown kind", AnchorRef{Kind: "week"}, phys, ErrInvalidRequest},
		{"bad subject", AnchorRef{Kind: note.AnchorUndated}, "a b", ErrInvalidRequest},
	}
	for _, tt := range tests {
		if _, err := e.svc.Add(ctx, AddRequest{Anchor: tt.ref, Subject: tt.sub, Uploads: uploads("a.txt")}); !errors.Is(err, tt.want) {
			t.Errorf("%s: Add = %v, want %v", tt.name, err, tt.want)
		}
	}
	if e.materials.calls() != 0 || e.notes.count() != 0 {
		t.Fatalf("stored %d files, %d notes for refused anchors", e.materials.calls(), e.notes.count())
	}
	if _, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys}); !errors.Is(err, ErrNoFiles) {
		t.Fatalf("Add(no files) = %v", err)
	}
	if _, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys,
		Uploads: make([]Upload, note.MaxParts+1)}); !errors.Is(err, ErrTooManyParts) {
		t.Fatalf("Add(too many) = %v", err)
	}
}

func TestAddToLessonDayAndUndated(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.addLesson(t, "l1", time.Date(2026, time.September, 12, 9, 0, 0, 0, msk), lesson.KindLab)
	lres, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorLesson, Lesson: "l1"}, Subject: phys, Uploads: uploads("лаба.txt")})
	if err != nil {
		t.Fatal(err)
	}
	if a := lres.View.Anchor; a.Lesson() != "l1" || a.LessonKind() != "lab" || a.Date() != date(t, 2026, 9, 12) ||
		a.Title() != "Семинар по механике" || e.materials.kinds[0] != "lab" {
		t.Fatalf("lesson anchor %+v, kinds %q", a, e.materials.kinds)
	}

	day := AnchorRef{Kind: note.AnchorDay, Date: date(t, 2026, 9, 5)}
	first, err := e.svc.Add(ctx, AddRequest{Anchor: day, Subject: phys, Title: "Пара 5 сентября", Uploads: uploads("a.txt")})
	if err != nil || first.View.Note.Title() != "Пара 5 сентября" {
		t.Fatalf("Add(day) = %+v, %v", first, err)
	}
	second, err := e.svc.Add(ctx, AddRequest{Anchor: day, Subject: phys, Uploads: uploads("b.txt")})
	if err != nil || second.Created || second.View.Note.ID() != first.View.Note.ID() {
		t.Fatalf("second Add(day) = %+v, %v: want the same note", second, err)
	}
	// Another subject on the same day gets its own note.
	other, err := e.svc.Add(ctx, AddRequest{Anchor: day, Subject: "s-chem", Uploads: uploads("c.txt")})
	if err != nil || !other.Created {
		t.Fatalf("Add(day, other subject) = %+v, %v", other, err)
	}

	undated := AnchorRef{Kind: note.AnchorUndated}
	u1, err1 := e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: phys, Uploads: uploads("u1.txt")})
	u2, err2 := e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: phys, Uploads: uploads("u2.txt")})
	if err1 != nil || err2 != nil || !u2.Created || u1.View.Note.ID() == u2.View.Note.ID() {
		t.Fatalf("Add(undated) twice = %v, %v: want two notes", err1, err2)
	}
	if u1.View.Anchor.LessonKind() != "" || e.materials.kinds[len(e.materials.kinds)-1] != "" {
		t.Fatalf("undated kinds %q", e.materials.kinds)
	}
}

// Every part must be a material of the note's subject (the material may come from outside).
func TestPartsBelongToTheSubject(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	foreign := e.materials.filed(t, "s-chem", "химия")
	undated := AnchorRef{Kind: note.AnchorUndated}

	// The bytes are a material of another subject: refused, no note.
	res, err := e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: phys, Uploads: []Upload{{Name: "x.txt", Data: []byte("химия")}}})
	if !errors.Is(err, ErrOtherSubject) || !errors.Is(res.Parts[0].Err, ErrOtherSubject) || e.notes.count() != 0 {
		t.Fatalf("Add(foreign) = %+v, %v; notes %d", res, err, e.notes.count())
	}

	// Mixed: the note gets the good file; the foreign one carries its error.
	res, err = e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: phys, Uploads: []Upload{
		{Name: "x.txt", Data: []byte("химия")}, {Name: "ok.txt", Data: []byte("физика")},
	}})
	if err != nil || !errors.Is(res.Parts[0].Err, ErrOtherSubject) || res.Parts[1].Err != nil || len(res.View.Note.Parts()) != 1 {
		t.Fatalf("Add(mixed) = %+v, %v", res, err)
	}
	n := res.View.Note
	ok := n.Parts()[0]

	// The undo of a replace takes a material id from the page: one of another subject is refused.
	if _, err := e.svc.ReplacePartWith(ctx, n.ID(), ok, foreign.ID()); !errors.Is(err, ErrOtherSubject) {
		t.Fatalf("ReplacePartWith(foreign) = %v", err)
	}
	if _, err := e.svc.ReplacePartWith(ctx, n.ID(), ok, "m-none"); !errors.Is(err, ErrMaterialNotFound) {
		t.Fatalf("ReplacePartWith(unknown) = %v", err)
	}
	removed := e.materials.filed(t, phys, "старое")
	if _, err := removed.Remove(t0); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.ReplacePartWith(ctx, n.ID(), ok, removed.ID()); !errors.Is(err, ErrMaterialRemoved) {
		t.Fatalf("ReplacePartWith(removed) = %v", err)
	}
	if _, err := e.svc.ReplacePartWith(ctx, n.ID(), ok, "a/b"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ReplacePartWith(bad id) = %v", err)
	}
	if _, err := e.svc.ReplacePart(ctx, n.ID(), ok, Upload{Name: "y.txt", Data: []byte("химия")}); !errors.Is(err, ErrOtherSubject) {
		t.Fatalf("ReplacePart(foreign bytes) = %v", err)
	}
	if got, _ := e.notes.Get(ctx, n.ID()); !reflect.DeepEqual(got.Parts(), []shared.MaterialID{ok}) {
		t.Fatalf("parts changed: %v", got.Parts())
	}
}

func TestReplacePart(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: uploads("a.txt", "b.txt")})
	if err != nil {
		t.Fatal(err)
	}
	n := res.View.Note
	old := n.Parts()[0]

	calls := e.materials.calls()
	if _, err := e.svc.ReplacePart(ctx, n.ID(), "m-none", Upload{Name: "c.txt", Data: []byte("новое")}); !errors.Is(err, ErrPartNotFound) {
		t.Fatalf("ReplacePart(not a part) = %v", err)
	}
	if e.materials.calls() != calls {
		t.Fatal("a file was stored for a part that does not exist")
	}

	rep, err := e.svc.ReplacePart(ctx, n.ID(), old, Upload{Name: "a2.txt", Data: []byte("новое")})
	if err != nil || rep.Previous != old || rep.Material == nil || rep.View.Note.Parts()[0] != rep.Material.ID() {
		t.Fatalf("ReplacePart = %+v, %v", rep, err)
	}
	if _, err := e.materials.Material(ctx, old); err != nil {
		t.Fatalf("the old material is gone: %v", err)
	}
	// Undo: the old one back in its place.
	undo, err := e.svc.ReplacePartWith(ctx, n.ID(), rep.Material.ID(), old)
	if err != nil || !reflect.DeepEqual(undo.View.Note.Parts(), n.Parts()) {
		t.Fatalf("ReplacePartWith = %+v, %v", undo, err)
	}
	// With the other part: a duplicate.
	if _, err := e.svc.ReplacePartWith(ctx, n.ID(), old, n.Parts()[1]); !errors.Is(err, ErrAlreadyAdded) {
		t.Fatalf("ReplacePartWith(another part) = %v", err)
	}
}

func TestPaste(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	undated := AnchorRef{Kind: note.AnchorUndated}
	text := "\n  Ряды Фурье  \nразложение периодической функции"
	res, err := e.svc.Paste(ctx, PasteRequest{Anchor: undated, Subject: phys, Text: text})
	if err != nil || !res.Created {
		t.Fatalf("Paste = %+v, %v", res, err)
	}
	m := res.Parts[0].Material
	if m.Type() != material.TypeTXT || m.Name() != "Ряды Фурье.txt" || res.View.Note.Title() != "Ряды Фурье" {
		t.Fatalf("material %q %s, title %q", m.Name(), m.Type(), res.View.Note.Title())
	}
	again, err := e.svc.Paste(ctx, PasteRequest{Note: res.View.Note.ID(), Text: text})
	if err != nil || !again.Parts[0].AlreadyAdded {
		t.Fatalf("Paste again = %+v, %v", again, err)
	}

	tests := []struct {
		name string
		text string
		want error
	}{
		{"empty", " \n\t ", ErrEmptyText},
		{"too large", strings.Repeat("я", MaxPastedBytes/2+1), ErrTextTooLarge},
		{"NUL", "a\x00b", ErrInvalidText},
		{"looks like a PDF", "%PDF-1.7 not really", ErrInvalidText},
		{"not UTF-8", "\xff\xfe\xfd", ErrInvalidText},
	}
	for _, tt := range tests {
		if _, err := e.svc.Paste(ctx, PasteRequest{Anchor: undated, Subject: phys, Text: tt.text}); !errors.Is(err, tt.want) {
			t.Errorf("%s: Paste = %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, err := e.svc.Paste(ctx, PasteRequest{Anchor: undated, Subject: phys, Text: strings.Repeat("a", MaxPastedBytes)}); err != nil {
		t.Fatalf("Paste(at the limit) = %v", err)
	}
}

func TestLibraryErrorsPassThrough(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	undated := AnchorRef{Kind: note.AnchorUndated}
	if _, err := e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: "nosuch", Uploads: uploads("a.txt")}); !errors.Is(err, errSubjectGone) {
		t.Fatalf("Add(no subject) = %v", err)
	}
	if _, err := e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: phys, Uploads: []Upload{{Name: "a.bin", Data: []byte{0, 1}}}}); !errors.Is(err, material.ErrUnsupportedType) {
		t.Fatalf("Add(binary) = %v", err)
	}
	// A subject removed between the check and the save: the store refuses the note.
	if _, err := e.svc.Add(ctx, AddRequest{Anchor: undated, Subject: "gone", Uploads: uploads("a.txt")}); !errors.Is(err, ErrChanged) {
		t.Fatalf("Add(subject gone at save) = %v", err)
	}
}

func TestChanges(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: uploads("a.txt", "b.txt")})
	if err != nil {
		t.Fatal(err)
	}
	id := res.View.Note.ID()
	parts := partIDs(res.View)

	v, err := e.svc.Reorder(ctx, id, []shared.MaterialID{parts[1], parts[0]})
	if err != nil || v.Note.Parts()[0] != parts[1] {
		t.Fatalf("Reorder = %v, %v", v.Note.Parts(), err)
	}
	if _, err := e.svc.Reorder(ctx, id, []shared.MaterialID{parts[0]}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("Reorder(missing part) = %v", err)
	}
	if v, err = e.svc.Retitle(ctx, id, "  Мой конспект "); err != nil || v.Note.Title() != "Мой конспект" {
		t.Fatalf("Retitle = %q, %v", v.Note.Title(), err)
	}
	if _, err := e.svc.Retitle(ctx, id, " "); !errors.Is(err, ErrEmptyTitle) {
		t.Fatalf("Retitle(empty) = %v", err)
	}
	day := date(t, 2026, 9, 1)
	if v, err = e.svc.Reanchor(ctx, id, AnchorRef{Kind: note.AnchorDay, Date: day}); err != nil || v.Anchor.Date() != day {
		t.Fatalf("Reanchor = %+v, %v", v.Anchor, err)
	}
	if _, err := e.svc.Reanchor(ctx, id, recAnchor("r-none")); !errors.Is(err, ErrRecordingUnavailable) {
		t.Fatalf("Reanchor(no recording) = %v", err)
	}

	if v, err = e.svc.Remove(ctx, id); err != nil || v.Note.Active() {
		t.Fatalf("Remove = %v", err)
	}
	if _, err := e.svc.Retitle(ctx, id, "x"); !errors.Is(err, ErrRemoved) {
		t.Fatalf("Retitle(removed) = %v", err)
	}
	if _, err := e.svc.Add(ctx, AddRequest{Note: id, Uploads: uploads("c.txt")}); !errors.Is(err, ErrRemoved) {
		t.Fatalf("Add(to a removed note) = %v", err)
	}
	if _, err := e.svc.Remove(ctx, id); err != nil {
		t.Fatalf("Remove twice = %v", err)
	}
	if v, err = e.svc.Restore(ctx, id); err != nil || !v.Note.Active() || v.Note.Title() != "Мой конспект" {
		t.Fatalf("Restore = %v", err)
	}

	if _, err := e.svc.Retitle(ctx, "n-none", "x"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("Retitle(unknown) = %v", err)
	}
	if _, err := e.svc.Remove(ctx, "a/b"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Remove(bad id) = %v", err)
	}
}

func TestSaveRetriesAndBusy(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: uploads("a.txt")})
	if err != nil {
		t.Fatal(err)
	}
	id := res.View.Note.ID()

	e.notes.saveErrs = []error{shared.ErrVersionConflict}
	if v, err := e.svc.Retitle(ctx, id, "после повтора"); err != nil || v.Note.Title() != "после повтора" {
		t.Fatalf("Retitle after one lost save = %v", err)
	}
	e.notes.saveErrs = []error{shared.ErrVersionConflict, shared.ErrVersionConflict, shared.ErrVersionConflict}
	if _, err := e.svc.Retitle(ctx, id, "занято"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Retitle while busy = %v", err)
	}
	e.notes.saveErrs = []error{errors.New("disk")}
	if _, err := e.svc.Retitle(ctx, id, "диск"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Retitle with a failing disk = %v", err)
	}
}
