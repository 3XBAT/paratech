package tests

// «Скачать PDF» (TASK-198, ADR-0020) end to end: a work written through the agent on the real job runner and SQLite,
// its PDF printed by the real pdf renderer (the print page of the same document as the .docx) through a fake
// WebView2 printer, filed by the real library next to the .docx. A new generation and the Undo of write_work take the
// PDF back to «Входящие» with the paper.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"paratech/desktop/internal/adapters/sqlite"
	"paratech/desktop/internal/domain/agent/action"
	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
	"paratech/desktop/internal/input/wails"
)

// fakePrinter stands for the WebView2 printer: it keeps the pages and answers a numbered fake PDF (a real print has
// its creation date, so two prints differ).
type fakePrinter struct {
	mu    sync.Mutex
	pages []string
}

func (p *fakePrinter) Print(_ context.Context, html []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pages = append(p.pages, string(html))
	return fmt.Appendf(nil, "%%PDF-1.7\n%% fake print %d\n%%%%EOF\n", len(p.pages)), nil
}

func (p *fakePrinter) printed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.pages...)
}

// setName stores the student's name in the profile of the title page.
func (e *turnkeyEnv) setName(t *testing.T, name string) {
	t.Helper()
	repo := sqlite.NewSettingsRepository(e.db)
	s, err := repo.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p, err := settings.NewProfile(settings.ProfileFields{FullName: name})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProfile(p, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(t.Context(), s); err != nil {
		t.Fatal(err)
	}
}

func TestTurnkeyPDFNextToThePaper(t *testing.T) {
	e := newTurnkeyEnv(t, "Реферат по истории Древней Руси: введение и заключение.")
	ctx := t.Context()
	w := e.confirmedByAgent(t)
	id := shared.WorkID(w.ID())
	sub := e.fileUnder(t, "История")
	e.setName(t, "Иванов Иван")
	e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, writingOutline, writingSection("Текст раздела <b>жирно</b>."))
	e.startRunner(t)
	e.runWriteWork(t, w)
	e.waitOutline(t, id, e.jobOfCall(t, "ww1"))
	v, _ := e.approveAndWait(t, id)
	if v.Writing.PDF != "" {
		t.Fatalf("a pdf before the click: %s", v.Writing.PDF)
	}

	// The name changed after the .docx: the PDF has the profile of the click.
	e.setName(t, "Петрова Анна")
	got, err := e.binding.ExportPDF(ctx, w.ID())
	if err != nil || got.Writing.PDF == "" || got.Writing.MaterialID != string(v.Writing.Material) {
		t.Fatalf("ExportPDF = %+v, %v", got.Writing, err)
	}
	pages := e.printer.printed()
	if len(pages) != 1 {
		t.Fatalf("%d prints", len(pages))
	}
	page := pages[0]
	for _, want := range []string{
		`content="default-src 'none'; style-src 'unsafe-inline'"`, homeworkdoc.GeneratedMark, "Петрова Анна",
		"Текст раздела &lt;b&gt;жирно&lt;/b&gt;.", "Древняя Русь",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the print page has no %q", want)
		}
	}
	if strings.Contains(page, "<b>") || strings.Contains(page, "<script") {
		t.Error("model text became markup")
	}

	pdfID := shared.MaterialID(got.Writing.PDF)
	pdfFile, err := e.lib.Material(ctx, pdfID)
	if err != nil {
		t.Fatal(err)
	}
	paper, err := e.lib.Material(ctx, v.Writing.Material)
	if err != nil {
		t.Fatal(err)
	}
	homework := subject.SystemFolderID(sub.ID(), subject.SystemHomework)
	if pdfFile.Type() != material.TypePDF || pdfFile.Folder() != homework || pdfFile.Subject() != sub.ID() ||
		pdfFile.Name() != strings.TrimSuffix(paper.Name(), homeworkdoc.DocxExtension)+homeworkdoc.PDFExtension {
		t.Fatalf("pdf = %+v next to %+v", pdfFile.State(), paper.State())
	}
	if want := fmt.Appendf(nil, "%%PDF-1.7\n%% fake print %d\n%%%%EOF\n", 1); pdfFile.Digest() != material.DigestOf(want) {
		t.Fatal("the library holds other bytes than the print")
	}

	// The work keeps it: read again, a second click prints nothing.
	again, err := e.binding.ExportPDF(ctx, w.ID())
	if err != nil || again.Writing.PDF != got.Writing.PDF || len(e.printer.printed()) != 1 {
		t.Fatalf("again = %+v, %v, %d prints", again.Writing, err, len(e.printer.printed()))
	}
	if read, err := e.binding.Work(ctx, w.ID()); err != nil || read.Writing.PDF != got.Writing.PDF {
		t.Fatalf("Work = %+v, %v", read.Writing, err)
	}

	// Undo of write_work takes the paper and its PDF back to «Входящие», the files kept.
	entry, err := e.journal.Find(ctx, "th1", 2, "ww1")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := e.journal.Undo(ctx, entry.ID); err != nil || u.Status != action.StatusUndone {
		t.Fatalf("Undo = %+v, %v", u, err)
	}
	for _, mid := range []shared.MaterialID{v.Writing.Material, pdfID} {
		m, err := e.lib.Material(ctx, mid)
		if err != nil || !m.InInbox() || !m.Active() {
			t.Fatalf("after the Undo %s = %+v, %v", mid, m, err)
		}
	}
}

// A new generation takes the PDF of the old paper out of «ДЗ»; the new paper has none until the next click.
func TestTurnkeyPDFOfAnOlderGeneration(t *testing.T) {
	e := newTurnkeyEnv(t, "Реферат по истории Древней Руси: введение и заключение.")
	ctx := t.Context()
	w := e.confirmedByAgent(t)
	id := shared.WorkID(w.ID())
	e.fileUnder(t, "История")
	e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, writingOutline, writingSection("Текст раздела."))
	e.startRunner(t)
	e.runWriteWork(t, w)
	e.waitOutline(t, id, e.jobOfCall(t, "ww1"))
	e.approveAndWait(t, id)
	first, err := e.binding.ExportPDF(ctx, w.ID())
	if err != nil {
		t.Fatal(err)
	}

	e.reader.AnswerInOrder(cvalues.OperationHomeworkDraft, writingSection("Короче и точнее."))
	re, err := e.binding.Regenerate(ctx, w.ID(), 1, "Короче")
	if err != nil || re.Writing.PDF != "" {
		t.Fatalf("Regenerate = %+v, %v", re.Writing, err)
	}
	if _, err := e.binding.ExportPDF(ctx, w.ID()); !errors.Is(err, wails.ErrTurnkeyWriting) {
		t.Fatalf("a pdf while written again = %v", err)
	}
	again := e.waitWritten(t, id, shared.JobID(re.Writing.Job))
	if again.Writing.PDF != "" {
		t.Fatalf("the new paper has the old pdf: %s", again.Writing.PDF)
	}
	old, err := e.lib.Material(ctx, shared.MaterialID(first.Writing.PDF))
	if err != nil || !old.InInbox() || !old.Active() {
		t.Fatalf("the old pdf = %+v, %v", old, err)
	}
	next, err := e.binding.ExportPDF(ctx, w.ID())
	if err != nil || next.Writing.PDF == "" || next.Writing.PDF == first.Writing.PDF {
		t.Fatalf("the pdf of the new paper = %+v, %v", next.Writing, err)
	}
	files, err := e.lib.Materials(ctx, again.Writing.Subject)
	if err != nil {
		t.Fatal(err)
	}
	var pdfs int
	for _, m := range files {
		if m.Type() == material.TypePDF && m.Folder() == subject.SystemFolderID(again.Writing.Subject, subject.SystemHomework) {
			pdfs++
		}
	}
	if pdfs != 1 {
		t.Fatalf("%d pdfs in «ДЗ», want the new one", pdfs)
	}
}
