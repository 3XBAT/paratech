package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/coursework/services/homeworkdoc"
	"paratech/desktop/internal/domain/coursework/values"
)

// fakePrinter captures the page it is given and answers out or err.
type fakePrinter struct {
	pages [][]byte
	out   []byte
	err   error
}

func (f *fakePrinter) Print(_ context.Context, html []byte) ([]byte, error) {
	f.pages = append(f.pages, bytes.Clone(html))
	return f.out, f.err
}

const cspMeta = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"/>`

// The page handed to the printer always carries the CSP without network (the only egress block of the print), and
// holds the document as escaped text, whatever the model wrote.
func TestRendererPageHasCSP(t *testing.T) {
	hostile := paperInput()
	hostile.Sections[0].Paragraphs = []string{`<script>fetch("https://x.example")</script><img src=x onerror=alert(1)>`}
	lab := homeworkdoc.LabInput{Paper: paperInput(), Sections: []homeworkdoc.LabSection{
		{Title: "Ход работы", Paragraphs: []homeworkdoc.LabParagraph{{Text: "Вставьте таблицу", Placeholder: true}}},
	}}
	docs := map[string]values.Document{
		"paper":      homeworkdoc.BuildPaper(paperInput()),
		"hostile":    homeworkdoc.BuildPaper(hostile),
		"lab report": homeworkdoc.BuildLabReport(lab),
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			p := &fakePrinter{out: []byte("%PDF-1.7")}
			out, err := NewRenderer(p).RenderPDF(t.Context(), doc)
			if err != nil || string(out) != "%PDF-1.7" || len(p.pages) != 1 {
				t.Fatalf("RenderPDF = %q, %v, %d pages", out, err, len(p.pages))
			}
			page := string(p.pages[0])
			if strings.Count(page, cspMeta) != 1 || strings.Index(page, cspMeta) > strings.Index(page, "<title>") {
				t.Fatal("the page has no CSP before its content")
			}
			for _, bad := range []string{"<script", "src=", "https:", "<img"} {
				if strings.Contains(page, bad) {
					t.Fatalf("the page holds %q", bad)
				}
			}
			want, err := printHTML(doc)
			if err != nil || !bytes.Equal(p.pages[0], want) {
				t.Fatal("the printer did not get the print page of the document")
			}
		})
	}
}

// Errors of the printer become the typed errors of the port; the caller's cancel stays it.
func TestRendererErrors(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		want      []error
		notWanted []error
	}{
		{"no runtime", ErrUnsupported, []error{ErrUnsupported, errors.ErrUnsupported}, []error{ErrFailed}},
		{"failed", fmt.Errorf("pdf: print: %w", ErrFailed), []error{ErrFailed}, []error{errors.ErrUnsupported}},
		{"timed out", context.DeadlineExceeded, []error{ErrFailed}, []error{context.DeadlineExceeded, errors.ErrUnsupported}},
		{"cancelled", context.Canceled, []error{context.Canceled}, []error{ErrFailed, errors.ErrUnsupported}},
		{"other", errors.New("boom"), []error{ErrFailed}, []error{errors.ErrUnsupported}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &fakePrinter{err: tt.err}
			out, err := NewRenderer(p).RenderPDF(t.Context(), homeworkdoc.BuildPaper(paperInput()))
			if out != nil || err == nil {
				t.Fatalf("RenderPDF = %d bytes, %v", len(out), err)
			}
			for _, w := range tt.want {
				if !errors.Is(err, w) {
					t.Errorf("%v is not %v", err, w)
				}
			}
			for _, w := range tt.notWanted {
				if errors.Is(err, w) {
					t.Errorf("%v is %v", err, w)
				}
			}
		})
	}
}

// A document that cannot be printed never reaches the printer.
func TestRendererInvalidDocument(t *testing.T) {
	p := &fakePrinter{out: []byte("%PDF-1.7")}
	_, err := NewRenderer(p).RenderPDF(t.Context(), values.Document{Blocks: []values.Block{{Kind: "video"}}})
	if !errors.Is(err, ErrRender) || len(p.pages) != 0 {
		t.Fatalf("RenderPDF = %v, %d pages", err, len(p.pages))
	}
}
