package homeworkflow

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"paratech/desktop/internal/domain/coursework/proposal"
	recvalues "paratech/desktop/internal/domain/recording/values"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// docxEnv is a draft env with version 1 of a1 ("Chapter 4"), the lesson the recording kept and a student profile.
func docxEnv(t *testing.T) *draftEnv {
	t.Helper()
	e := newDraftEnv(t)
	keepSnapshot(t, e.ppEnv, recvalues.LessonSnapshotSpec{
		Title: "Матанализ", Subject: "Математический анализ", Teacher: "Петров П. П.", Start: keptStart,
	})
	e.profiles.profile = settings.ProfileFields{
		FullName: "Иванов Иван", Group: "ИВТ-21", University: "МГУ", Faculty: "ВМК", City: "Москва",
	}
	if _, err := e.svc.EditDraft(context.Background(), DraftEdit{Assignment: "a1", Base: 0, Title: "Chapter 4",
		Body: "# Chapter 4\n\nDeltas.\n\n## Example\nThe Volga."}); err != nil {
		t.Fatal(err)
	}
	return e
}

// TASK-094: the Word file has the title page from the profile and the recorded lesson, the year of the clock, then
// the version's text; it is written to the Word export the send attaches.
func TestExportDocxWritesTheWordExport(t *testing.T) {
	ctx := context.Background()
	e := docxEnv(t)

	got, err := e.svc.ExportDocx(ctx, "a1", 1)
	ref, _ := proposal.ExportRefOf("a1", proposal.FormatDocx)
	if err != nil || got.Version != 1 || got.File != ref {
		t.Fatalf("ExportDocx = %+v, %v", got, err)
	}
	doc := e.renderer.last()
	if doc.Title.University != "МГУ" || doc.Title.Faculty != "ВМК" ||
		doc.Title.Heading != "Домашнее задание по дисциплине «Математический анализ»" || doc.Title.Topic != "Read chapter 4" ||
		doc.Title.Performer != "Выполнил: студент группы ИВТ-21 Иванов Иван" || doc.Title.Checker != "Проверил: Петров П. П." ||
		doc.Title.Place != "Москва 2026" {
		t.Fatalf("title page = %+v", doc.Title)
	}
	if len(doc.Blocks) != 4 || doc.Blocks[0].Lines[0] != "Chapter 4" || doc.Blocks[2].Lines[0] != "Example" {
		t.Fatalf("blocks = %+v", doc.Blocks)
	}

	rc, err := e.files.Open(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil || len(b) == 0 || string(b[:4]) != "DOCX" {
		t.Fatalf("export file = %q, %v", b, err)
	}
	// The text export is a file of its own and stays untouched.
	if txt, _ := proposal.ExportRef("a1"); txt == ref {
		t.Fatal("the Word export overwrites the text export")
	}
}

// Without a profile nothing dangles: the renderer gets empty lines, the use case adds no labels of its own.
func TestDocxWithoutAProfile(t *testing.T) {
	e := docxEnv(t)
	e.profiles.profile = settings.ProfileFields{}
	if _, err := e.svc.ExportDocx(context.Background(), "a1", 1); err != nil {
		t.Fatal(err)
	}
	if tp := e.renderer.last().Title; tp.Performer != "" || tp.University != "" || tp.Place != "2026" ||
		tp.Checker != "Проверил: Петров П. П." {
		t.Fatalf("title page = %+v", tp)
	}
}

func TestExportDocxRefusals(t *testing.T) {
	ctx := context.Background()
	e := docxEnv(t)
	for _, tc := range []struct {
		a shared.AssignmentID
		n int
	}{{"a1", 2}, {"a2", 1}} {
		if _, err := e.svc.ExportDocx(ctx, tc.a, tc.n); !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("ExportDocx(%s, %d) = %v, want ErrNotFound", tc.a, tc.n, err)
		}
	}
	e.deliveries.mark("a1", true)
	if _, err := e.svc.ExportDocx(ctx, "a1", 1); !errors.Is(err, ErrExportLocked) {
		t.Fatalf("unresolved email: %v", err)
	}
	if ref, _ := proposal.ExportRefOf("a1", proposal.FormatDocx); slices.Contains(e.files.Paths(), ref.String()) {
		t.Fatalf("the Word export was written: %v", e.files.Paths())
	}
	e.deliveries.mark("a1", false)

	boom := errors.New("settings locked")
	e.profiles.err = boom
	if _, err := e.svc.ExportDocx(ctx, "a1", 1); !errors.Is(err, boom) {
		t.Fatalf("profile read error: %v", err)
	}
}

// SaveDocx writes the same document where the student chose; an unresolved email does not lock that file.
func TestSaveDocx(t *testing.T) {
	ctx := context.Background()
	e := docxEnv(t)
	e.deliveries.mark("a1", true)
	const path = `C:\Users\student\Documents\Chapter 4.docx`
	if err := e.svc.SaveDocx(ctx, "a1", 1, path); err != nil {
		t.Fatal(err)
	}
	if b := e.saved.files[path]; len(b) == 0 || string(b[:4]) != "DOCX" {
		t.Fatalf("saved = %q", b)
	}
	if err := e.svc.SaveDocx(ctx, "a1", 1, ""); !errors.Is(err, ErrNoPath) {
		t.Fatalf("no path: %v", err)
	}
	if err := e.svc.SaveDocx(ctx, "a1", 9, path); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("missing version: %v", err)
	}
	boom := errors.New("disk full")
	e.saved.err = boom
	if err := e.svc.SaveDocx(ctx, "a1", 1, path); !errors.Is(err, boom) {
		t.Fatalf("write error: %v", err)
	}

	if name, err := e.svc.DocxName(ctx, "a1"); err != nil || name != "Read chapter 4.docx" {
		t.Fatalf("DocxName = %q, %v", name, err)
	}
	if _, err := e.svc.DocxName(ctx, "a2"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("DocxName of a missing assignment: %v", err)
	}
}
