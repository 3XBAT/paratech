package notes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
)

// textOf is a text of n pieces of kind, as an extractor would give it.
func textOf(t *testing.T, kind material.LocatorKind, n int) material.Text {
	t.Helper()
	fs := make([]material.Fragment, 0, n)
	for i := 1; i <= n; i++ {
		fs = append(fs, material.Fragment{Locator: material.Locator{Kind: kind, N: i}, Lines: []string{fmt.Sprintf("строка %d", i)}})
	}
	text, err := material.NewText(fs, false)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// Every part tells how its text is cut; a part without text, gone, removed or unread counts zero and never fails the
// list.
func TestPartCounts(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	logs := &bytes.Buffer{}
	e.svc.log = slog.New(slog.NewTextHandler(logs, nil))
	m := e.materials

	docx := m.filedAs(t, phys, material.TypeDOCX, "Лекция.docx", "docx with headings")
	m.texts[docx.ID()] = textOf(t, material.LocatorSection, 3)
	paged := m.filedAs(t, phys, material.TypeDOCX, "Разрывы.docx", "docx with page breaks")
	m.texts[paged.ID()] = textOf(t, material.LocatorPage, 2)
	slides := m.filedAs(t, phys, material.TypePPTX, "Слайды.pptx", "pptx")
	m.texts[slides.ID()] = textOf(t, material.LocatorSlide, 4)
	m.filedAs(t, phys, material.TypePDF, "Скан.pdf", "pdf")
	m.filedAs(t, phys, material.TypeDOCX, "Битый.docx", "broken docx") // no text the fake reads: unreadable
	damaged := m.filedAs(t, phys, material.TypeTXT, "Пропал.txt", "damaged")
	m.textErrs[damaged.ID()] = fmt.Errorf("fake: %w", material.ErrDamaged)
	failing := m.filedAs(t, phys, material.TypeTXT, "Диск.txt", "store failed")
	m.textErrs[failing.ID()] = errors.New("disk")
	removed := m.filedAs(t, phys, material.TypeTXT, "Удалён.txt", "# А\nа\n# Б\nб")
	gone := m.filedAs(t, phys, material.TypeTXT, "Стёрт.txt", "# А\nа\n# Б\nб\n# В")

	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: []Upload{
		{Name: "Ряды.md", Data: []byte("# Ряды\nтекст\n## Итоги\nещё")},
		{Name: "Заметка.txt", Data: []byte("просто текст")},
		{Name: "стр.jpg", Data: []byte("\xff\xd8\xff page")},
		{Name: "a", Data: []byte("docx with headings")},
		{Name: "b", Data: []byte("docx with page breaks")},
		{Name: "c", Data: []byte("pptx")},
		{Name: "d", Data: []byte("pdf")},
		{Name: "e", Data: []byte("broken docx")},
		{Name: "f", Data: []byte("damaged")},
		{Name: "g", Data: []byte("store failed")},
		{Name: "h", Data: []byte("# А\nа\n# Б\nб")},
		{Name: "i", Data: []byte("# А\nа\n# Б\nб\n# В")},
	}})
	if err != nil || len(res.View.Counts) != 12 || res.View.Counts[0].Sections != 2 {
		t.Fatalf("Add = %+v, %v", res.View.Counts, err)
	}
	if _, err := removed.Remove(t0); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	delete(m.rows, gone.ID())
	m.mu.Unlock()

	want := []material.TextCounts{
		{Sections: 2, Fragments: 2}, // Markdown with headings
		{Fragments: 1},              // plain text
		{},                          // photo
		{Sections: 3, Fragments: 3}, // DOCX with headings
		{Pages: 2, Fragments: 2},    // DOCX with page breaks
		{Fragments: 4},              // slides are pieces only
		{}, {}, {}, {}, {}, {},      // PDF, unreadable, damaged, store failed, removed, missing
	}
	reads := m.reads()
	list, err := e.svc.ForSubject(ctx, phys)
	if err != nil || len(list) != 1 {
		t.Fatalf("ForSubject = %+v, %v", list, err)
	}
	if got := list[0].Counts; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("counts = %+v\nwant %+v", got, want)
	}
	if list[0].Parts[11] != nil || list[0].Parts[10].Active() {
		t.Fatalf("parts %v", list[0].Parts)
	}

	// Counted texts and unreadable ones are not read again; a damaged file and a failed store are tried again.
	if got := m.reads() - reads; got != 2 {
		t.Fatalf("texts read again: %d, want 2 (damaged, store failed)", got)
	}
	if !strings.Contains(logs.String(), opPartCounts) || strings.Contains(logs.String(), "Диск") {
		t.Fatalf("log %s", logs.String())
	}
	if strings.Count(logs.String(), opPartCounts) != 2 { // once in Add, once in ForSubject
		t.Fatalf("log %s", logs.String())
	}
}

// A listing reads at most maxCountReads texts; the rest are counted by the next one.
func TestPartCountsBounded(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	ups := make([]Upload, 0, note.MaxParts)
	for i := range note.MaxParts {
		ups = append(ups, Upload{Name: fmt.Sprintf("%d.txt", i), Data: []byte(fmt.Sprintf("# А %d\nа\n# Б\nб", i))})
	}
	full, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: ups})
	if err != nil || len(full.View.Counts) != note.MaxParts || full.View.Counts[note.MaxParts-1].Sections != 2 {
		t.Fatalf("one full note is counted whole: %v", err)
	}
	day := AnchorRef{Kind: note.AnchorDay, Date: date(t, 2026, 10, 20)}
	if _, err := e.svc.Add(ctx, AddRequest{Anchor: day, Subject: phys, Uploads: uploads("день.txt")}); err != nil {
		t.Fatal(err)
	}
	e.svc.counts = newCountCache() // as after a restart

	reads := e.materials.reads()
	list, err := e.svc.ForSubject(ctx, phys)
	// The fake lists as made: the full note uses the whole bound, the next note is over it.
	if err != nil || len(list) != 2 || len(list[0].Parts) != note.MaxParts {
		t.Fatalf("ForSubject = %d notes, %v", len(list), err)
	}
	if got := e.materials.reads() - reads; got != maxCountReads {
		t.Fatalf("read %d texts, want %d", got, maxCountReads)
	}
	if list[0].Counts[note.MaxParts-1].Sections != 2 || list[1].Counts[0] != (material.TextCounts{}) {
		t.Fatalf("over the bound: %+v, %+v", list[0].Counts[note.MaxParts-1], list[1].Counts)
	}

	list, err = e.svc.ForSubject(ctx, phys)
	if err != nil || list[1].Counts[0].Fragments != 1 || e.materials.reads()-reads != maxCountReads+1 {
		t.Fatalf("next listing: %+v, %v", list[1].Counts, err)
	}
}
