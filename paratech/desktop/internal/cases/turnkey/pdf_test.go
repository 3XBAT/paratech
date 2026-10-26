package turnkey

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	cvalues "paratech/desktop/internal/domain/coursework/values"
	"paratech/desktop/internal/domain/coursework/work"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/settings/settings"
	"paratech/desktop/internal/domain/shared"
)

// fakePDF prints a document as a numbered fake PDF (every print other bytes, as a real one with its creation date);
// err refuses the print, during runs inside it (a change of the work meanwhile).
type fakePDF struct {
	docs      []cvalues.Document
	deadlines []time.Duration
	err       error
	during    func()
}

func (f *fakePDF) RenderPDF(ctx context.Context, doc cvalues.Document) ([]byte, error) {
	f.docs = append(f.docs, doc)
	if d, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(d))
	}
	if f.during != nil {
		f.during()
	}
	if f.err != nil {
		return nil, f.err
	}
	return fmt.Appendf(nil, "%%PDF-1.7 print %d", len(f.docs)), nil
}

// nameProfiles is a profile with the student's name.
type nameProfiles struct{ name *string }

func (p nameProfiles) Load(context.Context) (*settings.Settings, error) {
	s := settings.New()
	prof, err := settings.NewProfile(settings.ProfileFields{FullName: *p.name, Group: "ИС-21"})
	if err != nil {
		return nil, err
	}
	return s, s.SetProfile(prof, t0)
}

type yearClock struct{ year int }

func (c yearClock) Now() time.Time { return time.Date(c.year, 3, 1, 9, 0, 0, 0, time.UTC) }

// pdfEnv is a write env with the PDF port: done is a written paper (or lab report) of a1.
func pdfEnv(t *testing.T) (*writeEnv, *fakePDF) {
	t.Helper()
	e := newWriteEnv(t)
	p := &fakePDF{}
	e.svc.writing.PDF = p
	return e, p
}

func (e *writeEnv) done(t *testing.T, id shared.WorkID) {
	t.Helper()
	ap := e.approved(t, id)
	if _, err := e.run(t, ap.Job, 20); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.svc.Work(t.Context(), id); v.Status != work.StatusDone {
		t.Fatalf("work = %s", v.Status)
	}
}

// TestExportPDF: the PDF is printed from the document of the .docx, filed next to it under the same name and kept by
// the work; a second click prints nothing; a PDF removed from the library is printed again.
func TestExportPDF(t *testing.T) {
	tests := []struct {
		name    string
		confirm func(*writeEnv, *testing.T) shared.WorkID
	}{
		{"paper", (*writeEnv).confirmed},
		{"lab report", (*writeEnv).confirmedLab},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, p := pdfEnv(t)
			ctx := t.Context()
			id := tt.confirm(e, t)
			e.done(t, id)
			before, _ := e.svc.Work(ctx, id)
			if before.Writing.PDF != "" {
				t.Fatalf("a pdf before the click: %s", before.Writing.PDF)
			}

			got, err := e.svc.ExportPDF(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if got.Material == "" || got.View.Writing.PDF != got.Material || got.View.Writing.Material != before.Writing.Material {
				t.Fatalf("ExportPDF = %+v", got.View.Writing)
			}
			// The same document as the .docx, never a conversion of it.
			if len(p.docs) != 1 || !reflect.DeepEqual(p.docs[0], e.renderer.docs[len(e.renderer.docs)-1]) {
				t.Fatal("the pdf is not printed from the document of the .docx")
			}
			if len(p.deadlines) != 1 || p.deadlines[0] > PDFTimeout || p.deadlines[0] < PDFTimeout-10*time.Second {
				t.Fatalf("print deadline = %v", p.deadlines)
			}
			last, paper := e.shelf.files[len(e.shelf.files)-1], e.shelf.files[0]
			if last.sub != paper.sub || !strings.HasPrefix(last.data, "%PDF-") ||
				last.name != strings.TrimSuffix(paper.name, homeworkdoc.DocxExtension)+homeworkdoc.PDFExtension {
				t.Fatalf("filed = %+v next to %+v", last, paper)
			}
			if m, _ := e.shelf.paper(got.Material); m.Folder() != subject.SystemFolderID("s1", subject.SystemHomework) {
				t.Fatalf("the pdf is not in «ДЗ»: %+v", m.State())
			}

			// Again: the stored PDF, no print.
			again, err := e.svc.ExportPDF(ctx, id)
			if err != nil || again.Material != got.Material || len(p.docs) != 1 {
				t.Fatalf("again = %+v, %v, %d prints", again.Material, err, len(p.docs))
			}
			// Removed from the library: printed and filed again.
			m, _ := e.shelf.paper(got.Material)
			if _, err := m.Remove(t0); err != nil {
				t.Fatal(err)
			}
			third, err := e.svc.ExportPDF(ctx, id)
			if err != nil || third.Material == got.Material || len(p.docs) != 2 || third.View.Writing.PDF != third.Material {
				t.Fatalf("after the removal = %+v, %v, %d prints", third.Material, err, len(p.docs))
			}
		})
	}
}

func mustWriting(t *testing.T, e *writeEnv, id shared.WorkID) work.Writing {
	t.Helper()
	w, err := e.works.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	wr, _ := w.Writing()
	return wr
}

// The title page has the year of the .docx (not the click's) and the profile as it is at the click.
func TestExportPDFTitlePage(t *testing.T) {
	e, p := pdfEnv(t)
	ctx := t.Context()
	name := "Иванов Иван"
	e.svc.writing.Profiles = nameProfiles{name: &name}
	id := e.confirmed(t)
	e.done(t, id)
	if wr := mustWriting(t, e, id); wr.File.Year != t0.Year() {
		t.Fatalf("paper year = %d", wr.File.Year)
	}

	e.svc.clock = yearClock{year: t0.Year() + 1}
	name = "Петрова Анна"
	if _, err := e.svc.ExportPDF(ctx, id); err != nil {
		t.Fatal(err)
	}
	docx, pdf := e.renderer.docs[len(e.renderer.docs)-1].Title, p.docs[0].Title
	if pdf.Place != docx.Place || !strings.Contains(pdf.Place, fmt.Sprint(t0.Year())) {
		t.Fatalf("pdf place %q, docx %q", pdf.Place, docx.Place)
	}
	if !strings.Contains(pdf.Performer, "Петрова Анна") || !strings.Contains(docx.Performer, "Иванов Иван") {
		t.Fatalf("pdf performer %q, docx %q", pdf.Performer, docx.Performer)
	}
}

// Refusals leave the work as it is: nothing is printed or filed.
func TestExportPDFRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *writeEnv, *fakePDF) shared.WorkID
		want  error
	}{
		{"not configured", func(t *testing.T, e *writeEnv, _ *fakePDF) shared.WorkID {
			id := e.confirmed(t)
			e.done(t, id)
			e.svc.writing.PDF = nil
			return id
		}, ErrNotConfigured},
		{"unknown work", func(*testing.T, *writeEnv, *fakePDF) shared.WorkID { return "w-none" }, ErrNotFound},
		{"confirmed only", func(t *testing.T, e *writeEnv, _ *fakePDF) shared.WorkID { return e.confirmed(t) }, ErrNotWritable},
		{"being written", func(t *testing.T, e *writeEnv, _ *fakePDF) shared.WorkID {
			id := e.confirmed(t)
			e.approved(t, id)
			return id
		}, ErrWritingRunning},
		{"a presentation", func(t *testing.T, e *writeEnv, _ *fakePDF) shared.WorkID {
			id := e.confirmedDeck(t)
			e.done(t, id)
			return id
		}, ErrNotWritable},
		{"no webview2", func(t *testing.T, e *writeEnv, p *fakePDF) shared.WorkID {
			id := e.confirmed(t)
			e.done(t, id)
			p.err = fmt.Errorf("pdf: no runtime: %w", errors.ErrUnsupported)
			return id
		}, ErrPDFUnsupported},
		{"print failed", func(t *testing.T, e *writeEnv, p *fakePDF) shared.WorkID {
			id := e.confirmed(t)
			e.done(t, id)
			p.err = errors.New("pdf: printing failed")
			return id
		}, ErrPDFFailed},
		{"print timed out", func(t *testing.T, e *writeEnv, p *fakePDF) shared.WorkID {
			id := e.confirmed(t)
			e.done(t, id)
			p.err = context.DeadlineExceeded
			return id
		}, ErrPDFFailed},
		{"cancelled", func(t *testing.T, e *writeEnv, p *fakePDF) shared.WorkID {
			id := e.confirmed(t)
			e.done(t, id)
			p.err = context.Canceled
			return id
		}, context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, p := pdfEnv(t)
			ctx := t.Context()
			id := tt.setup(t, e, p)
			before, _ := e.works.Get(ctx, id)
			files := len(e.shelf.files)

			got, err := e.svc.ExportPDF(ctx, id)
			if !errors.Is(err, tt.want) || got.Material != "" {
				t.Fatalf("ExportPDF = %+v, %v; want %v", got.Material, err, tt.want)
			}
			if tt.want == context.Canceled && errors.Is(err, ErrPDFFailed) {
				t.Fatal("the caller's cancel taken for a failed print")
			}
			if len(e.shelf.files) != files {
				t.Fatalf("filed %d files", len(e.shelf.files)-files)
			}
			if before != nil {
				after, _ := e.works.Get(ctx, id)
				if after.Version() != before.Version() {
					t.Fatal("a refused pdf changed the work")
				}
			}
		})
	}
}

// A work written again while the PDF printed keeps nothing of it: the PDF goes to «Входящие», ErrChanged.
func TestExportPDFWorkChangedMeanwhile(t *testing.T) {
	e, p := pdfEnv(t)
	ctx := t.Context()
	id := e.confirmed(t)
	e.done(t, id)
	p.during = func() {
		if _, err := e.svc.Regenerate(ctx, id, 1, "Короче"); err != nil {
			t.Error(err)
		}
	}
	got, err := e.svc.ExportPDF(ctx, id)
	if !errors.Is(err, ErrChanged) || got.Material != "" {
		t.Fatalf("ExportPDF = %+v, %v", got, err)
	}
	pdf := e.shelf.files[len(e.shelf.files)-1]
	if !strings.HasPrefix(pdf.data, "%PDF-") || len(e.shelf.moves) != 1 {
		t.Fatalf("filed %+v, moved %v", pdf, e.shelf.moves)
	}
	if m, _ := e.shelf.paper(e.shelf.moves[0]); !m.InInbox() {
		t.Fatalf("the pdf stayed in «ДЗ»: %+v", m.State())
	}
	if wr := mustWriting(t, e, id); wr.PDF != nil {
		t.Fatalf("the work kept the pdf: %+v", wr.PDF)
	}
}

// A new generation takes the PDF out of «ДЗ» with the old paper; the new paper has no PDF until the next click.
func TestExportPDFNewGeneration(t *testing.T) {
	e, _ := pdfEnv(t)
	ctx := t.Context()
	id := e.confirmed(t)
	e.done(t, id)
	first, err := e.svc.ExportPDF(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	re, err := e.svc.Regenerate(ctx, id, 1, "Короче")
	if err != nil || re.View.Writing.PDF != "" {
		t.Fatalf("Regenerate = %+v, %v", re.View.Writing, err)
	}
	if _, err := e.svc.ExportPDF(ctx, id); !errors.Is(err, ErrWritingRunning) {
		t.Fatalf("a pdf while written again: %v", err)
	}
	e.writer.scripted = []string{`{"paragraphs":["Новое введение."],"sources":[]}`}
	if _, err := e.run(t, re.Job, 10); err != nil {
		t.Fatal(err)
	}
	v, _ := e.svc.Work(ctx, id)
	if v.Status != work.StatusDone || v.Writing.PDF != "" {
		t.Fatalf("after the new paper = %s, pdf %q", v.Status, v.Writing.PDF)
	}
	if m, _ := e.shelf.paper(first.Material); !m.InInbox() || !m.Active() {
		t.Fatalf("the old pdf = %+v", m.State())
	}
}
