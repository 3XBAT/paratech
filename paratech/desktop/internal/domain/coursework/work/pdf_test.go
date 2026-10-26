package work

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var t1 = t0.Add(time.Hour)

// donePaper is a done referat whose paper has no year (a file stored before TASK-198).
func donePaper(t *testing.T) *Work {
	t.Helper()
	w := confirmedWork(t)
	if err := w.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	mustOutline(t, w)
	for range 3 {
		mustWriteDue(t, w)
	}
	if err := w.Finish(fileOf(w), t0); err != nil {
		t.Fatal(err)
	}
	return w
}

func pdfOf(w *Work) File {
	return File{Material: "mat-pdf", Artifact: "materials/" + strings.Repeat("c", 64), Content: w.ContentKey(), Year: 2026}
}

func TestAttachPDF(t *testing.T) {
	deck := confirmedDeck(t)
	writtenSlides(t, deck)
	mustQuestions(t, deck)
	if err := deck.FinishDeck(fileOf(deck), talkFileOf(deck), t0); err != nil {
		t.Fatal(err)
	}
	writing := confirmedWork(t)
	if err := writing.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	mustOutline(t, writing)

	tests := []struct {
		name string
		w    *Work
		file func(*Work) File
		want error
	}{
		{"a paper being written", writing, pdfOf, ErrInvalidTransition},
		{"a presentation", deck, pdfOf, ErrInvalidTransition},
		{"another content", donePaper(t), func(w *Work) File { f := pdfOf(w); f.Content = "c:other"; return f }, ErrStaleStep},
		{"a bad artifact", donePaper(t), func(w *Work) File { f := pdfOf(w); f.Artifact = "x"; return f }, ErrInvalidWork},
		{"a bad year", donePaper(t), func(w *Work) File { f := pdfOf(w); f.Year = MaxFileYear + 1; return f }, ErrInvalidWork},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.w.State()
			if err := tt.w.AttachPDF(tt.file(tt.w), t1); !errors.Is(err, tt.want) {
				t.Fatalf("AttachPDF = %v, want %v", err, tt.want)
			}
			if _, ok := tt.w.PDF(); ok || tt.w.UpdatedAt() != before.UpdatedAt {
				t.Fatal("a refused pdf changed the work")
			}
		})
	}

	w := donePaper(t)
	if _, ok := w.PDF(); ok {
		t.Fatal("a pdf before AttachPDF")
	}
	if err := w.AttachPDF(pdfOf(w), t1); err != nil {
		t.Fatal(err)
	}
	got, ok := w.PDF()
	wr, _ := w.Writing()
	switch {
	case !ok || got.Material != "mat-pdf" || w.Status() != StatusDone || !w.UpdatedAt().Equal(t1):
		t.Fatalf("pdf = %+v %v, %s", got, ok, w.Status())
	case wr.File.Material != "mat-1" || wr.File.Year != 2026:
		// The paper stays the file of the work (the send attaches it); a paper without a year takes the pdf's.
		t.Fatalf("paper = %+v", wr.File)
	}
	if files := wr.Files(); len(files) != 2 || files[0].Material != "mat-1" || files[1].Material != "mat-pdf" {
		t.Fatalf("files = %+v", files)
	}

	// A pdf printed again (its material was gone) replaces the one before; the paper keeps its year.
	again := pdfOf(w)
	again.Material, again.Year = "mat-pdf-2", 2027
	if err := w.AttachPDF(again, t1); err != nil {
		t.Fatal(err)
	}
	wr, _ = w.Writing()
	if got, _ := w.PDF(); got.Material != "mat-pdf-2" || len(wr.Files()) != 2 || wr.File.Year != 2026 {
		t.Fatalf("again = %+v, files %+v", got, wr.Files())
	}
}

// A new generation leaves the pdf of the old one to Files (the use case takes it out of «ДЗ» with the paper); the
// next paper drops it.
func TestPDFOfAnEarlierGeneration(t *testing.T) {
	moves := []struct {
		name string
		move func(*Work) error
	}{
		{"regenerate", func(w *Work) error { return w.Regenerate(1, "Короче", t1) }},
		{"edit the outline", func(w *Work) error {
			return w.EditOutline([]OutlineItem{
				{Number: 1, Title: "Введение", Words: 300}, {Number: 2, Title: "Основная часть", Words: 800},
				{Number: 3, Title: "Заключение", Words: 200},
			}, t1)
		}},
	}
	for _, tt := range moves {
		t.Run(tt.name, func(t *testing.T) {
			w := donePaper(t)
			if err := w.AttachPDF(pdfOf(w), t1); err != nil {
				t.Fatal(err)
			}
			if err := tt.move(w); err != nil {
				t.Fatal(err)
			}
			wr, _ := w.Writing()
			if _, ok := w.PDF(); ok || w.Status() != StatusWriting || wr.Generation != 2 {
				t.Fatalf("after the move: %s, generation %d", w.Status(), wr.Generation)
			}
			if files := wr.Files(); len(files) != 2 || files[1].Material != "mat-pdf" {
				t.Fatalf("files = %+v", files)
			}
			if err := w.AttachPDF(pdfOf(w), t1); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("a pdf while writing: %v", err)
			}

			for w.Next().Kind == StepSection {
				mustWriteDue(t, w)
			}
			next := fileOf(w)
			next.Material = "mat-new"
			if err := w.Finish(next, t1); err != nil {
				t.Fatal(err)
			}
			wr, _ = w.Writing()
			if _, ok := w.PDF(); ok || wr.PDF != nil || len(wr.Files()) != 1 || wr.Files()[0].Material != "mat-new" {
				t.Fatalf("after the new paper: %+v", wr.Files())
			}
		})
	}
}

func TestPaperYear(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	late := time.Date(2026, 12, 31, 22, 0, 0, 0, time.UTC) // 2027 in Moscow

	w := confirmedWork(t)
	if got := w.PaperYear(msk); got != 0 {
		t.Fatalf("year without a paper = %d", got)
	}
	old := donePaper(t)
	old.updatedAt = late
	if got := old.PaperYear(msk); got != 2027 {
		t.Fatalf("year of an old paper = %d, want the zone's", got)
	}
	if got := old.PaperYear(nil); got != 2026 {
		t.Fatalf("year without a zone = %d", got)
	}

	built := confirmedWork(t)
	if err := built.StartWriting(snap, t0); err != nil {
		t.Fatal(err)
	}
	mustOutline(t, built)
	for range 3 {
		mustWriteDue(t, built)
	}
	f := fileOf(built)
	f.Year = 2025
	if err := built.Finish(f, late); err != nil {
		t.Fatal(err)
	}
	if got := built.PaperYear(msk); got != 2025 {
		t.Fatalf("year of a paper built in 2025 = %d", got)
	}
}

// The stored form: an old work stores (and keys) as before; a pdf is read back strictly with the same checks.
func TestRestorePDF(t *testing.T) {
	w := donePaper(t)
	key := w.ContentKey()
	plain, _ := w.Writing()
	if raw := plain.Encode(); strings.Contains(raw, `"pdf"`) || strings.Contains(raw, `"year"`) {
		t.Fatalf("an old paper stores new fields: %s", raw)
	}
	if err := w.AttachPDF(pdfOf(w), t1); err != nil {
		t.Fatal(err)
	}
	if w.ContentKey() != key {
		t.Fatal("a pdf changed the content key")
	}

	wr, _ := w.Writing()
	raw := wr.Encode()
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil || probe["pdf"] == nil {
		t.Fatalf("stored = %s, %v", raw, err)
	}
	back, err := ParseWriting(raw)
	if err != nil {
		t.Fatal(err)
	}
	st := w.State()
	st.Version = 5
	st.Writing = &back
	got, err := Restore(st)
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := got.PDF(); !ok || p != pdfOf(w) || got.ContentKey() != key {
		t.Fatalf("restored pdf = %+v %v", p, ok)
	}
	if _, err := ParseWriting(strings.Replace(raw, `"pdf":{`, `"pdf":{"pages":3,`, 1)); !errors.Is(err, ErrInvalidWork) {
		t.Fatalf("an unknown field of the pdf: %v", err)
	}

	breakers := []struct {
		name string
		edit func(*State)
	}{
		{"pdf of other content", func(s *State) { s.Writing.PDF.Content = "c:other" }},
		{"pdf without a paper", func(s *State) { s.Status, s.Writing.File = StatusWriting, nil }},
		{"pdf with a bad material", func(s *State) { s.Writing.PDF.Material = "" }},
		{"pdf with a negative year", func(s *State) { s.Writing.PDF.Year = -1 }},
		{"paper with a bad year", func(s *State) { s.Writing.File.Year = MaxFileYear + 1 }},
	}
	for _, tt := range breakers {
		t.Run(tt.name, func(t *testing.T) {
			st := w.State()
			st.Version = 5
			tt.edit(&st)
			if _, err := Restore(st); !errors.Is(err, ErrInvalidWork) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
