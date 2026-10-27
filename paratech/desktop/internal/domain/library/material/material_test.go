package material

import (
	"archive/zip"
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"paratech/desktop/internal/domain/shared"
)

var t0 = time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC)

// zipWith returns a ZIP archive holding empty files with the names.
func zipWith(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		if _, err := zw.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDetectType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want Type
		err  error
	}{
		{"docx", zipWith(t, "[Content_Types].xml", "word/document.xml"), TypeDOCX, nil},
		{"pptx", zipWith(t, "[Content_Types].xml", "ppt/presentation.xml", "ppt/slides/slide1.xml"), TypePPTX, nil},
		{"pdf", []byte("%PDF-1.7\n..."), TypePDF, nil},
		{"utf-8 text", []byte("Лекция 1. Введение\n"), TypeTXT, nil},
		{"text with BOM", []byte("\xef\xbb\xbfПривет"), TypeTXT, nil},
		{"obsidian markdown", []byte("---\ntags: [лекция]\n---\n# Тема\n\n- [[Ссылка]] и **жирный**\n"), TypeTXT, nil},
		{"empty", nil, "", ErrEmpty},
		{"only a BOM", []byte("\xef\xbb\xbf"), "", ErrUnsupportedType},
		{"other zip", zipWith(t, "readme.txt"), "", ErrUnsupportedType},
		{"zip with both main parts", zipWith(t, "word/document.xml", "ppt/presentation.xml"), "", ErrUnsupportedType},
		{"broken zip", []byte("PK\x03\x04garbage"), "", ErrUnsupportedType},
		{"cp1251 text", []byte{0xcf, 0xf0, 0xe8, 0xe2, 0xe5, 0xf2}, "", ErrUnsupportedType},
		{"binary with NUL", []byte("MZ\x00\x00"), "", ErrUnsupportedType},
		{"too large", make([]byte, MaxBytes+1), "", ErrTooLarge},
		{"jpeg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF"), TypeJPEG, nil},
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), TypePNG, nil},
		{"jpeg over the image limit", append([]byte("\xff\xd8\xff"), make([]byte, MaxImageBytes)...), "", ErrImageTooLarge},
		{"gif", []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"), "", ErrUnsupportedImage},
		{"webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 "), "", ErrUnsupportedImage},
		{"heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00"), "", ErrUnsupportedImage},
		{"avif", []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00"), "", ErrUnsupportedImage},
		{"bmp", []byte("BM\x3a\x00\x00\x00\x00\x00\x00\x00\x36\x00"), "", ErrUnsupportedImage},
		{"tiff", []byte("II*\x00\x08\x00\x00\x00\xff"), "", ErrUnsupportedImage},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "", ErrUnsupportedImage},
		{"svg with a prolog", []byte("\xef\xbb\xbf  <?xml version=\"1.0\"?>\n<SVG/>"), "", ErrUnsupportedImage},
		{"xml that is not svg", []byte(`<?xml version="1.0"?><note>svg</note>`), TypeTXT, nil},
		{"text that starts like a bmp", []byte("BMW и Audi"), TypeTXT, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectType(tt.data)
			if got != tt.want || !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("DetectType = %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestDigestAndBlobRef(t *testing.T) {
	d := DigestOf([]byte("abc"))
	if d != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" || d.Validate() != nil {
		t.Fatalf("DigestOf(abc) = %s", d)
	}
	ref, err := BlobRef(d)
	if err != nil || ref.String() != "materials/"+string(d) {
		t.Fatalf("BlobRef = %s, %v", ref, err)
	}
	for _, bad := range []Digest{"", "abc", Digest(strings.ToUpper(string(d))), Digest(strings.Repeat("g", 64)), "../" + d[3:]} {
		if _, err := BlobRef(bad); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("BlobRef(%q) = %v", bad, err)
		}
	}
}

func newMaterial(t *testing.T, typ Type) *Material {
	t.Helper()
	m, err := New(Spec{ID: "m1", Digest: DigestOf([]byte("x")), Size: 1, Type: typ, Name: " Лекция\n1.docx "}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNew(t *testing.T) {
	m := newMaterial(t, TypeDOCX)
	if !m.InInbox() || !m.Active() || m.TextStatus() != TextPending || m.Name() != "Лекция 1.docx" || m.Version() != 0 {
		t.Fatalf("material %+v", m.State())
	}
	if pdf := newMaterial(t, TypePDF); pdf.TextStatus() != TextPDFNotExtracted {
		t.Fatalf("pdf text status = %q", pdf.TextStatus())
	}
	for _, typ := range []Type{TypeJPEG, TypePNG} {
		if img := newMaterial(t, typ); img.TextStatus() != TextNoText {
			t.Fatalf("%s text status = %q", typ, img.TextStatus())
		}
	}
	if _, err := New(Spec{ID: "m3", Digest: DigestOf(nil), Size: MaxImageBytes + 1, Type: TypePNG}, t0); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("New of a photo over MaxImageBytes = %v", err)
	}
	long, err := New(Spec{ID: "m2", Digest: DigestOf(nil), Size: 1, Type: TypeTXT, Name: strings.Repeat("я", 300)}, t0)
	if err != nil || len([]rune(long.Name())) != MaxNameRunes {
		t.Fatalf("long name: %v, %d runes", err, len([]rune(long.Name())))
	}

	good := Spec{ID: "m1", Digest: DigestOf(nil), Size: 1, Type: TypeTXT}
	tests := []struct {
		name   string
		modify func(*Spec)
	}{
		{"bad id", func(s *Spec) { s.ID = "a/b" }},
		{"bad digest", func(s *Spec) { s.Digest = "x" }},
		{"zero size", func(s *Spec) { s.Size = 0 }},
		{"too large", func(s *Spec) { s.Size = MaxBytes + 1 }},
		{"unknown type", func(s *Spec) { s.Type = "exe" }},
	}
	for _, tt := range tests {
		spec := good
		tt.modify(&spec)
		if _, err := New(spec, t0); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("%s: New = %v", tt.name, err)
		}
	}
}

func TestPlaceAndInbox(t *testing.T) {
	m := newMaterial(t, TypeTXT)
	t1 := t0.Add(time.Minute)
	if changed, err := m.Place("s1", "s1-lectures", t1); err != nil || !changed || m.InInbox() || m.UpdatedAt() != t1 {
		t.Fatalf("Place = %v, %v", changed, err)
	}
	if changed, err := m.Place("s1", "s1-lectures", t1); err != nil || changed {
		t.Fatalf("Place again = %v, %v; want no change", changed, err)
	}
	if _, err := m.Place("", "f", t1); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("Place without a subject = %v", err)
	}
	if _, err := m.Place("s1", "", t1); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("Place without a folder = %v", err)
	}
	if changed, err := m.MoveToInbox(t1); err != nil || !changed || !m.InInbox() || m.Folder() != "" {
		t.Fatalf("MoveToInbox = %v, %v", changed, err)
	}
	if changed, err := m.MoveToInbox(t1); err != nil || changed {
		t.Fatalf("MoveToInbox again = %v, %v", changed, err)
	}
}

func TestLinks(t *testing.T) {
	m := newMaterial(t, TypeTXT)
	if changed, err := m.Link("l1", t0); err != nil || !changed {
		t.Fatalf("Link = %v, %v", changed, err)
	}
	if changed, err := m.Link("l1", t0); err != nil || changed {
		t.Fatalf("Link again = %v, %v", changed, err)
	}
	if _, err := m.Link("a b", t0); !errors.Is(err, ErrInvalidMaterial) {
		t.Fatalf("Link(bad id) = %v", err)
	}
	if changed, err := m.Unlink("l2", t0); err != nil || changed {
		t.Fatalf("Unlink(not linked) = %v, %v", changed, err)
	}
	if changed, err := m.Unlink("l1", t0); err != nil || !changed || len(m.Lessons()) != 0 {
		t.Fatalf("Unlink = %v, %v", changed, err)
	}
	for i := range MaxLessons {
		if _, err := m.Link(shared.LessonID("l"+strings.Repeat("x", i+1)), t0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Link("last", t0); !errors.Is(err, ErrTooManyLessons) {
		t.Fatalf("Link over the limit = %v", err)
	}
}

// The status machine: active ↔ removed; a removed material refuses every change but Restore and Reupload.
func TestStatusMachine(t *testing.T) {
	m := newMaterial(t, TypeTXT)
	if _, err := m.Place("s1", "f1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Link("l1", t0); err != nil {
		t.Fatal(err)
	}
	if changed, err := m.Remove(t0); err != nil || !changed || m.Status() != StatusRemoved {
		t.Fatalf("Remove = %v, %v", changed, err)
	}
	if changed, err := m.Remove(t0); err != nil || changed {
		t.Fatalf("Remove again = %v, %v", changed, err)
	}

	refused := map[string]func() (bool, error){
		"Place":       func() (bool, error) { return m.Place("s2", "f2", t0) },
		"MoveToInbox": func() (bool, error) { return m.MoveToInbox(t0) },
		"Link":        func() (bool, error) { return m.Link("l2", t0) },
		"Unlink":      func() (bool, error) { return m.Unlink("l1", t0) },
	}
	for name, fn := range refused {
		if _, err := fn(); !errors.Is(err, ErrRemoved) {
			t.Errorf("%s of a removed material = %v, want ErrRemoved", name, err)
		}
	}

	// Restore brings it back where it was, with its links.
	if changed, err := m.Restore(t0); err != nil || !changed || !m.Active() || m.Folder() != "f1" || len(m.Lessons()) != 1 {
		t.Fatalf("Restore = %v, %v: %+v", changed, err, m.State())
	}
	if changed, err := m.Restore(t0); err != nil || changed {
		t.Fatalf("Restore again = %v, %v", changed, err)
	}

	// Reupload of an active one keeps its place; of a removed one, returns it to «Входящие».
	if changed, err := m.Reupload(t0); err != nil || changed || m.InInbox() {
		t.Fatalf("Reupload(active) = %v, %v", changed, err)
	}
	if _, err := m.Remove(t0); err != nil {
		t.Fatal(err)
	}
	if changed, err := m.Reupload(t0); err != nil || !changed || !m.Active() || !m.InInbox() {
		t.Fatalf("Reupload(removed) = %v, %v: %+v", changed, err, m.State())
	}

	bogus := &Material{status: "lost"}
	if err := bogus.transition(StatusActive, t0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("transition from an unknown status = %v", err)
	}
}

func TestRestoreRoundTripAndRejects(t *testing.T) {
	m := newMaterial(t, TypeDOCX)
	if _, err := m.Place("s1", "f1", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Link("l1", t0); err != nil {
		t.Fatal(err)
	}
	st := m.State()
	st.Version = 2
	back, err := Restore(st)
	if err != nil || !reflect.DeepEqual(back.State(), st) {
		t.Fatalf("Restore = %+v, %v", back, err)
	}

	tests := []struct {
		name   string
		modify func(*State)
	}{
		{"version 0", func(s *State) { s.Version = 0 }},
		{"subject without folder", func(s *State) { s.Folder = "" }},
		{"folder without subject", func(s *State) { s.Subject = "" }},
		{"lesson twice", func(s *State) { s.Lessons = []shared.LessonID{"l1", "l1"} }},
		{"unknown status", func(s *State) { s.Status = "gone" }},
		{"unknown text status", func(s *State) { s.Text = "maybe" }},
		{"name not clean", func(s *State) { s.Name = "a\nb" }},
		{"no time", func(s *State) { s.UpdatedAt = time.Time{} }},
	}
	for _, tt := range tests {
		s := m.State()
		s.Version = 1
		tt.modify(&s)
		if _, err := Restore(s); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("%s: Restore = %v", tt.name, err)
		}
	}
}

// A photo always has TextNoText, and only a photo has it.
func TestRestoreImageTextStatus(t *testing.T) {
	img := newMaterial(t, TypeJPEG).State()
	img.Version = 1
	if _, err := Restore(img); err != nil {
		t.Fatalf("Restore of a photo = %v", err)
	}
	tests := []struct {
		name string
		typ  Type
		text TextStatus
		size int64
	}{
		{"photo read as text", TypeJPEG, TextExtracted, 1},
		{"photo pending", TypePNG, TextPending, 1},
		{"document without text", TypeDOCX, TextNoText, 1},
		{"pdf without text", TypePDF, TextNoText, 1},
		{"photo too large", TypePNG, TextNoText, MaxImageBytes + 1},
	}
	for _, tt := range tests {
		s := img
		s.Type, s.Text, s.Size = tt.typ, tt.text, tt.size
		if _, err := Restore(s); !errors.Is(err, ErrInvalidMaterial) {
			t.Errorf("%s: Restore = %v", tt.name, err)
		}
	}
}

func TestTypeImageAndReadable(t *testing.T) {
	tests := []struct {
		typ             Type
		image, readable bool
	}{
		{TypeDOCX, false, true},
		{TypePPTX, false, true},
		{TypeTXT, false, true},
		{TypePDF, false, false},
		{TypeJPEG, true, false},
		{TypePNG, true, false},
		{"gif", false, false},
	}
	for _, tt := range tests {
		if tt.typ.Image() != tt.image || tt.typ.Readable() != tt.readable {
			t.Errorf("%q: Image %v, Readable %v; want %v, %v", tt.typ, tt.typ.Image(), tt.typ.Readable(), tt.image, tt.readable)
		}
	}
}
