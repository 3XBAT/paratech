package app

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"paratech/desktop/internal/cases/library"
	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/input/wails"
)

// headedDOCX is a Word file with a Heading 1 paragraph per title, each followed by a text paragraph.
func headedDOCX(t *testing.T, titles ...string) []byte {
	t.Helper()
	var body bytes.Buffer
	for _, h := range titles {
		body.WriteString(`<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>` + h + `</w:t></w:r></w:p>`)
		body.WriteString(`<w:p><w:r><w:t>текст</w:t></w:r></w:p>`)
	}
	doc := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body.String() + `</w:body></w:document>`
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range map[string]string{"[Content_Types].xml": "<Types/>", "word/document.xml": doc} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// pngPage is a small real PNG: a photo page has no text, so it counts nothing.
func pngPage(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 8, 8))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The parts of a note count the sections of their text through the real library and readers, and the text is cached
// for the next read (TASK-253).
func TestNotePartCountsEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	core, err := newTestApp(t, dir).openCore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := core.Library.CreateSubject(ctx, library.NewSubject{Name: "Физика"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := core.Notes.Add(ctx, notes.AddRequest{Anchor: notes.AnchorRef{Kind: note.AnchorUndated}, Subject: sub.ID(), Uploads: []notes.Upload{
		{Name: "Лекция.docx", Data: headedDOCX(t, "Пределы", "Ряды", "Итоги")},
		{Name: "Ряды.md", Data: []byte("# Ряды\nтекст\n## Признаки\nещё")},
		{Name: "Заметка.txt", Data: []byte("просто текст")},
		{Name: "стр.png", Data: pngPage(t)},
	}})
	if err != nil || len(res.Parts) != 4 {
		t.Fatalf("Add = %+v, %v", res.Parts, err)
	}
	for _, p := range res.Parts {
		if p.Err != nil {
			t.Fatalf("%s: %v", p.Name, p.Err)
		}
	}
	docx := res.Parts[0].Material
	if _, err := os.Stat(filepath.Join(dir, artifactsDir, "texts", docx.Digest().String())); err != nil {
		t.Fatalf("the text of the DOCX is not cached: %v", err)
	}

	binding := wails.NewNotesService(core.Notes, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	list, err := binding.ForSubject(ctx, string(sub.ID()))
	if err != nil || len(list) != 1 || len(list[0].Parts) != 4 {
		t.Fatalf("ForSubject = %+v, %v", list, err)
	}
	type counts struct{ sections, pages, fragments int }
	want := []counts{{3, 0, 3}, {2, 0, 2}, {0, 0, 1}, {0, 0, 0}}
	for i, p := range list[0].Parts {
		if got := (counts{p.Sections, p.Pages, p.Fragments}); got != want[i] {
			t.Errorf("%s: %+v, want %+v", p.Name, got, want[i])
		}
	}
}
